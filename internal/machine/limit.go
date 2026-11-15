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

// carried is what a turn of the loop hands the next beyond the turn itself:
// what a fold's answer holds. Only an evaluation at compile time asks: its
// budget is for what it makes as well as how long it turns, and a fold whose
// answer grows makes it anew every turn.
func (f *frame) carried(loop *regLoop) int {
	site := loop.site
	if site.acc < 0 || bankOf(site.typ.kind) != valueBank {
		return 0
	}
	return holds(f.regs[site.acc])
}

// spend takes what an item a loop collects holds off an evaluation's
// budget. The turn after finds it spent: the last of it is left for that.
func (f *frame) spend(item Value) {
	if f.budget > 0 {
		f.budget = max(f.budget-holds(item), 1)
	}
}

// holds is how much there is in v: a string's bytes, a container's items
// and what each of them holds.
func holds(v Value) int {
	switch v.kind {
	case StringKind:
		return len(v.s)
	case RecordKind:
		n := 0
		if record, ok := v.box.(*recordValue); ok {
			for _, field := range record.fields {
				n += holds(field)
			}
		}
		return n
	case ArrayKind, DictKind:
		n := v.length()
		if elem := v.elemType().kind; elem != StringKind && elem != RecordKind && elem != ArrayKind && elem != DictKind {
			return n
		}
		if v.kind == ArrayKind {
			for i := range n {
				n += holds(v.at(i))
			}
			return n
		}
		_ = v.eachEntry(func(_ string, entry Value) error {
			n += holds(entry)
			return nil
		})
		return n
	}
	return 0
}

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
