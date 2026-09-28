package machine

import "slices"

// Value flow: where each array a program builds goes, found once, before it
// is lowered, as Go's compiler finds where a value escapes. An array of
// bools, ints, floats or strings that only feeds a loop, len or an index —
// directly or through let — never leaves the run, and is built in memory the
// frame keeps from run to run: no allocation once the frame has run it. The
// array a program answers, and the fields of the record it answers, are
// built where the host said, when it said (Program.RunInto). A record
// argument that is only read field by field is loaded into the frame's own
// record; so is each item of an array of plain records that is only walked
// or measured, and whose items are only read field by field — the loop's
// name holds the argument's origin, so any other use of an item lets the
// argument out.
//
// The walk is the verifier's: a state per join, one state carried through
// straight code. It tracks, for each stack slot and local, where the value
// came from: a producer — the pc of the make_array, the loop_init of the
// comprehension, the make_record — or a record argument, or anything else.
// A value from two places at once has no one place, and neither producer
// is given one.

// origin is where a value came from: a producer's pc when it is 0 or more,
// and otherwise one of the markers below, an argument (argOrigin), or the
// item of a loop over an array of plain records (itemOrigin).
type origin int32

const (
	fromElsewhere origin = -1
	unbound       origin = -2
	// itemBase is below every argument's origin: items are counted from it.
	itemBase origin = -1 << 24
)

func argOrigin(i int) origin { return origin(-3 - i) }

func (o origin) arg() (int, bool) { return int(-3 - o), o <= -3 && o > itemBase }

// itemOrigin is the item of the loop that starts at pc.
func itemOrigin(pc int) origin { return itemBase - origin(pc) }

func (o origin) item() (int, bool) { return int(itemBase - o), o <= itemBase }

// flow is what the analysis found.
type flow struct {
	// at is what the analysis says of each instruction, by pc.
	at []flowAt
	// answer is the make_record the program answers, or -1.
	answer int
	// fieldOnly is, by argument, a record that is only read field by field,
	// and viewOnly an array of natives that is only walked, measured or
	// indexed, or an array of plain records only walked or measured: a
	// Program hands either over in place.
	fieldOnly, viewOnly []bool
}

// flowAt is what the analysis says of the instruction at one pc. arena says
// the array it produces never leaves the run, and uses that it is a
// consumer reading an array where it is: in its arena slot, or an argument
// handed over in place. builds says it builds the program's answer, dest
// being 0 for the whole answer and i+1 for field i of the answer's record.
// itemsInPlace says it is a loop over an array of plain records whose item
// is only read field by field: it loads each item into the frame's own
// record, only the fields itemFields names.
type flowAt struct {
	arena, uses, builds, itemsInPlace bool
	dest                              int
	itemFields                        []int32
}

// fstate is what the walk knows on entering an instruction.
type fstate struct {
	stack, locals []origin
	loops         []int // the loop_init of each loop the walk is inside
}

func (s *fstate) clone() *fstate {
	return &fstate{stack: append([]origin(nil), s.stack...), locals: append([]origin(nil), s.locals...), loops: append([]int(nil), s.loops...)}
}

func (s *fstate) push(o origin) { s.stack = append(s.stack, o) }

func (s *fstate) pop(n int) []origin {
	out := s.stack[len(s.stack)-n:]
	s.stack = s.stack[:len(s.stack)-n]
	return out
}

// flower is the walk.
type flower struct {
	out       []fedge
	code      []Instruction
	args      []Parameter
	functions []*RegisteredFunction
	joins     []bool
	states    []*fstate
	work      []int
	// facts is what the walk finds of each instruction, by pc, and
	// argEscaped each argument some use lets out.
	facts      []pcFacts
	argEscaped []bool
	answer     origin
}

// pcFacts is what the walk finds of the instruction at one pc: whether a
// use lets out what it produces, or its loop's item; where what it produces
// is used, which matters only while that is one place — the walk may pass a
// use more than once; the origins of a make_record's fields; and what it
// reads where it is, when it does.
type pcFacts struct {
	escaped, itemEscaped bool
	// itemFields is the fields of a loop's item its body reads.
	itemFields    []int32
	firstUse      int32
	usedElsewhere bool
	consumes      bool
	consumer      origin
	fields        []origin
}

