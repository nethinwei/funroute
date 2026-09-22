package machine

import (
	"context"
	"errors"
	"fmt"
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
}

type fallbackFrame struct {
	target int
	stack  int
	loops  int
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
	runtime    *Runtime
	args       []Value
	stack      []Value
	locals     []Value
	localSet   []bool
	loops      []loopFrame
	fallbacks  []fallbackFrame
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
	// The compiler knows how deep the stack gets. Reserving it here is what
	// lets push skip its bounds check; a run whose limit is below the figure
	// keeps the per-push check instead.
	f.reserved = runtime.artifact.MaxStack
	if f.reserved > maxStack {
		f.reserved = 0
	}
	f.overflow = 0
	f.argsUsed = len(args)
	if f.reserved > cap(f.stack) {
		f.stack = make([]Value, 0, f.reserved)
	}
	locals := runtime.artifact.Locals
	if locals == 0 {
		// Constant folding leaves many programs with no locals at all; there
		// is nothing to clear.
		f.locals = f.locals[:0]
		f.localSet = f.localSet[:0]
		return
	}
	if cap(f.locals) < locals {
		f.locals = make([]Value, locals)
		f.localSet = make([]bool, locals)
	}
	f.locals = f.locals[:locals]
	f.localSet = f.localSet[:locals]
	clear(f.localSet)
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
	clearValues(f.stackArray[:f.stackUsed()])
	clearValues(f.argsArray[:min(f.argsUsed, len(f.argsArray))])
	clearValues(f.locals)
	f.loops = f.loops[:0]
	f.fallbacks = f.fallbacks[:0]
}

// stackUsed is how much of the inline stack array may hold a value: the
// compiler's figure, or how far an activation actually pushed past it.
func (f *frame) stackUsed() int {
	used := f.reserved
	if f.overflow > used {
		used = f.overflow
	}
	if used > len(f.stackArray) {
		used = len(f.stackArray)
	}
	return used
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
			err = fmt.Errorf("%w: extension panicked: %v", ErrExtension, recovered)
		}
	}()
	return f.run()
}

func (f *frame) run() (Value, error) {
	code := f.runtime.artifact.Instructions
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
	case OpMakeArray:
		return pc + 1, f.makeArray(instruction)
	case OpMakeDict:
		return pc + 1, f.makeDict(instruction)
	case OpEqual:
		return pc + 1, f.equal()
	case OpCall:
		return pc + 1, f.call(pc, instruction)
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
	case OpBeginFallback:
		f.fallbacks = append(f.fallbacks, fallbackFrame{
			target: instruction.A, stack: len(f.stack), loops: len(f.loops),
		})
		return pc + 1, nil
	case OpEndFallback:
		if len(f.fallbacks) == 0 {
			return 0, fmt.Errorf("fallback stack underflow")
		}
		f.fallbacks = f.fallbacks[:len(f.fallbacks)-1]
		return pc + 1, nil
	default:
		return 0, fmt.Errorf("unknown opcode %q", instruction.Op)
	}
}

func (f *frame) catchFallback(err error) (int, bool) {
	if len(f.fallbacks) == 0 || (!errors.Is(err, ErrExtension) && !errors.Is(err, ErrDeadline)) {
		return 0, false
	}
	last := len(f.fallbacks) - 1
	handler := f.fallbacks[last]
	f.fallbacks = f.fallbacks[:last]
	clearValues(f.stack[handler.stack:])
	f.stack = f.stack[:handler.stack]
	for i := handler.loops; i < len(f.loops); i++ {
		f.loops[i] = loopFrame{}
	}
	f.loops = f.loops[:handler.loops]
	return handler.target, true
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
		return Value{}, fmt.Errorf("stack underflow: need 1 value, have 0")
	}
	last := len(f.stack) - 1
	value := f.stack[last]
	f.stack = f.stack[:last]
	return value, nil
}

// loopKeys checks the source's shape and, for a dictionary walk, lists its
// keys in sorted order: the language guarantees a program replays identically
// and Go's map order does not. An array walk needs no key list.
func loopKeys(source Value, keySlot int) ([]string, error) {
	if keySlot == NoKey {
		if source.kind != ArrayKind {
			return nil, fmt.Errorf("loop source is %s, want array", source.Type())
		}
		return nil, nil
	}
	if source.kind != DictKind {
		return nil, fmt.Errorf("loop source is %s, want dictionary", source.Type())
	}
	return source.keys(), nil
}

