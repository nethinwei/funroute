package machine

import (
	"context"
	"errors"
	"time"

	"github.com/nethinwei/funroute/internal/kit"
)

// A program always ends — no recursion, no loop but over a finite input
// (docs/termination.md) — but one that walks a long input, or nests loops
// over literals, can take long. The request's context bounds it: a host's
// call looks at it before it starts — but a pure one (host_pure.go), which is
// quick and waits on nothing — and a loop every checkEvery turns, so a run
// stops within about a millisecond of its deadline or its cancellation. A
// context that can end neither is never looked at.

// checkEvery is how many turns of a loop go between two looks at the
// context: about a millisecond of the slowest common bodies (a record read
// from the host's slice, a string function under a filter: some 20 ns a
// turn), a twentieth of one for the fastest (1.5 ns, in a vector). A look
// costs a nanosecond, and the clock's reading every checkEvery turns.
const checkEvery = 1 << 15

// ErrFoldBudget is how an evaluation at compile time ends when its loops
// turn more than the budget the compiler gave it: the expression is then
// left for run time, not folded. It never reaches a host.
var ErrFoldBudget = errors.New("compile-time evaluation budget exhausted")

// watch readies the frame to run under ctx.
func (f *frame) watch(r *Runtime, ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}
	f.ctx = ctx
	// Only a host's call and a loop look at the context.
	f.deadline = (r.reg.hosts || len(r.reg.loops) > 0) && ctx.Done() != nil
	f.turns = checkEvery
}

// turn counts n turns of a loop: past an evaluation's budget it stops, and
// every checkEvery turns it looks whether the request is over. A loop calls
// it only when the run watches for either (watching).
func (f *frame) turn(n int) error {
	if f.budget > 0 {
		if f.budget -= n; f.budget <= 0 {
			return ErrFoldBudget
		}
	}
	if !f.deadline {
		return nil
	}
	if f.turns -= n; f.turns > 0 {
		return nil
	}
	f.turns = checkEvery
	return f.expired()
}

// watching reports a run whose loops count their turns.
func (f *frame) watching() bool { return f.deadline || f.budget > 0 }

// expired is the request's end as a failure, once it is cancelled or past its
// deadline, and nil until then. The deadline is held to the clock here: a
// loop that never lets the context's timer run — js/wasm has one thread —
// would otherwise never see it pass.
func (f *frame) expired() error {
	err := f.ctx.Err()
	if err == nil {
		if deadline, ok := f.ctx.Deadline(); ok && !time.Now().Before(deadline) {
			err = context.DeadlineExceeded
		}
	}
	if err != nil {
		return kit.Classify(ErrDeadline, "", err)
	}
	return nil
}
