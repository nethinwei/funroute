package compile

// Inference reads a program once. Every node gets one type term; what the
// program leaves open is a type variable, a variable may be held to a set
// of kinds (a literal: infer_literal.go), and a call with several overloads
// that fit is a choice decided once everything certain is known
// (infer_solve.go). Every change goes on a trail, so trying a candidate and
// taking it back costs what the candidate touched, never a copy of the
// state.

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/nethinwei/funroute/internal/kit"
	"github.com/nethinwei/funroute/internal/machine"
	"github.com/nethinwei/funroute/internal/syntax"
)

type typeTerm struct {
	kind   machine.Kind
	id     int
	elem   *typeTerm
	name   string // a handle's or an enum's name; empty otherwise
	values []string
	// fields is a record's, in order: a record unifies field by field, so a
	// literal in a record literal's field settles the way any literal does.
	fields []fieldTerm
}

type fieldTerm struct {
	name string
	term typeTerm
}

// varInfo is what a variable may still become, and how many literals
// written as an int or a float have it as their type.
type varInfo struct {
	domain       kindSet
	ints, floats int
}

// forced is how many of the variable's literals can no longer be what they
// were written as.
func (v varInfo) forced() int {
	n := 0
	if !v.domain.has(machine.IntKind) {
		n += v.ints
	}
	if !v.domain.has(machine.FloatKind) {
		n += v.floats
	}
	return n
}

type inferState struct {
	// vars is every type variable by id, from 1: what it is bound to, what
	// it may still become, and the open choices its binding may narrow.
	vars []varSlot
	// nodeTypes is every node's term by node id; selections is every
	// call's function by node id.
	nodeTypes  []typeTerm
	selections map[int]string
	// converted is how many literals became something other than what they
	// were written as: allowed, never preferred.
	converted int
	literals  []int
	choices   []*choice
	deferred  []*deferred
	// queue is the open choices to narrow again, and probing how deep inside
	// a trial the state is: a trial is taken back, so it narrows nothing.
	queue   []*choice
	probing int
	// checks run once everything is decided: what can only be proven of
	// settled types.
	checks []func() error
	// trail is every change a trial may take back, in order; what each
	// replaced is on the stack of its kind, last on top, so a change costs
	// the trail no more than its kind and where.
	trail        []undo
	oldTerms     []typeTerm
	oldInfos     []varInfo
	oldKeys      []string
	oldConverted []int
	oldChoices   []openChoice
	oldDeferred  []*deferred
}

// varSlot is one type variable. bound says term is what it is, and
// restricted that info says what it may become; a variable nothing
// restricts may become anything.
type varSlot struct {
	term       typeTerm
	info       varInfo
	bound      bool
	restricted bool
	watchers   []*choice
}

// undo is one change on the trail: its kind, what it changed, and whether
// there was a value before, which is on its kind's stack.
type undo struct {
	op  undoOp
	had bool
	id  int
}

// openChoice is a choice as it was: its candidates still open.
type openChoice struct {
	choice *choice
	open   []*machine.RegisteredFunction
}

type undoOp uint8

const (
	undoSubst undoOp = iota
	undoInfo
	undoSelection
	undoConverted
	undoChoice
	undoDeferred
)

func newInferState() *inferState {
	return &inferState{
		vars:       make([]varSlot, 1, 8),
		selections: map[int]string{},
		trail:      make([]undo, 0, 16),
		oldTerms:   make([]typeTerm, 0, 8),
	}
}

// inferStates keeps the states of finished inferences: a compile would
// otherwise make its lists and maps afresh, mostly to the same sizes again.
var inferStates = sync.Pool{New: func() any { return newInferState() }}

// maxKeptVars is the most variables a state goes back to the pool with; a
// larger one is left to the collector.
const maxKeptVars = 1 << 10

func acquireInferState() *inferState {
	if s, ok := inferStates.Get().(*inferState); ok {
		return s
	}
	return newInferState()
}

