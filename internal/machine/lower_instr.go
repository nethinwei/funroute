package machine

import "fmt"

// instruction lowers one stack instruction.
func (l *lowerer) instruction(in Instruction) {
	switch in.Op {
	case OpConstant:
		l.push(int32(in.A))
	case OpLoadArg:
		l.push(l.out.args + int32(in.A))
	case OpLoadLocal:
		l.push(l.localBase + int32(in.A))
	case OpStoreLocal:
		l.store(l.localBase + int32(in.A))
	case OpMakeArray, OpMakeDict, OpMakeRecord, OpRecordWith:
		l.build(in)
	case OpField:
		l.field(in.A)
	case OpEqual:
		l.binary(rEq, false)
	case OpCall:
		l.call(in)
	case OpJumpIfFalse:
		l.branch(in.A)
	case OpJump:
		l.flush(0)
		l.emit(rJump, 0, 0, 0)
		l.jumpTo(operandA, in.A)
	default:
		l.scoped(in)
	}
}

// scoped lowers what opens and closes a loop, a fallback or a using.
func (l *lowerer) scoped(in Instruction) {
	switch in.Op {
	case OpLoopInit:
		l.loopInit(in)
	case OpLoopCollect:
		l.collect(in)
	case OpLoopSpread:
		// A spread after the inner clause that put its items in place
		// already is nothing to do.
		if value := l.pop(); !l.spread {
			l.emit(rSpread, value, 0, 0)
		}
		l.spread = false
	case OpLoopNext:
		l.loopNext(in)
	case OpBeginFallback:
		l.flush(0)
		l.emit(rBeginFallback, 0, 0, 0)
		l.jumpTo(operandA, in.A)
	case OpEndFallback:
		l.emit(rEndFallback, 0, 0, 0)
	case OpFxPush:
		l.emit(rFxPush, l.window(in.B), int32(in.B), 0)
	case OpFxPop:
		l.emit(rFxPop, 0, 0, 0)
	case OpLoopFold:
		l.fold(in)
	case OpLoopBreak:
		answer := l.pop()
		l.flush(0)
		l.emit(rLoopBreak, answer, 0, l.open[len(l.open)-1])
		l.jumpTo(operandB, in.A)
	default:
		l.err = fmt.Errorf("internal error: no lowering of opcode %q", in.Op)
	}
}

// unary replaces the top value with op of it.
func (l *lowerer) unary(op rop, b int32) {
	value := l.pop()
	l.emit(op, value, b, l.top())
	l.push(l.top())
}

// binary replaces the top two values with op of them, the other way round
// when swap is set.
func (l *lowerer) binary(op rop, swap bool) {
	right, left := l.pop(), l.pop()
	if swap {
		left, right = right, left
	}
	l.emit(op, left, right, l.top())
	l.push(l.top())
}

// build lowers the instructions that take a run of values and make one.
func (l *lowerer) build(in Instruction) {
	op, count := rMakeArray, in.A
	switch in.Op {
	case OpMakeDict:
		op = rMakeDict
	case OpMakeRecord:
		op = rMakeRecord
	case OpRecordWith:
		op, count = rRecordWith, len(in.Keys)+1
	}
	answers := in.Op == OpMakeRecord || in.Op == OpRecordWith
	made := rmake{typ: in.Type, keys: in.Keys, built: built{arena: -1, dest: -1}, answer: answers && l.flow.answer == l.pc}
	if in.Op == OpMakeArray {
		made.built = l.builtFor(l.pc)
	}
	if in.Op == OpRecordWith {
		made.keys, made.fields = nil, fieldIndexes(in)
	}
	l.out.makes = append(l.out.makes, made)
	first := l.window(count)
	l.emit(op, first, int32(count), int32(len(l.out.makes)-1))
	l.push(first)
}

// fieldIndexes is where each field a record update names is in the record.
func fieldIndexes(in Instruction) []int {
	indexes := make([]int, len(in.Keys))
	for i, name := range in.Keys {
		indexes[i] = in.Type.FieldIndex(name)
	}
	return indexes
}

// call lowers a call: into the kernel function's own operation when it has
// one, and otherwise into a call of its arguments' registers.
func (l *lowerer) call(in Instruction) {
	function := l.functions[in.A]
	if kernel, ok := kernelOps[function.key]; ok && function.builtin {
		// An array in an arena slot is read there.
		if l.flow.uses[l.pc] && kernel.op == rAt {
			kernel.op = rAtA
		}
		if kernel.unary {
			l.unary(kernel.op, 0)
		} else {
			l.binary(kernel.op, kernel.swap)
		}
		return
	}
	first := l.window(in.B)
	// A pure function's call needs no deadline: only a host's other calls do.
	l.out.hosts = l.out.hosts || !function.builtin && function.pure == nil
	l.out.foreign = l.out.foreign || !function.builtin
	l.out.calls = append(l.out.calls, rcall{
		fn: function, typ: in.Type, args: first, argc: int32(in.B), dst: first, pc: int32(l.pc),
		kernel: function.builtin && !function.readsRun && function.Doc.Timeout == 0 && !function.Doc.Detached,
		direct: !function.builtin && function.EvalBatch == nil && function.Doc.Timeout == 0 && !function.Doc.Detached,
		pure:   function.pure,
	})
	l.emit(rCall, int32(len(l.out.calls)-1), 0, 0)
	l.push(first)
}

