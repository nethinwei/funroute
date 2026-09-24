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
	nextVar    int
	subst      map[int]typeTerm
	info       map[int]varInfo
	nodeTypes  map[int]typeTerm
	selections map[int]string
	// converted is how many literals became something other than what they
	// were written as: allowed, never preferred.
	converted int
	literals  []int
	choices   []*choice
	deferred  []*deferred
	// watchers is the open choices each variable's binding may narrow,
	// queue the ones to narrow again, and probing how deep inside a trial
	// the state is: a trial is taken back, so it narrows nothing.
	watchers map[int][]*choice
	queue    []*choice
	probing  int
	// checks run once everything is decided: what can only be proven of
	// settled types.
	checks []func() error
	trail  []undo
}

// undo is one change on the trail, with what it replaced.
type undo struct {
	op        undoOp
	id        int
	term      typeTerm
	info      varInfo
	had       bool
	key       string
	converted int
	choice    *choice
	deferred  *deferred
	open      []*machine.RegisteredFunction
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
		nextVar:    1,
		subst:      map[int]typeTerm{},
		info:       map[int]varInfo{},
		nodeTypes:  map[int]typeTerm{},
		selections: map[int]string{},
		watchers:   map[int][]*choice{},
	}
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
		restore(s.subst, entry.id, entry.term, entry.had)
	case undoInfo:
		restore(s.info, entry.id, entry.info, entry.had)
	case undoSelection:
		restore(s.selections, entry.id, entry.key, entry.had)
	case undoConverted:
		s.converted = entry.converted
	case undoChoice:
		entry.choice.open = entry.open
	case undoDeferred:
		entry.deferred.done = false
	}
}

func restore[V any](m map[int]V, id int, old V, had bool) {
	if had {
		m[id] = old
	} else {
		delete(m, id)
	}
}

func (s *inferState) bind(id int, term typeTerm) {
	old, had := s.subst[id]
	s.trail = append(s.trail, undo{op: undoSubst, id: id, term: old, had: had})
	s.subst[id] = term
	if s.probing > 0 {
		return
	}
	s.touch(id)
	for _, v := range s.freeVars(term, nil) {
		s.watchers[v] = append(s.watchers[v], s.watchers[id]...)
	}
}

func (s *inferState) setInfo(id int, info varInfo) {
	old, had := s.info[id]
	s.trail = append(s.trail, undo{op: undoInfo, id: id, info: old, had: had})
	s.info[id] = info
	if s.probing == 0 {
		s.touch(id)
	}
}

// touch queues the open choices watching variable id.
func (s *inferState) touch(id int) {
	for _, c := range s.watchers[id] {
		if c.open != nil && !c.queued {
			c.queued = true
			s.queue = append(s.queue, c)
		}
	}
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
	s.trail = append(s.trail, undo{op: undoSelection, id: node, key: old, had: had})
	s.selections[node] = key
}

func (s *inferState) convert(delta int) {
	if delta == 0 {
		return
	}
	s.trail = append(s.trail, undo{op: undoConverted, converted: s.converted})
	s.converted += delta
}

// infoOf is what variable id may become; a variable nothing restricts may
// become anything.
func (s *inferState) infoOf(id int) varInfo {
	if info, ok := s.info[id]; ok {
		return info
	}
	return varInfo{domain: allKinds}
}

func (s *inferState) fresh() typeTerm {
	term := varTerm(s.nextVar)
	s.nextVar++
	return term
}

// varTerm is the term of type variable id.
func varTerm(id int) typeTerm { return typeTerm{kind: machine.VarKind, id: id} }

func scalarTerm(kind machine.Kind) typeTerm { return typeTerm{kind: kind} }

func containerTerm(kind machine.Kind, elem typeTerm) typeTerm {
	return typeTerm{kind: kind, elem: &elem}
}

