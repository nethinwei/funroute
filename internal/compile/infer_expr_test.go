package compile

import (
	"fmt"
	"strings"
	"testing"

	"github.com/nethinwei/funroute/internal/machine"
)

// A record is what a host already has: one object with named fields of
// different types. The contract declares it, the rule reads fields off it and
// can build one back.
func TestRecordFieldsAreReadAndBuilt(t *testing.T) {
	t.Parallel()
	order := machine.RecordOf(
		machine.FieldOf("amount", machine.IntType),
		machine.FieldOf("currency", machine.StringType),
	)
	registry := consoleRegistry(t)
	artifact, err := CompileExpr(`{net: order.amount - fee, currency: order.currency}`, registry, CompileOptions{
		Args: []ArgSpec{{Name: "order", Type: order}, {Name: "fee", Type: machine.IntType}},
	})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := machine.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	value, err := runtime.Run(t.Context(),
		map[string]any{"order": map[string]any{"amount": 1200, "currency": "SGD"}, "fee": 30})
	if err != nil {
		t.Fatal(err)
	}
	want := machine.RecordOf(
		machine.FieldOf("net", machine.IntType),
		machine.FieldOf("currency", machine.StringType),
	)
	if !runtime.ResultType().Equal(want) {
		t.Fatalf("result type = %s, want %s", runtime.ResultType(), want)
	}
	result, _ := value.Any().(map[string]any)
	if result["net"] != int64(1170) || result["currency"] != "SGD" {
		t.Fatalf("value = %v, want net 1170 and currency SGD", value.Any())
	}
}

func TestRecordRejectsWhatIsNotThatRecord(t *testing.T) {
	t.Parallel()
	order := machine.RecordOf(machine.FieldOf("amount", machine.IntType))
	contract := CompileOptions{Args: []ArgSpec{{Name: "order", Type: order}}}
	if _, err := CompileExpr(`order.total`, consoleRegistry(t), contract); err == nil ||
		!strings.Contains(err.Error(), "no field") {
		t.Fatalf("an undeclared field must fail at compile time: %v", err)
	}
	// The field order is the type: the same names in another order is another type.
	swapped := machine.RecordOf(
		machine.FieldOf("b", machine.IntType), machine.FieldOf("a", machine.IntType))
	if swapped.Equal(machine.RecordOf(
		machine.FieldOf("a", machine.IntType), machine.FieldOf("b", machine.IntType))) {
		t.Fatal("field order is part of a record's identity")
	}
	// A missing field at the boundary is refused: there is no null to fill it.
	artifact, err := CompileExpr(`order.amount`, consoleRegistry(t), contract)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := machine.Instantiate(artifact, consoleRegistry(t))
	if err != nil {
		t.Fatal(err)
	}
	_, err = runtime.Run(t.Context(), map[string]any{"order": map[string]any{}})
	if err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("a record without its field must be refused: %v", err)
	}
}

// A record composes with everything else: it can sit in a container, be read
// after a subscript, be compared, and survive the ExprJSON round trip.
func TestRecordComposesWithTheRestOfTheLanguage(t *testing.T) {
	t.Parallel()
	orders := machine.ArrayOf(machine.RecordOf(machine.FieldOf("amount", machine.IntType)))
	contract := []ArgSpec{{Name: "orders", Type: orders}}
	for _, test := range []struct {
		source string
		want   any
	}{
		{`orders[0].amount`, int64(10)},
		{`reduce(o in orders, total = 0, total + o.amount)`, int64(30)},
		{`orders[0] == orders[1]`, false},
		{`{first: orders[0].amount}.first`, int64(10)},
	} {
		t.Run(test.source, func(t *testing.T) {
			t.Parallel()
			artifact, err := CompileExpr(test.source, consoleRegistry(t), CompileOptions{Args: contract})
			if err != nil {
				t.Fatalf("compile %s: %v", test.source, err)
			}
			runtime, err := machine.Instantiate(artifact, consoleRegistry(t))
			if err != nil {
				t.Fatal(err)
			}
			value, err := runtime.Run(t.Context(),
				map[string]any{"orders": []any{map[string]any{"amount": 10}, map[string]any{"amount": 20}}})
			if err != nil {
				t.Fatalf("run %s: %v", test.source, err)
			}
			if value.Any() != test.want {
				t.Fatalf("%s = %v, want %v", test.source, value.Any(), test.want)
			}
		})
	}

	// Two ways to write the same field read produce the same tree.
	assertExprJSONIsCanonical(t, `orders[0].amount`)
}

