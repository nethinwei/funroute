package machine

import (
	"context"
	"errors"
	"fmt"
	"slices"
)

// NoAccumulator marks a loop instruction as a mapping rather than a fold, and
// NoKey marks it as walking an array rather than a dictionary. The compiler
// emits them; the frame reads them.
const (
	NoAccumulator = -1
	NoKey         = -1
)

// loopFrame tracks one active loop: its source, the slots it binds and, for a
// mapping, the array being built.
type loopFrame struct {
	source   Value
	keys     []string // non-nil for a dictionary walk, sorted
	length   int
	index    int
	local    int
	keyLocal int
	acc      int
	output   arrayBuilder
	// collected is where a comprehension that produces a dictionary puts its
	// keys; the values go through output like any other comprehension's.
	collected []string
}

type fallbackFrame struct {
	target int
	stack  int
	loops  int
	scopes int
}

// item is the value the loop binds at index: an array item, or the entry
// under the index-th sorted key.
func (l *loopFrame) item(index int) Value {
	if l.keys == nil {
		return l.source.at(index)
	}
	value, _ := l.source.lookup(l.keys[index])
	return value
}

func (l *loopFrame) folds() bool { return l.acc != NoAccumulator }

// frame is one activation of the bytecode: its own stack, locals and loops.
// Without recursion there is exactly one activation per run.
type frame struct {
	runtime   *Runtime
	args      []Value
	stack     []Value
	locals    []Value
	loops     []loopFrame
	fallbacks []fallbackFrame
	// fxQuotes are the quotes of every using the run is inside, and
	// fxMarks where each using's begin, the innermost last.
	fxQuotes   []Value
	fxMarks    []int
	fxCtx      fxContext // handed to a kernel function that reads rates
	fuel       *uint64
	fuelLeft   uint64
	fuelCell   uint64 // budget storage for a top-level run, kept off the heap
	maxStack   int
	reserved   int
	overflow   int // how far past the reservation this activation pushed
	argsUsed   int
	ctx        context.Context    // the request's budget; extension calls see it
	deadline   bool               // whether ctx can expire at all; Background cannot
	prefetched map[int]Prefetched // a Batch's answers for hoisted calls, by pc
	stackArray [16]Value
	argsArray  [8]Value
	// fxQuotesArray and fxMarksArray hold the usings of a run as deep as
	// most rules go, so a fresh frame converts without allocating either.
	fxQuotesArray [4]Value
	fxMarksArray  [4]int
}

// reset rebinds a frame — pooled or fresh — to one activation.
func (f *frame) reset(runtime *Runtime, args []Value, fuel *uint64, maxStack int) {
	f.runtime = runtime
	f.args = args
	f.fuel = fuel
	f.fuelLeft = *fuel
	f.maxStack = maxStack
	f.stack = f.stackArray[:0]
	f.loops = f.loops[:0]
	f.fallbacks = f.fallbacks[:0]
	if f.fxQuotes == nil {
		f.fxQuotes, f.fxMarks = f.fxQuotesArray[:0], f.fxMarksArray[:0]
	}
	f.fxQuotes, f.fxMarks = f.fxQuotes[:0], f.fxMarks[:0]
	f.fxCtx.frame = f
	// Loading proved how deep the stack gets. Reserving it here is what lets
	// push skip its bounds check; a run whose limit is below the figure keeps
	// the per-push check instead.
	f.reserved = runtime.depth
	if f.reserved > maxStack {
		f.reserved = 0
	}
	f.overflow = 0
	f.argsUsed = len(args)
	if f.reserved > cap(f.stack) {
		f.stack = make([]Value, 0, f.reserved)
	}
	locals := runtime.artifact.parts.Locals
	if locals == 0 {
		// Constant folding leaves many programs with no locals at all; there
		// is nothing to clear.
		f.locals = f.locals[:0]
		return
	}
	if cap(f.locals) < locals {
		f.locals = make([]Value, locals)
	}
	f.locals = f.locals[:locals]
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
//
// It clears only the part of each inline array that was written. The tail was
// never touched, so clearing it would be pure work — and these arrays hold
// pointers, which makes that work the expensive kind.
func (f *frame) release() {
	f.runtime = nil
	f.args = nil
	f.fuel = nil
	f.ctx = nil
	f.prefetched = nil
	clear(f.stackArray[:f.stackUsed()])
	clear(f.argsArray[:min(f.argsUsed, len(f.argsArray))])
	clear(f.locals)
	f.loops = f.loops[:0]
	f.fallbacks = f.fallbacks[:0]
	f.dropScopes(0)
	f.fxCtx.Context = nil
}

// stackUsed is how much of the inline stack array may hold a value: the
// compiler's figure, or how far an activation actually pushed past it.
func (f *frame) stackUsed() int {
	return min(max(f.overflow, f.reserved), len(f.stackArray))
}

// guardedRun contains a panicking extension for the whole activation, which
// keeps the recover out of the per-call path.
func (f *frame) guardedRun() (value Value, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			*f.fuel = f.fuelLeft
			err = fmt.Errorf("%w: extension panicked: %v", ErrExtension, recovered)
		}
	}()
	return f.run()
}

