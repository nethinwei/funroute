package machine

import (
	"context"
	"fmt"
)

// The register machine runs a program's register form. Its hot loop holds
// the operations most programs spend their time in; the rest go through
// cold, a call away.

// exec is the hot loop: the operations most programs spend their time in,
// and cold for the rest. An operation that cannot answer on its fast path
// clears ok, and fault says why. It answers the register the program's
// answer is in, so the answer is copied once, where it is taken.
func (f *frame) exec() (int32, error) {
	code, b := f.runtime.reg.code, &f.banks
	for pc := 0; ; {
		in := &code[pc]
		pc++
		ok, err := true, error(nil)
		switch in.op {
		case rMoveI:
			b.ints[in.c] = b.ints[in.a]
		case rMove:
			b.regs[in.c] = b.regs[in.a]
		case rJump:
			pc = int(in.a)
		case rBranch:
			pc = branchUnless(b.ints[in.a] != 0, pc, in.b)
		case rAddI:
			ok = addI(b.ints, in)
		case rSubI:
			ok = subI(b.ints, in)
		case rMulI:
			ok = mulI(b.ints, in)
		case rField, rFieldI, rFieldF, rFieldB, rBranchField:
			pc, err = f.readField(pc, in)
		case rBranchLtI:
			pc = branchUnless(b.ints[in.a] < b.ints[in.b], pc, in.c)
		case rBranchLeI:
			pc = branchUnless(b.ints[in.a] <= b.ints[in.b], pc, in.c)
		case rLoopNext:
			pc, err = f.regLoopNext(pc, in.a)
		case rCollectNextI:
			f.collectInt(in.a)
			pc, err = f.regLoopNext(int(in.c), in.b)
		case rCollectNext:
			pc, err = f.collectNext(in)
		case rCall:
			err = f.callSite(in.a)
		case rCollect:
			f.collect(in.a, in.b, Kind(in.c))
		case rHalt:
			return in.a, nil
		default:
			pc, ok, err = f.cold(pc, in)
		}
		if (!ok || err != nil) && f.stops(&pc, ok, &err) {
			return 0, err
		}
	}
}

// stops takes the failure of the operation before pc to where the run goes
// on, and reports whether that is nowhere: no fallback takes it.
func (f *frame) stops(pc *int, ok bool, err *error) bool {
	*pc, *err = f.failed(*pc, ok, *err)
	return *err != nil
}

// failed is where a run goes on after the operation before pc failed: to
// the fallback that takes the failure, if one does.
func (f *frame) failed(pc int, ok bool, err error) (int, error) {
	if !ok {
		err = f.fault(pc - 1)
	}
	return f.caught(err)
}

// branchUnless is where a branch goes: on when cond holds, to target when
// it does not.
func branchUnless(cond bool, pc int, target int32) int {
	if cond {
		return pc
	}
	return int(target)
}

// caught hands a failure to the innermost fallback that takes it: the
// loops and usings its candidate opened are closed.
func (f *frame) caught(err error) (int, error) {
	if class, _ := classOf(err); len(f.fallbacks) == 0 || !class.fallback {
		return 0, err
	}
	last := len(f.fallbacks) - 1
	handler := f.fallbacks[last]
	f.fallbacks = f.fallbacks[:last]
	clear(f.loops[handler.loops:])
	f.loops = f.loops[:handler.loops]
	f.dropScopes(handler.scopes)
	return handler.target, nil
}

// fault is why the operation at pc could not answer: the kernel function's
// own failure, which asking it gives.
func (f *frame) fault(pc int) error {
	in, kinds := &f.runtime.reg.code[pc], f.runtime.reg.kinds[pc]
	origin := int(f.runtime.reg.origins[pc])
	source := f.runtime.artifact.parts.Instructions[origin]
	// The operands as the function takes them: values, out of their files.
	left, right := unarena(f.valueAt(in.a, kinds.a)), f.valueAt(in.b, kinds.b)
	if source.Op == OpEqual {
		_, err := compareEqual(left, right)
		return err
	}
	function := f.runtime.functions[source.A]
	args := []Value{left, right}[:len(function.Params)]
	// A run with no call never had a context to keep.
	ctx := f.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	if _, err := function.Eval(ctx, args); err != nil {
		return fmt.Errorf("%s: %w", function.Name, err)
	}
	return fmt.Errorf("internal error: %s refused what %s accepts", in.op, function.key)
}
