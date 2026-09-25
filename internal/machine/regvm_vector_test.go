package machine_test

import (
	"fmt"
	"math"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"

	"github.com/nethinwei/funroute/internal/compile"
	"github.com/nethinwei/funroute/internal/machine"
)

// The shapes most comprehensions and aggregates take are run by the vector.
func TestTheVectorRunsTheCommonShapes(t *testing.T) {
	t.Parallel()
	registry := aggregates(t)
	for _, source := range vectorShapes {
		runtime := loadVector(t, source, registry)
		if machine.Vectors(runtime) == 0 {
			t.Errorf("%s is not run by the vector:\n%s", source, machine.RegisterForm(runtime))
		}
	}
}

var vectorShapes = []string{
	`[x + 1 for x in xs]`,
	`[x * 2 for x in xs if x > 3]`,
	`reduce(x in xs, t = 0, t + x)`,
	`reduce(x in xs, t = 1, t * x)`,
	`len([x for x in xs if x % 3 == 0])`,
	`sum([x * 2 for x in xs if x % 3 == 0])`,
	`any([10 / x > 2 for x in xs])`,
	`all([x < 100 for x in xs])`,
	`[x > a for x in xs]`,
	`sum([float(x) for x in xs])`,
	`[f * 1.5 for f in fs if f < 2.5]`,
	`reduce(f in fs, t = 0.0, t + f / 3.0)`,
}

// aggregates is the random registry with the standard aggregates' folds.
func aggregates(t *testing.T) *machine.Registry {
	t.Helper()
	registry := randomRegistry(t)
	sum := func(xs []int64) int64 {
		total := int64(0)
		for _, x := range xs {
			total += x
		}
		return total
	}
	sumFloats := func(xs []float64) float64 {
		total := 0.0
		for _, x := range xs {
			total += x
		}
		return total
	}
	quantifier := func(stop bool) func([]bool) bool {
		return func(items []bool) bool { return slices.Contains(items, stop) == stop }
	}
	zero := machine.Float(0)
	for _, spec := range []machine.FunctionSpec{
		{Name: "sum", Go: sum, Fold: &machine.Fold{Step: "add", Init: machine.Int(0)}},
		{Name: "sum", Go: sumFloats, Fold: &machine.Fold{Step: "add", Init: zero}},
		{Name: "any", Go: quantifier(true), Fold: &machine.Fold{Init: machine.Bool(false), Stops: true, Stop: true}},
		{Name: "all", Go: quantifier(false), Fold: &machine.Fold{Init: machine.Bool(true), Stops: true, Stop: false}},
	} {
		if err := registry.Register(spec); err != nil {
			t.Fatal(err)
		}
	}
	return registry
}

func loadVector(t *testing.T, source string, registry *machine.Registry) *machine.Runtime {
	t.Helper()
	artifact, err := compile.CompileExpr(source, registry, randomContract)
	if err != nil {
		t.Fatalf("%s: %v", source, err)
	}
	runtime, err := machine.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	return runtime
}

// The vector answers as the body does: the value, the failure, and the
// fuel to the unit — for items that overflow, divide by zero or stop the
// loop anywhere in a block of columns, and for fuel that runs out at any
// item.
func TestTheVectorAnswersAsTheBody(t *testing.T) {
	t.Parallel()
	registry := aggregates(t)
	for _, source := range vectorShapes {
		vector, body := loadVector(t, source, registry), loadVector(t, source, registry)
		machine.WithoutVectors(body)
		for _, args := range vectorInputs() {
			assertVectorAsBody(t, source, vector, body, args)
		}
	}
}

// vectorInputs are arrays around the column's length, with the values that
// fail — zero, the extremes of int — and the one that stops all, at the
// edges of a block.
func vectorInputs() []map[string]any {
	var inputs []map[string]any
	rng := rand.New(rand.NewPCG(1, 2))
	for _, n := range []int{0, 1, 5, 255, 256, 257, 600} {
		for _, at := range []int{-1, 0, n / 2, n - 1, 255, 256} {
			for _, special := range []int64{0, 7, math.MaxInt64, math.MinInt64} {
				inputs = append(inputs, vectorInput(rng, n, at, special))
			}
		}
	}
	return inputs
}

// vectorInput is n random items with special at at, when at is one of them.
func vectorInput(rng *rand.Rand, n, at int, special int64) map[string]any {
	xs, fs := make([]any, n), make([]any, n)
	for i := range xs {
		xs[i], fs[i] = int64(rng.IntN(20)+1), float64(rng.IntN(8))/2
	}
	if at >= 0 && at < n {
		xs[at] = special
	}
	return map[string]any{"a": int64(4), "b": int64(1), "f": 0.5, "s": "", "flag": true, "xs": xs, "fs": fs}
}

func assertVectorAsBody(t *testing.T, source string, vector, body *machine.Runtime, args map[string]any) {
	t.Helper()
	got, want := outcome(t, vector, args, 1<<24), outcome(t, body, args, 1<<24)
	if got != want {
		t.Fatalf("%s with %s: the vector gives %s, the body %s", source, describe(args), got, want)
	}
	if got, want := fuelNeeded(t, vector, args), fuelNeeded(t, body, args); got != want {
		t.Fatalf("%s with %s: the vector needs %d fuel, the body %d", source, describe(args), got, want)
	}
	// Every budget below what it needs fails as the body does.
	for _, fuel := range []uint64{1, 7, 100, 1000, 1001} {
		if got, want := outcome(t, vector, args, fuel), outcome(t, body, args, fuel); got != want {
			t.Fatalf("%s with %s and %d fuel: the vector gives %s, the body %s", source, describe(args), fuel, got, want)
		}
	}
}

func describe(args map[string]any) string {
	xs := fmt.Sprint(args["xs"])
	if len(xs) > 80 {
		xs = xs[:80] + "…"
	}
	return strings.ReplaceAll(xs, "\n", " ")
}

// Random programs answer as they do without the vector, to the fuel.
func TestTheVectorAnswersRandomProgramsAsTheBody(t *testing.T) {
	t.Parallel()
	registry := aggregates(t)
	vectorized := 0
	for seed := range uint64(60) {
		g := &generator{rand: rand.New(rand.NewPCG(seed, 11)), ints: []string{"a", "b"}, arrays: []string{"xs"}}
		source := g.of("int", 4)
		vector, err := load(source, registry, randomContract)
		if err != nil || machine.Vectors(vector) == 0 {
			continue
		}
		vectorized++
		body, _ := load(source, registry, randomContract)
		machine.WithoutVectors(body)
		for range 2 {
			args := g.args()
			xs, _ := args["xs"].([]any)
			args["xs"] = longer(g.rand, xs)
			assertVectorAsBody(t, source, vector, body, args)
		}
	}
	if vectorized < 8 {
		t.Fatalf("only %d random programs have a loop the vector runs", vectorized)
	}
}

// longer repeats items past a column's length.
func longer(rng *rand.Rand, items []any) []any {
	if len(items) == 0 {
		return items
	}
	out := make([]any, 300+rng.IntN(20))
	for i := range out {
		out[i] = items[rng.IntN(len(items))]
	}
	return out
}
