package lang_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"

	"funroute/lang"
)

// These tests are written the way a host writes one: they may only use what
// package lang exports, so they also guard the public surface. If one of them
// needs something from internal/core, the surface is too narrow — or the change
// belongs elsewhere.

// The SDK's job is to let a host write its types down, and code that only
// ever uses := never notices a missing alias. So every public type a host
// names is named here, the way Go asserts a type at compile time: dropping one
// from lang.go stops this file compiling.
var (
	_ lang.Value
	_ lang.Kind
	_ lang.Type
	_ lang.Form
	_ *lang.Runtime
	_ *lang.Batch
	_ lang.LanguageCatalog
	_ lang.FunctionDescriptor
	_ lang.FormDescriptor
	_ lang.Example
	_ []lang.Parameter
	_ lang.Manifest
	_ *lang.Artifact
	_ lang.Field
	_ *lang.Binding[RouteIn, RouteOut]
	_ *lang.Program[RouteIn, RouteOut]
	_ *lang.ProgramBatch[RouteIn, RouteOut]
	_ lang.Money
	_ lang.Rate
	_ *lang.Rates
	_ lang.Currency
	_ lang.MoneySpec
	_ lang.CurrencySpec
	_ lang.Rounding
	_ lang.AllocationStrategy
	_ lang.Quote
	_ lang.QuoteSpec
	_ *lang.Currencies
)

func TestHostNamesTheValueTypes(t *testing.T) {
	t.Parallel()
	flag := lang.Bool(true)
	text := lang.String("SGD")
	kind := flag.Kind()
	boolType := lang.BoolType
	numbers, err := lang.Array(lang.IntType, []lang.Value{lang.Int(1), lang.Int(2)})
	if err != nil {
		t.Fatal(err)
	}
	weights, err := lang.Dict(lang.FloatType, map[string]lang.Value{"adyen": mustFloat(t, 0.6)})
	if err != nil {
		t.Fatal(err)
	}
	dictType := lang.DictOf(lang.FloatType)
	// A record is named fields with their own types, in an order that is part
	// of the type; the host writes both down.
	orderType := lang.RecordOf(
		lang.FieldOf("amount", lang.IntType),
		lang.FieldOf("currency", lang.StringType),
	)
	order, err := lang.Record(orderType, []lang.Value{lang.Int(1200), lang.String("SGD")})
	if err != nil {
		t.Fatal(err)
	}
	if amount, _ := order.Field(0).Int(); amount != 1200 || !order.Type().Equal(orderType) {
		t.Fatalf("record = %v of type %s, want amount 1200 of type %s", order.Any(), order.Type(), orderType)
	}
	if kind.String() != "bool" || !flag.Type().Equal(boolType) {
		t.Fatalf("Bool(true) has kind %s and type %s, want bool and %s", kind, flag.Type(), boolType)
	}
	if got, _ := text.String(); got != "SGD" {
		t.Fatalf("String(\"SGD\").String() = %q, want \"SGD\"", got)
	}
	if length, _ := numbers.Length(); length != 2 {
		t.Fatalf("array length = %d, want 2", length)
	}
	if !weights.Type().Equal(dictType) {
		t.Fatalf("dict type = %s, want %s", weights.Type(), dictType)
	}
}

