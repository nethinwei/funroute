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
		l.binary(eqOp(l.stack[len(l.stack)-1].kind), false)
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
			l.emit(rSpread, value.reg, 0, 0)
		}
		l.spread = false
	case OpLoopNext:
		l.loopNext(in)
	case OpBeginFallback:
		l.flush(0)
		l.emit(rBeginFallback, 0, 0, 0)
		l.jumpTo(operandA, in.A)
		l.candidates++
	case OpEndFallback:
		l.emit(rEndFallback, 0, 0, 0)
		l.candidates--
	case OpFxPush:
		l.emit(rFxPush, l.window(in.B), int32(in.B), 0)
	case OpFxPop:
		l.emit(rFxPop, 0, 0, 0)
	case OpLoopFold:
		l.fold(in)
	case OpLoopBreak:
		answer := l.pop()
		l.flush(0)
		l.emitOn(rLoopBreak, answer.reg, 0, l.open[len(l.open)-1], opKinds{a: answer.kind})
		l.jumpTo(operandB, in.A)
	default:
		l.err = fmt.Errorf("internal error: no lowering of opcode %q", in.Op)
	}
}

// unary replaces the top value with op of it.
func (l *lowerer) unary(op rop, b int32) {
	value := l.pop()
	l.emitOn(op, value.reg, b, l.top(), opKinds{a: value.kind})
	l.push(l.top())
}

// binary replaces the top two values with op of them, the other way round
// when swap is set.
func (l *lowerer) binary(op rop, swap bool) {
	right, left := l.pop(), l.pop()
	if swap {
		left, right = right, left
	}
	l.emitOn(op, left.reg, right.reg, l.top(), opKinds{a: left.kind, b: right.kind})
	l.push(l.top())
}

// boxes is which of the top n values, as window(n) puts them in their stack
// registers, are ints, bools or floats: what a call that takes values
// makes values of.
func (l *lowerer) boxes(n int) []scalarArg {
	var out []scalarArg
	for k := len(l.stack) - n; k < len(l.stack); k++ {
		if kind := l.stack[k].kind; bankOf(kind) != valueBank {
			out = append(out, scalarArg{reg: l.stackBase + int32(k), kind: kind})
		}
	}
	return out
}

// entryOp is the read of a dictionary's entry of kind: an int's, a float's
// or a bool's into its file, and otherwise as a value.
func entryOp(kind Kind) rop {
	switch kind {
	case IntKind:
		return rAtDI
	case FloatKind:
		return rAtDF
	case BoolKind:
		return rAtDB
	}
	return rAtD
}

// eqOp is the equality of two values of kind: of the ints, of the floats,
// or of the values.
func eqOp(kind Kind) rop {
	switch bankOf(kind) {
	case intBank:
		return rEqI
	case floatBank:
		return rEqF
	}
	return rEq
}

// atOp is the read of an item of kind: out of an array of ints or floats
// into its file, and otherwise as a value — in an arena slot, when arena.
func atOp(kind Kind, arena bool) rop {
	switch {
	case kind == IntKind:
		return rAtI
	case kind == FloatKind:
		return rAtF
	case kind == BoolKind:
		return rAtB
	case arena:
		return rAtA
	}
	return rAt
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
	made.boxes = l.boxes(count)
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
		l.kernel(kernel)
		return
	}
	// A Go function of a common shape is called straight from the files,
	// but in a fallback's candidate, where a call is made the ordinary way,
	// off the values.
	banked := function.banked
	if l.candidates > 0 {
		banked = nil
	}
	boxes := l.boxes(in.B)
	first := l.window(in.B)
	if banked != nil {
		boxes = nil
	}
	// A pure function's call needs no deadline: only a host's other calls do.
	l.out.hosts = l.out.hosts || !function.builtin && function.pure == nil
	l.out.foreign = l.out.foreign || !function.builtin
	l.out.calls = append(l.out.calls, rcall{
		fn: function, typ: in.Type, kind: in.Type.kind, args: first, argc: int32(in.B), dst: first, pc: int32(l.pc),
		kernel: function.builtin && !function.readsRun && function.Doc.Timeout == 0 && !function.Doc.Detached,
		direct: !function.builtin && function.EvalBatch == nil && function.Doc.Timeout == 0 && !function.Doc.Detached,
		banked: banked, deadline: !function.builtin && function.pure == nil, boxes: boxes,
	})
	l.emit(rCall, int32(len(l.out.calls)-1), 0, 0)
	l.push(first)
}