func (s *inferState) deref(term typeTerm) typeTerm {
	for term.kind == machine.VarKind {
		next, ok := s.subst[term.id]
		if !ok {
			return term
		}
		term = next
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
		return fmt.Errorf("cannot unify %s with %s", s.describe(left), s.describe(right))
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
		return fmt.Errorf("cannot unify %s with %s", s.describe(left), s.describe(right))
	}
	for i, field := range left.fields {
		if field.name != right.fields[i].name {
			return fmt.Errorf("cannot unify %s with %s", s.describe(left), s.describe(right))
		}
		if err := s.unify(field.term, right.fields[i].term); err != nil {
			return err
		}
	}
	return nil
}

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

// instantiate is a signature's type with its type variables replaced by the
// call's, one fresh variable per name.
func (s *inferState) instantiate(t machine.Type, vars map[string]typeTerm) typeTerm {
	switch t.Kind() {
	case machine.VarKind:
		if existing, ok := vars[t.Name()]; ok {
			return existing
		}
		fresh := s.fresh()
		vars[t.Name()] = fresh
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
		fields := make([]fieldTerm, len(t.Fields()))
		for i, field := range t.Fields() {
			fields[i] = fieldTerm{name: field.Name(), term: s.concrete(field.Type())}
		}
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
		fields := make([]string, len(term.fields))
		for i, field := range term.fields {
			fields[i] = field.name + ": " + s.describe(field.term)
		}
		return "record{" + strings.Join(fields, ", ") + "}"
	}
	return term.kind.String()
}

type inference struct {
	Params     []machine.Parameter
	Result     machine.Type
	ResultDoc  string
	NodeTypes  map[int]machine.Type
	Selections map[int]string
}

type inferContext struct {
	args     map[string]typeTerm
	registry *machine.Registry
	// hints is the contract's argument types.
	hints map[string]machine.Type
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
	names := syntax.FreeVariables(expr)
	if order != nil {
		names = order
	}
	state := newInferState()
	context := newInferContext(state, names, registry)
	context.hints = hints
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
	found := state.describe(result)
	if err := state.unify(result, state.concrete(*ret)); err != nil {
		return syntax.Around(expr, "type error: the contract returns %s but the expression returns %s", ret, found)
	}
	return nil
}

// newInferContext allocates one type variable per free variable, in the order
// the variables appear, which is also the argument order of the program.
func newInferContext(state *inferState, names []string, registry *machine.Registry) inferContext {
	args := make(map[string]typeTerm, len(names))
	for _, name := range names {
		args[name] = state.fresh()
	}
	_, money := registry.Money()
	return inferContext{args: args, registry: registry, enums: map[string]machine.Type{}, money: money}
}

func applyHints(state *inferState, args map[string]typeTerm, hints map[string]machine.Type) error {
	for name, hint := range hints {
		if err := state.unify(args[name], state.concrete(hint)); err != nil {
			return fmt.Errorf("type hint for %q: %w", name, err)
		}
	}
	return nil
}

// inference is what the solved state says of the program: every argument's
// type, the result's, every node's and every call's function.
func (s *inferState) inference(names []string, args map[string]typeTerm, result typeTerm) (*inference, error) {
	for _, check := range s.checks {
		if err := check(); err != nil {
			return nil, err
		}
	}
	params := make([]machine.Parameter, len(names))
	for i, name := range names {
		typ, ok := s.publicType(args[name])
		if !ok {
			return nil, fmt.Errorf("cannot infer a concrete type for %s; provide a compile-time type hint", name)
		}
		params[i] = machine.NewParameter(name, typ, "")
	}
	resultType, ok := s.publicType(result)
	if !ok {
		return nil, errors.New("cannot infer a concrete type for result; provide a compile-time type hint")
	}
	nodeTypes := make(map[int]machine.Type, len(s.nodeTypes))
	for id, term := range s.nodeTypes {
		if typ, ok := s.publicType(term); ok {
			nodeTypes[id] = typ
		}
	}
	return &inference{Params: params, Result: resultType, NodeTypes: nodeTypes, Selections: s.selections}, nil
}
