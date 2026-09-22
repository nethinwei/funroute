package lang_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"funroute/lang"
)

// Order is the shape a host already holds. A field is in the record because it
// says so; UpdatedAt and internal are simply not part of it.
type Order struct {
	Amount       int64    `funroute:"amount"`
	CurrencyCode string   `funroute:"currency_code"`
	Tags         []string `funroute:"tags"`
	UpdatedAt    string   // untagged: the language never sees it
	internal     int      //nolint:unused // unexported: invisible either way
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
		lang.Field{Name: "amount", Type: lang.IntType},
		lang.Field{Name: "currency_code", Type: lang.StringType},
		lang.Field{Name: "tags", Type: lang.ArrayOf(lang.StringType)},
	)}}}
}

// A Go struct is a record: the host passes the struct it has, the extension
// takes and returns structs, and the result comes back as a struct.
func TestHostPassesItsStructsThrough(t *testing.T) {
	registry := decisionRegistry(t)
	artifact, err := lang.CompileExpr(`route.decide_v1(order)`, registry, orderContract())
	if err != nil {
		t.Fatal(err)
	}
	want := lang.RecordOf(
		lang.Field{Name: "channel", Type: lang.StringType},
		lang.Field{Name: "net", Type: lang.IntType},
	)
	runtime, err := lang.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	if !runtime.ResultType().Equal(want) {
		t.Fatalf("result type = %s, want %s", runtime.ResultType(), want)
	}
	value, err := runtime.Run(context.Background(),
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
		t.Fatalf("decision = %+v", decision)
	}
}

