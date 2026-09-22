package compile

import (
	"context"
	"fmt"
	"funroute/lang/internal/machine"
	"testing"
)

func foldRegistry(t *testing.T) *machine.Registry {
	t.Helper()
	registry := machine.CoreRegistry()
	if err := registry.EnableForm(machine.SwitchForm, machine.ForForm, machine.ReduceForm); err != nil {
		t.Fatal(err)
	}
	// An extension that always fails, to show that a failing extension is not
	// a compile error the way a failing kernel expression is.
	err := machine.Logic(registry, "fold.boom_v1", machine.Doc{
		Label: "总是失败", Category: "演示", Cost: 1, Params: []string{"值"}, Result: "值",
	}, func(value int64) (int64, error) { return 0, fmt.Errorf("boom") })
	if err != nil {
		t.Fatal(err)
	}
	return registry
}

func compileFolded(t *testing.T, registry *machine.Registry, source string, contract ...ArgSpec) *machine.Artifact {
	t.Helper()
	artifact, err := CompileExpr(source, registry, CompileOptions{Args: contract})
	if err != nil {
		t.Fatal(err)
	}
	return artifact
}

// A closed expression is computed at compile time, so the bytecode carries the
// answer instead of the work.
func TestClosedExpressionsAreFoldedAway(t *testing.T) {
	registry := foldRegistry(t)
	for _, test := range []struct {
		source string
		want   int64
	}{
		{"1000 * 60 * 60 * 24", 86400000},
		{"(10 + 5) * 2 - 6 / 3", 28},
		{"reduce(x in [1,2,3,4], total = 0, total + x)", 10},
	} {
		artifact := compileFolded(t, registry, test.source)
		if len(artifact.Instructions) != 1 || artifact.Instructions[0].Op != machine.OpConstant {
			t.Fatalf("%s compiled to %d instructions", test.source, len(artifact.Instructions))
		}
		if len(artifact.Calls) != 0 {
			t.Fatalf("%s kept %d calls", test.source, len(artifact.Calls))
		}
		runtime, err := machine.Instantiate(artifact, registry)
		if err != nil {
			t.Fatal(err)
		}
		// Fuel of 1 proves the work is gone: the calls would have cost more.
		result, err := runtime.Run(context.Background(), map[string]any{}, machine.RunOptions{Fuel: 1})
		if err != nil {
			t.Fatalf("%s: %v", test.source, err)
		}
		if value, _ := result.Int(); value != test.want {
			t.Fatalf("%s = %d, want %d", test.source, value, test.want)
		}
	}
}

// Folding is transitive through header bindings, and a binding that folded
// needs no local slot: nothing is stored or loaded at run time.
func TestConstantBindingsUseNoLocalSlots(t *testing.T) {
	registry := foldRegistry(t)
	artifact := compileFolded(t, registry, `let(
  bps       = 250,
  base_fee  = 100 * 3 + 50,
  total_bps = bps * 2,
  amount * total_bps / 10000 + base_fee
)`, ArgSpec{Name: "amount", Type: machine.IntType})
	if artifact.Locals != 0 {
		t.Fatalf("locals = %d, want 0", artifact.Locals)
	}
	for _, instruction := range artifact.Instructions {
		if instruction.Op == machine.OpStoreLocal || instruction.Op == machine.OpLoadLocal {
			t.Fatalf("a folded binding still uses a local slot: %v", artifact.Instructions)
		}
	}
	runtime, err := machine.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	result, err := runtime.Run(context.Background(), map[string]any{"amount": 100000}, machine.RunOptions{Fuel: 100})
	if err != nil {
		t.Fatal(err)
	}
	if value, _ := result.Int(); value != 5350 {
		t.Fatalf("result = %d, want 5350", value)
	}
}

// A binding that reads an argument cannot be folded, so it keeps its slot and
// is evaluated once at run time.
func TestRuntimeBindingKeepsItsSlot(t *testing.T) {
	registry := foldRegistry(t)
	artifact := compileFolded(t, registry, `let(fee = amount / 100, amount + fee)`,
		ArgSpec{Name: "amount", Type: machine.IntType})
	if artifact.Locals != 1 {
		t.Fatalf("locals = %d, want 1", artifact.Locals)
	}
}

