package lang

import "fmt"

// loopFrame tracks one active `for` iteration: the input items, the local slot
// holding the current item, and the array being built.
// noAccumulator marks a mapping loop (for): it collects into output instead of
// folding into an accumulator slot.
const noAccumulator = -1

// tailCall marks a recur whose value is the value of the whole program.
const tailCall = 1

type loopFrame struct {
	items      []Value
	index      int
	local      int
	acc        int
	output     []Value
	resultType Type
}

func (l *loopFrame) folds() bool { return l.acc != noAccumulator }

// frame is one activation of the bytecode: its own stack, locals and loops,
// sharing the fuel budget with the whole run.
type frame struct {
	runtime      *Runtime
	args         []Value
	stack        []Value
	locals       []Value
	localSet     []bool
	loops        []loopFrame
	fuel         *uint64
	fuelLeft     uint64
	fuelCell     uint64 // budget storage for a top-level run, kept off the heap
	maxStack     int
	maxRecursion int
	depth        int
	stackArray   [16]Value
	argsArray    [8]Value
}

// reset rebinds a frame — pooled or fresh — to one activation.
func (f *frame) reset(runtime *Runtime, args []Value, fuel *uint64, maxStack, maxRecursion, depth int) {
	f.runtime = runtime
	f.args = args
	f.fuel = fuel
	f.fuelLeft = *fuel
	f.maxStack = maxStack
	f.maxRecursion = maxRecursion
	f.depth = depth
	f.stack = f.stackArray[:0]
	f.loops = f.loops[:0]
	locals := runtime.artifact.Locals
	if cap(f.locals) < locals {
		f.locals = make([]Value, locals)
		f.localSet = make([]bool, locals)
	}
	f.locals = f.locals[:locals]
	f.localSet = f.localSet[:locals]
	for i := range f.localSet {
		f.localSet[i] = false
	}
}

// argSpace returns storage for n top-level arguments, using the frame's inline
// array when it is large enough.
func (f *frame) argSpace(n int) []Value {
	if n <= len(f.argsArray) {
		return f.argsArray[:n]
	}
	return make([]Value, n)
}

// release drops references so a pooled frame keeps nothing alive.
func (f *frame) release() {
	f.runtime = nil
	f.args = nil
	f.fuel = nil
	clearValues(f.stackArray[:])
	clearValues(f.argsArray[:])
	clearValues(f.locals)
	f.loops = f.loops[:0]
}

func clearValues(values []Value) {
	for i := range values {
		values[i] = Value{}
	}
}

// guardedRun contains a panicking extension for the whole activation, which
// keeps the recover out of the per-call path.
func (f *frame) guardedRun() (value Value, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			*f.fuel = f.fuelLeft
			err = fmt.Errorf("extension panicked: %v", recovered)
		}
	}()
	return f.run()
}

func (f *frame) run() (Value, error) {
	code := f.runtime.artifact.Instructions
	for pc := 0; pc < len(code); {
		if f.fuelLeft == 0 {
			*f.fuel = 0
			return Value{}, fmt.Errorf("execution fuel exhausted at instruction %d", pc)
		}
		f.fuelLeft--
		next, err := f.step(pc, code[pc])
		if err != nil {
			*f.fuel = f.fuelLeft
			return Value{}, err
		}
		pc = next
	}
	*f.fuel = f.fuelLeft
	return f.result()
}

// step executes one instruction and returns the next program counter.
func (f *frame) step(pc int, instruction Instruction) (int, error) {
	switch instruction.Op {
	case OpConstant:
		return pc + 1, f.push(f.runtime.constants[instruction.A])
	case OpLoadArg:
		return pc + 1, f.push(f.args[instruction.A])
	case OpLoadLocal:
		return pc + 1, f.loadLocal(instruction)
	case OpMakeArray:
		return pc + 1, f.makeArray(instruction)
	case OpMakeDict:
		return pc + 1, f.makeDict(instruction)
	case OpEqual:
		return pc + 1, f.equal()
	case OpCall:
		return pc + 1, f.call(instruction)
	case OpRecur:
		return f.recur(pc, instruction)
	case OpLoopInit:
		return f.loopInit(pc, instruction)
	case OpLoopCollect:
		return pc + 1, f.loopCollect(instruction)
	case OpLoopNext:
		return f.loopNext(pc, instruction)
	case OpJumpIfFalse:
		return f.jumpIfFalse(pc, instruction)
	case OpJump:
		return instruction.A, nil
	default:
		return 0, fmt.Errorf("unknown opcode %q", instruction.Op)
	}
}