// flowOf analyzes a verified program.
func flowOf(parts *ArtifactParts, functions []*RegisteredFunction) flow {
	w := &flower{
		code: parts.Instructions, args: parts.Args, functions: functions, joins: joinsOf(parts.Instructions),
		states: make([]*fstate, len(parts.Instructions)+1), facts: make([]pcFacts, len(parts.Instructions)+1),
		argEscaped: make([]bool, len(parts.Args)), answer: fromElsewhere,
	}
	for pc := range w.facts {
		w.facts[pc].firstUse = -1
	}
	start := &fstate{locals: make([]origin, parts.Locals)}
	for i := range start.locals {
		start.locals[i] = unbound
	}
	w.states[0] = start
	w.work = []int{0}
	for len(w.work) > 0 {
		pc := w.work[len(w.work)-1]
		w.work = w.work[:len(w.work)-1]
		w.walk(pc, w.states[pc].clone())
	}
	return w.result(parts)
}

// walk carries s through the instructions that have one way in, as the
// verifier does, merging it into each join it reaches.
func (w *flower) walk(pc int, s *fstate) {
	for {
		edges := w.step(pc, s)
		onward := -1
		for i, next := range edges {
			if !w.joins[next.pc] {
				onward = i
				continue
			}
			w.merge(next.pc, next.state)
		}
		if onward < 0 {
			return
		}
		pc, s = edges[onward].pc, edges[onward].state
	}
}

type fedge struct {
	pc    int
	state *fstate
}

// to is the paths out of an instruction, in the walk's own list: the walk
// is done with them before it steps again.
func (w *flower) to(edges ...fedge) []fedge {
	w.out = append(w.out[:0], edges...)
	return w.out
}

// merge brings a path into a join. A slot that holds different values on
// two paths holds neither producer's: both are let out.
func (w *flower) merge(pc int, next *fstate) {
	known := w.states[pc]
	if known == nil {
		w.states[pc] = next
		w.work = append(w.work, pc)
		return
	}
	changed := mergeOrigins(w, known.stack, next.stack)
	changed = mergeOrigins(w, known.locals, next.locals) || changed
	if changed {
		w.work = append(w.work, pc)
	}
}

func mergeOrigins(w *flower, known, next []origin) bool {
	changed := false
	for i, o := range next {
		switch {
		case known[i] == o || o == unbound:
		case known[i] == unbound:
			known[i], changed = o, true
		case known[i] != fromElsewhere:
			w.escape(known[i])
			w.escape(o)
			known[i], changed = fromElsewhere, true
		default:
			w.escape(o)
		}
	}
	return changed
}

// escape lets a value out: its producer, its argument or its loop's item
// has no one place.
func (w *flower) escape(o origin) {
	if i, ok := o.arg(); ok {
		w.argEscaped[i] = true
	} else if pc, ok := o.item(); ok {
		w.facts[pc].itemEscaped = true
	} else if o >= 0 {
		w.facts[o].escaped = true
	}
}

// use records a use of o at pc, which lets it out unless stays is set.
func (w *flower) use(pc int, o origin, stays bool) {
	if o >= 0 {
		switch fact := &w.facts[o]; {
		case fact.firstUse < 0:
			fact.firstUse = int32(pc)
		case int(fact.firstUse) != pc:
			fact.usedElsewhere = true
		}
	}
	if !stays {
		w.escape(o)
	}
}

func (w *flower) useAll(pc int, origins []origin) {
	for _, o := range origins {
		w.use(pc, o, false)
	}
}

// usedOnce reports a producer used at one place only.
func (w *flower) usedOnce(pc int) bool {
	return w.facts[pc].firstUse >= 0 && !w.facts[pc].usedElsewhere
}

// step is the paths out of the instruction at pc, entered in state s.
func (w *flower) step(pc int, s *fstate) []fedge {
	if pc == len(w.code) {
		w.answer = s.stack[0]
		w.use(pc, w.answer, false)
		return nil
	}
	in := w.code[pc]
	switch in.Op {
	case OpJump:
		return w.to(fedge{in.A, s})
	case OpJumpIfFalse:
		s.pop(1)
		return w.to(fedge{pc + 1, s}, fedge{in.A, s.clone()})
	case OpBeginFallback:
		return w.to(fedge{pc + 1, s}, fedge{in.A, s.clone()})
	case OpLoopInit:
		return w.loopInit(pc, in, s)
	case OpLoopNext:
		past := w.pastLoop(s)
		return w.to(fedge{in.A, s}, fedge{pc + 1, past})
	case OpLoopBreak:
		w.use(pc, s.pop(1)[0], false)
		return w.to(fedge{in.A, w.pastLoop(s)})
	}
	w.straight(pc, in, s)
	return w.to(fedge{pc + 1, s})
}