func (f *frame) run() (Value, error) {
	code := f.runtime.artifact.parts.Instructions
	for pc := 0; pc < len(code); {
		if f.fuelLeft == 0 {
			*f.fuel = 0
			return Value{}, fmt.Errorf("%w at instruction %d", ErrFuel, pc)
		}
		f.fuelLeft--
		next, err := f.step(pc, code[pc])
		if err != nil {
			if target, caught := f.catchFallback(err); caught {
				pc = target
				continue
			}
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
	case OpStoreLocal:
		return pc + 1, f.storeLocal(instruction)
	case OpMakeArray, OpMakeDict, OpMakeRecord:
		return pc + 1, f.makeValue(instruction)
	case OpField:
		return pc + 1, f.field(instruction)
	case OpRecordWith:
		return pc + 1, f.recordWith(pc)
	case OpEqual:
		return pc + 1, f.equal()
	case OpCall:
		return pc + 1, f.call(pc, instruction)
	case OpLoopInit:
		return f.loopInit(pc, instruction)
	case OpLoopCollect:
		return pc + 1, f.loopCollect(instruction)
	case OpLoopSpread:
		return pc + 1, f.loopSpread(instruction)
	case OpLoopNext:
		return f.loopNext(pc, instruction)
	case OpJumpIfFalse:
		return f.jumpIfFalse(pc, instruction)
	case OpJump:
		return instruction.A, nil
	case OpBeginFallback:
		return f.beginFallback(pc, instruction)
	case OpEndFallback:
		return f.endFallback(pc)
	case OpFxPush:
		return pc + 1, f.pushScope(instruction)
	case OpFxPop:
		f.popScope()
		return pc + 1, nil
	default:
		return 0, fmt.Errorf("unknown opcode %q", instruction.Op)
	}
}

func (f *frame) beginFallback(pc int, instruction Instruction) (int, error) {
	f.fallbacks = append(f.fallbacks, fallbackFrame{
		target: instruction.A, stack: len(f.stack), loops: len(f.loops), scopes: len(f.fxMarks),
	})
	return pc + 1, nil
}

func (f *frame) endFallback(pc int) (int, error) {
	f.fallbacks = f.fallbacks[:len(f.fallbacks)-1]
	return pc + 1, nil
}

func (f *frame) catchFallback(err error) (int, bool) {
	if class, _ := classOf(err); len(f.fallbacks) == 0 || !class.fallback {
		return 0, false
	}
	last := len(f.fallbacks) - 1
	handler := f.fallbacks[last]
	f.fallbacks = f.fallbacks[:last]
	clear(f.stack[handler.stack:])
	f.stack = f.stack[:handler.stack]
	clear(f.loops[handler.loops:])
	f.loops = f.loops[:handler.loops]
	f.dropScopes(handler.scopes)
	return handler.target, true
}

// result is what the program left: loading proved it is its result alone.
func (f *frame) result() (Value, error) { return f.stack[0], nil }

func (f *frame) push(value Value) error {
	// Within the reserved depth the bound is already proven, so the common
	// case is a single append with no comparison against the limit.
	if len(f.stack) < f.reserved {
		f.stack = append(f.stack, value)
		return nil
	}
	if len(f.stack) >= f.maxStack {
		return fmt.Errorf("stack limit %d exceeded", f.maxStack)
	}
	f.stack = append(f.stack, value)
	// Past the reservation, remember how far we got: release clears exactly
	// this much, and nothing above it was ever written.
	if len(f.stack) > f.overflow {
		f.overflow = len(f.stack)
	}
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
		return Value{}, errors.New("stack underflow: need 1 value, have 0")
	}
	last := len(f.stack) - 1
	value := f.stack[last]
	f.stack = f.stack[:last]
	return value, nil
}

