package machine

import (
	"context"
	"fmt"
	"reflect"
	"unsafe"

	"github.com/nethinwei/funroute/internal/kit"
)

// A Program whose rule is straight code — no loop, no fallback, no using, no
// money, and a plain bool, int, float or string answer — runs by a shorter
// way: the arguments load as they always do, the frame gets its context and
// nothing else, and the answer is written straight into the host's Out. A host's function it
// calls sees the context and the deadline as in any run; a Program's run is
// never a Batch's, so no call is answered ahead.
// Such a run writes nothing a frame must put back but its context, its
// registers when what it reads holds a pointer, and what it built in the
// arena or borrowed from the host — so the frame goes back without the rest
// of a release. The budget, the failures and the
// recover are the ordinary run's.

// straightProgram reports a program that can run the shorter way.
func straightProgram(r *Runtime, result *codec) bool {
	reg := &r.reg
	if r.money.any || !plainResult(result) {
		return false
	}
	for _, in := range reg.code {
		switch in.op {
		case rLoopInit, rBeginFallback, rFxPush:
			return false
		}
	}
	return true
}

// plainResult reports a result stored with no check: a bool, an int64, a
// float64 or a string.
func plainResult(result *codec) bool {
	if result.shape != shapeScalar || result.typ.kind == EnumKind {
		return false
	}
	switch result.goKind {
	case reflect.Bool, reflect.Int64, reflect.Float64, reflect.String:
		return true
	}
	return false
}

// runStraight is run for a straight program.
func (p *Program[In, Out]) runStraight(ctx context.Context, in *In, out *Out) error {
	r := p.runtime
	f := r.acquireFrame()
	args := f.argSpace(len(p.args.params))
	f.onlyReads = true
	if err := encodeArgs(p.args, p.reads, args, in, f); err != nil {
		clear(args)
		r.releaseFrame(f)
		return kit.Classify(ErrContract, "", err)
	}
	value, err := r.runStraight(ctx, f)
	if err != nil {
		return err
	}
	storePlain(p.result.goKind, placeOf(out), value)
	return nil
}

// runStraight is runFrame for a straight program: the same context, the
// same recover, and the frame back.
func (r *Runtime) runStraight(ctx context.Context, f *frame) (value Value, err error) {
	f.watch(r, ctx)
	defer r.finishStraight(f, &err)
	return f.exec()
}

// finishStraight ends a straight run: a panic is its failure, as it is any
// run's, and the frame goes back with nothing of the run in it.
func (r *Runtime) finishStraight(f *frame, err *error) {
	if recovered := recover(); recovered != nil {
		*err = fmt.Errorf("%w: extension panicked: %v", ErrExtension, recovered)
	}
	// What release clears after a Program's run: only the arguments it read
	// were written.
	if !r.reg.scalarReads {
		clear(f.regs[f.argBase:])
	}
	f.returnMemory()
	f.ctx, f.onlyReads = nil, false
	r.frames.Put(f)
}

// storePlain writes a plain answer of kind at p.
func storePlain(kind reflect.Kind, p unsafe.Pointer, v Value) {
	switch kind {
	case reflect.Bool:
		*(*bool)(p) = v.b
	case reflect.Int64:
		*(*int64)(p) = v.i
	case reflect.Float64:
		*(*float64)(p) = v.f
	default:
		*(*string)(p) = v.s
	}
}