// straight follows an instruction that goes on to the next one.
func (w *flower) straight(pc int, in Instruction, s *fstate) {
	switch in.Op {
	case OpConstant:
		s.push(fromElsewhere)
	case OpLoadArg:
		s.push(fromElsewhere)
		if typ := w.args[in.A].typ; typ.kind == RecordKind || nativeElem(typ) || plainRecords(typ) {
			s.stack[len(s.stack)-1] = argOrigin(in.A)
		}
	case OpLoadLocal:
		s.push(s.locals[in.A])
	case OpStoreLocal:
		s.locals[in.A] = s.pop(1)[0]
	case OpMakeArray, OpMakeDict, OpMakeRecord:
		w.build(pc, in, s)
	case OpField:
		// A field read keeps a record argument where it is, and the item of
		// a loop over an array of plain records.
		record := s.pop(1)[0]
		i, argument := record.arg()
		loop, item := record.item()
		if item && !slices.Contains(w.facts[loop].itemFields, int32(in.A)) {
			w.facts[loop].itemFields = append(w.facts[loop].itemFields, int32(in.A))
		}
		w.use(pc, record, item || argument && w.args[i].typ.kind == RecordKind)
		s.push(fromElsewhere)
	case OpCall:
		w.call(pc, in, s)
	default:
		w.consume(pc, in, s)
	}
}

// build follows an instruction that makes a container or a record of the
// values on top: they go into it, which lets them out.
func (w *flower) build(pc int, in Instruction, s *fstate) {
	items := s.pop(in.A)
	if in.Op == OpMakeRecord {
		w.facts[pc].fields = append([]origin(nil), items...)
	}
	w.useAll(pc, items)
	made := fromElsewhere
	if in.Op == OpMakeRecord || in.Op == OpMakeArray && nativeElem(*in.Type) {
		made = origin(pc)
	}
	s.push(made)
}

// nativeElem reports an array type whose items are bools, ints, floats or
// strings: one native slice backs it.
func nativeElem(typ Type) bool {
	if typ.kind != ArrayKind || typ.elem == nil {
		return false
	}
	switch typ.elem.kind {
	case BoolKind, IntKind, FloatKind, StringKind:
		return true
	}
	return false
}

// plainRecords reports an array of records whose fields are all bools, ints,
// floats or strings: an item loads in place with nothing to check but a
// float's being finite.
func plainRecords(typ Type) bool {
	if typ.kind != ArrayKind || typ.elem == nil || typ.elem.kind != RecordKind {
		return false
	}
	for _, field := range typ.elem.fields {
		switch field.typ.kind {
		case BoolKind, IntKind, FloatKind, StringKind:
		default:
			return false
		}
	}
	return true
}

// arenaKernels are the kernel functions that read an array argument and
// keep nothing of it.
var arenaKernels = map[string]bool{"len(array<T>)->int": true, "at(array<T>,int)->T": true}

// call follows a call: its arguments are let out, but for the array a
// kernel function reads where it is.
func (w *flower) call(pc int, in Instruction, s *fstate) {
	function := w.functions[in.A]
	for i, arg := range s.pop(in.B) {
		// An index into an array of records would make a record of an item:
		// only len reads one where it is.
		reads := i == 0 && function.builtin && arenaKernels[function.key] && (function.key == "len(array<T>)->int" || !w.recordsArg(arg))
		w.use(pc, arg, reads)
		if reads {
			w.facts[pc].consumes, w.facts[pc].consumer = true, arg
		}
	}
	s.push(fromElsewhere)
}

// recordsArg reports an array of plain records given as an argument.
func (w *flower) recordsArg(o origin) bool {
	i, argument := o.arg()
	return argument && plainRecords(w.args[i].typ)
}

