package machine

import (
	"errors"
	"math"
	"slices"
)

// Lowering turns the verified stack bytecode into the register form, once,
// when the artifact is loaded. The verifier has proved how deep the stack is
// on entering each instruction, so stack position k is register k of the
// stack's part throughout, and every value's place is known before the
// program runs.
//
// A value that is only read — a constant, an argument, a local — is not
// copied onto the stack: the lowering remembers which register holds it, and
// an operation reads it there. It is written to its stack register only where
// paths meet or leave, where a builder or a call takes a run of registers, or
// before the local it lives in is written.
//
// Fuel is charged a basic block at a time, on entering it: one for each stack
// instruction in the block, and each call's cost. A run that finishes pays
// what the stack machine paid, to the unit; one without the fuel for a block
// stops on entering it.

// lowerer is the state of one lowering.
type lowerer struct {
	code      []Instruction
	constants []Value
	functions []*RegisteredFunction
	depths    []int32
	leaders   []bool
	out       regProgram
	// stack is the register holding each stack position's value.
	stack []int32
	// at is the operation each leader starts at; fixups the operands that
	// name a stack instruction until every leader has one.
	at     []int32
	fixups []fixup
	// open is the loops the lowering is inside, innermost last; spread is
	// set between an inner clause's end and the spread after it.
	open                 []int32
	spread               bool
	locals               int32
	stackBase, localBase int32
	// blockStart is the operation the current block starts at, blockCost
	// its fuel and blockSpent how much of that the instructions lowered so
	// far in it charge.
	blockStart            int32
	blockCost, blockSpent uint64
	pc                    int
	err                   error
	// flow is where the program's arrays go, and slots the arena slot each
	// producer builds in.
	flow  flow
	slots map[int]int32
}

// fixup is an operand of an operation that names the stack instruction
// target.
type fixup struct {
	at      int
	operand operand
	target  int
}

type operand uint8

const (
	operandA operand = iota
	operandB
	operandC
)

// lower makes the register form of a verified program.
func lower(artifact *Artifact, constants []Value, functions []*RegisteredFunction, proof proof) (regProgram, error) {
	parts := &artifact.parts
	nconst, args := int32(len(parts.Constants)), int32(len(parts.Args))
	l := &lowerer{
		code: parts.Instructions, constants: constants, functions: functions, depths: proof.depths, leaders: leadersOf(parts.Instructions, functions),
		at: make([]int32, len(parts.Instructions)+1), stackBase: nconst + args,
	}
	l.localBase = l.stackBase + int32(proof.depth)
	l.locals = int32(parts.Locals)
	l.out.args, l.out.size = nconst, l.localBase+int32(parts.Locals)
	l.out.scalar = scalarProgram(parts)
	l.flow, l.slots = flowOf(parts, functions), map[int]int32{}
	l.out.fieldOnly, l.out.viewOnly = l.flow.fieldOnly, l.flow.viewOnly
	for pc := range l.code {
		l.at[pc] = -1
	}
	for pc := 0; pc <= len(l.code); pc++ {
		if l.depths[pc] < 0 {
			continue
		}
		l.pc = pc
		if l.leaders[pc] {
			l.startBlock(pc)
		}
		if pc == len(l.code) {
			l.emit(rHalt, l.stack[0], 0, 0)
			break
		}
		l.instruction(l.code[pc])
		if l.code[pc].Op != OpJump && l.leaders[pc+1] {
			l.flush(0)
		}
	}
	if l.err != nil {
		return regProgram{}, l.err
	}
	if err := l.patch(); err != nil {
		return regProgram{}, err
	}
	l.vectorize()
	return l.out, nil
}

