package machine

// Value flow: where each array a program builds goes, found once, before it
// is lowered, as Go's compiler finds where a value escapes. An array of
// bools, ints, floats or strings that only feeds a loop, len or an index —
// directly or through let — never leaves the run, and is built in memory the
// frame keeps from run to run: no allocation once the frame has run it. The
// array a program answers, and the fields of the record it answers, are
// built where the host said, when it said (Program.RunInto). A record
// argument that is only read field by field is loaded into the frame's own
// record.
//
// The walk is the verifier's: a state per join, one state carried through
// straight code. It tracks, for each stack slot and local, where the value
// came from: a producer — the pc of the make_array, the loop_init of the
// comprehension, the make_record — or a record argument, or anything else.
// A value from two places at once has no one place, and neither producer
// is given one.

// origin is where a value came from: a producer's pc when it is 0 or more,
// and otherwise one of the markers below, or a record argument (argOrigin).
type origin int32

const (
	fromElsewhere origin = -1
	unbound       origin = -2
)

func argOrigin(i int) origin { return origin(-3 - i) }

func (o origin) arg() (int, bool) { return int(-3 - o), o <= -3 }

// flow is what the analysis found.
type flow struct {
	// arena is each producer whose array never leaves the run, by pc; its
	// uses, by pc, are the consumers that read it there.
	arena map[int]bool
	uses  map[int]bool
	// dest is each producer that builds the program's answer, by pc: 0 for
	// the whole answer, i+1 for field i of the answer's record.
	dest map[int]int
	// answer is the make_record the program answers, or -1.
	answer int
	// fieldOnly is, by argument, a record that is only read field by field,
	// and viewOnly an array of natives that is only walked, measured or
	// indexed: a Program hands either over in place.
	fieldOnly, viewOnly []bool
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
	code      []Instruction
	args      []Parameter
	functions []*RegisteredFunction
	joins     []bool
	states    []*fstate
	work      []int
	// escaped is each producer, and each record argument, some use lets
	// out; usedAt is where each producer is used — a set, since the walk
	// may pass an instruction more than once — and fields the origins of
	// each make_record's fields.
	escaped, argEscaped map[int]bool
	usedAt              map[int]map[int]bool
	fields              map[int][]origin
	consumers           map[int]origin
	answer              origin
}

// flowOf analyzes a verified program.
func flowOf(parts *ArtifactParts, functions []*RegisteredFunction) flow {
	w := &flower{
		code: parts.Instructions, args: parts.Args, functions: functions, joins: joinsOf(parts.Instructions),
		states: make([]*fstate, len(parts.Instructions)+1), escaped: map[int]bool{}, argEscaped: map[int]bool{},
		usedAt: map[int]map[int]bool{}, fields: map[int][]origin{}, consumers: map[int]origin{}, answer: fromElsewhere,
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

// escape lets a value out: its producer or its argument has no one place.
func (w *flower) escape(o origin) {
	if i, ok := o.arg(); ok {
		w.argEscaped[i] = true
	} else if o >= 0 {
		w.escaped[int(o)] = true
	}
}

// use records a use of o at pc, which lets it out unless stays is set.
func (w *flower) use(pc int, o origin, stays bool) {
	if o >= 0 {
		if w.usedAt[int(o)] == nil {
			w.usedAt[int(o)] = map[int]bool{}
		}
		w.usedAt[int(o)][pc] = true
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
func (w *flower) usedOnce(pc int) bool { return len(w.usedAt[pc]) == 1 }

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
		return []fedge{{in.A, s}}
	case OpJumpIfFalse:
		s.pop(1)
		return []fedge{{pc + 1, s}, {in.A, s.clone()}}
	case OpBeginFallback:
		return []fedge{{pc + 1, s}, {in.A, s.clone()}}
	case OpLoopInit:
		return w.loopInit(pc, in, s)
	case OpLoopNext:
		past := w.pastLoop(s)
		return []fedge{{in.A, s}, {pc + 1, past}}
	case OpLoopBreak:
		w.use(pc, s.pop(1)[0], false)
		return []fedge{{in.A, w.pastLoop(s)}}
	}
	w.straight(pc, in, s)
	return []fedge{{pc + 1, s}}
}

// straight follows an instruction that goes on to the next one.
func (w *flower) straight(pc int, in Instruction, s *fstate) {
	switch in.Op {
	case OpConstant:
		s.push(fromElsewhere)
	case OpLoadArg:
		s.push(fromElsewhere)
		if typ := w.args[in.A].typ; typ.kind == RecordKind || nativeElem(typ) {
			s.stack[len(s.stack)-1] = argOrigin(in.A)
		}
	case OpLoadLocal:
		s.push(s.locals[in.A])
	case OpStoreLocal:
		s.locals[in.A] = s.pop(1)[0]
	case OpMakeArray, OpMakeDict, OpMakeRecord:
		w.build(pc, in, s)
	case OpField:
		// A field read keeps a record argument where it is.
		record := s.pop(1)[0]
		i, argument := record.arg()
		w.use(pc, record, argument && w.args[i].typ.kind == RecordKind)
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
		w.fields[pc] = append([]origin(nil), items...)
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

// arenaKernels are the kernel functions that read an array argument and
// keep nothing of it.
var arenaKernels = map[string]bool{"len(array<T>)->int": true, "at(array<T>,int)->T": true}

// call follows a call: its arguments are let out, but for the array a
// kernel function reads where it is.
func (w *flower) call(pc int, in Instruction, s *fstate) {
	function := w.functions[in.A]
	for i, arg := range s.pop(in.B) {
		reads := i == 0 && function.builtin && arenaKernels[function.key]
		w.use(pc, arg, reads)
		if reads {
			w.consumers[pc] = arg
		}
	}
	s.push(fromElsewhere)
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
		w.useAll(pc, s.pop(len(in.Keys)+1))
		s.push(fromElsewhere)
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
		w.consumers[pc] = source
	}
	past := s.clone()
	past.push(w.loopResult(pc))
	for _, slot := range []int{in.B, in.C, in.D} {
		if slot != NoKey {
			s.locals[slot] = fromElsewhere
		}
	}
	s.loops = append(s.loops, pc)
	return []fedge{{pc + 1, s}, {in.A, past}}
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
		arena: map[int]bool{}, uses: map[int]bool{}, dest: map[int]int{}, answer: -1,
		fieldOnly: make([]bool, len(parts.Args)), viewOnly: make([]bool, len(parts.Args)),
	}
	for pc := range w.code {
		if w.arrayProducer(pc) && !w.escaped[pc] {
			f.arena[pc] = true
		}
	}
	for i, param := range parts.Args {
		f.fieldOnly[i] = param.typ.kind == RecordKind && !w.argEscaped[i]
		f.viewOnly[i] = nativeElem(param.typ) && !w.argEscaped[i]
	}
	for pc, o := range w.consumers {
		i, argument := o.arg()
		f.uses[pc] = o >= 0 && f.arena[int(o)] || argument && f.viewOnly[i]
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
		f.dest[answer] = 0
		return
	}
	if w.code[answer].Op != OpMakeRecord {
		return
	}
	f.answer = answer
	for i, field := range w.fields[answer] {
		if field >= 0 && w.arrayProducer(int(field)) && w.usedOnce(int(field)) {
			f.dest[int(field)] = i + 1
		}
	}
}
