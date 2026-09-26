package machine

import (
	"errors"
	"fmt"
	"slices"
)

// Loading proves the bytecode typed. A walk over every path through the
// program, carrying the type of each stack slot and of each bound local,
// holds every instruction to the types it takes and makes: what the compiler
// proved of the source is proved again of the artifact, which may have come
// from anywhere. What the program computes is then known to be of its type,
// so a run checks only what cannot be known before it: the answers of host
// functions. The walk also finds how deep the stack gets.

// vstate is what the walk knows on entering an instruction.
type vstate struct {
	stack []Type
	// locals is each slot's type while it is bound, nil while it is not.
	locals []*Type
	loops  []vloop
	// fallbacks is every fallback the walk is inside, innermost last.
	fallbacks []vfallback
	scopes    int
}

// vfallback is what a fallback's handler is entered with: the state at its
// begin_fallback, since a failure cuts the stack, the loops and the usings
// back to how they were there. That holds only if the candidate leaves what
// was there alone: it pops nothing under the stack it began on, rebinds no
// local that was bound, and closes no loop or using it did not open.
type vfallback struct {
	depth, loops, scopes int
	bound                []bool
}

func (f vfallback) same(other vfallback) bool {
	return f.depth == other.depth && f.loops == other.loops && f.scopes == other.scopes
}

// vloop is a loop the walk is inside: its loop_init's type and slots.
type vloop struct {
	result            Type
	folds             bool
	local, key, accum int
}

func (l vloop) same(other vloop) bool {
	return l.result.Equal(other.result) && l.folds == other.folds && l.local == other.local && l.key == other.key && l.accum == other.accum
}

func (s *vstate) clone() *vstate {
	return &vstate{stack: slices.Clone(s.stack), locals: slices.Clone(s.locals), loops: slices.Clone(s.loops), fallbacks: slices.Clone(s.fallbacks), scopes: s.scopes}
}

func (s *vstate) push(typ Type) { s.stack = append(s.stack, typ) }

// pop takes the top n types, bottom first.
func (s *vstate) pop(n int) ([]Type, error) {
	if n < 0 || len(s.stack) < n {
		return nil, fmt.Errorf("needs %d values, has %d", n, len(s.stack))
	}
	if guard, inside := s.fallback(); inside && len(s.stack)-n < guard.depth {
		return nil, errors.New("a fallback's candidate takes a value from under it")
	}
	out := slices.Clone(s.stack[len(s.stack)-n:])
	s.stack = s.stack[:len(s.stack)-n]
	return out, nil
}

// pop1 takes the top type and holds it to want, when want is given.
func (s *vstate) pop1(want *Type) (Type, error) {
	types, err := s.pop(1)
	if err != nil {
		return Type{}, err
	}
	if want != nil && !types[0].Equal(*want) {
		return Type{}, fmt.Errorf("takes %s, has %s", want.Summary(), types[0].Summary())
	}
	return types[0], nil
}

// bind binds a local slot. One a fallback's handler reads is not rebound
// inside its candidate.
func (s *vstate) bind(slot int, typ Type) error {
	for _, guard := range s.fallbacks {
		if guard.bound[slot] {
			return fmt.Errorf("a fallback's candidate rebinds local %d", slot)
		}
	}
	s.locals[slot] = &typ
	return nil
}

// fallback is the innermost fallback the walk is inside, if any.
func (s *vstate) fallback() (vfallback, bool) {
	if len(s.fallbacks) == 0 {
		return vfallback{}, false
	}
	return s.fallbacks[len(s.fallbacks)-1], true
}

// edge is a path out of an instruction: where it goes, in which state.
type edge struct {
	pc    int
	state *vstate
}

type verifier struct {
	artifact  *Artifact
	functions []*RegisteredFunction
	// joins marks where paths can meet: the start, and every instruction a
	// jump lands on. Only there is a state kept; any other instruction has
	// one way in, from the one before it.
	joins   []bool
	states  []*vstate
	work    []int
	deepest int
	// depths is how deep the stack is on entering each instruction, and -1
	// for one no path reaches: what the register form is laid out by.
	depths []int32
	// out holds the paths out of the instruction last stepped, which walk
	// is done with before it steps again.
	out []edge
}

// to is the paths out of an instruction, in the verifier's own list.
func (v *verifier) to(edges ...edge) []edge {
	v.out = append(v.out[:0], edges...)
	return v.out
}

// proof is what the walk establishes about a program beyond its being typed:
// how deep its stack gets, overall and on entering each instruction.
type proof struct {
	depth  int
	depths []int32
}