// scalarProgram reports a program whose every value is a bool, an int or a
// float: its constants, its arguments and what each instruction makes. Such
// a program's registers never hold a pointer, and a frame need not clear
// them for the collector.
func scalarProgram(parts *ArtifactParts) bool {
	scalar := func(typ Type) bool { return typ.kind == BoolKind || typ.kind == IntKind || typ.kind == FloatKind }
	for _, constant := range parts.Constants {
		if !scalar(constant.Type) {
			return false
		}
	}
	for _, param := range parts.Args {
		if !scalar(param.typ) {
			return false
		}
	}
	for _, in := range parts.Instructions {
		if in.Type != nil && !scalar(*in.Type) {
			return false
		}
	}
	return true
}

// builtFor is where the producer at pc builds its array: its arena slot,
// the answer's slot, or neither.
func (l *lowerer) builtFor(pc int) built {
	where := built{arena: -1, dest: -1}
	if l.flow.arena[pc] {
		slot, ok := l.slots[pc]
		if !ok {
			slot = int32(len(l.slots))
			l.slots[pc] = slot
			l.out.arenas = len(l.slots)
		}
		where.arena = slot
	}
	if dest, ok := l.flow.dest[pc]; ok {
		where.dest = int32(dest)
		l.out.dests = max(l.out.dests, dest+1)
	}
	return where
}

// leadersOf marks where a basic block starts: the start, every instruction a
// jump lands on, and every one after a jump. So does every one after a call
// in a fallback's candidate that can fail the way fallback takes: the stack
// machine had charged nothing past the call when the failure left the block,
// and a block ending there charged no more either.
func leadersOf(code []Instruction, functions []*RegisteredFunction) []bool {
	leaders := joinsOf(code)
	candidates := 0
	for pc, in := range code {
		switch in.Op {
		case OpJump, OpJumpIfFalse, OpLoopInit, OpLoopNext, OpLoopBreak:
			leaders[pc+1] = true
		case OpBeginFallback:
			candidates++
		case OpEndFallback:
			candidates--
		case OpCall:
			if function := functions[in.A]; candidates > 0 && (!function.builtin || function.readsRun) {
				leaders[pc+1] = true
			}
		}
	}
	return leaders
}

// patch resolves every operand that names a stack instruction.
func (l *lowerer) patch() error {
	for _, fix := range l.fixups {
		target := l.at[fix.target]
		if target < 0 {
			return errors.New("internal error: a jump lands where no block starts")
		}
		in := &l.out.code[fix.at]
		*[...]*int32{&in.a, &in.b, &in.c}[fix.operand] = target
	}
	for i := range l.out.loops {
		exit := &l.out.loops[i].exit
		if *exit = l.at[*exit]; *exit < 0 {
			return errors.New("internal error: a loop exits where no block starts")
		}
	}
	return nil
}

// startBlock begins the block at pc: every value is in its stack register,
// and the block's fuel is charged.
func (l *lowerer) startBlock(pc int) {
	l.at[pc] = int32(len(l.out.code))
	l.blockStart = l.at[pc]
	l.stack = l.stack[:0]
	for k := range l.depths[pc] {
		l.stack = append(l.stack, l.stackBase+k)
	}
	cost, deepest := l.blockOf(pc)
	l.blockCost, l.blockSpent = cost, 0
	if cost > 0 {
		l.emit(rFuel, deepest, int32(uint32(cost>>32)), int32(uint32(cost)))
	}
}

// blockOf is the fuel of the block at pc and the deepest its stack gets.
func (l *lowerer) blockOf(pc int) (uint64, int32) {
	var cost uint64
	deepest := l.depths[pc]
	for end := pc; end < len(l.code) && (end == pc || !l.leaders[end]); end++ {
		cost = addFuel(cost, l.costOf(end))
		deepest = max(deepest, l.depths[end]+stackGrowth(l.code[end]))
	}
	return cost, deepest
}

// costOf is what the stack machine charged for the instruction at pc.
func (l *lowerer) costOf(pc int) uint64 {
	if in := l.code[pc]; in.Op == OpCall {
		return addFuel(1, l.functions[in.A].Doc.Cost)
	}
	return 1
}