// loopKeys lists a dictionary walk's keys in sorted order: the language
// guarantees a program replays identically and Go's map order does not. An
// array walk needs no key list.
func loopKeys(source Value, keySlot int) []string {
	if keySlot == NoKey {
		return nil
	}
	return source.keys()
}

// bindItem binds the loop's value variable, plus its key variable when walking
// a dictionary.
func (f *frame) bindItem(loop *loopFrame, index int) {
	f.bindLocal(loop.local, loop.item(index))
	if loop.keyLocal != NoKey {
		f.bindLocal(loop.keyLocal, String(loop.keys[index]))
	}
}

func (f *frame) bindLocal(slot int, value Value) { f.locals[slot] = value }

// loadLocal pushes a local, which loading proved bound wherever it is read.
func (f *frame) loadLocal(instruction Instruction) error { return f.push(f.locals[instruction.A]) }

// storeLocal binds a let binding's value to its slot.
func (f *frame) storeLocal(instruction Instruction) error {
	value, err := f.pop1()
	if err != nil {
		return err
	}
	f.bindLocal(instruction.A, value)
	return nil
}

// makeValue builds an array, a dictionary or a record from the top A values.
func (f *frame) makeValue(instruction Instruction) error {
	items, err := f.popN(instruction.A)
	if err != nil {
		return err
	}
	// Loading proved every item of the type it goes in as, so nothing is
	// checked on the way.
	switch instruction.Op {
	case OpMakeArray:
		builder := newArrayBuilder(*instruction.Type.elem, len(items))
		for _, item := range items {
			builder.add(item)
		}
		return f.push(builder.finish())
	case OpMakeDict:
		return f.push(packDict(*instruction.Type.elem, zipEntries(instruction.Keys, items)))
	}
	return f.push(Value{kind: RecordKind, box: &recordValue{typ: *instruction.Type, fields: slices.Clone(items)}})
}

// zipEntries pairs keys with values, one for one.
func zipEntries(keys []string, values []Value) map[string]Value {
	entries := make(map[string]Value, len(values))
	for i, value := range values {
		entries[keys[i]] = value
	}
	return entries
}

func (f *frame) field(instruction Instruction) error {
	value, err := f.pop1()
	if err != nil {
		return err
	}
	record, ok := value.box.(*recordValue)
	if !ok {
		return errNotARecord
	}
	return f.push(record.fields[instruction.A])
}

// errNotARecord is a field read or an update of what is not a record, which
// loading proved no program does.
var errNotARecord = errors.New("internal error: not a record")

// recordWith copies the record's fields once and replaces the ones named. The
// copy shares the record's type: the type does not change, so neither does it.
func (f *frame) recordWith(pc int) error {
	indexes := f.runtime.updates[pc]
	values, err := f.popN(len(indexes))
	if err != nil {
		return err
	}
	base, err := f.pop1()
	if err != nil {
		return err
	}
	// Loading proved the base the instruction's record and each value its
	// field's type.
	record, ok := base.box.(*recordValue)
	if !ok {
		return errNotARecord
	}
	fields := slices.Clone(record.fields)
	for i, index := range indexes {
		fields[index] = values[i]
	}
	return f.push(Value{kind: RecordKind, box: &recordValue{typ: record.typ, fields: fields}})
}

