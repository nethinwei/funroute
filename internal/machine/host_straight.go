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

// runStraight is run for a straight program, in f: the arguments loaded,
// the program run, the answer written from its register straight into out.
func (p *Program[In, Out]) runStraight(ctx context.Context, f *frame, in *In, out *Out) error {
	r := p.runtime
	f.onlyReads = true
	if err := p.loadStraight(f, in); err != nil {
		clear(f.argSpace(len(p.args.params)))
		r.releaseFrame(f)
		return kit.Classify(ErrContract, "", err)
	}
	// A program with no call and no loop never looks at its context.
	if len(r.reg.calls) > 0 {
		f.watch(r, ctx)
	}
	if r.reg.foreign {
		return p.runStraightGuarded(f, out)
	}
	answer, err := f.exec()
	if err == nil {
		storePlain(p.result.goKind, placeOf(out), &f.regs[answer])
	}
	r.finishStraight(f)
	return err
}

// runStraightGuarded is runStraight's run under a recover: a program that
// calls a host's function, whose panic is the run's failure.
func (p *Program[In, Out]) runStraightGuarded(f *frame, out *Out) (err error) {
	defer p.runtime.recoverStraight(f, &err)
	answer, err := f.exec()
	if err == nil {
		storePlain(p.result.goKind, placeOf(out), &f.regs[answer])
	}
	return err
}

// loadStraight loads the arguments a straight program reads: by kind, a
// loop each, when every one is a plain field (plainLoads), and the ordinary
// way otherwise.
func (p *Program[In, Out]) loadStraight(f *frame, in *In) error {
	if p.plain == nil {
		return encodeArgs(p.args, p.reads, f.argSpace(len(p.args.params)), in, f)
	}
	base, regs := unsafe.Pointer(in), f.regs
	for _, at := range p.plain.ints {
		regs[at.reg] = Value{kind: IntKind, i: *(*int64)(unsafe.Add(base, at.offset))}
	}
	for _, at := range p.plain.floats {
		regs[at.reg] = Value{kind: FloatKind, f: *(*float64)(unsafe.Add(base, at.offset))}
	}
	for _, at := range p.plain.bools {
		regs[at.reg] = Value{kind: BoolKind, b: *(*bool)(unsafe.Add(base, at.offset))}
	}
	for _, at := range p.plain.strings {
		regs[at.reg] = Value{kind: StringKind, s: *(*string)(unsafe.Add(base, at.offset))}
	}
	return nil
}

// plainLoads is where each argument a straight program reads sits in the
// host's struct and which register it goes in, by kind: what loading them
// is when every one is an int64, a float64, a bool or a string field.
type plainLoads struct {
	ints, floats, bools, strings []plainLoad
}

type plainLoad struct {
	offset uintptr
	reg    int32
}

// plainLoadsOf is the plainLoads of reads, or nil when one of them is not
// a plain field, or is a record whose fields are promoted.
func plainLoadsOf(reads []argRead, args int32) *plainLoads {
	loads := &plainLoads{}
	for _, read := range reads {
		at := plainLoad{offset: read.offset, reg: args + int32(read.index)}
		switch {
		case read.promoted != nil:
			return nil
		case read.kind == reflect.Int64:
			loads.ints = append(loads.ints, at)
		case read.kind == reflect.Float64:
			loads.floats = append(loads.floats, at)
		case read.kind == reflect.Bool:
			loads.bools = append(loads.bools, at)
		case read.kind == reflect.String:
			loads.strings = append(loads.strings, at)
		default:
			return nil
		}
	}
	return loads
}

// recoverStraight makes a panic the run's failure, as it is any run's, and
// ends the run.
func (r *Runtime) recoverStraight(f *frame, err *error) {
	if recovered := recover(); recovered != nil {
		*err = fmt.Errorf("%w: extension panicked: %v", ErrExtension, recovered)
	}
	r.finishStraight(f)
}

// finishStraight ends a straight run: the frame goes back with nothing of
// the run in it.
func (r *Runtime) finishStraight(f *frame) {
	// What release clears after a Program's run: only the arguments it read
	// were written.
	if !r.reg.scalarReads {
		clear(f.regs[f.argBase:])
	}
	f.returnMemory()
	f.ctx, f.onlyReads = nil, false
	r.putFrame(f)
}

// storePlain writes a plain answer of kind at p.
func storePlain(kind reflect.Kind, p unsafe.Pointer, v *Value) {
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