// verify walks the artifact's bytecode, calling functions.
func verify(artifact *Artifact, functions []*RegisteredFunction) (proof, error) {
	instructions := artifact.parts.Instructions
	v := &verifier{artifact: artifact, functions: functions, joins: joinsOf(instructions), states: make([]*vstate, len(instructions)+1)}
	v.depths = make([]int32, len(instructions)+1)
	for pc := range v.depths {
		v.depths[pc] = -1
	}
	v.states[0] = &vstate{stack: make([]Type, 0, 8), locals: make([]*Type, artifact.parts.Locals)}
	v.work = []int{0}
	for len(v.work) > 0 {
		pc := v.work[len(v.work)-1]
		v.work = v.work[:len(v.work)-1]
		if err := v.walk(pc, v.states[pc].clone()); err != nil {
			return proof{}, err
		}
	}
	return proof{depth: max(v.deepest, 1), depths: v.depths}, nil
}

// joinsOf marks the start and every instruction a jump lands on.
func joinsOf(instructions []Instruction) []bool {
	joins := make([]bool, len(instructions)+1)
	joins[0] = true
	for _, in := range instructions {
		switch in.Op {
		case OpJump, OpJumpIfFalse, OpLoopInit, OpLoopNext, OpBeginFallback, OpLoopBreak:
			if in.A >= 0 && in.A <= len(instructions) {
				joins[in.A] = true
			}
		}
	}
	return joins
}

// walk steps from pc in state s, carrying s itself through the instructions
// that have one way in, and merges it into each join it reaches. A run of
// straight code costs one state, however deep its stack gets, not one per
// instruction. An instruction's paths out never share a state, so the one
// carried on is no other path's.
func (v *verifier) walk(pc int, s *vstate) error {
	for {
		v.depths[pc] = int32(len(s.stack))
		edges, err := v.step(pc, s)
		if err != nil {
			return fmt.Errorf("invalid bytecode at %d: %w", pc, err)
		}
		onward := -1
		for i, next := range edges {
			if !v.joins[next.pc] {
				onward = i
				continue
			}
			if err := v.merge(next); err != nil {
				return fmt.Errorf("invalid bytecode at %d: %w", next.pc, err)
			}
		}
		if onward < 0 {
			return nil
		}
		pc, s = edges[onward].pc, edges[onward].state
		v.deepest = max(v.deepest, len(s.stack))
	}
}

// merge brings a path into an instruction. Paths that meet must agree on
// the stack, the loops and the scopes; a local stays bound only where every
// path bound it to one type.
func (v *verifier) merge(next edge) error {
	v.deepest = max(v.deepest, len(next.state.stack))
	known := v.states[next.pc]
	if known == nil {
		v.states[next.pc] = next.state
		v.work = append(v.work, next.pc)
		return nil
	}
	if !slices.EqualFunc(known.stack, next.state.stack, Type.Equal) || !slices.EqualFunc(known.loops, next.state.loops, vloop.same) ||
		!slices.EqualFunc(known.fallbacks, next.state.fallbacks, vfallback.same) || known.scopes != next.state.scopes {
		return errors.New("paths meet with different stacks")
	}
	changed := false
	for slot, typ := range known.locals {
		if other := next.state.locals[slot]; typ != nil && (other == nil || !typ.Equal(*other)) {
			known.locals[slot], changed = nil, true
		}
	}
	if changed {
		v.work = append(v.work, next.pc)
	}
	return nil
}

// step is the paths out of the instruction at pc, entered in state s.
func (v *verifier) step(pc int, s *vstate) ([]edge, error) {
	instructions := v.artifact.parts.Instructions
	if pc == len(instructions) {
		return nil, v.finish(s)
	}
	in := instructions[pc]
	switch in.Op {
	case OpJump:
		return v.to(edge{in.A, s}), nil
	case OpJumpIfFalse:
		if _, err := s.pop1(&BoolType); err != nil {
			return nil, err
		}
		return v.to(edge{pc + 1, s}, edge{in.A, s.clone()}), nil
	case OpBeginFallback:
		handler := s.clone()
		bound := make([]bool, len(s.locals))
		for slot, typ := range s.locals {
			bound[slot] = typ != nil
		}
		s.fallbacks = append(s.fallbacks, vfallback{depth: len(s.stack), loops: len(s.loops), scopes: s.scopes, bound: bound})
		return v.to(edge{pc + 1, s}, edge{in.A, handler}), nil
	case OpLoopInit:
		return v.loopInit(pc, in, s)
	case OpLoopNext:
		return v.loopNext(pc, in, s)
	case OpLoopBreak:
		return v.loopBreak(in, s)
	}
	return v.to(edge{pc + 1, s}), v.straight(in, s)
}

