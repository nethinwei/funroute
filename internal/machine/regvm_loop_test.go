package machine_test

import (
	goruntime "runtime"
	"testing"

	"github.com/nethinwei/funroute/internal/compile"
	"github.com/nethinwei/funroute/internal/machine"
)

// A dictionary a comprehension or a literal builds is built once: the map it
// is, of the native kind its values have, or of values for a record — never
// one map built and a second packed from it. Ten entries of a native kind
// cost what an int's do; a record's, its ten records besides.
func TestADictionaryIsBuiltInOneMap(t *testing.T) {
	registry := machine.CoreRegistry()
	if err := registry.EnableForm(machine.ForForm); err != nil {
		t.Fatal(err)
	}
	xs, err := machine.ToValue([]int64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10})
	if err != nil {
		t.Fatal(err)
	}
	contract := compile.CompileOptions{Args: []compile.ArgSpec{{Name: "xs", Type: machine.ArrayOf(machine.IntType)}, {Name: "a", Type: machine.IntType}}}
	for source, want := range map[string]float64{
		`{string(x): x for x in xs}`:      4,
		`{string(x): x > 3 for x in xs}`:  4,
		`{string(x): "v" for x in xs}`:    4,
		`{string(x): {n: x} for x in xs}`: 15,
		`{"a": a, "b": a + 1}`:            2,
		`{"a": a > 1, "b": a > 2}`:        2,
	} {
		artifact, err := compile.CompileExpr(source, registry, contract)
		if err != nil {
			t.Fatal(err)
		}
		runtime, err := machine.Instantiate(artifact, registry)
		if err != nil {
			t.Fatal(err)
		}
		ctx, args := t.Context(), []machine.Value{xs, machine.Int(5)}
		allocs := testing.AllocsPerRun(100, func() {
			if _, err := runtime.RunValues(ctx, args); err != nil {
				t.Fatal(err)
			}
		})
		if allocs != want {
			t.Errorf("%s allocated %v times, want %v", source, allocs, want)
		}
	}
}

// A loop that filters its items is told from one that keeps them all, and
// makes room for what it keeps as it keeps it: a hundred thousand items of
// which one is kept cost what ten would, not a hundred thousand's memory.
func TestAFilteringLoopIsSizedAsItFills(t *testing.T) {
	registry := machine.CoreRegistry()
	if err := registry.EnableForm(machine.ForForm); err != nil {
		t.Fatal(err)
	}
	load := func(source string) *machine.Runtime {
		artifact, err := compile.CompileExpr(source, registry, compile.CompileOptions{Args: []compile.ArgSpec{{Name: "xs", Type: machine.ArrayOf(machine.IntType)}}})
		if err != nil {
			t.Fatal(err)
		}
		runtime, err := machine.Instantiate(artifact, registry)
		if err != nil {
			t.Fatal(err)
		}
		return runtime
	}
	for source, want := range map[string]bool{"[x + 1 for x in xs]": false, "[x for x in xs if x == 0]": true, "[x for x in xs if x > 1 && x < 5]": true} {
		if got := machine.LoopsFilter(load(source)); len(got) != 1 || got[0] != want {
			t.Errorf("%s: its loop filters %v, want %v", source, got, want)
		}
	}
	runtime := load("[x for x in xs if x == 0]")
	bytes := func(n int) uint64 {
		items := make([]int64, n)
		for i := range items {
			items[i] = int64(i)
		}
		xs, _ := machine.ToValue(items)
		var before, after goruntime.MemStats
		goruntime.ReadMemStats(&before)
		if _, err := runtime.RunValues(t.Context(), []machine.Value{xs}); err != nil {
			t.Fatal(err)
		}
		goruntime.ReadMemStats(&after)
		return after.TotalAlloc - before.TotalAlloc
	}
	if short, long := bytes(10), bytes(100_000); long > short+64<<10 {
		t.Fatalf("keeping one of 10 items allocates %d bytes and one of 100000 %d; want them alike", short, long)
	}
}