// release empties s and gives it back: nothing it holds outlives the
// inference, whose answer is built of its own, except selections, which the
// answer takes and s replaces.
func (s *inferState) release() {
	if len(s.vars) > maxKeptVars {
		return
	}
	clear(s.vars)
	clear(s.nodeTypes)
	clear(s.choices)
	clear(s.deferred)
	clear(s.queue)
	clear(s.checks)
	clear(s.oldTerms)
	clear(s.oldKeys)
	clear(s.oldChoices)
	clear(s.oldDeferred)
	*s = inferState{
		vars: s.vars[:1], nodeTypes: s.nodeTypes[:0], selections: map[int]string{}, literals: s.literals[:0],
		choices: s.choices[:0], deferred: s.deferred[:0], queue: s.queue[:0], checks: s.checks[:0],
		trail: s.trail[:0], oldTerms: s.oldTerms[:0], oldInfos: s.oldInfos[:0], oldKeys: s.oldKeys[:0],
		oldConverted: s.oldConverted[:0], oldChoices: s.oldChoices[:0], oldDeferred: s.oldDeferred[:0],
	}
	inferStates.Put(s)
}

// mark is a point on the trail to come back to.
func (s *inferState) mark() int { return len(s.trail) }

// undoTo takes back every change made since mark.
func (s *inferState) undoTo(mark int) {
	for len(s.trail) > mark {
		entry := s.trail[len(s.trail)-1]
		s.trail = s.trail[:len(s.trail)-1]
		s.revert(entry)
	}
}

func (s *inferState) revert(entry undo) {
	switch entry.op {
	case undoSubst:
		slot := &s.vars[entry.id]
		slot.term, slot.bound = popped(&s.oldTerms), entry.had
	case undoInfo:
		slot := &s.vars[entry.id]
		slot.info, slot.restricted = popped(&s.oldInfos), entry.had
	case undoSelection:
		if old := popped(&s.oldKeys); entry.had {
			s.selections[entry.id] = old
		} else {
			delete(s.selections, entry.id)
		}
	case undoConverted:
		s.converted = popped(&s.oldConverted)
	case undoChoice:
		old := popped(&s.oldChoices)
		old.choice.open = old.open
	case undoDeferred:
		popped(&s.oldDeferred).done = false
	}
}

// changed puts a change on the trail and what it replaced on its stack.
func changed[V any](s *inferState, entry undo, stack *[]V, old V) {
	s.trail = append(s.trail, entry)
	*stack = append(*stack, old)
}

// popped takes the top of a stack of what a change replaced.
func popped[V any](stack *[]V) V {
	last := len(*stack) - 1
	old := (*stack)[last]
	var zero V
	(*stack)[last] = zero
	*stack = (*stack)[:last]
	return old
}

func (s *inferState) bind(id int, term typeTerm) {
	slot := &s.vars[id]
	changed(s, undo{op: undoSubst, id: id, had: slot.bound}, &s.oldTerms, slot.term)
	slot.term, slot.bound = term, true
	if s.probing > 0 {
		return
	}
	s.touch(id)
	for _, v := range s.freeVars(term, nil) {
		s.vars[v].watchers = append(s.vars[v].watchers, s.vars[id].watchers...)
	}
}

func (s *inferState) setInfo(id int, info varInfo) {
	slot := &s.vars[id]
	changed(s, undo{op: undoInfo, id: id, had: slot.restricted}, &s.oldInfos, slot.info)
	slot.info, slot.restricted = info, true
	if s.probing == 0 {
		s.touch(id)
	}
}

// touch queues the open choices watching variable id.
func (s *inferState) touch(id int) {
	for _, c := range s.vars[id].watchers {
		if c.open != nil && !c.queued {
			c.queued = true
			s.queue = append(s.queue, c)
		}
	}
}

// watch has c narrowed again whenever variable id is bound.
func (s *inferState) watch(id int, c *choice) {
	s.vars[id].watchers = append(s.vars[id].watchers, c)
}

// freeVars appends the open variables term holds to vars.
func (s *inferState) freeVars(term typeTerm, vars []int) []int {
	term = s.deref(term)
	switch {
	case term.kind == machine.VarKind:
		return append(vars, term.id)
	case term.elem != nil:
		return s.freeVars(*term.elem, vars)
	}
	for _, field := range term.fields {
		vars = s.freeVars(field.term, vars)
	}
	return vars
}

func (s *inferState) selectKey(node int, key string) {
	old, had := s.selections[node]
	changed(s, undo{op: undoSelection, id: node, had: had}, &s.oldKeys, old)
	s.selections[node] = key
}

func (s *inferState) convert(delta int) {
	if delta == 0 {
		return
	}
	changed(s, undo{op: undoConverted}, &s.oldConverted, s.converted)
	s.converted += delta
}

// setType stamps node id's term.
func (s *inferState) setType(id int, term typeTerm) {
	if id >= len(s.nodeTypes) {
		s.nodeTypes = append(s.nodeTypes, make([]typeTerm, id+1-len(s.nodeTypes))...)
	}
	s.nodeTypes[id] = term
}

