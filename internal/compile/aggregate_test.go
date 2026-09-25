package compile

import (
	"encoding/json"
	"errors"
	"math"
	"slices"
	"strings"
	"testing"

	"github.com/nethinwei/funroute/internal/machine"
)

// aggregateRegistry is the language with three folds of its own: a sum that
// steps with add, an any and an all that stop — and the kernel's len, which
// counts.
func aggregateRegistry(t *testing.T) *machine.Registry {
	t.Helper()
	registry := machine.CoreRegistry()
	if err := registry.EnableForm(machine.ForForm, machine.ReduceForm); err != nil {
		t.Fatal(err)
	}
	sum := func(items []int64) (int64, error) {
		total := int64(0)
		for _, item := range items {
			if item > 0 && total > math.MaxInt64-item || item < 0 && total < math.MinInt64-item {
				return 0, machine.ErrArithmetic
			}
			total += item
		}
		return total, nil
	}
	quantifier := func(stop bool) func([]bool) bool {
		return func(items []bool) bool { return slices.Contains(items, stop) == stop }
	}
	for _, spec := range []machine.FunctionSpec{
		{Name: "t.sum_v1", Go: sum, Fold: &machine.Fold{Step: "add", Init: machine.Int(0)}, Doc: machine.Doc{Cost: 4, Constexpr: true}},
		{Name: "t.any_v1", Go: quantifier(true), Fold: &machine.Fold{Init: machine.Bool(false), Stops: true, Stop: true}, Doc: machine.Doc{Cost: 3}},
		{Name: "t.all_v1", Go: quantifier(false), Fold: &machine.Fold{Init: machine.Bool(true), Stops: true, Stop: false}, Doc: machine.Doc{Cost: 3}},
	} {
		if err := registry.Register(spec); err != nil {
			t.Fatal(err)
		}
	}
	return registry
}

// A call of a fold on a comprehension folds as the loop runs, and answers
// as the call on the built array does, with no more fuel to finish. The one
// difference is the stop: an any or an all no longer computes, or fails on,
// the items after the one that decides it.
func TestAFoldOfAComprehensionAnswersAsTheCall(t *testing.T) {
	t.Parallel()
	registry := aggregateRegistry(t)
	options := CompileOptions{Args: []ArgSpec{{Name: "xs", Type: machine.ArrayOf(machine.IntType)}}}
	sources := []string{
		`t.sum_v1([x * 2 for x in xs if x > 1])`,
		`t.sum_v1([x for x in xs])`,
		`t.sum_v1([9223372036854775807 + x for x in xs])`,
		`len([x for x in xs if x % 2 == 0])`,
		`len([7 for x in xs])`,
		`t.any_v1([x > 2 for x in xs])`,
		`t.all_v1([x > 0 for x in xs])`,
		`t.any_v1([10 / x > 2 for x in xs])`,
		`t.all_v1([10 / x > 2 for x in xs])`,
		`t.sum_v1([t.sum_v1([y for y in xs if y < x]) for x in xs])`,
	}
	inputs := [][]any{{}, {1, 2, 3}, {3, 0, -1}, {0}, {5, 5, 1}, {2, 2}}
	for _, source := range sources {
		fused, plain := compileBoth(t, source, registry, options)
		for _, xs := range inputs {
			assertFusedAnswersAsPlain(t, source, fused, plain, map[string]any{"xs": xs})
		}
	}
}

// compileBoth is the program fused and as written; the fused one calls no
// fold on an array.
func compileBoth(t *testing.T, source string, registry *machine.Registry, options CompileOptions) (fused, plain *machine.Runtime) {
	t.Helper()
	var encoded []byte
	load := func(options CompileOptions) *machine.Runtime {
		artifact, err := CompileExpr(source, registry, options)
		if err != nil {
			t.Fatalf("%s: %v", source, err)
		}
		runtime, err := machine.Instantiate(artifact, registry)
		if err != nil {
			t.Fatal(err)
		}
		encoded, _ = json.Marshal(artifact)
		return runtime
	}
	options.plain = true
	plain = load(options)
	options.plain = false
	fused = load(options)
	var called struct {
		Calls []machine.CallReference `json:"calls"`
	}
	if err := json.Unmarshal(encoded, &called); err != nil {
		t.Fatal(err)
	}
	for _, call := range called.Calls {
		if call.Name != "add" && call.Name != "mul" && call.Name != "gt" && call.Name != "lt" && call.Name != "mod" && call.Name != "div" && call.Name != "eq" {
			t.Errorf("%s still calls %s", source, call.Signature)
		}
	}
	return fused, plain
}