// The field order of a record is part of its type, so the JSON a host reads
// back is in the contract's order rather than alphabetical.
func TestRecordJSONKeepsTheFieldOrder(t *testing.T) {
	artifact, err := lang.CompileExpr(`{net: order.amount, channel: order.currency_code}`,
		lang.CoreRegistry(), orderContract())
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := lang.Instantiate(artifact, lang.CoreRegistry())
	if err != nil {
		t.Fatal(err)
	}
	value, err := runtime.Run(context.Background(),
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
	registry := decisionRegistry(t)
	_, err := lang.CompileExpr(`order.updated_at`, registry, orderContract())
	if err == nil || !strings.Contains(err.Error(), "no field") {
		t.Fatalf("an untagged field must not be readable: %v", err)
	}
	// A struct that tags nothing is not a record at all.
	type Untagged struct {
		Amount int64
	}
	err = lang.Logic(lang.CoreRegistry(), "demo.untagged_v1",
		lang.Doc{Label: "无标签", Category: "演示", Cost: 1, Params: []string{"值"}, Result: "值"},
		func(u Untagged) (int64, error) { return u.Amount, nil })
	if err == nil || !strings.Contains(err.Error(), "no record fields") {
		t.Fatalf("a struct without any tag must be refused: %v", err)
	}
}

// A field name is a plain name in both doors: the type text and the source.
func TestRecordFieldNamesFollowOneRule(t *testing.T) {
	if _, err := lang.ParseType(`record{in: int}`); err == nil || !strings.Contains(err.Error(), "field name") {
		t.Fatalf("a reserved word is not a field name: %v", err)
	}
	if _, err := lang.CompileExpr(`{in: 1}`, lang.CoreRegistry(), lang.CompileOptions{}); err == nil ||
		!strings.Contains(err.Error(), "field name") {
		t.Fatalf("the literal must follow the same rule: %v", err)
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
	basket := Basket{
		Customer: Order{Amount: 1200, CurrencyCode: "SGD", Tags: []string{"vip"}, UpdatedAt: "ignored"},
		Lines:    []Line{{SKU: "a", Amount: 10}, {SKU: "b", Amount: 20}},
	}
	contract := lang.CompileOptions{Args: []lang.ArgSpec{{Name: "basket", Type: mustParse(t,
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
	value, err := runtime.Run(context.Background(), map[string]any{"basket": basket}, lang.RunOptions{Fuel: 10_000})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != `{"who":"SGD","first":"a","total":30}` {
		t.Fatalf("value = %s", encoded)
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
	listed, err := linesRuntime.Run(context.Background(), map[string]any{"basket": basket}, lang.RunOptions{Fuel: 10_000})
	if err != nil {
		t.Fatal(err)
	}
	encodedLines, err := json.Marshal(listed)
	if err != nil {
		t.Fatal(err)
	}
	if string(encodedLines) != `[{"sku":"a","amount":10},{"sku":"b","amount":20}]` {
		t.Fatalf("nested records lost their field order: %s", encodedLines)
	}
}

// A host struct serves many rules: a contract may declare fewer fields than
// the struct carries, and a struct may have fields no record mentions.
func TestRecordsMatchByNameNotByCount(t *testing.T) {
	artifact, err := lang.CompileExpr(`{amount: order.amount}`, consoleRegistry(t), lang.CompileOptions{
		Args: []lang.ArgSpec{{Name: "order", Type: mustParse(t, `record{amount: int}`)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := lang.Instantiate(artifact, consoleRegistry(t))
	if err != nil {
		t.Fatal(err)
	}
	// The payload carries more than the contract asks for.
	value, err := runtime.Run(context.Background(),
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
		t.Fatalf("wide = %+v, err = %v", wide, err)
	}
	// A field the contract does ask for is still required.
	_, err = runtime.Run(context.Background(), map[string]any{"order": map[string]any{"extra": 9}}, lang.RunOptions{Fuel: 1000})
	if err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("a missing field must be refused: %v", err)
	}
}

// Widening a number is lossless and allowed, whichever door it comes through;
// losing a fraction is not.
func TestNumbersWidenButNeverNarrow(t *testing.T) {
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
		if _, err := runtime.Run(context.Background(), map[string]any{"rate": input}, lang.RunOptions{Fuel: 1000}); err != nil {
			t.Fatalf("%T should widen to float: %v", input, err)
		}
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
	if _, err := intRuntime.Run(context.Background(), map[string]any{"amount": 1.7}, lang.RunOptions{Fuel: 1000}); err == nil {
		t.Fatal("1.7 is not an int and must not be rounded into one")
	}
}

func mustParse(t *testing.T, text string) lang.Type {
	t.Helper()
	typ, err := lang.ParseType(text)
	if err != nil {
		t.Fatal(err)
	}
	return typ
}

func consoleRegistry(t *testing.T) *lang.Registry {
	t.Helper()
	registry := lang.CoreRegistry()
	if err := registry.EnableForm(lang.SwitchForm, lang.ForForm, lang.ReduceForm); err != nil {
		t.Fatal(err)
	}
	return registry
}

// A declared type is spelling, nothing more: naming one produces the same
// artifact — digest included — as writing the record out at every argument.
// That is what lets a console declare `Order` once without changing the ABI.
func TestDeclaredTypesAreSpellingOnly(t *testing.T) {
	registry := decisionRegistry(t)
	written := "record{amount: int, currency_code: string, tags: array<string>}"
	aliases := map[string]lang.Type{"Order": mustParseType(t, written)}

	digests := map[string]string{}
	for label, text := range map[string]string{"named": "Order", "written": written} {
		typ, err := lang.ParseTypeWith(text, aliases)
		if err != nil {
			t.Fatalf("%s: %v", label, err)
		}
		artifact, err := lang.CompileExpr(`route.decide_v1(order).channel`, registry,
			lang.CompileOptions{Args: []lang.ArgSpec{{Name: "order", Type: typ}}})
		if err != nil {
			t.Fatalf("%s: %v", label, err)
		}
		digests[label] = artifact.Digest
	}
	if digests["named"] != digests["written"] {
		t.Fatalf("an alias changed the artifact: %s vs %s", digests["named"], digests["written"])
	}

	// A name nobody declared is still a name nobody declared.
	if _, err := lang.ParseTypeWith("Missing", aliases); err == nil {
		t.Fatal("an undeclared type name compiled")
	}
	// Aliases do not nest, so a declaration cannot name another one.
	if _, err := lang.ParseType("array<Order>"); err == nil {
		t.Fatal("ParseType resolved an alias it was never given")
	}
}

func mustParseType(t *testing.T, text string) lang.Type {
	t.Helper()
	typ, err := lang.ParseType(text)
	if err != nil {
		t.Fatal(err)
	}
	return typ
}