func (f *frame) result() (Value, error) {
	if len(f.stack) != 1 {
		return Value{}, fmt.Errorf("program finished with %d stack values", len(f.stack))
	}
	result := f.stack[0]
	if !result.hasType(f.runtime.artifact.Result) {
		return Value{}, fmt.Errorf("program returned %s, artifact declares %s", result.Type(), f.runtime.artifact.Result)
	}
	return result, nil
}

func (f *frame) push(value Value) error {
	if len(f.stack) >= f.maxStack {
		return fmt.Errorf("stack limit %d exceeded", f.maxStack)
	}
	f.stack = append(f.stack, value)
	return nil
}

// popN returns a window onto the stack itself: no copy. The window stays valid
// until the next push, which is enough for every consumer here — and extension
// functions are documented not to retain it.
func (f *frame) popN(count int) ([]Value, error) {
	if count < 0 || len(f.stack) < count {
		return nil, fmt.Errorf("stack underflow: need %d values, have %d", count, len(f.stack))
	}
	start := len(f.stack) - count
	values := f.stack[start:len(f.stack):len(f.stack)]
	f.stack = f.stack[:start]
	return values, nil
}

func (f *frame) pop1() (Value, error) {
	if len(f.stack) == 0 {
		return Value{}, fmt.Errorf("stack underflow: need 1 value, have 0")
	}
	last := len(f.stack) - 1
	value := f.stack[last]
	f.stack = f.stack[:last]
	return value, nil
}

func (f *frame) bindLocal(slot int, value Value) {
	f.locals[slot] = value
	f.localSet[slot] = true
}

func (f *frame) loadLocal(instruction Instruction) error {
	if !f.localSet[instruction.A] {
		return fmt.Errorf("local %d is not bound", instruction.A)
	}
	return f.push(f.locals[instruction.A])
}

func (f *frame) makeArray(instruction Instruction) error {
	items, err := f.popN(instruction.A)
	if err != nil {
		return err
	}
	value, err := Array(*instruction.Type.Elem, items)
	if err != nil {
		return fmt.Errorf("make array: %w", err)
	}
	return f.push(value)
}

func (f *frame) makeDict(instruction Instruction) error {
	items, err := f.popN(instruction.A)
	if err != nil {
		return err
	}
	entries := make(map[string]Value, len(items))
	for i, item := range items {
		entries[instruction.Keys[i]] = item
	}
	value, err := Dict(*instruction.Type.Elem, entries)
	if err != nil {
		return fmt.Errorf("make dictionary: %w", err)
	}
	return f.push(value)
}

func (f *frame) equal() error {
	values, err := f.popN(2)
	if err != nil {
		return err
	}
	if !values[0].hasType(values[1].Type()) {
		return fmt.Errorf("equality requires one type, got %s and %s", values[0].Type(), values[1].Type())
	}
	return f.push(Bool(values[0].Equal(values[1])))
}

func (f *frame) call(instruction Instruction) error {
	function := f.runtime.functions[instruction.A]
	if f.fuelLeft < function.Cost {
		return fmt.Errorf("execution fuel exhausted before %s", function.Name)
	}
	f.fuelLeft -= function.Cost
	callArgs, err := f.popN(instruction.B)
	if err != nil {
		return err
	}
	value, err := function.Eval(callArgs)
	if err != nil {
		return fmt.Errorf("%s: %w", function.Name, err)
	}
	if !value.hasType(*instruction.Type) {
		return fmt.Errorf("%s returned %s, contract requires %s", function.Name, value.Type(), *instruction.Type)
	}
	return f.push(value)
}

func (f *frame) recur(pc int, instruction Instruction) (int, error) {
	callArgs, err := f.popN(instruction.B)
	if err != nil {
		return 0, err
	}
	for i, param := range f.runtime.artifact.Args {
		if !callArgs[i].hasType(param.Type) {
			return 0, fmt.Errorf("recursive argument %q is %s, want %s", param.Name, callArgs[i].Type(), param.Type)
		}
	}
	if instruction.C == tailCall {
		return 0, f.restart(callArgs)
	}
	*f.fuel = f.fuelLeft
	value, err := f.runtime.execute(callArgs, f.fuel, f.maxStack, f.maxRecursion, f.depth+1)
	f.fuelLeft = *f.fuel
	if err != nil {
		return 0, err
	}
	if !value.hasType(*instruction.Type) {
		return 0, fmt.Errorf("recursion returned %s, contract requires %s", value.Type(), *instruction.Type)
	}
	return pc + 1, f.push(value)
}