// infoOf is what variable id may become; a variable nothing restricts may
// become anything.
func (s *inferState) infoOf(id int) varInfo {
	if slot := &s.vars[id]; slot.restricted {
		return slot.info
	}
	return varInfo{domain: allKinds}
}

func (s *inferState) fresh() typeTerm {
	s.vars = append(s.vars, varSlot{})
	return varTerm(len(s.vars) - 1)
}

// varTerm is the term of type variable id.
func varTerm(id int) typeTerm { return typeTerm{kind: machine.VarKind, id: id} }

func scalarTerm(kind machine.Kind) typeTerm { return typeTerm{kind: kind} }

func containerTerm(kind machine.Kind, elem typeTerm) typeTerm {
	return typeTerm{kind: kind, elem: &elem}
}

func (s *inferState) deref(term typeTerm) typeTerm {
	for term.kind == machine.VarKind {
		slot := &s.vars[term.id]
		if !slot.bound {
			return term
		}
		term = slot.term
	}
	return term
}

func (s *inferState) unify(left, right typeTerm) error {
	left, right = s.deref(left), s.deref(right)
	switch {
	case left.kind == machine.VarKind:
		return s.bindVar(left, right)
	case right.kind == machine.VarKind:
		return s.bindVar(right, left)
	case left.kind != right.kind || left.name != right.name || !slices.Equal(left.values, right.values):
		return s.cannotUnify(left, right)
	case left.kind == machine.RecordKind:
		return s.unifyFields(left, right)
	case left.elem != nil && right.elem != nil:
		return s.unify(*left.elem, *right.elem)
	}
	return nil
}

// unifyFields unifies two records: the same names in the same order, and
// each field's type.
func (s *inferState) unifyFields(left, right typeTerm) error {
	if len(left.fields) != len(right.fields) {
		return s.cannotUnify(left, right)
	}
	for i, field := range left.fields {
		if field.name != right.fields[i].name {
			return s.cannotUnify(left, right)
		}
		if err := s.unify(field.term, right.fields[i].term); err != nil {
			return err
		}
	}
	return nil
}

func (s *inferState) cannotUnify(left, right typeTerm) error {
	if s.probing > 0 {
		return errNoFit
	}
	return fmt.Errorf("cannot unify %s with %s", s.describe(left), s.describe(right))
}

// errNoFit is every failure inside a trial: a trial asks only whether a
// candidate fits, and is taken back, so what failed is never said.
var errNoFit = errors.New("does not fit")

// bindVar binds an open variable to term, keeping what the variable may
// become: two variables meet in what both may become, and a variable meets
// a type only if that type's kind is one it may be.
func (s *inferState) bindVar(variable, term typeTerm) error {
	if term.kind == machine.VarKind && term.id == variable.id {
		return nil
	}
	mine := s.infoOf(variable.id)
	if term.kind == machine.VarKind {
		theirs := s.infoOf(term.id)
		merged := varInfo{domain: mine.domain & theirs.domain, ints: mine.ints + theirs.ints, floats: mine.floats + theirs.floats}
		if merged.domain == 0 {
			return errors.New("the literals cannot share a type")
		}
		if merged != theirs {
			s.setInfo(term.id, merged)
		}
		s.convert(merged.forced() - mine.forced() - theirs.forced())
		s.bind(variable.id, term)
		return nil
	}
	if !mine.domain.has(term.kind) {
		if s.probing > 0 {
			return errNoFit
		}
		return fmt.Errorf("a literal cannot be %s", s.describe(term))
	}
	if s.occurs(variable.id, term) {
		return errors.New("recursive type")
	}
	s.convert(varInfo{domain: kindsOf(term.kind), ints: mine.ints, floats: mine.floats}.forced() - mine.forced())
	s.bind(variable.id, term)
	return nil
}

func (s *inferState) occurs(id int, term typeTerm) bool {
	term = s.deref(term)
	switch {
	case term.kind == machine.VarKind:
		return term.id == id
	case term.elem != nil:
		return s.occurs(id, *term.elem)
	}
	return slices.ContainsFunc(term.fields, func(field fieldTerm) bool { return s.occurs(id, field.term) })
}

// namedTerms is a few terms by name: a program's arguments, or the call's
// variable for each type variable of a signature met so far. There are few
// enough that a list is quicker than a map.
type namedTerms []namedTerm

type namedTerm struct {
	name string
	term typeTerm
}