func mustFloat(t *testing.T, value float64) lang.Value {
	t.Helper()
	out, err := lang.Float(value)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func mustParseType(t *testing.T, text string) lang.Type {
	t.Helper()
	typ, err := lang.ParseType(text)
	if err != nil {
		t.Fatal(err)
	}
	return typ
}

// consoleRegistry is the kernel with every structural form: what a console
// that lets its rules map, fold and branch compiles against.
func consoleRegistry(t *testing.T) *lang.Registry {
	t.Helper()
	registry := lang.CoreRegistry()
	if err := registry.EnableForm(lang.SwitchForm, lang.ForForm, lang.ReduceForm); err != nil {
		t.Fatal(err)
	}
	return registry
}

func TestPublicFloatRejectsNonFiniteValues(t *testing.T) {
	t.Parallel()
	if _, err := lang.Float(math.NaN()); err == nil {
		t.Fatal("Float(NaN) error = nil, want an error")
	}
	if value, err := lang.Float(0.75); err != nil {
		t.Fatal(err)
	} else if number, _ := value.Float(); number != 0.75 {
		t.Fatalf("Float(0.75).Float() = %v, want 0.75", number)
	}
}

// The deep-learning shape: a feature vector goes host → VM → extension and
// a score comes back. The extension must receive the host's own slice.
func TestHostVectorsReachExtensionsWithoutCopying(t *testing.T) {
	t.Parallel()
	registry := lang.CoreRegistry()
	var received []float64
	err := lang.Logic(registry, "model.score_v1", lang.Doc{Cost: 10}, func(xs []float64) (float64, error) {
		received = xs
		return xs[0], nil
	})
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := lang.CompileExpr(`model.score_v1(features)`, registry, lang.CompileOptions{
		Args: []lang.ArgSpec{{Name: "features", Type: lang.ArrayOf(lang.FloatType)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := lang.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	features := []float64{0.25, 0.5}
	value, err := lang.ToValue(features)
	if err != nil {
		t.Fatal(err)
	}
	result, err := runtime.RunValues(t.Context(), []lang.Value{value}, lang.RunOptions{Fuel: 100})
	if err != nil {
		t.Fatal(err)
	}
	if score, _ := lang.FromValue[float64](result); score != 0.25 {
		t.Fatalf("score = %v, want 0.25", score)
	}
	if &received[0] != &features[0] {
		t.Fatal("the extension received a copy of the host's vector")
	}
	// Run's by-name path takes the same shortcut for a Go slice.
	if _, err := runtime.Run(t.Context(), map[string]any{"features": features}, lang.RunOptions{Fuel: 100}); err != nil {
		t.Fatal(err)
	}
	if &received[0] != &features[0] {
		t.Fatal("Run copied the host's vector")
	}
}

// Widening a number is lossless and allowed, whichever door it comes through;
// losing a fraction is not.
func TestNumbersWidenButNeverNarrow(t *testing.T) {
	t.Parallel()
	artifact, err := lang.CompileExpr(`rate`, lang.CoreRegistry(), lang.CompileOptions{
		Args: []lang.ArgSpec{{Name: "rate", Type: lang.FloatType}},
	})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := lang.Instantiate(artifact, lang.CoreRegistry())
	if err != nil {
		t.Fatal(err)
	}
	for _, input := range []any{1, int64(1), int32(1), uint16(1), float32(1), 1.0} {
		t.Run(fmt.Sprintf("%T", input), func(t *testing.T) {
			t.Parallel()
			if _, err := runtime.Run(t.Context(), map[string]any{"rate": input}, lang.RunOptions{Fuel: 1000}); err != nil {
				t.Fatalf("%T should widen to float: %v", input, err)
			}
		})
	}
	whole, err := lang.CompileExpr(`amount`, lang.CoreRegistry(), lang.CompileOptions{
		Args: []lang.ArgSpec{{Name: "amount", Type: lang.IntType}},
	})
	if err != nil {
		t.Fatal(err)
	}
	intRuntime, err := lang.Instantiate(whole, lang.CoreRegistry())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := intRuntime.Run(t.Context(), map[string]any{"amount": 1.7}, lang.RunOptions{Fuel: 1000}); err == nil {
		t.Fatal("1.7 is not an int and must not be rounded into one")
	}
}

// Order is the shape a host already holds. A field is in the record because it
// says so; UpdatedAt and internal are simply not part of it.
type Order struct {
	Amount       int64    `funroute:"amount"`
	CurrencyCode string   `funroute:"currency_code"`
	Tags         []string `funroute:"tags"`
	UpdatedAt    string   // untagged: the language never sees it
	//lint:ignore U1000 unexported: invisible either way, which is what this field is here to show
	internal int
}

type Decision struct {
	Channel string `funroute:"channel"`
	Net     int64  `funroute:"net"`
}

func decisionRegistry(t *testing.T) *lang.Registry {
	t.Helper()
	registry := lang.CoreRegistry()
	err := lang.Logic(registry, "route.decide_v1", lang.Doc{
		Label: "决策", Category: "路由", Cost: 10, Params: []string{"订单"}, Result: "决策",
	}, func(order Order) (Decision, error) {
		return Decision{Channel: "adyen_" + order.CurrencyCode, Net: order.Amount - 30}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return registry
}

func orderContract() lang.CompileOptions {
	return lang.CompileOptions{Args: []lang.ArgSpec{{Name: "order", Type: lang.RecordOf(
		lang.FieldOf("amount", lang.IntType),
		lang.FieldOf("currency_code", lang.StringType),
		lang.FieldOf("tags", lang.ArrayOf(lang.StringType)),
	)}}}
}

// A Go struct is a record: the host passes the struct it has, the extension
// takes and returns structs, and the result comes back as a struct.
func TestHostPassesItsStructsThrough(t *testing.T) {
	t.Parallel()
	registry := decisionRegistry(t)
	artifact, err := lang.CompileExpr(`route.decide_v1(order)`, registry, orderContract())
	if err != nil {
		t.Fatal(err)
	}
	want := lang.RecordOf(
		lang.FieldOf("channel", lang.StringType),
		lang.FieldOf("net", lang.IntType),
	)
	runtime, err := lang.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	if !runtime.ResultType().Equal(want) {
		t.Fatalf("result type = %s, want %s", runtime.ResultType(), want)
	}
	value, err := runtime.Run(t.Context(),
		map[string]any{"order": Order{Amount: 5000, CurrencyCode: "USD", Tags: []string{"vip"}}},
		lang.RunOptions{Fuel: 10_000})
	if err != nil {
		t.Fatal(err)
	}
	decision, err := lang.FromValue[Decision](value)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Channel != "adyen_USD" || decision.Net != 4970 {
		t.Fatalf("decision = %+v, want {Channel:adyen_USD Net:4970}", decision)
	}
}

// The field order of a record is part of its type, so the JSON a host reads
// back is in the contract's order rather than alphabetical.
func TestRecordJSONKeepsTheFieldOrder(t *testing.T) {
	t.Parallel()
	artifact, err := lang.CompileExpr(`{net: order.amount, channel: order.currency_code}`,
		lang.CoreRegistry(), orderContract())
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := lang.Instantiate(artifact, lang.CoreRegistry())
	if err != nil {
		t.Fatal(err)
	}
	value, err := runtime.Run(t.Context(),
		map[string]any{"order": Order{Amount: 1200, CurrencyCode: "SGD"}}, lang.RunOptions{Fuel: 10_000})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != `{"net":1200,"channel":"SGD"}` {
		t.Fatalf("JSON = %s, want the contract's field order", encoded)
	}
}

// A field is in the record because it says so. Forgetting a tag is not silent:
// the field is absent, and any expression that reads it fails to compile.
func TestOnlyTaggedFieldsAreInTheRecord(t *testing.T) {
	t.Parallel()
	registry := decisionRegistry(t)
	_, err := lang.CompileExpr(`order.updated_at`, registry, orderContract())
	if err == nil || !strings.Contains(err.Error(), "no field") {
		t.Fatalf("CompileExpr(%q) error = %v, want \"no field\": an untagged field must not be readable", "order.updated_at", err)
	}
	// A struct that tags nothing is not a record at all.
	type Untagged struct {
		Amount int64
	}
	err = lang.Logic(lang.CoreRegistry(), "demo.untagged_v1",
		lang.Doc{Label: "无标签", Category: "演示", Cost: 1, Params: []string{"值"}, Result: "值"},
		func(u Untagged) (int64, error) { return u.Amount, nil })
	if err == nil || !strings.Contains(err.Error(), "no record fields") {
		t.Fatalf("Logic(func(Untagged)) error = %v, want \"no record fields\"", err)
	}
}

// A field name is a plain name in both doors: the type text and the source.
func TestRecordFieldNamesFollowOneRule(t *testing.T) {
	t.Parallel()
	if _, err := lang.ParseType(`record{in: int}`); err == nil || !strings.Contains(err.Error(), "field name") {
		t.Fatalf("ParseType(%q) error = %v, want \"field name\": a reserved word is not a field name", "record{in: int}", err)
	}
	if _, err := lang.CompileExpr(`{in: 1}`, lang.CoreRegistry(), lang.CompileOptions{}); err == nil ||
		!strings.Contains(err.Error(), "field name") {
		t.Fatalf("CompileExpr(%q) error = %v, want \"field name\", the rule of the type text", "{in: 1}", err)
	}
}

type Line struct {
	SKU    string `funroute:"sku"`
	Amount int64  `funroute:"amount"`
}

type Basket struct {
	Customer Order  `funroute:"customer"`
	Lines    []Line `funroute:"lines"`
}

// Records nest, and so does everything about them: the type, the Go struct it
// maps to, the field order in JSON, and taking it all back out again.
func TestRecordsNest(t *testing.T) {
	t.Parallel()
	basket := Basket{
		Customer: Order{Amount: 1200, CurrencyCode: "SGD", Tags: []string{"vip"}, UpdatedAt: "ignored"},
		Lines:    []Line{{SKU: "a", Amount: 10}, {SKU: "b", Amount: 20}},
	}
	contract := lang.CompileOptions{Args: []lang.ArgSpec{{Name: "basket", Type: mustParseType(t,
		`record{customer: record{amount: int, currency_code: string, tags: array<string>}, lines: array<record{sku: string, amount: int}>}`)}}}
	artifact, err := lang.CompileExpr(
		`{who: basket.customer.currency_code, first: basket.lines[0].sku, total: reduce(l in basket.lines, sum = 0, sum + l.amount)}`,
		consoleRegistry(t), contract)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := lang.Instantiate(artifact, consoleRegistry(t))
	if err != nil {
		t.Fatal(err)
	}
	value, err := runtime.Run(t.Context(), map[string]any{"basket": basket}, lang.RunOptions{Fuel: 10_000})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != `{"who":"SGD","first":"a","total":30}` {
		t.Fatalf("value = %s, want %s", encoded, `{"who":"SGD","first":"a","total":30}`)
	}
	// A record nested inside an array keeps its field order too.
	lines, err := lang.CompileExpr(`basket.lines`, consoleRegistry(t), contract)
	if err != nil {
		t.Fatal(err)
	}
	linesRuntime, err := lang.Instantiate(lines, consoleRegistry(t))
	if err != nil {
		t.Fatal(err)
	}
	listed, err := linesRuntime.Run(t.Context(), map[string]any{"basket": basket}, lang.RunOptions{Fuel: 10_000})
	if err != nil {
		t.Fatal(err)
	}
	encodedLines, err := json.Marshal(listed)
	if err != nil {
		t.Fatal(err)
	}
	if string(encodedLines) != `[{"sku":"a","amount":10},{"sku":"b","amount":20}]` {
		t.Fatalf("nested records lost their field order: %s, want %s", encodedLines, `[{"sku":"a","amount":10},{"sku":"b","amount":20}]`)
	}
}

// A host struct serves many rules: a contract may declare fewer fields than
// the struct carries, and a struct may have fields no record mentions.
func TestRecordsMatchByNameNotByCount(t *testing.T) {
	t.Parallel()
	artifact, err := lang.CompileExpr(`{amount: order.amount}`, consoleRegistry(t), lang.CompileOptions{
		Args: []lang.ArgSpec{{Name: "order", Type: mustParseType(t, `record{amount: int}`)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := lang.Instantiate(artifact, consoleRegistry(t))
	if err != nil {
		t.Fatal(err)
	}
	// The payload carries more than the contract asks for.
	value, err := runtime.Run(t.Context(),
		map[string]any{"order": map[string]any{"amount": 7, "extra": 9, "note": "x"}}, lang.RunOptions{Fuel: 1000})
	if err != nil {
		t.Fatal(err)
	}
	// And the struct taken back out may carry more than the record does.
	type Wide struct {
		Amount  int64  `funroute:"amount"`
		Channel string `funroute:"channel"` // absent from the record: stays zero
	}
	wide, err := lang.FromValue[Wide](value)
	if err != nil || wide.Amount != 7 || wide.Channel != "" {
		t.Fatalf("FromValue[Wide] = %+v, %v; want {Amount:7 Channel:}", wide, err)
	}
	// A field the contract does ask for is still required.
	_, err = runtime.Run(t.Context(), map[string]any{"order": map[string]any{"extra": 9}}, lang.RunOptions{Fuel: 1000})
	if err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("Run(order without amount) error = %v, want \"missing\"", err)
	}
}

// A declared type is spelling, nothing more: naming one produces the same
// artifact — digest included — as writing the record out at every argument.
// That is what lets a console declare `Order` once without changing the ABI.
func TestDeclaredTypesAreSpellingOnly(t *testing.T) {
	t.Parallel()
	registry := decisionRegistry(t)
	written := "record{amount: int, currency_code: string, tags: array<string>}"
	aliases := map[string]lang.Type{"Order": mustParseType(t, written)}

	// The subtests fill one map that is compared after them, so they run one
	// after the other rather than in parallel.
	digests := map[string]string{}
	for label, text := range map[string]string{"named": "Order", "written": written} {
		t.Run(label, func(t *testing.T) {
			typ, err := lang.ParseTypeWith(text, aliases)
			if err != nil {
				t.Fatalf("ParseTypeWith(%q) error = %v", text, err)
			}
			artifact, err := lang.CompileExpr(`route.decide_v1(order).channel`, registry,
				lang.CompileOptions{Args: []lang.ArgSpec{{Name: "order", Type: typ}}})
			if err != nil {
				t.Fatalf("CompileExpr with order: %s: %v", text, err)
			}
			digests[label] = artifact.Digest()
		})
	}
	if digests["named"] != digests["written"] {
		t.Fatalf("an alias changed the artifact: %s vs %s", digests["named"], digests["written"])
	}

	// A name nobody declared is still a name nobody declared.
	if _, err := lang.ParseTypeWith("Missing", aliases); err == nil {
		t.Fatal("ParseTypeWith(\"Missing\") error = nil, want an undeclared type name")
	}
	// Aliases do not nest, so a declaration cannot name another one.
	if _, err := lang.ParseType("array<Order>"); err == nil {
		t.Fatal("ParseType(\"array<Order>\") error = nil, want an undeclared type name: it was never given the alias")
	}
}

// Two fields tagged with one name are refused where the tags are read, naming
// both, by every path a struct takes into the language.
func TestTwoFieldsCannotShareATag(t *testing.T) {
	t.Parallel()
	type twice struct {
		A int64 `funroute:"a"`
		B int64 `funroute:"a"`
	}
	const want = `fields A and B of lang_test.twice are both tagged "a"`
	if _, err := lang.ToValue(twice{}); err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("ToValue(twice{}) error = %v, want %q", err, want)
	}
	if err := lang.Logic(lang.CoreRegistry(), "f.g_v1", lang.Doc{}, func(twice) (int64, error) { return 0, nil }); err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("Logic(func(twice)) error = %v, want %q", err, want)
	}
	type in struct {
		R twice `funroute:"r"`
	}
	if _, err := lang.Bind[in, int64](lang.CoreRegistry()); err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("Bind[in, int64] error = %v, want %q", err, want)
	}
}

// A rate prints and crosses JSON as a decimal string, and reads back from a
// string or a number.
func TestRatesCrossJSONAsDecimals(t *testing.T) {
	t.Parallel()
	for rate, want := range map[lang.Rate]string{rateUnits(290_000_000): "0.029", rateUnits(150 * rateScale): "150", rateUnits(-1): "-0.0000000001", rateUnits(0): "0"} {
		if got := rate.String(); got != want {
			t.Errorf("Rate(%d).String() = %q, want %q", rate, got, want)
		}
		encoded, err := json.Marshal(rate)
		if err != nil || string(encoded) != `"`+want+`"` {
			t.Errorf("json.Marshal(Rate(%d)) = %s, %v, want %q", rate, encoded, err, want)
		}
	}
	for data, want := range map[string]lang.Rate{`"0.029"`: rateUnits(290_000_000), `0.029`: rateUnits(290_000_000), `2`: rateUnits(2 * rateScale), `"-1.5"`: rateUnits(-15_000_000_000)} {
		var got lang.Rate
		if err := json.Unmarshal([]byte(data), &got); err != nil || got != want {
			t.Errorf("json.Unmarshal(%s) = %d, %v, want %d", data, got, err, want)
		}
	}
	for _, data := range []string{`"abc"`, `1e3`, `"0.00000000001"`, `true`, `"1_0"`} {
		var got lang.Rate
		if err := json.Unmarshal([]byte(data), &got); err == nil {
			t.Errorf("json.Unmarshal(%s) = %v, nil, want an error", data, got)
		}
	}
}

// A host's []Money reaches an extension as the very slice: money arrays
// keep the zero-copy boundary.
func TestMoneyArraysReachExtensionsWithoutCopying(t *testing.T) {
	t.Parallel()
	registry := moneyConsole(t)
	var received []lang.Money
	err := lang.Logic(registry, "ledger.count_v1", lang.Doc{Cost: 1}, func(lines []lang.Money) (int64, error) {
		received = lines
		return int64(len(lines)), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := lang.CompileExpr(`ledger.count_v1(lines)`, registry, lang.CompileOptions{
		Args: []lang.ArgSpec{{Name: "lines", Type: lang.ArrayOf(lang.MoneyOf(""))}},
	})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := lang.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	lines := []lang.Money{amount("USD", 1), amount("EUR", 2)}
	value, err := lang.ToValue(lines)
	if err != nil {
		t.Fatal(err)
	}
	if result, err := runtime.RunValues(t.Context(), []lang.Value{value}, lang.RunOptions{}); err != nil || result.Any() != int64(2) {
		t.Fatalf("RunValues(ledger.count_v1) = %v, %v, want 2", result.Any(), err)
	}
	if &received[0] != &lines[0] {
		t.Error("the extension received a copy of the host's money")
	}
	back, err := lang.FromValue[[]lang.Money](value)
	if err != nil || &back[0] != &lines[0] {
		t.Errorf("FromValue[[]Money] = %v, %v, want the host's own slice", back, err)
	}
}

// RunValues checks money the way Run does: an undeclared currency is a
// currency failure and a contract one.
func TestRunValuesChecksTheCurrencies(t *testing.T) {
	t.Parallel()
	_, runtime := everyKind(t)
	valid := func() []lang.Value {
		return []lang.Value{
			valueOf(amount("USD", 1000)), valueOf(rateUnits(290_000_000)), valueOf(currency("EUR")),
		}
	}
	value, err := runtime.RunValues(t.Context(), valid(), lang.RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if encoded, err := json.Marshal(value); err != nil || string(encoded) != `{"fee":{"currency":"USD","minor":29},"made":{"currency":"EUR","minor":100}}` {
		t.Errorf("RunValues = %s, %v, want the fee and EUR 1.00", encoded, err)
	}
	for name, edit := range map[string]func([]lang.Value){
		"undeclared amount": func(args []lang.Value) { args[0] = valueOf(looseMoney("XXX", 1)) },
		"undeclared payout": func(args []lang.Value) { args[2] = valueOf(currency("XXX")) },
	} {
		args := valid()
		edit(args)
		if _, err := runtime.RunValues(t.Context(), args, lang.RunOptions{}); !errors.Is(err, lang.ErrCurrency) || !errors.Is(err, lang.ErrContract) {
			t.Errorf("RunValues with %s error = %v, want ErrCurrency and ErrContract", name, err)
		}
	}
	args := valid()
	args[1] = lang.Int(1)
	if _, err := runtime.RunValues(t.Context(), args, lang.RunOptions{}); !errors.Is(err, lang.ErrContract) || errors.Is(err, lang.ErrCurrency) {
		t.Errorf("RunValues with an int for a rate error = %v, want ErrContract alone", err)
	}
}

// A declared currency the contract does not allow is the same failure on
// both doors: Run says it is a currency mismatch, and so should RunValues.
func TestBothDoorsCallAWrongCurrencyACurrencyError(t *testing.T) {
	t.Parallel()
	registry := moneyConsole(t)
	artifact, err := lang.CompileExpr(`amount`, registry, lang.CompileOptions{Args: []lang.ArgSpec{{Name: "amount", Type: lang.MoneyOf("USD")}}})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := lang.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	_, byName := runtime.Run(t.Context(), map[string]any{"amount": "EUR 1.00"}, lang.RunOptions{})
	_, byValue := runtime.RunValues(t.Context(), []lang.Value{valueOf(amount("EUR", 100))}, lang.RunOptions{})
	if !errors.Is(byName, lang.ErrCurrency) || !errors.Is(byValue, lang.ErrCurrency) {
		t.Errorf("EUR for money<USD>: Run error = %v, RunValues error = %v, want ErrCurrency from both", byName, byValue)
	}
}

// EncodeJSON writes money the way a person reads it, in the registry's
// places, inside records (in field order), arrays and dictionaries; the
// currency-less zero is 0. json.Marshal, which knows no places, writes minor
// units.
func TestEncodeJSONWritesAmountsAsText(t *testing.T) {
	t.Parallel()
	registry := moneyConsole(t)
	artifact, err := lang.CompileExpr(`{z: USD -1.70, a: [JPY 5, JPY 0], d: {"k": EUR 0.05}, r: 25bps, c: EUR, n: 1, fx: JPY 150 / USD 1}`, registry, lang.CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := lang.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	value, err := runtime.Run(t.Context(), nil, lang.RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"z":"USD -1.70","a":["JPY 5","JPY 0"],"d":{"k":"EUR 0.05"},"r":"0.0025","c":"EUR","n":1,"fx":{"base":"USD","quote":"JPY","rate":"150"}}`
	if encoded, err := registry.EncodeJSON(value); err != nil || string(encoded) != want {
		t.Errorf("EncodeJSON = %s, %v\nwant %s", encoded, err, want)
	}
	raw := `{"z":{"currency":"USD","minor":-170},"a":[{"currency":"JPY","minor":5},{"currency":"JPY","minor":0}],"d":{"k":{"currency":"EUR","minor":5}},"r":"0.0025","c":"EUR","n":1,"fx":{"base":"USD","quote":"JPY","rate":"150"}}`
	if encoded, err := json.Marshal(value); err != nil || string(encoded) != raw {
		t.Errorf("json.Marshal = %s, %v\nwant %s", encoded, err, raw)
	}
	if encoded, err := registry.EncodeJSON(valueOf(lang.Money{})); err != nil || string(encoded) != "0" {
		t.Errorf("EncodeJSON(currency-less zero) = %s, %v, want 0", encoded, err)
	}
	if encoded, err := lang.CoreRegistry().EncodeJSON(valueOf(amount("USD", 170))); err != nil || string(encoded) != `{"currency":"USD","minor":170}` {
		t.Errorf("EncodeJSON without money declared = %s, %v, want the minor units", encoded, err)
	}
}

// Declaring money changes nothing a program without money compiles to, and
// an artifact that uses money loads only where the same money is declared.
func TestMoneyIsStampedOnlyWhereItIsUsed(t *testing.T) {
	t.Parallel()
	plain, withMoney := lang.CoreRegistry(), moneyConsole(t)
	options := lang.CompileOptions{Args: []lang.ArgSpec{{Name: "x", Type: lang.IntType}}}
	before, err := lang.CompileExpr(`x * 250 / 10000`, plain, options)
	if err != nil {
		t.Fatal(err)
	}
	after, err := lang.CompileExpr(`x * 250 / 10000`, withMoney, options)
	if err != nil || after.Digest() != before.Digest() {
		t.Fatalf("a program without money: digest %s after declaring money, %s before (%v), want the same", after.Digest(), before.Digest(), err)
	}
	fee, err := lang.CompileExpr(`USD 1.70`, withMoney, lang.CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lang.Instantiate(fee, plain); err == nil {
		t.Error("Instantiate(money artifact) on a registry without money error = nil, want a refusal")
	}
	evenly := lang.CoreRegistry()
	spec, _ := withMoney.Money()
	spec.Rounding = lang.RoundHalfEven
	if err := evenly.DeclareMoney(spec); err != nil {
		t.Fatal(err)
	}
	if _, err := lang.Instantiate(fee, evenly); err == nil {
		t.Error("Instantiate(money artifact) on a registry rounding otherwise error = nil, want a refusal")
	}
	if _, err := lang.CompileExpr(`USD 1.70`, plain, lang.CompileOptions{}); !errors.Is(err, lang.ErrCompile) {
		t.Errorf("CompileExpr(USD 1.70) without money error = %v, want ErrCompile", err)
	}
}

// No data type the package hands a host has a field the host can write:
// every one is made by a constructor, a table or a registry, and read
// through methods, so what a host holds already keeps the rules. The option
// structs a host fills in — CompileOptions, FunctionSpec, Doc, MoneySpec,
// CurrencySpec, BatchOptions, RunOptions, TextContract — are input, checked
// where they are used, and are not in this list.
func TestDataTypesHaveNoWritableFields(t *testing.T) {
	t.Parallel()
	for _, typ := range []reflect.Type{
		reflect.TypeFor[lang.Value](), reflect.TypeFor[lang.Type](), reflect.TypeFor[lang.Field](),
		reflect.TypeFor[lang.Money](), reflect.TypeFor[lang.Rate](), reflect.TypeFor[lang.FxRate](),
		reflect.TypeFor[lang.Currency](), reflect.TypeFor[lang.Rates](), reflect.TypeFor[lang.Quote](), reflect.TypeFor[lang.Artifact](),
		reflect.TypeFor[lang.Parameter](), reflect.TypeFor[lang.Manifest](), reflect.TypeFor[lang.LanguageCatalog](),
		reflect.TypeFor[lang.FunctionDescriptor](), reflect.TypeFor[lang.FormDescriptor](), reflect.TypeFor[lang.PositionError](),
	} {
		if typ.Kind() != reflect.Struct {
			t.Errorf("%s is a %s, want a struct a host cannot convert into", typ, typ.Kind())
			continue
		}
		for field := range typ.Fields() {
			if field.IsExported() {
				t.Errorf("%s has the exported field %s, want only methods", typ, field.Name)
			}
		}
	}
}
