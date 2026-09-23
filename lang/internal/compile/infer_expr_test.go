package compile

import (
	"fmt"
	"strings"
	"testing"

	"funroute/lang/internal/machine"
)

// A record is what a host already has: one object with named fields of
// different types. The contract declares it, the rule reads fields off it and
// can build one back.
func TestRecordFieldsAreReadAndBuilt(t *testing.T) {
	t.Parallel()
	order := machine.RecordOf(
		machine.Field{Name: "amount", Type: machine.IntType},
		machine.Field{Name: "currency", Type: machine.StringType},
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
		map[string]any{"order": map[string]any{"amount": 1200, "currency": "SGD"}, "fee": 30},
		machine.RunOptions{Fuel: 10_000})
	if err != nil {
		t.Fatal(err)
	}
	want := machine.RecordOf(
		machine.Field{Name: "net", Type: machine.IntType},
		machine.Field{Name: "currency", Type: machine.StringType},
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
	order := machine.RecordOf(machine.Field{Name: "amount", Type: machine.IntType})
	contract := CompileOptions{Args: []ArgSpec{{Name: "order", Type: order}}}
	if _, err := CompileExpr(`order.total`, consoleRegistry(t), contract); err == nil ||
		!strings.Contains(err.Error(), "no field") {
		t.Fatalf("an undeclared field must fail at compile time: %v", err)
	}
	// The field order is the type: the same names in another order is another type.
	swapped := machine.RecordOf(
		machine.Field{Name: "b", Type: machine.IntType}, machine.Field{Name: "a", Type: machine.IntType})
	if swapped.Equal(machine.RecordOf(
		machine.Field{Name: "a", Type: machine.IntType}, machine.Field{Name: "b", Type: machine.IntType})) {
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
	_, err = runtime.Run(t.Context(), map[string]any{"order": map[string]any{}}, machine.RunOptions{Fuel: 1000})
	if err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("a record without its field must be refused: %v", err)
	}
}

// A record composes with everything else: it can sit in a container, be read
// after a subscript, be compared, and survive the ExprJSON round trip.
func TestRecordComposesWithTheRestOfTheLanguage(t *testing.T) {
	t.Parallel()
	orders := machine.ArrayOf(machine.RecordOf(machine.Field{Name: "amount", Type: machine.IntType}))
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
				map[string]any{"orders": []any{map[string]any{"amount": 10}, map[string]any{"amount": 20}}},
				machine.RunOptions{Fuel: 10_000})
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
	value, err := runtime.Run(t.Context(), args, machine.RunOptions{Fuel: 10_000})
	if err != nil {
		t.Fatalf("%s: %v", source, err)
	}
	if got := fmt.Sprint(value.Any()); got != want {
		t.Fatalf("%s = %s, want %s", source, got, want)
	}
}