// finish holds the end of the program: its result, and nothing open.
func (v *verifier) finish(s *vstate) error {
	if len(s.stack) != 1 || !s.stack[0].Equal(v.artifact.parts.Result) || len(s.loops) > 0 || len(s.fallbacks) > 0 || s.scopes > 0 {
		return fmt.Errorf("the program ends with %d values, want its %s alone", len(s.stack), v.artifact.parts.Result.Summary())
	}
	return nil
}

// straight types an instruction that goes on to the next one.
func (v *verifier) straight(in Instruction, s *vstate) error {
	switch in.Op {
	case OpConstant:
		s.push(v.artifact.parts.Constants[in.A].Type)
	case OpLoadArg:
		s.push(v.artifact.parts.Args[in.A].typ)
	case OpLoadLocal:
		if s.locals[in.A] == nil {
			return fmt.Errorf("local %d is read unbound", in.A)
		}
		s.push(*s.locals[in.A])
	case OpStoreLocal:
		typ, err := s.pop1(nil)
		if err != nil {
			return err
		}
		return s.bind(in.A, typ)
	case OpMakeArray, OpMakeDict, OpMakeRecord:
		return v.make(in, s)
	case OpField:
		return v.field(in, s)
	case OpEqual:
		return v.equal(s)
	case OpCall:
		return v.call(in, s)
	case OpRecordWith:
		return v.recordWith(in, s)
	default:
		return v.scoped(in, s)
	}
	return nil
}

// scoped types the instructions that open and close what a program is
// inside: a loop's body, a fallback's candidate, a using.
func (v *verifier) scoped(in Instruction, s *vstate) error {
	switch in.Op {
	case OpLoopCollect:
		return v.loopCollect(in, s)
	case OpLoopFold:
		return v.loopFold(in, s)
	case OpLoopSpread:
		return v.loopSpread(in, s)
	case OpEndFallback:
		guard, inside := s.fallback()
		if !inside || len(s.stack) != guard.depth+1 {
			return errors.New("end_fallback without a fallback that made one value")
		}
		s.fallbacks = s.fallbacks[:len(s.fallbacks)-1]
	case OpFxPush:
		return v.fxPush(in, s)
	case OpFxPop:
		if guard, inside := s.fallback(); s.scopes == 0 || inside && s.scopes == guard.scopes {
			return errors.New("fx_pop without a using")
		}
		s.scopes--
	default:
		return fmt.Errorf("unknown opcode %q", in.Op)
	}
	return nil
}

// make types a container or a record built from the top values: each is the
// element type, or its field's.
func (v *verifier) make(in Instruction, s *vstate) error {
	items, err := s.pop(in.A)
	if err != nil {
		return err
	}
	for i, item := range items {
		want := in.Type.elem
		if in.Op == OpMakeRecord {
			want = &in.Type.fields[i].typ
		}
		if !item.Equal(*want) {
			return fmt.Errorf("item %d is %s, want %s", i, item.Summary(), want.Summary())
		}
	}
	s.push(*in.Type)
	return nil
}

func (v *verifier) field(in Instruction, s *vstate) error {
	record, err := s.pop1(nil)
	if err != nil {
		return err
	}
	if record.kind != RecordKind || in.A >= len(record.fields) || !record.fields[in.A].typ.Equal(*in.Type) {
		return fmt.Errorf("field %d of %s is not %s", in.A, record.Summary(), in.Type.Summary())
	}
	s.push(*in.Type)
	return nil
}

func (v *verifier) equal(s *vstate) error {
	operands, err := s.pop(2)
	if err != nil {
		return err
	}
	if !operands[0].Equal(operands[1]) {
		return fmt.Errorf("compares %s with %s", operands[0].Summary(), operands[1].Summary())
	}
	s.push(BoolType)
	return nil
}

