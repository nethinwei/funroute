package machine

import (
	"fmt"

	"github.com/nethinwei/funroute/internal/kit"
)

// The register machine runs a program's register form. Its hot loop holds
// the operations most programs spend their time in; the rest go through
// cold, a call away.

// exec is the hot loop: the operations most programs spend their time in,
// and cold for the rest. An operation that cannot answer on its fast path
// clears ok, and fault says why.
func (f *frame) exec() (Value, error) {
	code, regs := f.runtime.reg.code, f.regs
	for pc := 0; ; {
		in := &code[pc]
		pc++
		ok, err := true, error(nil)
		switch in.op {
		case rFuel:
			ok = f.charge(in)
		case rMove:
			regs[in.c] = regs[in.a]
		case rJump:
			pc = int(in.a)
		case rBranch:
			pc = branchUnless(regs[in.a].b, pc, in.b)
		case rAddI:
			ok = addI(regs, in)
		case rSubI:
			ok = subI(regs, in)
		case rMulI:
			ok = mulI(regs, in)
		case rDivI:
			ok = divI(regs, in)
		case rAddF:
			ok = addF(regs, in)
		case rBranchLtI:
			pc = branchUnless(regs[in.a].i < regs[in.b].i, pc, in.c)
		case rBranchLeI:
			pc = branchUnless(regs[in.a].i <= regs[in.b].i, pc, in.c)
		case rLoopNext:
			pc, err = f.regLoopNext(pc, in.a)
		case rCollect:
			f.collect(in.a, in.b)
		case rCollectNext:
			pc, err = f.collectNext(pc, in)
		case rCall:
			err = f.callSite(in.a)
		case rHalt:
			return regs[in.a], nil
		default:
			pc, ok, err = f.cold(pc, in)
		}
		if !ok || err != nil {
			if pc, err = f.failed(pc, ok, err); err != nil {
				return Value{}, err
			}
		}
	}
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

// charge pays for the block the operation opens, and holds its stack to the
// run's limit.
func (f *frame) charge(in *rinstr) bool {
	cost := uint64(uint32(in.b))<<32 | uint64(uint32(in.c))
	if f.fuelLeft < cost || int(in.a) > f.stackLimit {
		return false
	}
	f.fuelLeft -= cost
	return true
}

// caught hands a failure to the innermost fallback that takes it: the
// loops and usings its candidate opened are closed, and the fuel its block
// paid for what did not run is given back.
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
	f.fuelLeft += f.refund
	f.refund = 0
	return handler.target, nil
}

// fault is why the operation at pc could not answer: the block's fuel or
// stack, or the kernel function's own failure, which asking it gives.
func (f *frame) fault(pc int) error {
	in := &f.runtime.reg.code[pc]
	origin := int(f.runtime.reg.origins[pc])
	if in.op == rFuel {
		return f.fuelFault(origin, in)
	}
	source := f.runtime.artifact.parts.Instructions[origin]
	left, right := f.regs[in.a], f.regs[in.b]
	if source.Op == OpEqual {
		_, err := compareEqual(left, right)
		return err
	}
	function := f.runtime.functions[source.A]
	args := []Value{left, right}[:len(function.Params)]
	if _, err := function.Eval(f.ctx, args); err != nil {
		return fmt.Errorf("%s: %w", function.Name, err)
	}
	return fmt.Errorf("internal error: %s refused what %s accepts", in.op, function.key)
}

// fuelFault is a block the run cannot pay for. The stack machine charged an
// instruction at a time, so the failure names the instruction it would
// have stopped at: the one with no fuel left, or the call it could not pay.
func (f *frame) fuelFault(start int, in *rinstr) error {
	if int(in.a) > f.stackLimit {
		return kit.Errorf(ErrFuel, "stack limit %d exceeded", f.maxStack)
	}
	left := f.fuelLeft
	f.fuelLeft = 0
	code := f.runtime.artifact.parts.Instructions
	for pc := start; pc < len(code); pc++ {
		if left == 0 {
			return fmt.Errorf("%w at instruction %d", ErrFuel, pc)
		}
		left--
		if code[pc].Op != OpCall {
			continue
		}
		function := f.runtime.functions[code[pc].A]
		if left < function.Doc.Cost {
			return fmt.Errorf("%w before %s", ErrFuel, function.Name)
		}
		left -= function.Doc.Cost
	}
	return fmt.Errorf("%w at instruction %d", ErrFuel, start)
}