// fusedBranch is the branch the operation at last makes one with, when it is
// this block's ordering of condition.
func (l *lowerer) fusedBranch(last int, condition int32) (rop, bool) {
	if int32(last) < l.blockStart || l.out.code[last].c != condition {
		return rInvalid, false
	}
	fused, ok := branchOps[l.out.code[last].op]
	return fused, ok
}

// branch lowers jump_if_false. A condition an ordering just made becomes one
// operation with the jump; a constant one, a jump or nothing.
func (l *lowerer) branch(target int) {
	condition := l.pop()
	last := len(l.out.code) - 1
	// Only an operation of this block fuses — there may be none yet.
	if fused, ok := l.fusedBranch(last, condition); ok {
		ordering, origin := l.out.code[last], l.out.origins[last]
		l.out.code, l.out.origins = l.out.code[:last], l.out.origins[:last]
		l.flush(0)
		l.emit(fused, ordering.a, ordering.b, 0)
		// A comparison that can fail fails as the one it was.
		l.out.origins[len(l.out.origins)-1] = origin
		l.jumpTo(operandC, target)
		return
	}
	l.flush(0)
	if condition < l.out.args {
		// A constant condition: the compiler folds most, not all.
		if l.constants[condition].b {
			return
		}
		l.emit(rJump, 0, 0, 0)
		l.jumpTo(operandA, target)
		return
	}
	l.emit(rBranch, condition, 0, 0)
	l.jumpTo(operandB, target)
}

// branchOps is the branch each ordering makes with the jump after it.
var branchOps = map[rop]rop{
	rLtI: rBranchLtI, rLeI: rBranchLeI, rLtF: rBranchLtF, rLeF: rBranchLeF, rLtS: rBranchLtS, rLeS: rBranchLeS, rEq: rBranchEq,
}

// loopInit lowers the start of a loop: the source, and a fold's seed.
func (l *lowerer) loopInit(in Instruction) {
	seed := int32(-1)
	if in.C != NoAccumulator {
		seed = l.pop()
	}
	source := l.pop()
	l.flush(0)
	loop := rloop{typ: in.Type, item: l.localBase + int32(in.B), key: -1, acc: -1, dst: l.top(), exit: int32(in.A), built: l.builtFor(l.pc),
		itemsInPlace: l.flow.itemsInPlace[l.pc]}
	if in.D != NoKey {
		loop.key = l.localBase + int32(in.D)
	}
	if in.C != NoAccumulator {
		loop.acc = l.localBase + int32(in.C)
	}
	l.out.loops = append(l.out.loops, loop)
	index := int32(len(l.out.loops) - 1)
	l.open = append(l.open, index)
	l.out.nesting = max(l.out.nesting, len(l.open))
	l.emit(rLoopInit, source, seed, index)
}

// fold lowers an item folded into its loop's answer: into the step's own
// operation when the kernel has one, and otherwise into a call of the answer
// and the item, moved into two registers of their own.
func (l *lowerer) fold(in Instruction) {
	acc := l.out.loops[l.open[len(l.open)-1]].acc
	item := l.pop()
	l.clobber(acc)
	step := l.functions[in.A]
	if kernel, ok := kernelOps[step.key]; ok && step.builtin && !kernel.unary {
		left, right := acc, item
		if kernel.swap {
			left, right = right, left
		}
		l.emit(kernel.op, left, right, acc)
		return
	}
	scratch := l.scratch()
	l.emit(rMove, acc, 0, scratch)
	l.emit(rMove, item, 0, scratch+1)
	l.out.calls = append(l.out.calls, rcall{
		fn: step, typ: in.Type, args: scratch, argc: 2, dst: acc, pc: int32(l.pc),
		kernel: !step.readsRun,
	})
	l.emit(rCall, int32(len(l.out.calls)-1), 0, 0)
}

// scratch is the first of the two registers past the locals a folding call
// takes its arguments from, made there the first time one is asked for.
func (l *lowerer) scratch() int32 {
	first := l.localBase + l.locals
	l.out.size = max(l.out.size, first+2)
	return first
}

// collect lowers what one iteration adds: a fold's accumulator is stored
// like a local.
func (l *lowerer) collect(in Instruction) {
	loop := l.out.loops[l.open[len(l.open)-1]]
	if loop.acc >= 0 {
		l.store(loop.acc)
		return
	}
	value, key := l.pop(), int32(-1)
	if in.A == 1 {
		key = l.pop()
	}
	l.emit(rCollect, value, key, 0)
}

// loopNext lowers the end of a loop's body. An unkeyed collect just before
// it becomes one operation with it.
func (l *lowerer) loopNext(in Instruction) {
	l.flush(0)
	loop := l.open[len(l.open)-1]
	l.open = l.open[:len(l.open)-1]
	if l.pc+1 < len(l.code) && l.code[l.pc+1].Op == OpLoopSpread {
		l.out.loops[loop].spread, l.spread = true, true
	}
	if last := &l.out.code[len(l.out.code)-1]; last.op == rCollect && last.b < 0 && int32(len(l.out.code)-1) >= l.blockStart {
		last.op, last.c = rCollectNext, loop
		l.jumpTo(operandB, in.A)
	} else {
		l.emit(rLoopNext, 0, 0, loop)
		l.jumpTo(operandA, in.A)
	}
	l.push(l.out.loops[loop].dst)
}