// call types a call by the function's signature, its type variables bound
// by the arguments: the result is what the signature makes of them. A lazy
// construct is compiled into jumps and never called.
func (v *verifier) call(in Instruction, s *vstate) error {
	function := v.functions[in.A]
	args, err := s.pop(in.B)
	if err != nil {
		return err
	}
	if function.special != specialNone || len(function.Params) != len(args) {
		return fmt.Errorf("%s cannot be called with %d arguments", function.key, len(args))
	}
	vars := map[string]Type{}
	for i, param := range function.Params {
		if !matchType(param, args[i], vars) {
			return fmt.Errorf("%s takes %s, has %s", function.key, param.Summary(), args[i].Summary())
		}
	}
	if result, ok := substituteType(function.Result, vars); !ok || !result.Equal(*in.Type) {
		return fmt.Errorf("%s does not return %s", function.key, in.Type.Summary())
	}
	s.push(*in.Type)
	return nil
}

func (v *verifier) recordWith(in Instruction, s *vstate) error {
	values, err := s.pop(len(in.Keys))
	if err != nil {
		return err
	}
	if _, err := s.pop1(in.Type); err != nil {
		return err
	}
	for i, name := range in.Keys {
		if want := in.Type.fields[in.Type.FieldIndex(name)].typ; !values[i].Equal(want) {
			return fmt.Errorf("field %q takes %s, has %s", name, want.Summary(), values[i].Summary())
		}
	}
	s.push(*in.Type)
	return nil
}

// loopInit types a loop over the source on the stack. An empty source goes
// straight past the loop with the seed, or an empty result; otherwise the
// body runs with the loop's names bound.
func (v *verifier) loopInit(pc int, in Instruction, s *vstate) ([]edge, error) {
	loop := vloop{result: *in.Type, folds: in.C != NoAccumulator, local: in.B, key: in.D, accum: in.C}
	if loop.folds {
		if _, err := s.pop1(in.Type); err != nil {
			return nil, err
		}
	}
	source, err := s.pop1(nil)
	if err != nil {
		return nil, err
	}
	if want := loopKind(in.D); source.kind != want {
		return nil, fmt.Errorf("a loop walks %s, not %s", want, source.Summary())
	}
	past := s.clone()
	past.push(*in.Type)
	if err := v.bindLoop(in, s, *source.elem); err != nil {
		return nil, err
	}
	s.loops = append(s.loops, loop)
	return v.to(edge{pc + 1, s}, edge{in.A, past}), nil
}

// bindLoop binds a loop's names: the item, the key of a dictionary's entry
// and a fold's accumulator.
func (v *verifier) bindLoop(in Instruction, s *vstate, item Type) error {
	if err := s.bind(in.B, item); err != nil {
		return err
	}
	if in.D != NoKey {
		if err := s.bind(in.D, StringType); err != nil {
			return err
		}
	}
	if in.C != NoAccumulator {
		return s.bind(in.C, *in.Type)
	}
	return nil
}

// loopKind is the kind a loop's source is: a dictionary when the loop binds
// a key.
func loopKind(key int) Kind {
	if key == NoKey {
		return ArrayKind
	}
	return DictKind
}

// innermost is the loop the walk is in.
func (s *vstate) innermost() (vloop, error) {
	if len(s.loops) == 0 {
		return vloop{}, errors.New("not inside a loop")
	}
	return s.loops[len(s.loops)-1], nil
}

// loopCollect types what one iteration adds: a fold's next accumulator, or
// an item of the result, with its key when the result is a dictionary.
func (v *verifier) loopCollect(in Instruction, s *vstate) error {
	loop, err := s.innermost()
	if err != nil {
		return err
	}
	keyed := in.A == 1
	fits := in.Type.Equal(loop.result) && !keyed
	if !loop.folds {
		fits = keyed == (loop.result.kind == DictKind) && in.Type.Equal(*loop.result.elem)
	}
	if !fits {
		return fmt.Errorf("collects %s into %s", in.Type.Summary(), loop.result.Summary())
	}
	if _, err := s.pop1(in.Type); err != nil || !keyed {
		return err
	}
	_, err = s.pop1(&StringType)
	return err
}

// loopFold types a fold's item going into its answer: the step is a kernel
// function of the answer and the item that gives the answer.
func (v *verifier) loopFold(in Instruction, s *vstate) error {
	loop, err := s.innermost()
	if err != nil {
		return err
	}
	step := v.functions[in.A]
	if !loop.folds || !in.Type.Equal(loop.result) || !step.builtin || step.special != specialNone || len(step.Params) != 2 {
		return fmt.Errorf("%s does not fold into %s", step.key, loop.result.Summary())
	}
	item, err := s.pop1(nil)
	if err != nil {
		return err
	}
	vars := map[string]Type{}
	if !matchType(step.Params[0], loop.result, vars) || !matchType(step.Params[1], item, vars) {
		return fmt.Errorf("%s does not fold %s into %s", step.key, item.Summary(), loop.result.Summary())
	}
	if result, ok := substituteType(step.Result, vars); !ok || !result.Equal(loop.result) {
		return fmt.Errorf("%s does not fold into %s", step.key, loop.result.Summary())
	}
	return nil
}