// addFuel adds costs, saturating: a block no budget covers stays one.
func addFuel(a, b uint64) uint64 {
	if a > math.MaxUint64-b {
		return math.MaxUint64
	}
	return a + b
}

// stackGrowth is how much deeper than on entering an instruction the stack is
// once it is done: a push is one, and a loop's end pushes its result.
func stackGrowth(in Instruction) int32 {
	switch in.Op {
	case OpConstant, OpLoadArg, OpLoadLocal, OpLoopNext:
		return 1
	case OpCall:
		return int32(1 - in.B)
	case OpMakeArray, OpMakeDict, OpMakeRecord:
		return int32(1 - in.A)
	}
	return 0
}

func (l *lowerer) emit(op rop, a, b, c int32) {
	l.out.code = append(l.out.code, rinstr{op: op, a: a, b: b, c: c})
	l.out.origins = append(l.out.origins, int32(l.pc))
}

// jumpTo makes an operand of the last operation name the stack instruction
// target, once that has been lowered.
func (l *lowerer) jumpTo(op operand, target int) {
	l.fixups = append(l.fixups, fixup{at: len(l.out.code) - 1, operand: op, target: target})
}

func (l *lowerer) push(reg int32) { l.stack = append(l.stack, reg) }

func (l *lowerer) pop() int32 {
	reg := l.stack[len(l.stack)-1]
	l.stack = l.stack[:len(l.stack)-1]
	return reg
}

// top is the register the next push goes in.
func (l *lowerer) top() int32 { return l.stackBase + int32(len(l.stack)) }

// flush puts every value from stack position from up in its stack register.
func (l *lowerer) flush(from int) {
	for k := from; k < len(l.stack); k++ {
		if home := l.stackBase + int32(k); l.stack[k] != home {
			l.emit(rMove, l.stack[k], 0, home)
			l.stack[k] = home
		}
	}
}

// window pops n values into their stack registers, in order, and returns the
// first register.
func (l *lowerer) window(n int) int32 {
	l.flush(len(l.stack) - n)
	l.stack = l.stack[:len(l.stack)-n]
	return l.top()
}

// clobber is about to write reg: a value on the stack that is read there
// moves to its stack register first.
func (l *lowerer) clobber(reg int32) {
	for k, held := range l.stack {
		if held == reg {
			home := l.stackBase + int32(k)
			l.emit(rMove, reg, 0, home)
			l.stack[k] = home
		}
	}
}

// store pops a value into reg. A value the last operation just made is made
// there instead, when nothing else reads reg.
func (l *lowerer) store(reg int32) {
	value := l.pop()
	if l.retarget(value, reg) {
		return
	}
	l.clobber(reg)
	if value != reg {
		l.emit(rMove, value, 0, reg)
	}
}

// retarget has the last operation, which wrote value, write reg instead:
// one of this block's, writing the register value is on top of the stack
// in, when no value on the stack is read from reg.
func (l *lowerer) retarget(value, reg int32) bool {
	last := int32(len(l.out.code) - 1)
	if last <= l.blockStart || value != l.top() || slices.Contains(l.stack, reg) {
		return false
	}
	in := &l.out.code[last]
	switch {
	case in.op == rCall && l.out.calls[in.a].dst == value:
		l.out.calls[in.a].dst = reg
	case writesC(in.op) && in.c == value:
		in.c = reg
	default:
		return false
	}
	return true
}

// writesC reports an operation whose c is the register it writes, and
// nothing else.
func writesC(op rop) bool {
	switch op {
	case rMove, rAddI, rSubI, rMulI, rDivI, rModI, rAddF, rSubF, rMulF, rDivF, rConcat,
		rLtI, rLeI, rLtF, rLeF, rLtS, rLeS, rEq, rLen, rAt, rField:
		return true
	}
	return false
}