func TestDictionaryWalkNeedsTwoVariables(t *testing.T) {
	t.Parallel()
	registry := consoleRegistry(t)
	dictContract := []ArgSpec{{Name: "weights", Type: machine.DictOf(machine.FloatType)}}
	// Two variables walk a dictionary in sorted key order; one walks an array.
	for _, test := range []struct {
		source   string
		contract []ArgSpec
		args     map[string]any
		want     string
	}{
		{`[k for k, v in weights if v > 0.0]`, dictContract, map[string]any{"weights": map[string]any{"b": 1.0, "a": 2.0}}, "[a b]"},
		{`[v for k, v in weights if v > 0.0]`, dictContract, map[string]any{"weights": map[string]any{"b": 1.0, "a": 2.0}}, "[2 1]"},
		{`reduce(k, v in weights, t = 0.0, t + v)`, dictContract, map[string]any{"weights": map[string]any{"a": 1.5, "b": 2.5}}, "4"},
		{`[x for x in items if x > 0]`, nil, map[string]any{"items": []any{1, 2}}, "[1 2]"},
	} {
		t.Run(test.source, func(t *testing.T) {
			t.Parallel()
			assertRunsTo(t, registry, test.source, test.contract, test.args, test.want)
		})
	}
	// The variable count and the source shape must agree.
	for _, test := range []struct {
		source string
		args   []ArgSpec
		want   string
	}{
		// With the hint forcing a dict, one variable is the wrong shape.
		{`[v for v in weights if v > 0.0]`, dictContract, "must be an array"},
		{`[x for k, x in items if x > 0]`, []ArgSpec{{Name: "items", Type: machine.ArrayOf(machine.IntType)}}, "must be a dict"},
		{`[x for x, x in items]`, nil, `"x" is bound twice`},
	} {
		t.Run(test.source, func(t *testing.T) {
			t.Parallel()
			if _, err := CompileExpr(test.source, registry, CompileOptions{Args: test.args}); err == nil ||
				!strings.Contains(err.Error(), test.want) {
				t.Fatalf("%s error = %v, want %q", test.source, err, test.want)
			}
		})
	}
}

// assertRunsTo compiles source under contract, runs it on args and checks
// that the result prints as want.
func assertRunsTo(t *testing.T, registry *machine.Registry, source string, contract []ArgSpec, args map[string]any, want string) {
	t.Helper()
	artifact, err := CompileExpr(source, registry, CompileOptions{Args: contract})
	if err != nil {
		t.Fatalf("%s: %v", source, err)
	}
	runtime, err := machine.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	value, err := runtime.Run(t.Context(), args)
	if err != nil {
		t.Fatalf("%s: %v", source, err)
	}
	if got := fmt.Sprint(value.Any()); got != want {
		t.Fatalf("%s = %s, want %s", source, got, want)
	}
}

// formContract gives money to every form: arrays in a code and in any
// currency, a record field, a dictionary and rates.
const formContract = "amount: money; ms: array<money>; cs: array<money>; r: record{fee: money}; " +
	"fees: dict<money>; rates: array<ratio>; k: int; m: money; quotes: array<fxrate>"

func formArgs() map[string]any {
	return map[string]any{
		"amount": "USD 10.00", "ms": []any{"USD 1.00", "USD -2.00"}, "cs": []any{"EUR 1.00"},
		"r": map[string]any{"fee": "EUR 0.50"}, "fees": map[string]any{"a": "EUR 1.00"},
		"rates": []any{"0.1"}, "k": 2, "m": "EUR 10.00", "quotes": testQuotes(),
	}
}

