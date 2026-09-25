package compile

import (
	"encoding/json"
	"errors"
	"math"
	"slices"
	"strings"
	"sync/atomic"
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
		{Name: "t.sum_v1", Go: sum, Fold: &machine.Fold{Step: "add", Init: machine.Int(0)}, Doc: machine.Doc{Constexpr: true}},
		{Name: "t.any_v1", Go: quantifier(true), Fold: &machine.Fold{Init: machine.Bool(false), Stops: true, Stop: true}},
		{Name: "t.all_v1", Go: quantifier(false), Fold: &machine.Fold{Init: machine.Bool(true), Stops: true, Stop: false}},
	} {
		if err := registry.Register(spec); err != nil {
			t.Fatal(err)
		}
	}
	return registry
}

// A call of a fold on a comprehension folds as the loop runs, and answers
// as the call on the built array does. The one difference is the stop: an any or an all no longer computes, or fails on,
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
	got, gotErr := fused.Run(t.Context(), args)
	want, wantErr := plain.Run(t.Context(), args)
	stops := strings.Contains(source, "any") || strings.Contains(source, "all")
	switch {
	case wantErr == nil && (gotErr != nil || !got.Equal(want)):
		t.Errorf("%s with %v: fused %v, %v; as written %v", source, args, got.Any(), gotErr, want.Any())
	case wantErr != nil && gotErr == nil && !stops:
		t.Errorf("%s with %v: fused %v; as written it fails: %v", source, args, got.Any(), wantErr)
	case wantErr != nil && gotErr != nil && !errors.Is(gotErr, machine.ErrArithmetic) && !errors.Is(gotErr, machine.ErrDomain):
		t.Errorf("%s with %v: fused fails with %v; as written %v", source, args, gotErr, wantErr)
	}
}

// A first of a comprehension stops at the first item the comprehension
// yields, which is the answer; with none it fails as the call on the empty
// array does, in the same words. The items after the answer are not
// computed, so do not fail.
func TestAFirstOfAComprehensionStopsAtItsItem(t *testing.T) {
	t.Parallel()
	registry := machine.CoreRegistry()
	if err := registry.EnableForm(machine.ForForm); err != nil {
		t.Fatal(err)
	}
	first := func(items []int64) (int64, error) {
		if len(items) == 0 {
			return 0, errors.New("first of an empty array")
		}
		return items[0], nil
	}
	if err := registry.Register(machine.FunctionSpec{Name: "t.first_v1", Go: first, Fold: &machine.Fold{First: true}}); err != nil {
		t.Fatal(err)
	}
	options := CompileOptions{Args: []ArgSpec{{Name: "xs", Type: machine.ArrayOf(machine.IntType)}}}
	for _, source := range []string{
		`t.first_v1([x * 2 for x in xs if x > 1])`,
		`t.first_v1([x for x in xs])`,
		`t.first_v1([10 / x for x in xs])`,
		`t.first_v1([t.first_v1([y for y in xs if y > x]) for x in xs if x < 3])`,
	} {
		fused, plain := compileHoisted(t, source, registry, options)
		for _, xs := range [][]any{{}, {1, 2, 3}, {3, 0, -1}, {0}, {2, 0}, {1, 1}} {
			args := map[string]any{"xs": xs}
			got, gotErr := fused.Run(t.Context(), args)
			want, wantErr := plain.Run(t.Context(), args)
			switch {
			case wantErr == nil && (gotErr != nil || !got.Equal(want)):
				t.Errorf("%s with %v: fused %v, %v; as written %v", source, xs, got.Any(), gotErr, want.Any())
			case wantErr != nil && gotErr != nil && gotErr.Error() != wantErr.Error():
				t.Errorf("%s with %v: fused fails with %v; as written %v", source, xs, gotErr, wantErr)
			case wantErr != nil && gotErr == nil && !strings.Contains(source, "10 / x"):
				t.Errorf("%s with %v: fused %v; as written it fails: %v", source, xs, got.Any(), wantErr)
			}
		}
	}
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
		{Name: "t.first_v1", Go: func(a []int64) bool { return false }, Fold: &machine.Fold{First: true}},
		{Name: "t.firsts_v1", Go: func(a []int64) int64 { return 0 }, Fold: &machine.Fold{First: true, Init: machine.Int(0)}},
		{Name: "t.both_v1", Go: func(a []int64) int64 { return 0 }, Fold: &machine.Fold{First: true, Counts: true}},
	} {
		if err := machine.CoreRegistry().Register(spec); err == nil {
			t.Errorf("%s registers with its fold %+v", spec.Name, *spec.Fold)
		}
	}
}

// The inner source of a nested comprehension that reads nothing of the
// outer item is computed once: the same arrays, the source computed by the
// first item and read by the rest; one that reads the outer item, or sits
// under a filter, is computed for each item.
func TestAnInvariantInnerSourceIsComputedOnce(t *testing.T) {
	t.Parallel()
	registry := aggregateRegistry(t)
	var calls atomic.Int64
	inner := func(items []int64) []int64 { calls.Add(1); return items }
	if err := registry.Register(machine.FunctionSpec{Name: "t.inner_v1", Go: inner, Doc: machine.Doc{Constexpr: true}}); err != nil {
		t.Fatal(err)
	}
	options := CompileOptions{Args: []ArgSpec{{Name: "xs", Type: machine.ArrayOf(machine.IntType)}}}
	for _, test := range []struct {
		source  string
		hoisted bool
	}{
		{`[x * y for x in xs for y in t.inner_v1([len(xs), 2])]`, true},
		{`[x - y for x in xs for y in t.inner_v1([z * 2 for z in xs])]`, true},
		{`[x * y for x in xs for y in t.inner_v1([x, 2])]`, false},
		{`[x * y for x in xs if x > 1 for y in t.inner_v1([len(xs), 2])]`, false},
	} {
		hoisted, plain := compileHoisted(t, test.source, registry, options)
		for _, xs := range [][]any{{}, {1, 2, 3}, {4}} {
			args := map[string]any{"xs": xs}
			calls.Store(0)
			got, gotErr := hoisted.Run(t.Context(), args)
			hoistedCalls := calls.Swap(0)
			want, wantErr := plain.Run(t.Context(), args)
			plainCalls := calls.Load()
			if gotErr != nil || wantErr != nil || !got.Equal(want) {
				t.Errorf("%s with %v: hoisted %v, %v; as written %v, %v", test.source, xs, got.Any(), gotErr, want.Any(), wantErr)
			}
			wantCalls := plainCalls
			if test.hoisted {
				wantCalls = min(plainCalls, 1)
			}
			if hoistedCalls != wantCalls {
				t.Errorf("%s with %v: the inner source computed %d times, as written %d; want %d", test.source, xs, hoistedCalls, plainCalls, wantCalls)
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
