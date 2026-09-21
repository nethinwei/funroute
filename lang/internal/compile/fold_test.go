package compile

import (
	"context"
	"funroute/lang/internal/machine"
	"testing"
)

func foldRegistry(t *testing.T) *machine.Registry {
	t.Helper()
	registry := machine.CoreRegistry()
	if err := registry.EnableForm(machine.SwitchForm, machine.ForForm, machine.ReduceForm); err != nil {
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
		{"reduce(x in [1,2,3,4], total from 0, total + x)", 10},
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

// Folding must not turn a branch that never runs into a compile error: `if` is
// lazy, so a failing closed branch is left as work.
func TestFoldingRespectsLaziness(t *testing.T) {
	registry := foldRegistry(t)
	artifact := compileFolded(t, registry, "if(use_bad, 1 / 0, 42)", ArgSpec{Name: "use_bad", Type: machine.BoolType})
	runtime, err := machine.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	result, err := runtime.Run(context.Background(), map[string]any{"use_bad": false}, machine.RunOptions{Fuel: 100})
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
func TestContainersFallBackToBeingBuilt(t *testing.T) {
	registry := foldRegistry(t)
	artifact := compileFolded(t, registry, "[1, 2, 3]")
	if len(artifact.Instructions) == 1 {
		t.Fatal("an array was interned as a constant; the pool cannot hold one")
	}
	runtime, err := machine.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	result, err := runtime.Run(context.Background(), map[string]any{}, machine.RunOptions{Fuel: 100})
	if err != nil {
		t.Fatal(err)
	}
	if items, _ := result.Array(); len(items) != 3 {
		t.Fatalf("result = %#v", result.Any())
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
			"reduce(x in xs, t from 100 - 1, t + x)",
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