// consume follows the instructions that take values and make none.
func (w *flower) consume(pc int, in Instruction, s *fstate) {
	switch in.Op {
	case OpLoopCollect:
		w.useAll(pc, s.pop(1+in.A))
	case OpLoopSpread, OpLoopFold:
		w.useAll(pc, s.pop(1))
	case OpFxPush:
		w.useAll(pc, s.pop(in.B))
	case OpEqual:
		w.useAll(pc, s.pop(2))
		s.push(fromElsewhere)
	case OpRecordWith:
		// The base is read, its fields copied: a record argument stays.
		values := s.pop(len(in.Keys) + 1)
		i, argument := values[0].arg()
		w.use(pc, values[0], argument && w.args[i].typ.kind == RecordKind)
		w.useAll(pc, values[1:])
		s.push(origin(pc))
	}
}

// loopInit follows the start of a loop: an array walked stays where it is.
func (w *flower) loopInit(pc int, in Instruction, s *fstate) []fedge {
	if in.C != NoAccumulator {
		w.use(pc, s.pop(1)[0], false)
	}
	source := s.pop(1)[0]
	walks := in.D == NoKey
	w.use(pc, source, walks)
	if walks {
		w.facts[pc].consumes, w.facts[pc].consumer = true, source
	}
	past := s.clone()
	past.push(w.loopResult(pc))
	for _, slot := range []int{in.B, in.C, in.D} {
		if slot != NoKey {
			s.locals[slot] = fromElsewhere
		}
	}
	if walks && w.recordsArg(source) {
		s.locals[in.B] = itemOrigin(pc)
	}
	s.loops = append(s.loops, pc)
	return w.to(fedge{pc + 1, s}, fedge{in.A, past})
}

// pastLoop is the state after the innermost loop, its result pushed.
func (w *flower) pastLoop(s *fstate) *fstate {
	past := s.clone()
	init := past.loops[len(past.loops)-1]
	past.loops = past.loops[:len(past.loops)-1]
	past.push(w.loopResult(init))
	return past
}

// loopResult is where a loop's result comes from: the comprehension that
// builds an array of natives is its producer.
func (w *flower) loopResult(init int) origin {
	if in := w.code[init]; in.C == NoAccumulator && nativeElem(*in.Type) {
		return origin(init)
	}
	return fromElsewhere
}

// arrayProducer reports a producer of an array of natives.
func (w *flower) arrayProducer(pc int) bool {
	in := w.code[pc]
	return (in.Op == OpMakeArray || in.Op == OpLoopInit && in.C == NoAccumulator) && nativeElem(*in.Type)
}

// result reads the walk's findings out.
func (w *flower) result(parts *ArtifactParts) flow {
	f := flow{
		at: make([]flowAt, len(w.code)+1), answer: -1,
		fieldOnly: make([]bool, len(parts.Args)), viewOnly: make([]bool, len(parts.Args)),
	}
	for pc := range w.code {
		f.at[pc].arena = w.arrayProducer(pc) && !w.facts[pc].escaped
	}
	for i, param := range parts.Args {
		f.fieldOnly[i] = param.typ.kind == RecordKind && !w.argEscaped[i]
		f.viewOnly[i] = (nativeElem(param.typ) || plainRecords(param.typ)) && !w.argEscaped[i]
	}
	for pc := range w.code {
		if !w.facts[pc].consumes {
			continue
		}
		o := w.facts[pc].consumer
		i, argument := o.arg()
		f.at[pc].uses = o >= 0 && f.at[o].arena || argument && f.viewOnly[i]
		f.at[pc].itemsInPlace = w.code[pc].Op == OpLoopInit && w.recordsArg(o) && !w.facts[pc].itemEscaped
		f.at[pc].itemFields = w.facts[pc].itemFields
	}
	if answer := int(w.answer); w.answer >= 0 && w.usedOnce(answer) {
		w.answerDest(&f, answer)
	}
	return f
}

// answerDest marks what builds the answer: the array, or the record and
// each array field of it built for it alone.
func (w *flower) answerDest(f *flow, answer int) {
	if w.arrayProducer(answer) {
		f.at[answer].builds, f.at[answer].dest = true, 0
		return
	}
	if op := w.code[answer].Op; op != OpMakeRecord && op != OpRecordWith {
		return
	}
	f.answer = answer
	for i, field := range w.facts[answer].fields {
		if field >= 0 && w.arrayProducer(int(field)) && w.usedOnce(int(field)) {
			f.at[field].builds, f.at[field].dest = true, i+1
		}
	}
}