// find is the term named name.
func (n namedTerms) find(name string) (typeTerm, bool) {
	for _, named := range n {
		if named.name == name {
			return named.term, true
		}
	}
	return typeTerm{}, false
}

// instantiate is a signature's type with its type variables replaced by the
// call's, one fresh variable per name.
func (s *inferState) instantiate(t machine.Type, vars *namedTerms) typeTerm {
	switch t.Kind() {
	case machine.VarKind:
		if existing, ok := vars.find(t.Name()); ok {
			return existing
		}
		fresh := s.fresh()
		*vars = append(*vars, namedTerm{name: t.Name(), term: fresh})
		return fresh
	case machine.ArrayKind, machine.DictKind:
		if !hasElem(t) {
			return scalarTerm(machine.InvalidKind)
		}
		return containerTerm(t.Kind(), s.instantiate(elemOf(t), vars))
	case machine.EnumKind:
		if t.Name() == "" {
			// The wildcard a signature writes for any enum is a variable
			// only an enum can fill.
			fresh := s.fresh()
			s.setInfo(fresh.id, varInfo{domain: kindsOf(machine.EnumKind)})
			return fresh
		}
	}
	return s.concrete(t)
}

// concrete is the term of a known type.
func (s *inferState) concrete(t machine.Type) typeTerm {
	switch t.Kind() {
	case machine.ArrayKind, machine.DictKind:
		return containerTerm(t.Kind(), s.concrete(elemOf(t)))
	case machine.RecordKind:
		fields := kit.Map(t.Fields(), func(field machine.Field) fieldTerm {
			return fieldTerm{name: field.Name(), term: s.concrete(field.Type())}
		})
		return typeTerm{kind: machine.RecordKind, fields: fields}
	default:
		return typeTerm{kind: t.Kind(), name: t.Name(), values: t.Values()}
	}
}

// publicType is the type term stands for, and false while any part of it is
// open.
func (s *inferState) publicType(term typeTerm) (machine.Type, bool) {
	term = s.deref(term)
	switch term.kind {
	case machine.VarKind, machine.InvalidKind:
		return machine.Type{}, false
	case machine.RecordKind:
		fields := make([]machine.Field, len(term.fields))
		for i, field := range term.fields {
			typ, ok := s.publicType(field.term)
			if !ok {
				return machine.Type{}, false
			}
			fields[i] = machine.FieldOf(field.name, typ)
		}
		return machine.RecordOf(fields...), true
	case machine.ArrayKind, machine.DictKind:
		if term.elem == nil {
			return machine.Type{}, false
		}
		elem, ok := s.publicType(*term.elem)
		if !ok {
			return machine.Type{}, false
		}
		if term.kind == machine.ArrayKind {
			return machine.ArrayOf(elem), true
		}
		return machine.DictOf(elem), true
	default:
		return machine.ScalarType(term.kind, term.name, term.values), true
	}
}

func (s *inferState) describe(term typeTerm) string {
	term = s.deref(term)
	if typ, ok := s.publicType(term); ok {
		return typ.String()
	}
	switch {
	case term.kind == machine.VarKind:
		return s.describeOpen(term)
	case term.elem != nil:
		return fmt.Sprintf("%s<%s>", term.kind, s.describe(*term.elem))
	case term.kind == machine.RecordKind:
		fields := kit.Map(term.fields, func(field fieldTerm) string { return field.name + ": " + s.describe(field.term) })
		return "record{" + strings.Join(fields, ", ") + "}"
	}
	return term.kind.String()
}

type inference struct {
	Params    []machine.Parameter
	Result    machine.Type
	ResultDoc string
	// nodeTypes is every node's type by node id: an open one's is none.
	nodeTypes  []machine.Type
	Selections map[int]string
}

type inferContext struct {
	// args is the program's arguments, and locals the names the loops and
	// lets around a node bind, innermost first, hiding an argument's.
	args     namedTerms
	locals   *localName
	registry *machine.Registry
	// money is whether the registry declares money, which is what lets a
	// literal become a ratio or money.
	money bool
	// enums is the contract's enum namespace: every enum type the host
	// declared for this program, by name. A bare @member is resolved in it.
	enums map[string]machine.Type
}