func (f *frame) equal() error {
	values, err := f.popN(2)
	if err != nil {
		return err
	}
	result, err := compareEqual(values[0], values[1])
	if err != nil {
		return err
	}
	return f.push(result)
}

func (f *frame) loopInit(pc int, instruction Instruction) (int, error) {
	folds := instruction.C != NoAccumulator
	operands := 1
	if folds {
		operands = 2
	}
	values, err := f.popN(operands)
	if err != nil {
		return 0, err
	}
	keys := loopKeys(values[0], instruction.D)
	seed, err := f.loopSeed(instruction, values, folds)
	if err != nil {
		return 0, err
	}
	length := values[0].length()
	if length == 0 {
		return instruction.A, f.push(seed)
	}
	if folds {
		f.bindLocal(instruction.C, seed)
	}
	loop := loopFrame{
		source: values[0], keys: keys, length: length,
		local: instruction.B, keyLocal: instruction.D, acc: instruction.C,
	}
	if !loop.folds() {
		loop.output = newArrayBuilder(*instruction.Type.elem, length)
		if instruction.Type.kind == DictKind {
			loop.collected = make([]string, 0, length)
		}
	}
	f.bindItem(&loop, 0)
	f.loops = append(f.loops, loop)
	return pc + 1, nil
}

// loopSeed is the value an empty source produces: the initial accumulator, or
// an empty array.
func (f *frame) loopSeed(instruction Instruction, values []Value, folds bool) (Value, error) {
	if !folds {
		if instruction.Type.kind == DictKind {
			return Dict(*instruction.Type.elem, nil)
		}
		return Array(*instruction.Type.elem, nil)
	}
	return values[1], nil
}

// loopCollect stores one body result: folded into the accumulator, or appended
// to the output array.
func (f *frame) loopCollect(instruction Instruction) error {
	value, err := f.pop1()
	if err != nil {
		return err
	}
	loop := &f.loops[len(f.loops)-1]
	if loop.folds() {
		f.bindLocal(loop.acc, value)
		return nil
	}
	if instruction.A == 1 {
		key, err := f.pop1()
		if err != nil {
			return err
		}
		loop.collected = append(loop.collected, key.s)
	}
	loop.output.add(value)
	return nil
}

// loopSpread is loopCollect for a nested comprehension: the value on the stack
// is the whole array the inner loop built, and its elements — not it — belong
// in the output. Every element was already produced by an iteration that spent
// its own fuel, so splicing them costs no more steps than collecting them.
func (f *frame) loopSpread(Instruction) error {
	value, err := f.pop1()
	if err != nil {
		return err
	}
	f.loops[len(f.loops)-1].output.addAll(value)
	return nil
}

func (f *frame) loopNext(pc int, instruction Instruction) (int, error) {
	index := len(f.loops) - 1
	loop := &f.loops[index]
	loop.index++
	if loop.index < loop.length {
		f.bindItem(loop, loop.index)
		return instruction.A, nil
	}
	result, err := f.loopResult(loop)
	if err != nil {
		return 0, err
	}
	f.loops = f.loops[:index]
	return pc + 1, f.push(result)
}

// loopResult hands the built array over directly: every element was type
// checked by loop_collect on its way in.
func (f *frame) loopResult(loop *loopFrame) (Value, error) {
	if loop.folds() {
		return f.locals[loop.acc], nil
	}
	if loop.collected == nil {
		return loop.output.finish(), nil
	}
	// A repeated key is refused here the way a literal's is refused when it
	// is parsed: one key, one value, whichever door the dictionary came in by.
	built := loop.output.finish()
	entries := make(map[string]Value, len(loop.collected))
	for i, key := range loop.collected {
		if _, taken := entries[key]; taken {
			return Value{}, fmt.Errorf("the comprehension produced the key %q twice", key)
		}
		entries[key] = built.at(i)
	}
	return Dict(built.elemType(), entries)
}

func (f *frame) jumpIfFalse(pc int, instruction Instruction) (int, error) {
	value, err := f.pop1()
	if err != nil {
		return 0, err
	}
	if value.b {
		return pc + 1, nil
	}
	return instruction.A, nil
}
