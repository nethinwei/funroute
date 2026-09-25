package machine

import (
	"context"
	"fmt"
)

// NoAccumulator marks a loop instruction as a mapping rather than a fold, and
// NoKey marks it as walking an array rather than a dictionary. The compiler
// emits them; lowering reads them.
const (
	NoAccumulator = -1
	NoKey         = -1
)

// fallbackFrame is a fallback the run is inside: where its handler starts,
// and how many loops and usings were open at its begin, which a failure it
// takes closes back to.
type fallbackFrame struct {
	target int
	loops  int
	scopes int
}

// frame is one activation of a program's register form: its registers,
// loops, fallbacks and usings. Without recursion there is exactly one
// activation per run. A frame is its runtime's alone and serves one run at a
// time; the runtime keeps them between runs.
type frame struct {
	runtime *Runtime
	// regs is the registers, the constants first, and argBase where the
	// arguments start.
	regs      []Value
	argBase   int
	loops     []regLoop
	fallbacks []fallbackFrame
	// fxQuotes are the quotes of every using the run is inside, and
	// fxMarks where each using's begin, the innermost last.
	fxQuotes   []Value
	fxMarks    []int
	fxCtx      fxContext       // handed to a kernel function that reads rates
	ctx        context.Context // the request's budget; extension calls see it
	deadline   bool            // whether ctx can end, and a host's call or a loop looks
	prefetched []Prefetched    // a Batch's answers for hoisted calls, by call
	// turns is how many turns of a loop are left before the run looks at
	// its context again, and budget, for an evaluation at compile time, how
	// many it has left in all (limit.go).
	turns, budget int
	// fxQuotesArray and fxMarksArray hold the usings of a run as deep as
	// most rules go, so a fresh frame converts without allocating either.
	fxQuotesArray [4]Value
	fxMarksArray  [4]int
	// arena is the memory of the arrays that never leave a run, kept from
	// run to run. dest is the answer's slots, which the host lends and
	// lending says it did; the answer's record is then answer, the frame's
	// own, read before the frame goes.
	arena   []arenaSlot
	dest    []arenaSlot
	lending bool
	answer  recordValue
	// records and views are, by argument, the frame's record and slot a
	// Program loads an argument into when the program only reads a
	// record's fields, or only walks, measures or indexes an array.
	records  []recordValue
	views    []arenaSlot
	borrowed bool
	// recordViews are, by argument, the arrays of plain records a Program
	// hands over as the host's slice, and items, by loop, the record each
	// loop over one loads its item into (host_view.go).
	recordViews []recordsView
	items       []recordValue
	// onlyReads says a Program loaded the arguments: only the slots of the
	// ones the program reads were written.
	onlyReads bool
	// vector is the vector's columns and run, kept from run to run
	// (regvm_vector.go); made the first time a loop is run by it.
	vector *vectorState
}

// vectorState is a frame's vector: its columns, and its run.
type vectorState struct {
	columns vecScratch
	run     vecRun
}

// newFrame is a frame for the runtime, its constants already in place: they
// are copied once, not once a run, and so is everything else a run does not
// change.
func (r *Runtime) newFrame() *frame {
	f := &frame{
		runtime: r, regs: make([]Value, r.reg.size), argBase: int(r.reg.args), loops: make([]regLoop, 0, r.reg.nesting),
		arena: make([]arenaSlot, r.reg.arenas), dest: make([]arenaSlot, r.reg.dests),
		records: make([]recordValue, len(r.reg.fieldOnly)), views: make([]arenaSlot, len(r.reg.viewOnly)),
		recordViews: make([]recordsView, len(r.reg.viewOnly)), items: make([]recordValue, len(r.reg.loops)),
	}
	copy(f.regs, r.constants)
	f.fxQuotes, f.fxMarks = f.fxQuotesArray[:0], f.fxMarksArray[:0]
	f.fxCtx.frame = f
	return f
}

// argSpace is the registers the n arguments go in.
func (f *frame) argSpace(n int) []Value { return f.regs[f.argBase : f.argBase+n] }

// runFrame runs the program in the frame, its arguments in place, under ctx,
// and gives the frame back. A Batch hands the answers it has for the
// program's calls (prefetched). A Program's run delivers the answer into the
// host's Out first (sink), and may lend the answer's slots.
func (r *Runtime) runFrame(ctx context.Context, f *frame, args []Value, prefetched []Prefetched, sink resultSink) (value Value, err error) {
	f.watch(r, ctx)
	f.prefetched = prefetched
	defer r.finish(f, &value, &err, sink)
	if f.lending = sink.plan != nil; f.lending && sink.reuse {
		sink.lend(f, args)
	}
	if r.money.any {
		if err = r.checkUnits(args); err != nil {
			return Value{}, err
		}
	}
	// A Program loaded the promoted fields out of the host's struct.
	if len(r.reg.promotions) > 0 && !f.onlyReads {
		f.promote(args)
	}
	return f.exec()
}

// finish ends a run: an extension that panicked is its failure — the recover
// is here, once a run, not around each call — and the frame goes back for
// the next run.
func (r *Runtime) finish(f *frame, value *Value, err *error, sink resultSink) {
	if recovered := recover(); recovered != nil {
		*err = fmt.Errorf("%w: extension panicked: %v", ErrExtension, recovered)
	}
	if sink.plan != nil && *err == nil {
		*err = sink.deliver(*value)
		*value = Value{}
	}
	r.releaseFrame(f)
}

// release drops what a run left, so a kept frame holds nothing alive: a
// program of scalars leaves nothing in its registers.
func (f *frame) release() {
	if !f.runtime.reg.scalar && !(f.onlyReads && f.runtime.reg.scalarReads) {
		clear(f.regs[f.argBase:])
	}
	f.onlyReads = false
	f.ctx = nil
	f.prefetched = nil
	f.budget = 0
	if len(f.loops) > 0 {
		clear(f.loops)
		f.loops = f.loops[:0]
	}
	f.fallbacks = f.fallbacks[:0]
	if len(f.fxMarks) > 0 {
		f.dropScopes(0)
	}
	if f.fxCtx.Context != nil {
		f.fxCtx.Context = nil
	}
	f.returnMemory()
	if f.lending {
		// The answer is the host's now: nothing of it stays.
		clear(f.dest)
		clear(f.answer.fields)
		f.answer.fields = f.answer.fields[:0]
		f.lending = false
	}
}

// returnMemory lets go of what the run built in the frame's arena and what
// it borrowed from the host: the records, slices and arrays of records a
// Program loaded in place.
func (f *frame) returnMemory() {
	for i := range f.arena {
		f.arena[i].release()
	}
	if f.borrowed {
		for i := range f.records {
			clear(f.records[i].fields)
		}
		clear(f.views)
		clear(f.recordViews)
		for i := range f.items {
			clear(f.items[i].fields)
		}
		f.borrowed = false
	}
}

// slotFor is the slot an array is built in, or nil for memory of its own:
// an answer's slot only when the host lent one.
func (f *frame) slotFor(where built) *arenaSlot {
	switch {
	case where.arena >= 0:
		return &f.arena[where.arena]
	case where.dest >= 0 && f.lending:
		return &f.dest[where.dest]
	}
	return nil
}
