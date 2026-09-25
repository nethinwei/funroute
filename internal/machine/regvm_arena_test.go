package machine_test

import (
	"testing"

	"github.com/nethinwei/funroute/internal/compile"
	"github.com/nethinwei/funroute/internal/machine"
)

// An array that never leaves the run is built in the frame's memory: once
// the frame has run the program, running it again allocates nothing.
func TestArraysThatStayInTheRunDoNotAllocate(t *testing.T) {
	registry := randomRegistry(t)
	contract := compile.CompileOptions{Args: []compile.ArgSpec{{Name: "xs", Type: machine.ArrayOf(machine.IntType)}}}
	items := make([]int64, 500)
	for i := range items {
		items[i] = int64(i)
	}
	xs, err := machine.ToValue(items)
	if err != nil {
		t.Fatal(err)
	}
	for _, source := range []string{
		`len([y * 2 for y in [x + 1 for x in xs]])`,
		`[x + 1 for x in xs][3]`,
		`let(ys = [x * 3 for x in xs if x > 10], reduce(y in ys, t = 0, t + y) + len(ys))`,
		`reduce(b in [x > 3 for x in xs], n = 0, if(b, n + 1, n))`,
		`len([len([x * y for y in [1, 2, 3]]) for x in xs])`,
	} {
		artifact, err := compile.CompileExpr(source, registry, contract)
		if err != nil {
			t.Fatalf("%s: %v", source, err)
		}
		runtime, err := machine.Instantiate(artifact, registry)
		if err != nil {
			t.Fatal(err)
		}
		run := func() {
			if _, err := runtime.RunValues(t.Context(), []machine.Value{xs}, machine.RunOptions{Fuel: 1 << 30}); err != nil {
				t.Fatalf("%s: %v", source, err)
			}
		}
		run()
		if allocs := testing.AllocsPerRun(20, run); allocs != 0 {
			t.Errorf("%s allocates %v times a run, want none", source, allocs)
		}
	}
}