// A branch that is not taken is not evaluated: the failing division reads an
// argument, so nothing about it is settled at compile time.
func TestFoldingRespectsLaziness(t *testing.T) {
	registry := foldRegistry(t)
	artifact := compileFolded(t, registry, "if(use_bad, 1 / zero, 42)",
		ArgSpec{Name: "use_bad", Type: machine.BoolType}, ArgSpec{Name: "zero", Type: machine.IntType})
	runtime, err := machine.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	result, err := runtime.Run(context.Background(), map[string]any{"use_bad": false, "zero": 0}, machine.RunOptions{Fuel: 100})
	if err != nil {
		t.Fatalf("a failing branch that is not taken broke the program: %v", err)
	}
	if value, _ := result.Int(); value != 42 {
		t.Fatalf("result = %d", value)
	}
	// Taking the branch is still a run-time error, exactly as before folding.
	if _, err := runtime.Run(context.Background(), map[string]any{"use_bad": true}, machine.RunOptions{Fuel: 100}); err == nil {
		t.Fatal("division by zero was silently folded away")
	}
}

// Containers have no constant form, so a closed array or dictionary falls back
// to being built at run time rather than failing to compile.
func TestClosedContainersAreInterned(t *testing.T) {
	registry := foldRegistry(t)
	// A list, a dictionary and a record that read no argument are built once,
	// while the rule compiles, and the bytecode is a single load.
	for _, source := range []string{`[1, 2, 3]`, `{"a": 1, "b": 2}`, `{amount: 1, currency: "SGD"}`} {
		artifact := compileFolded(t, registry, source)
		if len(artifact.Instructions) != 1 {
			t.Fatalf("%s compiled to %d instructions, want one load", source, len(artifact.Instructions))
		}
		runtime, err := machine.Instantiate(artifact, registry)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := runtime.Run(context.Background(), map[string]any{}, machine.RunOptions{Fuel: 100}); err != nil {
			t.Fatalf("run %s: %v", source, err)
		}
	}
	// A container that reads an argument is still built at run time.
	built := compileFolded(t, registry, `[1, n]`, ArgSpec{Name: "n", Type: machine.IntType})
	if len(built.Instructions) == 1 {
		t.Fatal("an array that reads an argument cannot be a constant")
	}
}

// Folding changes the bytecode, so it changes the digest — but never the value.
func TestFoldingPreservesResults(t *testing.T) {
	registry := foldRegistry(t)
	for _, test := range []struct {
		source   string
		contract []ArgSpec
		args     map[string]any
		want     any
	}{
		{"n * (2 + 3)", []ArgSpec{{Name: "n", Type: machine.IntType}}, map[string]any{"n": 7}, int64(35)},
		{"n > 10 * 10", []ArgSpec{{Name: "n", Type: machine.IntType}}, map[string]any{"n": 99}, false},
		{
			"reduce(x in xs, t = 100 - 1, t + x)",
			[]ArgSpec{{Name: "xs", Type: machine.ArrayOf(machine.IntType)}},
			map[string]any{"xs": []any{1, 2}}, int64(102),
		},
		{
			`switch(s, case "a" => "x", else "y")`,
			[]ArgSpec{{Name: "s", Type: machine.StringType}},
			map[string]any{"s": "a"}, "x",
		},
	} {
		artifact := compileFolded(t, registry, test.source, test.contract...)
		runtime, err := machine.Instantiate(artifact, registry)
		if err != nil {
			t.Fatal(err)
		}
		result, err := runtime.Run(context.Background(), test.args, machine.RunOptions{Fuel: 1000})
		if err != nil {
			t.Fatalf("%s: %v", test.source, err)
		}
		if result.Any() != test.want {
			t.Fatalf("%s = %#v, want %#v", test.source, result.Any(), test.want)
		}
	}
}

// Anything that reads no argument is settled while the rule is compiled —
// including inside a branch that happens not to be taken, the way a constant
// division by zero is an error in Go even under `if false`.
func TestClosedFailuresAreCompileErrors(t *testing.T) {
	registry := foldRegistry(t)
	for _, source := range []string{
		`1 / 0`,
		`let(x = 10 / 0, x)`,
		`if(use_bad, 1 / 0, 42)`,
		`switch(case use_bad => 1 / 0, else 2)`,
		`fallback(1 / 0, 7)`,
		`[1, 2][5]`,
		`9223372036854775807 + 1`,
	} {
		_, err := CompileExpr(source, registry, CompileOptions{
			Args: []ArgSpec{{Name: "use_bad", Type: machine.BoolType}},
		})
		if err == nil {
			t.Fatalf("%s must fail to compile", source)
		}
	}
	// An extension can fail for reasons that are not in the program, so a
	// failing one is left as work rather than reported.
	if _, err := CompileExpr(`fold.boom_v1(1)`, registry, CompileOptions{}); err != nil {
		t.Fatalf("a failing extension must not become a compile error: %v", err)
	}
}