// bindItem binds the loop's value variable, plus its key variable when walking
// a dictionary.
func (f *frame) bindItem(loop *loopFrame, index int) {
	f.bindLocal(loop.local, loop.item(index))
	if loop.keyLocal != NoKey {
		f.bindLocal(loop.keyLocal, String(loop.keys[index]))
	}
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

// storeLocal binds a let binding's value to its slot.
func (f *frame) storeLocal(instruction Instruction) error {
	value, err := f.pop1()
	if err != nil {
		return err
	}
	f.bindLocal(instruction.A, value)
	return nil
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
	result, err := compareEqual(values[0], values[1])
	if err != nil {
		return err
	}
	return f.push(result)
}

func (f *frame) call(pc int, instruction Instruction) error {
	function := f.runtime.functions[instruction.A]
	if !function.IsBuiltin() && f.deadline && f.ctx.Err() != nil {
		return fmt.Errorf("%s: %w: %v", function.Name, ErrDeadline, f.ctx.Err())
	}
	if f.fuelLeft < function.Cost {
		return fmt.Errorf("%w before %s", ErrFuel, function.Name)
	}
	f.fuelLeft -= function.Cost
	callArgs, err := f.popN(instruction.B)
	if err != nil {
		return err
	}
	// Three ways to get the value, cheapest first: a Batch already computed
	// it; the plain call every kernel function takes; or the bounded call of
	// a function with a Timeout or Detached. A hoisted call still costs its
	// fuel so a program's budget does not depend on how it ran.
	var value Value
	if ready, ok := f.prefetchedAt(pc); ok {
		value, err = ready.Value, ready.Err
	} else if function.Timeout == 0 && !function.Detached {
		if len(f.fallbacks) > 0 {
			value, err = callSafely(f.ctx, function, callArgs)
		} else {
			value, err = function.Eval(f.ctx, callArgs)
		}
	} else {
		value, err = f.invokeBounded(function, callArgs)
	}
	if err != nil {
		return fmt.Errorf("%s: %w", function.Name, f.functionError(function, err))
	}
	if !value.hasType(*instruction.Type) {
		err = fmt.Errorf("returned %s, contract requires %s", value.Type(), *instruction.Type)
		return fmt.Errorf("%s: %w", function.Name, f.functionError(function, err))
	}
	if err := value.validateInvariant(); err != nil {
		return fmt.Errorf("%s: %w", function.Name, f.functionError(function, fmt.Errorf("invalid result: %v", err)))
	}
	return f.push(value)
}

// prefetchedAt is small enough to inline, so the common case — no batch — is
// one nil check on the call path.
func (f *frame) prefetchedAt(pc int) (Prefetched, bool) {
	if f.prefetched == nil {
		return Prefetched{}, false
	}
	ready, ok := f.prefetched[pc]
	return ready, ok
}

// invokeBounded caps one call by the function's Timeout and, for a Detached
// function, stops waiting when it passes. The deadline check before the call
// is what makes a program stop promptly once its time is up.
func (f *frame) invokeBounded(function *RegisteredFunction, args []Value) (Value, error) {
	if f.deadline && f.ctx.Err() != nil {
		return Value{}, fmt.Errorf("%w: %v", ErrDeadline, f.ctx.Err())
	}
	ctx := f.ctx
	if function.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, function.Timeout)
		defer cancel()
	}
	var value Value
	var err error
	if function.Detached {
		value, err = callDetached(ctx, function, args)
	} else if len(f.fallbacks) > 0 {
		value, err = callSafely(ctx, function, args)
	} else {
		value, err = function.Eval(ctx, args)
	}
	if err != nil && ctx.Err() != nil {
		return Value{}, fmt.Errorf("%w: %v", ErrDeadline, err)
	}
	return value, err
}

func (f *frame) functionError(function *RegisteredFunction, err error) error {
	if function.IsBuiltin() {
		return err
	}
	return f.classify(err)
}

// classify wraps an extension's failure as ErrDeadline when the request's
// budget ran out and ErrExtension otherwise, so fallback and monitoring can
// tell the two apart. An error a Batch or invokeBounded already classified is
// left as it is.
func (f *frame) classify(err error) error {
	if errors.Is(err, ErrDeadline) || errors.Is(err, ErrExtension) {
		return err
	}
	if f.ctx.Err() != nil {
		return fmt.Errorf("%w: %v", ErrDeadline, err)
	}
	return fmt.Errorf("%w: %v", ErrExtension, err)
}

// callDetached runs the function on its own goroutine and stops waiting at the
// deadline. The arguments are copied first: the stack window they live in is
// reused once this returns, and the abandoned call may still be reading them.
func callDetached(ctx context.Context, function *RegisteredFunction, args []Value) (Value, error) {
	type outcome struct {
		value Value
		err   error
	}
	owned := make([]Value, len(args))
	copy(owned, args)
	done := make(chan outcome, 1)
	go func() {
		value, err := callSafely(ctx, function, owned)
		done <- outcome{value, err}
	}()
	select {
	case result := <-done:
		return result.value, result.err
	case <-ctx.Done():
		return Value{}, ctx.Err()
	}
}

func callSafely(ctx context.Context, function *RegisteredFunction, args []Value) (value Value, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("extension panicked: %v", recovered)
		}
	}()
	return function.Eval(ctx, args)
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
	keys, err := loopKeys(values[0], instruction.D)
	if err != nil {
		return 0, err
	}
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
		loop.output = newArrayBuilder(*instruction.Type.Elem, length)
	}
	f.bindItem(&loop, 0)
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
	loop.output.add(value)
	return nil
}

func (f *frame) loopNext(pc int, instruction Instruction) (int, error) {
	if len(f.loops) == 0 {
		return 0, fmt.Errorf("loop next without active loop")
	}
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
	f.localSet[loop.local] = false
	if loop.keyLocal != NoKey {
		f.localSet[loop.keyLocal] = false
	}
	if loop.folds() {
		f.localSet[loop.acc] = false
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
	return loop.output.finish(), nil
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
