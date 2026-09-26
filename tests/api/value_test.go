package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/nethinwei/funroute"
)

// These tests are written the way a host writes one: they may only use what
// package funroute exports, so they also guard the public surface. If one of
// them needs something from internal/, the surface is too narrow — or the
// change belongs elsewhere.

// The SDK's job is to let a host write its types down, and code that only
// ever uses := never notices a missing alias. So every public type a host
// names is named here, the way Go asserts a type at compile time: dropping one
// from the package stops this file compiling.
var (
	_ funroute.Value
	_ funroute.Kind
	_ funroute.Type
	_ funroute.Form
	_ *funroute.Runtime
	_ *funroute.Batch
	_ funroute.LanguageCatalog
	_ funroute.FunctionDescriptor
	_ funroute.FormDescriptor
	_ funroute.Example
	_ funroute.Fold
	_ []funroute.Parameter
	_ funroute.Manifest
	_ *funroute.Artifact
	_ funroute.Field
	_ *funroute.Binding[RouteIn, RouteOut]
	_ *funroute.Program[RouteIn, RouteOut]
	_ *funroute.ProgramBatch[RouteIn, RouteOut]
	_ *funroute.Session[RouteIn, RouteOut]
	_ funroute.Money
	_ funroute.ExactMoney
	_ funroute.Ratio
	_ funroute.Currency
	_ funroute.MoneySpec
	_ funroute.CurrencySpec
	_ funroute.Rounding
	_ funroute.AllocationStrategy
	_ *funroute.Currencies
)

