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

// A fold whose step is no kernel instruction — add(float, int) — calls it
// with its arguments in registers of its own: a field read out of a record
// argument before the loop, on either side of the fold, keeps its value.
func TestAFoldLeavesThePromotedFieldsAlone(t *testing.T) {
	t.Parallel()
	registry := aggregateRegistry(t)
	total := func(items []int64) float64 {
		sum := 0.0
		for _, item := range items {
			sum += float64(item)
		}
		return sum
	}
	err := registry.Register(machine.FunctionSpec{Name: "t.total_v1", Go: total, Fold: &machine.Fold{Step: "add", Init: machine.Float(0)}})
	if err != nil {
		t.Fatal(err)
	}
	order := machine.RecordOf(machine.FieldOf("rate", machine.FloatType))
	options := CompileOptions{Args: []ArgSpec{{Name: "xs", Type: machine.ArrayOf(machine.IntType)}, {Name: "order", Type: order}}}
	args := map[string]any{"xs": []any{1, 2, 3}, "order": map[string]any{"rate": 100.0}}
	for _, source := range []string{
		`t.total_v1([x for x in xs]) + order.rate`,
		`order.rate + t.total_v1([x for x in xs])`,
	} {
		fused, plain := compileBoth(t, source, registry, options)
		assertFusedAnswersAsPlain(t, source, fused, plain, args)
		if got, err := fused.Run(t.Context(), args); err != nil || got.Any() != 106.0 {
			t.Errorf("%s = %v, %v; want 106", source, got.Any(), err)
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

// assertFusedAnswersAsPlain holds the fused program to the one as written:
// the answer, and when it fails the class it fails with — which is what a
// fallback around it takes or does not. A fold that stops may answer where
// the array built fails, on an item past the one that decides it; when it
// fails, it fails as the array built does.
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
	case wantErr != nil && gotErr != nil && machine.ClassName(gotErr) != machine.ClassName(wantErr):
		t.Errorf("%s with %v: fused fails with %v; as written %v", source, args, gotErr, wantErr)
	}
}

// A fold's step that fails does not hide what the items after it do: the
// array built computes every item before it folds any, so an item that
// fails past the step that overflows is the failure — and what a fallback
// around the fold takes. However long the loop: the vector hands the item
// it cannot fold to the body.
func TestAFoldFailsAsTheArrayBuiltDoes(t *testing.T) {
	t.Parallel()
	registry := aggregateRegistry(t)
	missing := func(int64) (int64, error) { return 0, errors.New("no such engine") }
	if err := registry.Register(machine.FunctionSpec{Name: "t.missing_v1", Go: missing}); err != nil {
		t.Fatal(err)
	}
	options := CompileOptions{Args: []ArgSpec{{Name: "xs", Type: machine.ArrayOf(machine.IntType)}}}
	const big = "4611686018427387904"
	long := make([]any, 40)
	for i := range long {
		long[i] = 1
	}
	zeroLate, zeroLast := slices.Clone(long), slices.Clone(long)
	zeroLate[20], zeroLast[39] = 0, 0
	for _, test := range []struct {
		source string
		xs     []any
		want   any
		fails  string
	}{
		{`t.sum_v1([` + big + ` / x for x in xs])`, []any{1, 1, 1, 0}, nil, "division by zero"},
		{`t.sum_v1([` + big + ` / x for x in xs])`, zeroLate, nil, "division by zero"},
		{`t.sum_v1([` + big + ` / x for x in xs])`, zeroLast, nil, "division by zero"},
		{`t.sum_v1([` + big + ` / x for x in xs])`, long, nil, "integer overflow"},
		{`sum([` + big + ` / x for x in xs if 8 / x > 0])`, zeroLate, nil, "division by zero"},
		{`fallback(t.sum_v1([if(x < 3, 9223372036854775807, t.missing_v1(x)) for x in xs]), 0)`, []any{1, 2, 3}, int64(0), ""},
		{`fallback(t.sum_v1([if(x < 3, 9223372036854775807, t.missing_v1(x)) for x in xs]), 0)`, []any{1, 2}, nil, "integer overflow"},
		{`t.sum_v1([fallback(t.missing_v1(x), 9223372036854775807) for x in xs])`, []any{1, 2, 3}, nil, "integer overflow"},
		{`[t.sum_v1([` + big + ` / (x + y) for x in xs]) for y in [1, 0]]`, []any{1, 1, 0}, nil, "integer overflow"},
		{`t.sum_v1([t.sum_v1([` + big + ` / (x * y) for x in xs]) for y in xs])`, []any{1, 1, 0}, nil, "division by zero"},
	} {
		fused, plain := compileHoisted(t, test.source, registry, options)
		args := map[string]any{"xs": test.xs}
		got, err := fused.Run(t.Context(), args)
		if test.fails == "" && (err != nil || got.Any() != test.want) || test.fails != "" && (err == nil || !strings.Contains(err.Error(), test.fails)) {
			t.Errorf("%s with %d items = %v, %v; want %v, failing with %q", test.source, len(test.xs), got.Any(), err, test.want, test.fails)
		}
		assertFusedAnswersAsPlain(t, test.source, fused, plain, args)
	}
}

// any, all and first stop where the answer is known whatever the items are
// and however many clauses make them (docs/semantics.md S4): an item past
// the answer is not computed, so does not fail.
func TestAShortCircuitFormStopsWhateverItsItems(t *testing.T) {
	t.Parallel()
	registry := machine.CoreRegistry()
	if err := registry.EnableForm(machine.ForForm); err != nil {
		t.Fatal(err)
	}
	options := CompileOptions{Args: []ArgSpec{{Name: "xs", Type: machine.ArrayOf(machine.IntType)}}}
	for _, test := range []struct {
		source string
		xs     []any
		want   string
		fails  string
	}{
		{`first([[10 / x] for x in xs])`, []any{1, 0}, `[10]`, ""},
		{`first([{v: [10 / x]} for x in xs])`, []any{1, 0}, `{"v":[10]}`, ""},
		{`first([[[string(10 / x)]] for x in xs if x > 0])`, []any{0, 5, 0}, `[["2"]]`, ""},
		{`first([[10 / x] for x in xs])`, []any{0, 1}, "", "division by zero"},
		{`first([[10 / x] for x in xs])`, []any{}, "", "empty"},
		{`first([10 / y for x in xs for y in xs])`, []any{1, 0}, `10`, ""},
		{`first([[x, y] for x in xs if x > 1 for y in xs if 10 / y > 4])`, []any{1, 2, 0}, `[2,1]`, ""},
		{`first([x * y for x in xs for y in xs if y > 5])`, []any{1, 2}, "", "empty"},
		{`first([x + y + z for x in xs for y in xs for z in xs if 10 / z > 0])`, []any{3, 0}, `9`, ""},
		{`any([10 / x > 2 for x in xs for y in [1]])`, []any{1, 0}, `true`, ""},
		{`any([10 / y > 20 for x in xs for y in xs])`, []any{1, 0}, "", "division by zero"},
		{`any([x > y for x in xs for y in xs])`, []any{1, 1}, `false`, ""},
		{`any([x > 5 for x in xs for y in xs])`, []any{}, `false`, ""},
		{`all([10 / y < 2 for x in xs for y in xs])`, []any{1, 0}, `false`, ""},
		{`all([x >= y for x in xs for y in xs])`, []any{2, 2}, `true`, ""},
		{`all([10 / z < 0 for x in xs for y in xs for z in xs if x > y])`, []any{1, 0}, `false`, ""},
		{`all([10 / z > 0 for x in xs for y in xs for z in xs if x > y])`, []any{1, 0}, "", "division by zero"},
	} {
		fused, plain := compileHoisted(t, test.source, registry, options)
		args := map[string]any{"xs": test.xs}
		got, err := fused.Run(t.Context(), args)
		encoded, _ := json.Marshal(got.Any())
		if test.fails == "" && (err != nil || string(encoded) != test.want) || test.fails != "" && (err == nil || !strings.Contains(err.Error(), test.fails)) {
			t.Errorf("%s with %v = %s, %v; want %s, failing with %q", test.source, test.xs, encoded, err, test.want, test.fails)
		}
		// Where the array built answers, it answers the same.
		if want, wantErr := plain.Run(t.Context(), args); wantErr == nil && (err != nil || !got.Equal(want)) {
			t.Errorf("%s with %v: %v, %v; as written %v", test.source, test.xs, got.Any(), err, want.Any())
		}
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

// any, all and first on a comprehension are short-circuit forms, and let is
// strict: binding the comprehension first builds every item, so the items
// after the one that decides the answer are computed — and fail — there.
// That is the language's meaning (docs/semantics.md S4), not an accident of
// the fusion.
func TestAShortCircuitFormIsNotItsLetBinding(t *testing.T) {
	t.Parallel()
	registry := aggregateRegistry(t)
	options := CompileOptions{Args: []ArgSpec{{Name: "xs", Type: machine.ArrayOf(machine.IntType)}}}
	args := map[string]any{"xs": []any{1, 0}}
	for _, pair := range [][2]string{
		{`t.any_v1([10 / x > 2 for x in xs])`, `let(big = [10 / x > 2 for x in xs], t.any_v1(big))`},
		{`t.all_v1([10 / x > 20 for x in xs])`, `let(big = [10 / x > 20 for x in xs], t.all_v1(big))`},
	} {
		short, _ := compileHoisted(t, pair[0], registry, options)
		if _, err := short.Run(t.Context(), args); err != nil {
			t.Errorf("%s with %v fails: %v; it stops at the first item", pair[0], args, err)
		}
		bound, _ := compileHoisted(t, pair[1], registry, options)
		if got, err := bound.Run(t.Context(), args); !errors.Is(err, machine.ErrArithmetic) {
			t.Errorf("%s with %v: %v, %v; want the division by zero let computes", pair[1], args, got.Any(), err)
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

// A first of a comprehension of records fuses too, its seed a record of
// zeros: the record found is the only one built, and the answer and the
// failure are those of the call on the array built.
func TestAFirstOfRecordsBuildsOnlyItsAnswer(t *testing.T) {
	t.Parallel()
	registry := machine.CoreRegistry()
	if err := registry.EnableForm(machine.ForForm); err != nil {
		t.Fatal(err)
	}
	options := CompileOptions{Args: []ArgSpec{{Name: "xs", Type: machine.ArrayOf(machine.IntType)}}}
	source := `first([{n: x, name: string(x), big: x > 2, half: float(x) / 2.0} for x in xs if x > 1])`
	artifact, err := CompileExpr(source, registry, options)
	if err != nil {
		t.Fatal(err)
	}
	if encoded, _ := json.Marshal(artifact); !strings.Contains(string(encoded), "loop_break") {
		t.Fatalf("%s does not stop at its record: %s", source, encoded)
	}
	fused, plain := compileHoisted(t, source, registry, options)
	for _, xs := range [][]any{{}, {1, 2, 3}, {0}, {5}} {
		args := map[string]any{"xs": xs}
		got, gotErr := fused.Run(t.Context(), args)
		want, wantErr := plain.Run(t.Context(), args)
		if (gotErr == nil) != (wantErr == nil) || gotErr == nil && !got.Equal(want) || gotErr != nil && gotErr.Error() != wantErr.Error() {
			t.Errorf("%s with %v: fused %v, %v; as written %v, %v", source, xs, got.Any(), gotErr, want.Any(), wantErr)
		}
	}
}

// A first whose answer has no seed — a handle cannot be a constant — keeps
// the item it finds, and stops there as any other first: the engine is not
// asked for the items after it. With no item it fails as the call on an
// array of none does, its sources read again, whether or not the inner one
// was computed once.
func TestAFirstOfHandlesStopsAtItsItem(t *testing.T) {
	t.Parallel()
	registry := machine.CoreRegistry()
	if err := registry.EnableForm(machine.ForForm); err != nil {
		t.Fatal(err)
	}
	if err := machine.DefineHandle[*tensor](registry, "onnx.tensor"); err != nil {
		t.Fatal(err)
	}
	var asked atomic.Int64
	load := func(x int64) (*tensor, error) {
		asked.Add(1)
		if x == 0 {
			return nil, errors.New("no tensor of 0")
		}
		return &tensor{norm: float64(x)}, nil
	}
	for _, spec := range []machine.FunctionSpec{{Name: "t.load_v1", Go: load}, {Name: "t.norm_v1", Go: func(v *tensor) float64 { return v.norm }}} {
		if err := registry.Register(spec); err != nil {
			t.Fatal(err)
		}
	}
	options := CompileOptions{Args: []ArgSpec{{Name: "xs", Type: machine.ArrayOf(machine.IntType)}}}
	for _, test := range []struct {
		source string
		xs     []any
		want   float64
		asked  int64
		fails  string
	}{
		{`t.norm_v1(first([t.load_v1(x) for x in xs]))`, []any{2, 0, 0}, 2, 1, ""},
		{`t.norm_v1(first([t.load_v1(x) for x in xs if x > 2]))`, []any{2, 3, 0}, 3, 1, ""},
		{`t.norm_v1(first([t.load_v1(x) for x in xs]))`, []any{0, 2}, 0, 1, "no tensor of 0"},
		{`t.norm_v1(first([t.load_v1(x) for x in xs]))`, []any{}, 0, 0, "empty"},
		{`t.norm_v1(first([t.load_v1(x * y) for x in xs for y in [len(xs), 0] if y > 0]))`, []any{2, 0}, 4, 1, ""},
		{`t.norm_v1(first([t.load_v1(x * y) for x in xs for y in [len(xs), 0] if y > 5]))`, []any{2, 0}, 0, 0, "empty"},
	} {
		fused, plain := compileHoisted(t, test.source, registry, options)
		args := map[string]any{"xs": test.xs}
		asked.Store(0)
		got, err := fused.Run(t.Context(), args)
		if test.fails == "" && (err != nil || got.Any() != test.want) || test.fails != "" && (err == nil || !strings.Contains(err.Error(), test.fails)) || asked.Load() != test.asked {
			t.Errorf("%s with %v = %v, %v, the engine asked %d times; want %v, failing with %q, asked %d times",
				test.source, test.xs, got.Any(), err, asked.Load(), test.want, test.fails, test.asked)
		}
		if _, wantErr := plain.Run(t.Context(), args); test.fails != "" && (wantErr == nil || wantErr.Error() != err.Error()) {
			t.Errorf("%s with %v fails with %v; as written %v", test.source, test.xs, err, wantErr)
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