// restart rebinds the arguments and jumps back to instruction 0. Fuel still
// counts every instruction, so a non-terminating tail loop is caught by fuel
// rather than by the recursion limit.
func (f *frame) restart(args []Value) error {
	copy(f.args, args)
	f.stack = f.stack[:0]
	f.loops = f.loops[:0]
	for i := range f.localSet {
		f.localSet[i] = false
	}
	return nil
}

// loopInit consumes the source array — plus the initial accumulator when the
// loop folds — and either enters the loop or yields the empty result.
func (f *frame) loopInit(pc int, instruction Instruction) (int, error) {
	folds := instruction.C != noAccumulator
	operands := 1
	if folds {
		operands = 2
	}
	values, err := f.popN(operands)
	if err != nil {
		return 0, err
	}
	if values[0].kind != ArrayKind {
		return 0, fmt.Errorf("loop source is %s, want array", values[0].Type())
	}
	items := values[0].items
	seed, err := f.loopSeed(instruction, values, folds)
	if err != nil {
		return 0, err
	}
	if len(items) == 0 {
		return instruction.A, f.push(seed)
	}
	if folds {
		f.bindLocal(instruction.C, seed)
	}
	f.bindLocal(instruction.B, items[0])
	loop := loopFrame{
		items: items, local: instruction.B, acc: instruction.C,
		resultType: cloneType(*instruction.Type),
	}
	if !loop.folds() {
		loop.output = make([]Value, 0, len(items))
	}
	f.loops = append(f.loops, loop)
	return pc + 1, nil
}

// loopSeed is the value an empty source produces: the initial accumulator, or
// an empty array.
func (f *frame) loopSeed(instruction Instruction, values []Value, folds bool) (Value, error) {
	if !folds {
		return Array(*instruction.Type.Elem, nil)
	}
	if !values[1].hasType(*instruction.Type) {
		return Value{}, fmt.Errorf("loop init is %s, want %s", values[1].Type(), *instruction.Type)
	}
	return values[1], nil
}

// loopCollect stores one body result: folded into the accumulator, or appended
// to the output array.
func (f *frame) loopCollect(instruction Instruction) error {
	if len(f.loops) == 0 {
		return fmt.Errorf("loop collect without active loop")
	}
	value, err := f.pop1()
	if err != nil {
		return err
	}
	if !value.hasType(*instruction.Type) {
		return fmt.Errorf("loop body returned %s, want %s", value.Type(), *instruction.Type)
	}
	loop := &f.loops[len(f.loops)-1]
	if loop.folds() {
		f.bindLocal(loop.acc, value)
		return nil
	}
	loop.output = append(loop.output, value)
	return nil
}

func (f *frame) loopNext(pc int, instruction Instruction) (int, error) {
	if len(f.loops) == 0 {
		return 0, fmt.Errorf("loop next without active loop")
	}
	index := len(f.loops) - 1
	loop := &f.loops[index]
	loop.index++
	if loop.index < len(loop.items) {
		f.bindLocal(loop.local, loop.items[loop.index])
		return instruction.A, nil
	}
	result, err := f.loopResult(loop)
	if err != nil {
		return 0, err
	}
	f.localSet[loop.local] = false
	if loop.folds() {
		f.localSet[loop.acc] = false
	}
	f.loops = f.loops[:index]
	return pc + 1, f.push(result)
}

// loopResult hands the accumulated slice over directly: every element was type
// checked by loop_collect on its way in, so Array's re-checking copy is waste.
func (f *frame) loopResult(loop *loopFrame) (Value, error) {
	if loop.folds() {
		return f.locals[loop.acc], nil
	}
	return Value{kind: ArrayKind, items: loop.output, elemType: *loop.resultType.Elem}, nil
}

func (f *frame) jumpIfFalse(pc int, instruction Instruction) (int, error) {
	value, err := f.pop1()
	if err != nil {
		return 0, err
	}
	condition, ok := value.Bool()
	if !ok {
		return 0, fmt.Errorf("if condition is %s, want bool", value.Type())
	}
	if condition {
		return pc + 1, nil
	}
	return instruction.A, nil
}