// loopSpread types the outer clause of a nested comprehension: the inner
// clause's whole array, spliced into the result.
func (v *verifier) loopSpread(in Instruction, s *vstate) error {
	loop, err := s.innermost()
	if err != nil {
		return err
	}
	if loop.folds || loop.result.kind != ArrayKind || !in.Type.Equal(loop.result) {
		return fmt.Errorf("spreads %s into %s", in.Type.Summary(), loop.result.Summary())
	}
	_, err = s.pop1(in.Type)
	return err
}

// loopNext goes back to the loop's start while there are items, and past the
// loop with its result when there are none.
func (v *verifier) loopNext(pc int, in Instruction, s *vstate) ([]edge, error) {
	loop, err := s.innermost()
	if err != nil {
		return nil, err
	}
	if !in.Type.Equal(loop.result) {
		return nil, fmt.Errorf("the loop is %s, loop_next says %s", loop.result.Summary(), in.Type.Summary())
	}
	past, err := s.pastLoop(loop)
	if err != nil {
		return nil, err
	}
	return v.to(edge{in.A, s}, edge{pc + 1, past}), nil
}

// pastLoop is the state after the innermost loop ends: its names unbound,
// its result pushed.
func (s *vstate) pastLoop(loop vloop) (*vstate, error) {
	if guard, inside := s.fallback(); inside && len(s.loops) == guard.loops {
		return nil, errors.New("a fallback's candidate ends a loop it is inside")
	}
	past := s.clone()
	past.loops = past.loops[:len(past.loops)-1]
	for _, slot := range []int{loop.local, loop.key, loop.accum} {
		if slot != NoKey {
			past.locals[slot] = nil
		}
	}
	past.push(loop.result)
	return past, nil
}

// loopBreak ends a fold's loop with the answer on the stack: the path past
// the loop is loop_next's last one, the answer the loop's.
func (v *verifier) loopBreak(in Instruction, s *vstate) ([]edge, error) {
	loop, err := s.innermost()
	if err != nil {
		return nil, err
	}
	if !loop.folds || !in.Type.Equal(loop.result) {
		return nil, fmt.Errorf("loop_break of %s ends %s", in.Type.Summary(), loop.result.Summary())
	}
	if _, err := s.pop1(in.Type); err != nil {
		return nil, err
	}
	past, err := s.pastLoop(loop)
	if err != nil {
		return nil, err
	}
	return v.to(edge{in.A, past}), nil
}

// fxPush types a using's quotes: each an exchange rate or an array of them.
func (v *verifier) fxPush(in Instruction, s *vstate) error {
	quotes, err := s.pop(in.B)
	if err != nil {
		return err
	}
	for _, quote := range quotes {
		if !quote.Equal(FxRateType) && !quote.Equal(ArrayOf(FxRateType)) {
			return fmt.Errorf("a using quotes %s, not exchange rates", quote.Summary())
		}
	}
	s.scopes++
	return nil
}

// matchType reports whether actual is an instance of the signature's
// pattern, binding the pattern's type variables as it goes: a variable is
// one type throughout, and the enum wildcard is any enum.
func matchType(pattern, actual Type, vars map[string]Type) bool {
	switch {
	case pattern.kind == VarKind:
		if bound, ok := vars[pattern.name]; ok {
			return bound.Equal(actual)
		}
		vars[pattern.name] = actual
		return true
	case pattern.kind == EnumKind && pattern.name == "":
		return actual.kind == EnumKind
	case (pattern.kind == ArrayKind || pattern.kind == DictKind) && pattern.elem != nil:
		return actual.kind == pattern.kind && actual.elem != nil && matchType(*pattern.elem, *actual.elem, vars)
	}
	return pattern.Equal(actual)
}

// substituteType is a signature's type with its variables replaced, and
// false when one is not bound.
func substituteType(t Type, vars map[string]Type) (Type, bool) {
	switch {
	case t.kind == VarKind:
		bound, ok := vars[t.name]
		return bound, ok
	case (t.kind == ArrayKind || t.kind == DictKind) && t.elem != nil:
		elem, ok := substituteType(*t.elem, vars)
		return Type{kind: t.kind, elem: &elem}, ok
	}
	return t, true
}
