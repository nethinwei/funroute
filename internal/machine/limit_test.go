package machine_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nethinwei/funroute/internal/compile"
	"github.com/nethinwei/funroute/internal/machine"
)

type longIn struct {
	Xs []int64 `funroute:"xs"`
}

// A long run stops at its request's end: a loop looks at the context every
// few tens of thousands of turns — a scalar loop, a vector's blocks, a body
// that calls a host — so a run of a hundred million turns whose request is
// already over ends at the first look, not the last turn.
func TestALongLoopStopsWhenTheRequestIsOver(t *testing.T) {
	t.Parallel()
	registry := machine.CoreRegistry()
	if err := registry.EnableForm(machine.ForForm, machine.ReduceForm); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register(machine.FunctionSpec{Name: "h.twice_v1", Go: func(a int64) int64 { return 2 * a }}); err != nil {
		t.Fatal(err)
	}
	binding, err := compile.Bind[longIn, int64](registry)
	if err != nil {
		t.Fatal(err)
	}
	in := &longIn{Xs: make([]int64, 10_000)}
	over, cancel := context.WithCancel(t.Context())
	cancel()
	past, cancelPast := context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
	defer cancelPast()
	for _, source := range []string{
		`len([x for x in xs for y in xs if x == y + 1])`,
		`reduce(x in xs, t = 0, t + len([y * 2 for y in xs]))`,
		`reduce(x in xs, t = 0, t + reduce(y in xs, u = 0, u + h.twice_v1(y)))`,
	} {
		program, err := binding.Compile(source)
		if err != nil {
			t.Fatalf("%s: %v", source, err)
		}
		for name, ctx := range map[string]context.Context{"cancelled": over, "past its deadline": past} {
			start := time.Now()
			_, err := program.Run(ctx, in)
			if !errors.Is(err, machine.ErrDeadline) {
				t.Errorf("%s, %s: %v; want ErrDeadline", source, name, err)
			}
			if took := time.Since(start); took > time.Second {
				t.Errorf("%s, %s: stopped after %v; want at the first look", source, name, took)
			}
		}
	}
}
