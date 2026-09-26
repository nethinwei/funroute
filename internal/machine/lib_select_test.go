package machine_test

import (
	"math"
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/nethinwei/funroute/internal/compile"
	"github.com/nethinwei/funroute/internal/machine"
)

// A dictionary's missing key stays an error, because the language has no null
// to put in a routing decision. get is where a rule says what the absence
// means, and merge is how two layers of configuration become one.
func TestDictionaryDefaultsAndLayers(t *testing.T) {
	t.Parallel()
	defaults := compile.ArgSpec{Name: "defaults", Type: machine.DictOf(machine.IntType)}
	overrides := compile.ArgSpec{Name: "overrides", Type: machine.DictOf(machine.IntType)}
	specs := []compile.ArgSpec{defaults, overrides}
	args := map[string]any{
		"defaults":  map[string]any{"SG": 250, "US": 300},
		"overrides": map[string]any{"SG": 180, "JP": 120},
	}
	for _, test := range []struct {
		name, source string
		want         any
	}{
		{"命中就用它", `get(defaults, "SG", 0)`, int64(250)},
		{"缺键用默认", `get(defaults, "ZZ", 0)`, int64(0)},
		{"覆盖层优先", `get(merge(defaults, overrides), "SG", 0)`, int64(180)},
		{"没被覆盖的留着", `get(merge(defaults, overrides), "US", 0)`, int64(300)},
		{"覆盖层带来的新键", `get(merge(defaults, overrides), "JP", 0)`, int64(120)},
		{"合并后的键数", `len(merge(defaults, overrides))`, int64(3)},
		{"合并自己不改变什么", `len(merge(defaults, defaults))`, int64(2)},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := libRun(t, test.source, args, specs...)
			if err != nil {
				t.Fatalf("run %s: %v", test.source, err)
			}
			if got != test.want {
				t.Fatalf("%s = %v (%T), want %v", test.source, got, got, test.want)
			}
		})
	}
	// The plain lookup keeps reporting a missing key, which is what makes get
	// a decision rather than a habit.
	if _, err := libRun(t, `defaults["ZZ"]`, args, specs...); err == nil {
		t.Fatal("a missing key was not an error")
	}
}

// top_k and bottom_k pick what the stable sort's first k are, ties in
// place, whatever the keys — repeats, NaN — and whatever k is.
func TestRankedIsTheSortsFirst(t *testing.T) {
	t.Parallel()
	rng := rand.New(rand.NewPCG(3, 5))
	for _, n := range []int{0, 1, 5, 17, 64, 200} {
		ints, floats := make([]any, n), make([]any, n)
		for i := range n {
			ints[i], floats[i] = int64(rng.IntN(6)), []float64{math.NaN(), 0, 1.5, -2}[rng.IntN(4)]
		}
		for _, keys := range [][]any{ints, floats} {
			for k := 0; k <= n+1; k++ {
				assertRankedAsSorted(t, keys, k)
			}
		}
	}
}

func assertRankedAsSorted(t *testing.T, keys []any, k int) {
	t.Helper()
	typ := machine.IntType
	if len(keys) > 0 {
		if _, ok := keys[0].(float64); ok {
			typ = machine.FloatType
		}
	}
	specs := []compile.ArgSpec{{Name: "keys", Type: machine.ArrayOf(typ)}, {Name: "k", Type: machine.IntType}}
	args := map[string]any{"keys": keys, "k": int64(k)}
	for ranked, sorted := range map[string]string{
		"top_k(indices(keys), keys, k)":    "take(sort_by_desc(indices(keys), keys), k)",
		"bottom_k(indices(keys), keys, k)": "take(sort_by(indices(keys), keys), k)",
	} {
		got, gotErr := libRun(t, ranked, args, specs...)
		want, wantErr := libRun(t, sorted, args, specs...)
		gotIndices, _ := got.([]int64)
		wantIndices, _ := want.([]int64)
		if gotErr != nil || wantErr != nil || !slices.Equal(gotIndices, wantIndices) {
			t.Fatalf("%s with keys %v, k %d = %v, %v; %s = %v, %v", ranked, keys, k, got, gotErr, sorted, want, wantErr)
		}
	}
}