// Money keeps its currency through for, reduce, let, switch, record
// literals, field reads and dictionary comprehensions.
func TestMoneyFlowsThroughForms(t *testing.T) {
	t.Parallel()
	for _, test := range []struct{ source, typ, want string }{
		{"[x * 2 for x in ms]", "array<money>", "[{USD 200} {USD -400}]"},
		{"[x for x in ms if x > 0]", "array<money>", "[{USD 100}]"},
		{"using(quotes, [round(x -> JPY, @half_up) for x in ms])", "array<money>", "[{JPY 151} {JPY -301}]"},
		{"[round(x * q, @half_even) for x in ms for q in rates]", "array<money>", "[{USD 10} {USD -20}]"},
		{"reduce(x in ms, total = 0, total + x)", "money", "{USD -100}"},
		{"reduce(x in ms, total = USD 0, total + x)", "money", "{USD -100}"},
		{"reduce(x in cs, total = 0, total + x)", "money", "{EUR 100}"},
		{"reduce(x in ms if x > 0, n = 0, n + 1)", "int", "1"},
		{"let(fee = round(amount * 2.9%, @half_even), cap = USD 0.25, if(fee > cap, cap, fee))", "money", "{USD 25}"},
		{"switch(currency(m), case USD => round(m * 2%, @half_even), case EUR => round(m * 3%, @half_even), else => m)", "money", "{EUR 30}"},
		{"{fee: round(amount * 2.9%, @half_even), net: amount - round(amount * 2.9%, @half_even)}.net", "money", "{USD 971}"},
		{"r.fee + r.fee", "money", "{EUR 100}"},
		{"r.fee + m", "money", "{EUR 1050}"},
		{"{string(currency(x)): minor(x) for x in cs}", "dict<int>", "map[EUR:100]"},
		{"{key: v * 2 for key, v in fees}", "dict<money>", "map[a:{EUR 200}]"},
		{"allocate(amount, [1, 2])", "array<money>", "[{USD 333} {USD 667}]"},
		{"len(allocate(amount, len(ms)))", "int", "2"},
		{"allocate(m, 3)[0]", "money", "{EUR 334}"},
	} {
		t.Run(test.source, func(t *testing.T) {
			t.Parallel()
			artifact, err := compileMoney(t, test.source, formContract, "")
			if err != nil {
				t.Fatalf("CompileExpr(%q) error = %v", test.source, err)
			}
			got, err := runArtifact(t, artifact, formArgs())
			if artifact.Result().String() != test.typ || err != nil || got != test.want {
				t.Fatalf("%s = %s (%s), %v, want %s (%s)", test.source, got, artifact.Result(), err, test.want, test.typ)
			}
		})
	}
}

// Splitting into a count the input does not bound is refused, as range is.
func TestAllocateNeedsABoundedCount(t *testing.T) {
	t.Parallel()
	if _, err := compileMoney(t, "allocate(amount, k)", formContract, ""); err == nil {
		t.Fatal("allocate(amount, k) compiled, want the count to need a bound")
	}
}

// A record literal's field needs its type on the spot, so a literal there is
// settled once per kind it may be: the plain kind wins unless something
// later — the declared result, a use of the field — asks for the other.
func TestRecordFieldLiteralsAreForked(t *testing.T) {
	t.Parallel()
	const contract = "usd: money"
	for _, test := range []struct{ source, result, want string }{
		{"{fee: 0}", "", "record{fee: int}"},
		{"{r: 0.5}", "", "record{r: float}"},
		{"{fee: 0, r: 0.5}", "", "record{fee: int, r: float}"},
		{"{r: 0.5}", "record{r: ratio}", "record{r: ratio}"},
		{"{fee: 0}", "record{fee: money}", "record{fee: money}"},
		{"round({r: 0.5}.r * usd, @half_even)", "", "money"},
		// The field is read over the record's own unit class: the zero
		// meets dollars and the sum is proven dollars.
		{"{fee: 0}.fee + usd", "", "money"},
	} {
		t.Run(test.source+" as "+test.result, func(t *testing.T) {
			t.Parallel()
			artifact, err := compileMoney(t, test.source, contract, test.result)
			if err != nil {
				t.Fatalf("CompileExpr(%q) error = %v", test.source, err)
			}
			if got := artifact.Result().String(); got != test.want {
				t.Fatalf("CompileExpr(%q) result = %s, want %s", test.source, got, test.want)
			}
		})
	}
}