func assertFusedAnswersAsPlain(t *testing.T, source string, fused, plain *machine.Runtime, args map[string]any) {
	t.Helper()
	got, gotErr := fused.Run(t.Context(), args, machine.RunOptions{})
	want, wantErr := plain.Run(t.Context(), args, machine.RunOptions{})
	stops := strings.Contains(source, "any") || strings.Contains(source, "all")
	switch {
	case wantErr == nil && (gotErr != nil || !got.Equal(want)):
		t.Errorf("%s with %v: fused %v, %v; as written %v", source, args, got.Any(), gotErr, want.Any())
	case wantErr != nil && gotErr == nil && !stops:
		t.Errorf("%s with %v: fused %v; as written it fails: %v", source, args, got.Any(), wantErr)
	case wantErr != nil && gotErr != nil && !errors.Is(gotErr, machine.ErrArithmetic) && !errors.Is(gotErr, machine.ErrDomain):
		t.Errorf("%s with %v: fused fails with %v; as written %v", source, args, gotErr, wantErr)
	}
	// A run that finishes needs no more fuel fused; one that fails may need
	// the one instruction more that seeds the fold before it fails.
	if gotFuel, wantFuel := fuelToAnswer(t, fused, args), fuelToAnswer(t, plain, args); wantErr == nil && gotFuel > wantFuel {
		t.Errorf("%s with %v: fused needs %d fuel, as written %d", source, args, gotFuel, wantFuel)
	}
}

// fuelToAnswer is the least fuel a run needs to end other than out of fuel.
func fuelToAnswer(t *testing.T, runtime *machine.Runtime, args map[string]any) uint64 {
	t.Helper()
	low, high := uint64(1), uint64(1<<20)
	for low < high {
		middle := low + (high-low)/2
		if _, err := runtime.Run(t.Context(), args, machine.RunOptions{Fuel: middle}); errors.Is(err, machine.ErrFuel) {
			low = middle + 1
		} else {
			high = middle
		}
	}
	return low
}

// A fold must fold for the function it is declared on: one array in, a step
// the kernel has, a stop only of bools, a count of an int.
func TestAFoldIsHeldToItsFunction(t *testing.T) {
	t.Parallel()
	for _, spec := range []machine.FunctionSpec{
		{Name: "t.two_v1", Go: func(a, b []int64) int64 { return 0 }, Fold: &machine.Fold{Step: "add", Init: machine.Int(0)}},
		{Name: "t.step_v1", Go: func(a []int64) int64 { return 0 }, Fold: &machine.Fold{Step: "nope", Init: machine.Int(0)}},
		{Name: "t.init_v1", Go: func(a []int64) int64 { return 0 }, Fold: &machine.Fold{Step: "add", Init: machine.Bool(false)}},
		{Name: "t.stop_v1", Go: func(a []int64) int64 { return 0 }, Fold: &machine.Fold{Init: machine.Int(0), Stops: true}},
		{Name: "t.count_v1", Go: func(a []int64) bool { return false }, Fold: &machine.Fold{Counts: true}},
		{Name: "t.none_v1", Go: func(a []int64) int64 { return 0 }, Fold: &machine.Fold{Init: machine.Int(0)}},
	} {
		if err := machine.CoreRegistry().Register(spec); err == nil {
			t.Errorf("%s registers with its fold %+v", spec.Name, *spec.Fold)
		}
	}
}

// The inner source of a nested comprehension that reads nothing of the
// outer item is computed once: the same arrays, in less fuel from the
// second item on — the first pays two instructions to keep it; one that
// reads the outer item, or sits under a filter, is computed for each item.
func TestAnInvariantInnerSourceIsComputedOnce(t *testing.T) {
	t.Parallel()
	registry := aggregateRegistry(t)
	options := CompileOptions{Args: []ArgSpec{{Name: "xs", Type: machine.ArrayOf(machine.IntType)}}}
	for _, test := range []struct {
		source  string
		hoisted bool
	}{
		{`[x * y for x in xs for y in [len(xs), 2]]`, true},
		{`[x - y for x in xs for y in [z * 2 for z in xs]]`, true},
		{`[x * y for x in xs for y in [x, 2]]`, false},
		{`[x * y for x in xs if x > 1 for y in [len(xs), 2]]`, false},
	} {
		hoisted, plain := compileHoisted(t, test.source, registry, options)
		for _, xs := range [][]any{{}, {1, 2, 3}, {4}} {
			args := map[string]any{"xs": xs}
			got, gotErr := hoisted.Run(t.Context(), args, machine.RunOptions{})
			want, wantErr := plain.Run(t.Context(), args, machine.RunOptions{})
			if gotErr != nil || wantErr != nil || !got.Equal(want) {
				t.Errorf("%s with %v: hoisted %v, %v; as written %v, %v", test.source, xs, got.Any(), gotErr, want.Any(), wantErr)
			}
			gotFuel, wantFuel := fuelToAnswer(t, hoisted, args), fuelToAnswer(t, plain, args)
			keep := uint64(0)
			if test.hoisted && len(xs) == 1 {
				keep = 2
			}
			if cheaper := gotFuel < wantFuel; gotFuel > wantFuel+keep || cheaper != (test.hoisted && len(xs) > 1) {
				t.Errorf("%s with %v: hoisted needs %d fuel, as written %d", test.source, xs, gotFuel, wantFuel)
			}
		}
	}
}

func compileHoisted(t *testing.T, source string, registry *machine.Registry, options CompileOptions) (hoisted, plain *machine.Runtime) {
	t.Helper()
	load := func(options CompileOptions) *machine.Runtime {
		artifact, err := CompileExpr(source, registry, options)
		if err != nil {
			t.Fatalf("%s: %v", source, err)
		}
		runtime, err := machine.Instantiate(artifact, registry)
		if err != nil {
			t.Fatal(err)
		}
		return runtime
	}
	hoisted = load(options)
	options.plain = true
	return hoisted, load(options)
}