// kernel lowers a kernel function's call into its own operation, of the
// files its operands' and its answer's kinds say.
func (l *lowerer) kernel(kernel kernelOp) {
	op := kernel.op
	switch op {
	case rEq:
		op = eqOp(l.stack[len(l.stack)-1].kind)
	case rAt:
		// An array in an arena slot is read there.
		op = atOp(l.pushedKind(), l.flow.at[l.pc].uses)
	case rAtD:
		op = entryOp(l.pushedKind())
	}
	if kernel.unary {
		l.unary(op, 0)
	} else {
		l.binary(op, kernel.swap)
	}
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
	condition := l.pop().reg
	last := len(l.out.code) - 1
	// Only an operation of this block fuses — there may be none yet.
	if fused, ok := l.fusedBranch(last, condition); ok {
		ordering, origin, kinds := l.out.code[last], l.out.origins[last], l.out.kinds[last]
		l.out.code, l.out.origins, l.out.kinds = l.out.code[:last], l.out.origins[:last], l.out.kinds[:last]
		l.flush(0)
		l.emitOn(fused, ordering.a, ordering.b, 0, kinds)
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
	rEqI: rBranchEqI, rEqF: rBranchEqF, rFieldB: rBranchField,
}

// loopInit lowers the start of a loop: the source, and a fold's seed.
func (l *lowerer) loopInit(in Instruction) {
	seed := slot{reg: -1}
	if in.C != NoAccumulator {
		seed = l.pop()
	}
	source := l.pop()
	l.flush(0)
	loop := rloop{typ: in.Type, item: l.localBase + int32(in.B), key: -1, acc: -1, dst: l.top(), exit: int32(in.A), built: l.builtFor(l.pc),
		itemsInPlace: l.flow.at[l.pc].itemsInPlace, fields: l.flow.at[l.pc].itemFields}
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
	l.emitOn(rLoopInit, source.reg, seed.reg, index, opKinds{a: source.kind, b: seed.kind})
}

// fold lowers an item folded into its loop's answer: into the step's own
// operation when the kernel has one, and otherwise into a call of the answer
// and the item, moved into two registers of their own.
func (l *lowerer) fold(in Instruction) {
	loop := l.out.loops[l.open[len(l.open)-1]]
	acc := slot{reg: loop.acc, kind: loop.typ.kind}
	item := l.pop()
	l.clobber(acc.reg)
	step := l.functions[in.A]
	if kernel, ok := kernelOps[step.key]; ok && step.builtin && !kernel.unary {
		left, right := acc, item
		if kernel.swap {
			left, right = right, left
		}
		op := kernel.op
		if op == rEq {
			op = eqOp(left.kind)
		}
		l.emitOn(op, left.reg, right.reg, acc.reg, opKinds{a: left.kind, b: right.kind})
		return
	}
	scratch := l.scratch()
	l.move(acc.kind, acc.reg, scratch)
	l.move(item.kind, item.reg, scratch+1)
	var boxes []scalarArg
	for i, value := range []slot{acc, item} {
		if bankOf(value.kind) != valueBank {
			boxes = append(boxes, scalarArg{reg: scratch + int32(i), kind: value.kind})
		}
	}
	l.out.calls = append(l.out.calls, rcall{
		fn: step, typ: in.Type, kind: acc.kind, args: scratch, argc: 2, dst: acc.reg, pc: int32(l.pc),
		kernel: !step.readsRun, boxes: boxes,
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
	value := l.pop()
	if in.A == 1 {
		// A dictionary's entry is read out of the file its kind says, c.
		key := l.pop()
		l.emitOn(rCollect, value.reg, key.reg, int32(value.kind), opKinds{a: value.kind})
		return
	}
	l.emitOn(collectOp(value.kind), value.reg, -1, 0, opKinds{a: value.kind})
}

// collectOp is what adds an item of kind to an array: an int or a bool, or
// a float, out of its file, and anything else as a value.
func collectOp(kind Kind) rop {
	switch bankOf(kind) {
	case intBank:
		return rCollectI
	case floatBank:
		return rCollectF
	}
	return rCollect
}

// collectNext is the collect of an item that the loop's next makes one
// operation with.
var collectNext = map[rop]rop{rCollect: rCollectNext, rCollectI: rCollectNextI, rCollectF: rCollectNextF}

// loopNext lowers the end of a loop's body. An unkeyed collect just before
// it becomes one operation with it.
func (l *lowerer) loopNext(in Instruction) {
	l.flush(0)
	loop := l.open[len(l.open)-1]
	l.open = l.open[:len(l.open)-1]
	if l.pc+1 < len(l.code) && l.code[l.pc+1].Op == OpLoopSpread {
		l.out.loops[loop].spread, l.spread = true, true
	}
	collect := len(l.out.code) - 1
	last := &l.out.code[collect]
	switch fuses := collectNext[last.op] != rInvalid && last.b < 0; {
	case fuses && int32(collect) >= l.blockStart:
		// The collect goes on by itself; after the loop is what comes next.
		last.op, last.c = collectNext[last.op], int32(collect+1)
		l.jumpTo(operandB, in.A)
	case fuses:
		// A branch skips the collect to this next: the collect goes on by
		// itself all the same, the next stays for the items the branch
		// sends it, and after the loop is past both.
		last.op = collectNext[last.op]
		l.fixups = append(l.fixups, fixup{at: collect, operand: operandB, target: in.A})
		l.emit(rLoopNext, 0, 0, loop)
		l.jumpTo(operandA, in.A)
		l.out.code[collect].c = int32(len(l.out.code))
	default:
		l.emit(rLoopNext, 0, 0, loop)
		l.jumpTo(operandA, in.A)
	}
	l.push(l.out.loops[loop].dst)
}