func TestHostNamesTheValueTypes(t *testing.T) {
	t.Parallel()
	flag := funroute.Bool(true)
	text := funroute.String("SGD")
	kind := flag.Kind()
	boolType := funroute.BoolType
	numbers, err := funroute.Array(funroute.IntType, []funroute.Value{funroute.Int(1), funroute.Int(2)})
	if err != nil {
		t.Fatal(err)
	}
	weights, err := funroute.Dict(funroute.FloatType, map[string]funroute.Value{"adyen": funroute.Float(0.6)})
	if err != nil {
		t.Fatal(err)
	}
	dictType := funroute.DictOf(funroute.FloatType)
	// A record is named fields with their own types, in an order that is part
	// of the type; the host writes both down.
	orderType := funroute.RecordOf(
		funroute.FieldOf("amount", funroute.IntType),
		funroute.FieldOf("currency", funroute.StringType),
	)
	order, err := funroute.Record(orderType, []funroute.Value{funroute.Int(1200), funroute.String("SGD")})
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

func mustParseType(t *testing.T, text string) funroute.Type {
	t.Helper()
	typ, err := funroute.ParseType(text)
	if err != nil {
		t.Fatal(err)
	}
	return typ
}

// consoleRegistry is the kernel with every structural form: what a console
// that lets its rules map, fold and branch compiles against.
func consoleRegistry(t *testing.T) *funroute.Registry {
	t.Helper()
	registry := funroute.CoreRegistry()
	if err := registry.EnableForm(funroute.SwitchForm, funroute.ForForm, funroute.ReduceForm); err != nil {
		t.Fatal(err)
	}
	return registry
}

// A float is any float IEEE 754 has: NaN and the infinities go in and come
// out as they are.
func TestPublicFloatHoldsEveryFloat(t *testing.T) {
	t.Parallel()
	for _, want := range []float64{0.75, math.Copysign(0, -1), math.Inf(1), math.Inf(-1), math.NaN()} {
		got, ok := funroute.Float(want).Float()
		if !ok || math.Float64bits(got) != math.Float64bits(want) {
			t.Errorf("Float(%v).Float() = %v, %v; want %v, true", want, got, ok, want)
		}
	}
}

// The deep-learning shape: a feature vector goes host → VM → extension and
// a score comes back. The extension must receive the host's own slice.
func TestHostVectorsReachExtensionsWithoutCopying(t *testing.T) {
	t.Parallel()
	registry := funroute.CoreRegistry()
	var received []float64
	err := registry.Register(funroute.FunctionSpec{
		Name: "model.score_v1",
		Go: func(xs []float64) (float64, error) {
			received = xs
			return xs[0], nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := funroute.CompileExpr(`model.score_v1(features)`, registry, funroute.CompileOptions{
		Args: []funroute.ArgSpec{{Name: "features", Type: funroute.ArrayOf(funroute.FloatType)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := funroute.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	features := []float64{0.25, 0.5}
	value, err := funroute.ToValue(features)
	if err != nil {
		t.Fatal(err)
	}
	result, err := runtime.RunValues(t.Context(), []funroute.Value{value})
	if err != nil {
		t.Fatal(err)
	}
	if score, _ := funroute.FromValue[float64](result); score != 0.25 {
		t.Fatalf("score = %v, want 0.25", score)
	}
	if &received[0] != &features[0] {
		t.Fatal("the extension received a copy of the host's vector")
	}
	// Run's by-name path takes the same shortcut for a Go slice.
	if _, err := runtime.Run(t.Context(), map[string]any{"features": features}); err != nil {
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
	artifact, err := funroute.CompileExpr(`rate`, funroute.CoreRegistry(), funroute.CompileOptions{
		Args: []funroute.ArgSpec{{Name: "rate", Type: funroute.FloatType}},
	})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := funroute.Instantiate(artifact, funroute.CoreRegistry())
	if err != nil {
		t.Fatal(err)
	}
	for _, input := range []any{1, int64(1), int32(1), uint16(1), float32(1), 1.0} {
		t.Run(fmt.Sprintf("%T", input), func(t *testing.T) {
			t.Parallel()
			if _, err := runtime.Run(t.Context(), map[string]any{"rate": input}); err != nil {
				t.Fatalf("%T should widen to float: %v", input, err)
			}
		})
	}
	whole, err := funroute.CompileExpr(`amount`, funroute.CoreRegistry(), funroute.CompileOptions{
		Args: []funroute.ArgSpec{{Name: "amount", Type: funroute.IntType}},
	})
	if err != nil {
		t.Fatal(err)
	}
	intRuntime, err := funroute.Instantiate(whole, funroute.CoreRegistry())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := intRuntime.Run(t.Context(), map[string]any{"amount": 1.7}); err == nil {
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
	internal     int      // unexported: invisible either way
}

type Decision struct {
	Channel string `funroute:"channel"`
	Net     int64  `funroute:"net"`
}

func decisionRegistry(t *testing.T) *funroute.Registry {
	t.Helper()
	registry := funroute.CoreRegistry()
	err := registry.Register(funroute.FunctionSpec{
		Name: "route.decide_v1",
		Doc: funroute.Doc{
			Label: "决策", Category: "路由", Params: []string{"订单"}, Result: "决策",
		},
		Go: func(order Order) (Decision, error) {
			return Decision{Channel: "adyen_" + order.CurrencyCode, Net: order.Amount - 30}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return registry
}

func orderContract() funroute.CompileOptions {
	return funroute.CompileOptions{Args: []funroute.ArgSpec{{Name: "order", Type: funroute.RecordOf(
		funroute.FieldOf("amount", funroute.IntType),
		funroute.FieldOf("currency_code", funroute.StringType),
		funroute.FieldOf("tags", funroute.ArrayOf(funroute.StringType)),
	)}}}
}

// A Go struct is a record: the host passes the struct it has, the extension
// takes and returns structs, and the result comes back as a struct.
func TestHostPassesItsStructsThrough(t *testing.T) {
	t.Parallel()
	registry := decisionRegistry(t)
	artifact, err := funroute.CompileExpr(`route.decide_v1(order)`, registry, orderContract())
	if err != nil {
		t.Fatal(err)
	}
	want := funroute.RecordOf(
		funroute.FieldOf("channel", funroute.StringType),
		funroute.FieldOf("net", funroute.IntType),
	)
	runtime, err := funroute.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	if !runtime.ResultType().Equal(want) {
		t.Fatalf("result type = %s, want %s", runtime.ResultType(), want)
	}
	value, err := runtime.Run(t.Context(),
		map[string]any{"order": Order{Amount: 5000, CurrencyCode: "USD", Tags: []string{"vip"}}})
	if err != nil {
		t.Fatal(err)
	}
	decision, err := funroute.FromValue[Decision](value)
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
	artifact, err := funroute.CompileExpr(`{net: order.amount, channel: order.currency_code}`,
		funroute.CoreRegistry(), orderContract())
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := funroute.Instantiate(artifact, funroute.CoreRegistry())
	if err != nil {
		t.Fatal(err)
	}
	value, err := runtime.Run(t.Context(),
		map[string]any{"order": Order{Amount: 1200, CurrencyCode: "SGD"}})
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
	_, err := funroute.CompileExpr(`order.updated_at`, registry, orderContract())
	if err == nil || !strings.Contains(err.Error(), "no field") {
		t.Fatalf("CompileExpr(%q) error = %v, want \"no field\": an untagged field must not be readable", "order.updated_at", err)
	}
	// A struct that tags nothing is not a record at all.
	type Untagged struct {
		Amount int64
	}
	err = funroute.CoreRegistry().Register(funroute.FunctionSpec{
		Name: "demo.untagged_v1",
		Doc:  funroute.Doc{Label: "无标签", Category: "演示", Params: []string{"值"}, Result: "值"},
		Go:   func(u Untagged) (int64, error) { return u.Amount, nil },
	})
	if err == nil || !strings.Contains(err.Error(), "no record fields") {
		t.Fatalf("Register(func(Untagged)) error = %v, want \"no record fields\"", err)
	}
}

// A field name is a plain name in both doors: the type text and the source.
func TestRecordFieldNamesFollowOneRule(t *testing.T) {
	t.Parallel()
	if _, err := funroute.ParseType(`record{in: int}`); err == nil || !strings.Contains(err.Error(), "field name") {
		t.Fatalf("ParseType(%q) error = %v, want \"field name\": a reserved word is not a field name", "record{in: int}", err)
	}
	if _, err := funroute.CompileExpr(`{in: 1}`, funroute.CoreRegistry(), funroute.CompileOptions{}); err == nil ||
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
		Customer: Order{Amount: 1200, CurrencyCode: "SGD", Tags: []string{"vip"}, UpdatedAt: "ignored", internal: 7},
		Lines:    []Line{{SKU: "a", Amount: 10}, {SKU: "b", Amount: 20}},
	}
	contract := funroute.CompileOptions{Args: []funroute.ArgSpec{{Name: "basket", Type: mustParseType(t,
		`record{customer: record{amount: int, currency_code: string, tags: array<string>}, lines: array<record{sku: string, amount: int}>}`)}}}
	artifact, err := funroute.CompileExpr(
		`{who: basket.customer.currency_code, first: basket.lines[0].sku, total: reduce(l in basket.lines, sum = 0, sum + l.amount)}`,
		consoleRegistry(t), contract)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := funroute.Instantiate(artifact, consoleRegistry(t))
	if err != nil {
		t.Fatal(err)
	}
	value, err := runtime.Run(t.Context(), map[string]any{"basket": basket})
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
	lines, err := funroute.CompileExpr(`basket.lines`, consoleRegistry(t), contract)
	if err != nil {
		t.Fatal(err)
	}
	linesRuntime, err := funroute.Instantiate(lines, consoleRegistry(t))
	if err != nil {
		t.Fatal(err)
	}
	listed, err := linesRuntime.Run(t.Context(), map[string]any{"basket": basket})
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
	artifact, err := funroute.CompileExpr(`{amount: order.amount}`, consoleRegistry(t), funroute.CompileOptions{
		Args: []funroute.ArgSpec{{Name: "order", Type: mustParseType(t, `record{amount: int}`)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := funroute.Instantiate(artifact, consoleRegistry(t))
	if err != nil {
		t.Fatal(err)
	}
	// The payload carries more than the contract asks for.
	value, err := runtime.Run(t.Context(),
		map[string]any{"order": map[string]any{"amount": 7, "extra": 9, "note": "x"}})
	if err != nil {
		t.Fatal(err)
	}
	// And the struct taken back out may carry more than the record does.
	type Wide struct {
		Amount  int64  `funroute:"amount"`
		Channel string `funroute:"channel"` // absent from the record: stays zero
	}
	wide, err := funroute.FromValue[Wide](value)
	if err != nil || wide.Amount != 7 || wide.Channel != "" {
		t.Fatalf("FromValue[Wide] = %+v, %v; want {Amount:7 Channel:}", wide, err)
	}
	// A field the contract does ask for is still required.
	_, err = runtime.Run(t.Context(), map[string]any{"order": map[string]any{"extra": 9}})
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
	aliases := map[string]funroute.Type{"Order": mustParseType(t, written)}

	// The subtests fill one map that is compared after them, so they run one
	// after the other rather than in parallel.
	digests := map[string]string{}
	for label, text := range map[string]string{"named": "Order", "written": written} {
		t.Run(label, func(t *testing.T) {
			typ, err := funroute.ParseTypeWith(text, aliases)
			if err != nil {
				t.Fatalf("ParseTypeWith(%q) error = %v", text, err)
			}
			artifact, err := funroute.CompileExpr(`route.decide_v1(order).channel`, registry,
				funroute.CompileOptions{Args: []funroute.ArgSpec{{Name: "order", Type: typ}}})
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
	if _, err := funroute.ParseTypeWith("Missing", aliases); err == nil {
		t.Fatal("ParseTypeWith(\"Missing\") error = nil, want an undeclared type name")
	}
	// Aliases do not nest, so a declaration cannot name another one.
	if _, err := funroute.ParseType("array<Order>"); err == nil {
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
	const want = `fields A and B of api.twice are both tagged "a"`
	if _, err := funroute.ToValue(twice{}); err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("ToValue(twice{}) error = %v, want %q", err, want)
	}
	if err := funroute.CoreRegistry().Register(funroute.FunctionSpec{
		Name: "f.g_v1",
		Go:   func(twice) (int64, error) { return 0, nil },
	}); err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("Register(func(twice)) error = %v, want %q", err, want)
	}
	type in struct {
		R twice `funroute:"r"`
	}
	if _, err := funroute.Bind[in, int64](funroute.CoreRegistry()); err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("Bind[in, int64] error = %v, want %q", err, want)
	}
}

// A ratio prints and crosses JSON as its exact text — a decimal, or a
// fraction where it has no finite decimal — and reads back from a string or
// a number.
func TestRatiosCrossJSONAsDecimals(t *testing.T) {
	t.Parallel()
	for ratio, want := range map[funroute.Ratio]string{third(t, "1/3"): "1/3", ratioText("0.029"): "0.029", ratioText("150"): "150", ratioText("-0.0000000001"): "-0.0000000001", ratioText("0"): "0"} {
		if got := ratio.String(); got != want {
			t.Errorf("Ratio(%s).String() = %q, want %q", want, got, want)
		}
		encoded, err := json.Marshal(ratio)
		if err != nil || string(encoded) != `"`+want+`"` {
			t.Errorf("json.Marshal(Ratio(%s)) = %s, %v, want %q", want, encoded, err, want)
		}
	}
	for data, want := range map[string]funroute.Ratio{`"0.029"`: ratioText("0.029"), `0.029`: ratioText("0.029"), `2`: ratioText("2"), `"-1.5"`: ratioText("-1.5"), `"0.00000000001"`: third(t, "0.00000000001")} {
		var got funroute.Ratio
		if err := json.Unmarshal([]byte(data), &got); err != nil || got != want {
			t.Errorf("json.Unmarshal(%s) = %s, %v, want %s", data, got, err, want)
		}
	}
	for _, data := range []string{`"abc"`, `1e3`, `"1/0"`, `true`, `"1_0"`} {
		var got funroute.Ratio
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
	var received []funroute.Money
	err := registry.Register(funroute.FunctionSpec{
		Name: "ledger.count_v1",
		Go: func(lines []funroute.Money) (int64, error) {
			received = lines
			return int64(len(lines)), nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := funroute.CompileExpr(`ledger.count_v1(lines)`, registry, funroute.CompileOptions{
		Args: []funroute.ArgSpec{{Name: "lines", Type: funroute.ArrayOf(funroute.MoneyType)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := funroute.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	lines := []funroute.Money{amount("USD", 1), amount("EUR", 2)}
	value, err := funroute.ToValue(lines)
	if err != nil {
		t.Fatal(err)
	}
	if result, err := runtime.RunValues(t.Context(), []funroute.Value{value}); err != nil || result.Any() != int64(2) {
		t.Fatalf("RunValues(ledger.count_v1) = %v, %v, want 2", result.Any(), err)
	}
	if &received[0] != &lines[0] {
		t.Error("the extension received a copy of the host's money")
	}
	back, err := funroute.FromValue[[]funroute.Money](value)
	if err != nil || &back[0] != &lines[0] {
		t.Errorf("FromValue[[]Money] = %v, %v, want the host's own slice", back, err)
	}
}

// RunValues checks money the way Run does: an undeclared currency is a
// currency failure and a contract one.
func TestRunValuesChecksTheCurrencies(t *testing.T) {
	t.Parallel()
	_, runtime := everyKind(t)
	valid := func() []funroute.Value {
		return []funroute.Value{
			valueOf(amount("USD", 1000)), valueOf(ratioText("0.029")), valueOf(currency("EUR")),
		}
	}
	value, err := runtime.RunValues(t.Context(), valid())
	if err != nil {
		t.Fatal(err)
	}
	if encoded, err := json.Marshal(value); err != nil || string(encoded) != `{"fee":{"currency":"USD","minor":29},"made":{"currency":"EUR","minor":100}}` {
		t.Errorf("RunValues = %s, %v, want the fee and EUR 1.00", encoded, err)
	}
	for name, edit := range map[string]func([]funroute.Value){
		"undeclared amount": func(args []funroute.Value) { args[0] = valueOf(looseMoney(1)) },
		"undeclared payout": func(args []funroute.Value) { args[2] = valueOf(currency("XXX")) },
	} {
		args := valid()
		edit(args)
		if _, err := runtime.RunValues(t.Context(), args); !errors.Is(err, funroute.ErrCurrency) || !errors.Is(err, funroute.ErrContract) {
			t.Errorf("RunValues with %s error = %v, want ErrCurrency and ErrContract", name, err)
		}
	}
	args := valid()
	args[1] = funroute.Int(1)
	if _, err := runtime.RunValues(t.Context(), args); !errors.Is(err, funroute.ErrContract) || errors.Is(err, funroute.ErrCurrency) {
		t.Errorf("RunValues with an int for a rate error = %v, want ErrContract alone", err)
	}
}

// EncodeJSON writes money the way a person reads it, in the registry's
// places, inside records (in field order), arrays and dictionaries; the
// currency-less zero is 0. json.Marshal, which knows no places, writes minor
// units.
func TestEncodeJSONWritesAmountsAsText(t *testing.T) {
	t.Parallel()
	registry := moneyConsole(t)
	artifact, err := funroute.CompileExpr(`{z: USD -1.70, a: [JPY 5, JPY 0], d: {"k": EUR 0.05}, r: 25bps, c: EUR, n: 1, fx: implied(JPY 150, USD 1)}`, registry, funroute.CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := funroute.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	value, err := runtime.Run(t.Context(), nil)
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
	if encoded, err := registry.EncodeJSON(valueOf(funroute.Money{})); err != nil || string(encoded) != "0" {
		t.Errorf("EncodeJSON(currency-less zero) = %s, %v, want 0", encoded, err)
	}
	if encoded, err := funroute.CoreRegistry().EncodeJSON(valueOf(amount("USD", 170))); err != nil || string(encoded) != `{"currency":"USD","minor":170}` {
		t.Errorf("EncodeJSON without money declared = %s, %v, want the minor units", encoded, err)
	}
}

// Declaring money changes nothing a program without money compiles to, and
// an artifact that uses money loads only where the same money is declared.
func TestMoneyIsStampedOnlyWhereItIsUsed(t *testing.T) {
	t.Parallel()
	plain, withMoney := funroute.CoreRegistry(), moneyConsole(t)
	options := funroute.CompileOptions{Args: []funroute.ArgSpec{{Name: "x", Type: funroute.IntType}}}
	before, err := funroute.CompileExpr(`x * 250 / 10000`, plain, options)
	if err != nil {
		t.Fatal(err)
	}
	after, err := funroute.CompileExpr(`x * 250 / 10000`, withMoney, options)
	if err != nil || after.Digest() != before.Digest() {
		t.Fatalf("a program without money: digest %s after declaring money, %s before (%v), want the same", after.Digest(), before.Digest(), err)
	}
	fee, err := funroute.CompileExpr(`USD 1.70`, withMoney, funroute.CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := funroute.Instantiate(fee, plain); err == nil {
		t.Error("Instantiate(money artifact) on a registry without money error = nil, want a refusal")
	}
	if _, err := funroute.CompileExpr(`USD 1.70`, plain, funroute.CompileOptions{}); !errors.Is(err, funroute.ErrCompile) {
		t.Errorf("CompileExpr(USD 1.70) without money error = %v, want ErrCompile", err)
	}
}

// No data type the package hands a host has a field the host can write:
// every one is made by a constructor, a table or a registry, and read
// through methods, so what a host holds already keeps the rules. The option
// structs a host fills in — CompileOptions, FunctionSpec, Doc, MoneySpec,
// CurrencySpec, BatchOptions, TextContract — are input, checked
// where they are used, and are not in this list.
func TestDataTypesHaveNoWritableFields(t *testing.T) {
	t.Parallel()
	for _, typ := range []reflect.Type{
		reflect.TypeFor[funroute.Value](), reflect.TypeFor[funroute.Type](), reflect.TypeFor[funroute.Field](),
		reflect.TypeFor[funroute.Money](), reflect.TypeFor[funroute.ExactMoney](), reflect.TypeFor[funroute.Ratio](), reflect.TypeFor[funroute.FxRate](),
		reflect.TypeFor[funroute.Currency](), reflect.TypeFor[funroute.Artifact](),
		reflect.TypeFor[funroute.Parameter](), reflect.TypeFor[funroute.Manifest](), reflect.TypeFor[funroute.LanguageCatalog](),
		reflect.TypeFor[funroute.FunctionDescriptor](), reflect.TypeFor[funroute.FormDescriptor](), reflect.TypeFor[funroute.PositionError](),
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

// third is ParseRatio's ratio for text the test knows is one.
func third(t *testing.T, text string) funroute.Ratio {
	t.Helper()
	ratio, err := funroute.ParseRatio(text)
	if err != nil {
		t.Fatal(err)
	}
	return ratio
}

// A part of an array is the array's own backing, not a copy: a host's
// vector sliced is still the host's vector, read-only as it was.
func TestASliceSharesTheArraysBacking(t *testing.T) {
	t.Parallel()
	vector := []float64{0.5, 1.5, 2.5, 3.5}
	value, err := funroute.ToValue(vector)
	if err != nil {
		t.Fatal(err)
	}
	part, ok := value.Slice(1, 3)
	got, isFloats := part.Any().([]float64)
	if !ok || !isFloats || len(got) != 2 || &got[0] != &vector[1] {
		t.Fatalf("Slice(1, 3) of %v = %v, %v, want [1.5 2.5] over the same memory", vector, part.Any(), ok)
	}
	if _, ok := value.Slice(3, 5); ok {
		t.Fatal("Slice(3, 5) of four items = ok, want false")
	}
}
