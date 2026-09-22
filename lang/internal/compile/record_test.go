package compile

import (
	"context"
	"strings"
	"testing"

	"funroute/lang/internal/machine"
	"funroute/lang/internal/syntax"
)

// A record is what a host already has: one object with named fields of
// different types. The contract declares it, the rule reads fields off it and
// can build one back.
func TestRecordFieldsAreReadAndBuilt(t *testing.T) {
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
	value, err := runtime.Run(context.Background(),
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
		t.Fatalf("value = %v", value.Any())
	}
}

func TestRecordRejectsWhatIsNotThatRecord(t *testing.T) {
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
	_, err = runtime.Run(context.Background(), map[string]any{"order": map[string]any{}}, machine.RunOptions{Fuel: 1000})
	if err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("a record without its field must be refused: %v", err)
	}
}

// A record composes with everything else: it can sit in a container, be read
// after a subscript, be compared, and survive the ExprJSON round trip.
func TestRecordComposesWithTheRestOfTheLanguage(t *testing.T) {
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
		artifact, err := CompileExpr(test.source, consoleRegistry(t), CompileOptions{Args: contract})
		if err != nil {
			t.Fatalf("compile %s: %v", test.source, err)
		}
		runtime, err := machine.Instantiate(artifact, consoleRegistry(t))
		if err != nil {
			t.Fatal(err)
		}
		value, err := runtime.Run(context.Background(),
			map[string]any{"orders": []any{map[string]any{"amount": 10}, map[string]any{"amount": 20}}},
			machine.RunOptions{Fuel: 10_000})
		if err != nil {
			t.Fatalf("run %s: %v", test.source, err)
		}
		if value.Any() != test.want {
			t.Fatalf("%s = %v, want %v", test.source, value.Any(), test.want)
		}
	}

	// Two ways to write the same field read produce the same tree.
	dotted, err := syntax.Parse(`orders[0].amount`)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := syntax.ExportExprJSON(dotted)
	if err != nil {
		t.Fatal(err)
	}
	imported, err := syntax.ImportExprJSON(encoded)
	if err != nil {
		t.Fatal(err)
	}
	again, err := syntax.ExportExprJSON(imported)
	if err != nil || string(again) != string(encoded) {
		t.Fatalf("round trip changed the tree:\n%s\n%s", encoded, again)
	}
}
