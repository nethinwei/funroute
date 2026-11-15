//go:build !race

package api

import (
	"testing"

	"github.com/nethinwei/funroute"
)

// The tests that count allocations: the race detector allocates on its own,
// and would be counted, so under it this file is not built.

type SumIn struct {
	A int64 `funroute:"a"`
	B int64 `funroute:"b"`
}

// A session's run of a rule of scalars allocates nothing, RunInto's neither.
func TestASessionRunAllocatesNothing(t *testing.T) {
	binding, err := funroute.Bind[SumIn, int64](funroute.CoreRegistry())
	if err != nil {
		t.Fatal(err)
	}
	program, err := binding.Compile("a * b - a % b + 1")
	if err != nil {
		t.Fatal(err)
	}
	session, in, out, ctx := program.Session(), &SumIn{A: 7, B: 3}, int64(0), t.Context()
	if allocs := testing.AllocsPerRun(100, func() {
		if err := session.RunInto(ctx, in, &out); err != nil {
			t.Fatal(err)
		}
	}); allocs != 0 || out != 21 {
		t.Errorf("a run answered %d and allocated %v times, want 21 and 0", out, allocs)
	}
}