// inferProgram infers the argument and result types.
//
// order, when non-nil, replaces the appearance order of the free variables as
// the argument order: that is how the host's contract pins the ABI down, and it
// is also what lets a declared-but-unused argument stay in the signature.
//
// ret, when non-nil, is unified with the result before anything is decided,
// so a declared float result settles `1 + 2` as float arithmetic instead of
// rejecting it, and every node the result flows from — the items of an
// array, the branches of an if — is typed by it.
func inferProgram(expr syntax.Expr, registry *machine.Registry, hints map[string]machine.Type, order []string, ret *machine.Type) (*inference, error) {
	names := order
	if names == nil {
		names = syntax.FreeVariables(expr)
	}
	state := acquireInferState()
	defer state.release()
	context := newInferContext(state, names, registry)
	if err := collectEnums(context.enums, hints, ret); err != nil {
		return nil, err
	}
	addRegistryEnums(context.enums, registry)
	if err := applyHints(state, context.args, hints); err != nil {
		return nil, err
	}
	result, err := inferExpr(expr, state, context)
	if err != nil {
		return nil, err
	}
	if err := applyResultType(expr, state, result, ret); err != nil {
		return nil, err
	}
	if err := state.solve(); err != nil {
		// A choice that settled wrong says where it is; only what has no
		// place of its own is placed on the whole program.
		if _, placed := errors.AsType[*syntax.PosError](err); placed {
			return nil, err
		}
		return nil, syntax.Around(expr, "type error: %v", err)
	}
	inferred, err := state.inference(names, context.args, result)
	if err != nil {
		return nil, err
	}
	if ret != nil {
		// The declared result is the ABI.
		inferred.Result = *ret
	}
	return inferred, nil
}

// applyResultType unifies the result with the declared type before anything
// is decided, which is what makes the contract's result disambiguating
// rather than merely checking.
func applyResultType(expr syntax.Expr, state *inferState, result typeTerm, ret *machine.Type) error {
	if ret == nil {
		return nil
	}
	mark := state.mark()
	if err := state.unify(result, state.concrete(*ret)); err != nil {
		// Said of the result as it was before the contract met it.
		state.undoTo(mark)
		return syntax.Around(expr, "type error: the contract returns %s but the expression returns %s", ret, state.describe(result))
	}
	return nil
}

// newInferContext allocates one type variable per free variable, in the order
// the variables appear, which is also the argument order of the program.
func newInferContext(state *inferState, names []string, registry *machine.Registry) inferContext {
	args := make(namedTerms, len(names))
	for i, name := range names {
		args[i] = namedTerm{name: name, term: state.fresh()}
	}
	_, money := registry.Money()
	return inferContext{args: args, registry: registry, enums: map[string]machine.Type{}, money: money}
}

func applyHints(state *inferState, args namedTerms, hints map[string]machine.Type) error {
	// In name order, so a failure is said the same every time.
	for _, name := range kit.SortedKeys(hints) {
		hint := hints[name]
		arg, _ := args.find(name)
		if err := state.unify(arg, state.concrete(hint)); err != nil {
			return fmt.Errorf("type hint for %q: %w", name, err)
		}
	}
	return nil
}

// inference is what the solved state says of the program: every argument's
// type, the result's, every node's and every call's function.
func (s *inferState) inference(names []string, args namedTerms, result typeTerm) (*inference, error) {
	for _, check := range s.checks {
		if err := check(); err != nil {
			return nil, err
		}
	}
	params := make([]machine.Parameter, len(names))
	for i, name := range names {
		typ, ok := s.publicType(args[i].term)
		if !ok {
			return nil, fmt.Errorf("cannot infer a concrete type for %s; provide a compile-time type hint", name)
		}
		params[i] = machine.NewParameter(name, typ, "")
	}
	resultType, ok := s.publicType(result)
	if !ok {
		return nil, errors.New("cannot infer a concrete type for result; provide a compile-time type hint")
	}
	nodeTypes := make([]machine.Type, len(s.nodeTypes))
	for id, term := range s.nodeTypes {
		nodeTypes[id], _ = s.publicType(term)
	}
	return &inference{Params: params, Result: resultType, nodeTypes: nodeTypes, Selections: s.selections}, nil
}

// nodeType is the type of node id, and false when inference left it open.
func (i *inference) nodeType(id int) (machine.Type, bool) {
	if id < 0 || id >= len(i.nodeTypes) || i.nodeTypes[id].Kind() == machine.InvalidKind {
		return machine.Type{}, false
	}
	return i.nodeTypes[id], true
}

// typeOf is the type of node id, the zero type when inference left it open.
func (i *inference) typeOf(id int) machine.Type {
	typ, _ := i.nodeType(id)
	return typ
}
