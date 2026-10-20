package compile

import (
	"strings"
	"testing"

	"github.com/nethinwei/funroute/internal/machine"
	"github.com/nethinwei/funroute/internal/syntax"
)

func TestReduceFoldsArrayWithLocalAccumulator(t *testing.T) {
	t.Parallel()
	value, runtime := compileAndRun(t,
		`reduce(price in prices, total = 0, add(total,price))`,
		consoleRegistry(t), map[string]any{"prices": []any{10, 20, 30}})
	params := runtime.Args()
	if len(params) != 1 || params[0].Name() != "prices" || !params[0].Type().Equal(machine.ArrayOf(machine.IntType)) {
		t.Fatalf("args = %#v, want prices: array<int>", params)
	}
	if !runtime.ResultType().Equal(machine.IntType) {
		t.Fatalf("result type = %s, want int", runtime.ResultType())
	}
	if got, _ := value.Int(); got != 60 {
		t.Fatalf("value = %v, want 60", value.Any())
	}

	// An empty source yields the initial accumulator without running the body.
	empty, _ := compileAndRun(t,
		`reduce(price in prices, total = 7, add(total,price))`,
		consoleRegistry(t), map[string]any{"prices": []any{}})
	if got, _ := empty.Int(); got != 7 {
		t.Fatalf("empty reduce = %v, want 7", empty.Any())
	}
}

// TestReduceSkipsFilteredItems is the "if" clause the fold shares with the
// comprehension: a rejected item is not folded, so the accumulator keeps the
// value the previous step left.
func TestReduceSkipsFilteredItems(t *testing.T) {
	t.Parallel()
	value, _ := compileAndRun(t,
		`reduce(price in prices if price >= 1000, total = 0, total + price)`,
		consoleRegistry(t), map[string]any{"prices": []any{100, 2500, 900, 4000}})
	if got, _ := value.Int(); got != 6500 {
		t.Fatalf("filtered fold = %v, want 6500", value.Any())
	}

	// The condition sees the loop variables but not the accumulator, which is
	// what makes filtering happen before folding rather than inside it.
	contract := CompileOptions{Args: []ArgSpec{{Name: "prices", Type: machine.ArrayOf(machine.IntType)}}}
	_, err := CompileExpr(`reduce(price in prices if total > 0, total = 0, total + price)`, consoleRegistry(t), contract)
	if err == nil || !strings.Contains(err.Error(), "total") {
		t.Fatalf("the filter must not see the accumulator: %v", err)
	}

	// A dictionary walk filters on either loop variable.
	dictValue, _ := compileAndRun(t,
		`reduce(name, weight in weights if weight > 0.1, total = 0.0, total + weight)`,
		consoleRegistry(t), map[string]any{"weights": map[string]any{"a": 0.5, "b": 0.05, "c": 0.2}})
	if got, _ := dictValue.Float(); got < 0.69 || got > 0.71 {
		t.Fatalf("filtered dictionary fold = %v, want 0.7", dictValue.Any())
	}
}

// A name the condition reads that the accumulator shares is the argument of
// that name, as inference typed it: the accumulator is the body's alone. The
// answer is the argument's, and a program whose argument is of another type
// than the accumulator compiles to an artifact that loads.
func TestReduceConditionReadsTheArgumentTheAccumulatorShadows(t *testing.T) {
	t.Parallel()
	registry := consoleRegistry(t)
	for name, test := range map[string]struct {
		source string
		acc    machine.Type
		args   []machine.Value
	}{
		"an int":  {"reduce(x in xs if acc > 1000, acc = 100, acc + x)", machine.IntType, []machine.Value{machine.Int(5000)}},
		"a bool":  {"reduce(x in xs if acc, acc = 100, acc + x)", machine.BoolType, []machine.Value{machine.Bool(true)}},
		"a let's": {"let(acc = 5000, reduce(x in xs if acc > 1000, acc = 100, acc + x))", machine.IntType, nil},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			args := []ArgSpec{{Name: "xs", Type: machine.ArrayOf(machine.IntType)}}
			if test.args != nil {
				args = append([]ArgSpec{{Name: "acc", Type: test.acc}}, args...)
			}
			artifact, err := CompileExpr(test.source, registry, CompileOptions{Args: args})
			if err != nil {
				t.Fatal(err)
			}
			runtime, err := machine.Instantiate(artifact, registry)
			if err != nil {
				t.Fatalf("Instantiate(%s) = %v, want the artifact its compile made to load", test.source, err)
			}
			xs, _ := machine.ToValue([]int64{1, 2})
			value, err := runtime.RunValues(t.Context(), append(test.args, xs))
			if got, _ := value.Int(); err != nil || got != 103 {
				t.Fatalf("%s = %v, %v, want 103", test.source, value.Any(), err)
			}
		})
	}
}

func TestReduceRejectsBodyThatChangesAccumulatorType(t *testing.T) {
	t.Parallel()
	registry := consoleRegistry(t)
	_, err := CompileExpr(`reduce(price in prices, total = 0, string(total))`, registry, CompileOptions{})
	if err == nil || !strings.Contains(err.Error(), "accumulator") {
		t.Fatalf("CompileExpr(string(total) as the body) error = %v, want an accumulator error", err)
	}
	if _, err := CompileExpr(`reduce(price in prices, price = 0, price)`, registry, CompileOptions{}); err == nil {
		t.Fatal("reduce accepted the same name for item and accumulator")
	}
}

func TestReduceAndComprehensionSurviveExprJSONRoundTrip(t *testing.T) {
	t.Parallel()
	for _, source := range []string{
		`reduce(price in prices, total = 0, add(total,price))`,
		`reduce(price in prices if gt(price,minimum), total = 0, add(total,price))`,
		`[channel for channel in channels if eq(channel,"UP")]`,
		`[add(x,1) for x in [mul(y,2) for y in items]]`,
	} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			assertExprJSONIsCanonical(t, source)
		})
	}
}

// assertExprJSONIsCanonical parses source, and checks that exporting,
// importing and exporting again gives the same ExprJSON. It returns it.
func assertExprJSONIsCanonical(t *testing.T, source string) []byte {
	t.Helper()
	expr, err := syntax.Parse(source)
	if err != nil {
		t.Fatalf("parse %s: %v", source, err)
	}
	first, err := syntax.ExportExprJSON(expr)
	if err != nil {
		t.Fatal(err)
	}
	imported, err := syntax.ImportExprJSON(first)
	if err != nil {
		t.Fatalf("import %s: %v", source, err)
	}
	second, err := syntax.ExportExprJSON(imported)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatalf("%s is not canonical:\n%s\n%s", source, first, second)
	}
	return first
}
