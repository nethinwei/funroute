package machine_test

import (
	"fmt"
	"math"
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/nethinwei/funroute/internal/compile"
	"github.com/nethinwei/funroute/internal/machine"
)

// The shapes most comprehensions and aggregates take are run by the vector.
func TestTheVectorRunsTheCommonShapes(t *testing.T) {
	t.Parallel()
	registry := randomRegistry(t) // the kernel's sum, any, all and first carry their folds
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
	`first([x * 2 for x in xs if x > 3])`,
	`any([x > 15 for x in xs if x % 2 == 0])`,
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

// The vector answers as the body does: the value and the failure, word for
// word — for items that overflow, divide by zero or stop the loop anywhere in
// a block of columns.
func TestTheVectorAnswersAsTheBody(t *testing.T) {
	t.Parallel()
	registry := randomRegistry(t) // the kernel's sum, any, all and first carry their folds
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
// edges of a block, a full one's and those of a loop that may stop.
func vectorInputs() []map[string]any {
	var inputs []map[string]any
	rng := rand.New(rand.NewPCG(1, 2))
	for _, n := range []int{0, 1, 5, 8, 9, 255, 256, 257, 600} {
		// A loop that may stop runs its first 8 items in the body, then
		// takes blocks of 16, 32, 64 … items: their edges are at 8, 24, 56,
		// 120 and 248. A full block's are at 256.
		for _, at := range []int{-1, 0, n / 2, n - 1, 7, 8, 23, 24, 55, 56, 119, 120, 247, 248, 255, 256} {
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
	if got, want := outcome(t, vector, args), outcome(t, body, args); got != want {
		t.Fatalf("%s with %s: the vector gives %s, the body %s", source, describe(args), got, want)
	}
}

func describe(args map[string]any) string {
	xs := fmt.Sprint(args["xs"])
	if len(xs) > 80 {
		xs = xs[:80] + "…"
	}
	return strings.ReplaceAll(xs, "\n", " ")
}

// Random programs answer as they do without the vector.
func TestTheVectorAnswersRandomProgramsAsTheBody(t *testing.T) {
	t.Parallel()
	registry := randomRegistry(t) // the kernel's sum, any, all and first carry their folds
	vectorized := 0
	for seed := range uint64(240) {
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
			// Past a column's length for a single loop; a nest of loops
			// over three hundred items each would take minutes for what a
			// short array tests as well.
			if xs, _ := args["xs"].([]any); loops(source) == 1 {
				args["xs"] = longer(g.rand, xs)
			}
			assertVectorAsBody(t, source, vector, body, args)
		}
	}
	if vectorized < 8 {
		t.Fatalf("only %d random programs have a loop the vector runs", vectorized)
	}
}

// loops is how many loops source writes.
func loops(source string) int {
	return strings.Count(source, " for ") + strings.Count(source, "reduce(")
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
