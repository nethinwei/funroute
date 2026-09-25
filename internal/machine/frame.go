package machine

import (
	"cmp"
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
	fxQuotes []Value
	fxMarks  []int
	fxCtx    fxContext // handed to a kernel function that reads rates
	fuelLeft uint64
	// refund is the fuel a failure fallback takes gives back: its block paid
	// for what did not run.
	refund uint64
	// maxStack is the run's stack limit, and stackLimit what a block is held
	// to: the limit, when it is below how deep the program goes.
	maxStack   int
	stackLimit int
	ctx        context.Context // the request's budget; extension calls see it
	deadline   bool            // whether ctx can expire, and a host's call looks
	prefetched []Prefetched    // a Batch's answers for hoisted calls, by call
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
	}
	copy(f.regs, r.constants)
	f.fxQuotes, f.fxMarks = f.fxQuotesArray[:0], f.fxMarksArray[:0]
	f.fxCtx.frame = f
	return f
}

// argSpace is the registers the n arguments go in.
func (f *frame) argSpace(n int) []Value { return f.regs[f.argBase : f.argBase+n] }

// defaultMaxStack is a run's stack limit when its options set none.
const defaultMaxStack = 1_024

// runFrame runs the program in the frame, its arguments in place, with the
// budget in options, and gives the frame back. A Program's run delivers the
// answer into the host's Out first (sink), and may lend the answer's slots.
func (r *Runtime) runFrame(ctx context.Context, f *frame, args []Value, options RunOptions, sink resultSink) (value Value, err error) {
	if ctx == nil {
		ctx = context.Background()
	}
	f.fuelLeft = cmp.Or(options.Fuel, DefaultFuel)
	f.maxStack, f.stackLimit = defaultMaxStack, r.stackLimit
	if options.MaxStack != 0 {
		f.maxStack, f.stackLimit = options.MaxStack, stackLimitFor(r.depth, options.MaxStack)
	}
	f.ctx = ctx
	// Only a host's call looks at the deadline.
	f.deadline = r.reg.hosts && ctx.Done() != nil
	f.prefetched = options.prefetched
	defer r.finish(f, &value, &err, sink)
	if f.lending = sink.plan != nil; f.lending && sink.reuse {
		sink.lend(f, args)
	}
	if r.money.any {
		if err = r.checkUnits(args); err != nil {
			return Value{}, err
		}
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
	if !f.runtime.reg.scalar {
		clear(f.regs[f.argBase:])
	}
	f.ctx = nil
	f.prefetched = nil
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
	for i := range f.arena {
		f.arena[i].release()
	}
	if f.borrowed {
		for i := range f.records {
			clear(f.records[i].fields)
		}
		clear(f.views)
		f.borrowed = false
	}
	if f.lending {
		// The answer is the host's now: nothing of it stays.
		clear(f.dest)
		clear(f.answer.fields)
		f.answer.fields = f.answer.fields[:0]
		f.lending = false
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
