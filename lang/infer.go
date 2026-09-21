package lang

import (
	"fmt"
	"sort"
	"strings"
)

type typeTerm struct {
	kind Kind
	id   int
	elem *typeTerm
}

type inferState struct {
	nextVar    int
	subst      map[int]typeTerm
	nodeTypes  map[int]typeTerm
	selections map[int]string
	mixed      int
}

func newInferState() *inferState {
	return &inferState{
		nextVar:    1,
		subst:      map[int]typeTerm{},
		nodeTypes:  map[int]typeTerm{},
		selections: map[int]string{},
	}
}

func (s *inferState) clone() *inferState {
	out := &inferState{
		nextVar:    s.nextVar,
		mixed:      s.mixed,
		subst:      make(map[int]typeTerm, len(s.subst)),
		nodeTypes:  make(map[int]typeTerm, len(s.nodeTypes)),
		selections: make(map[int]string, len(s.selections)),
	}
	for key, value := range s.subst {
		out.subst[key] = value
	}
	for key, value := range s.nodeTypes {
		out.nodeTypes[key] = value
	}
	for key, value := range s.selections {
		out.selections[key] = value
	}
	return out
}

func (s *inferState) fresh() typeTerm {
	term := typeTerm{kind: VarKind, id: s.nextVar}
	s.nextVar++
	return term
}

func scalarTerm(kind Kind) typeTerm { return typeTerm{kind: kind} }

func containerTerm(kind Kind, elem typeTerm) typeTerm {
	return typeTerm{kind: kind, elem: &elem}
}

func (s *inferState) deref(term typeTerm) typeTerm {
	seen := map[int]bool{}
	for term.kind == VarKind {
		if seen[term.id] {
			return term
		}
		seen[term.id] = true
		next, ok := s.subst[term.id]
		if !ok {
			return term
		}
		term = next
	}
	return term
}

func (s *inferState) unify(left, right typeTerm) error {
	left = s.deref(left)
	right = s.deref(right)
	if left.kind == VarKind {
		if right.kind == VarKind && left.id == right.id {
			return nil
		}
		if s.occurs(left.id, right) {
			return fmt.Errorf("recursive type")
		}
		s.subst[left.id] = right
		return nil
	}
	if right.kind == VarKind {
		return s.unify(right, left)
	}
	if left.kind != right.kind {
		return fmt.Errorf("cannot unify %s with %s", s.describe(left), s.describe(right))
	}
	if left.kind == ArrayKind || left.kind == DictKind {
		if left.elem == nil || right.elem == nil {
			return fmt.Errorf("malformed container type")
		}
		return s.unify(*left.elem, *right.elem)
	}
	return nil
}

func (s *inferState) occurs(id int, term typeTerm) bool {
	term = s.deref(term)
	if term.kind == VarKind {
		return term.id == id
	}
	if term.elem != nil {
		return s.occurs(id, *term.elem)
	}
	return false
}

func (s *inferState) instantiate(t Type, vars map[string]typeTerm) typeTerm {
	switch t.Kind {
	case VarKind:
		if existing, ok := vars[t.Name]; ok {
			return existing
		}
		fresh := s.fresh()
		vars[t.Name] = fresh
		return fresh
	case ArrayKind, DictKind:
		if t.Elem == nil {
			return typeTerm{kind: InvalidKind}
		}
		elem := s.instantiate(*t.Elem, vars)
		return containerTerm(t.Kind, elem)
	default:
		return scalarTerm(t.Kind)
	}
}

func concreteTerm(t Type) typeTerm {
	if t.Kind == ArrayKind || t.Kind == DictKind {
		elem := concreteTerm(*t.Elem)
		return containerTerm(t.Kind, elem)
	}
	return scalarTerm(t.Kind)
}

func (s *inferState) publicType(term typeTerm) (Type, bool) {
	term = s.deref(term)
	switch term.kind {
	case VarKind, InvalidKind:
		return Type{}, false
	case ArrayKind, DictKind:
		if term.elem == nil {
			return Type{}, false
		}
		elem, ok := s.publicType(*term.elem)
		if !ok {
			return Type{}, false
		}
		if term.kind == ArrayKind {
			return ArrayOf(elem), true
		}
		return DictOf(elem), true
	default:
		return Type{Kind: term.kind}, true
	}
}

func (s *inferState) describe(term typeTerm) string {
	term = s.deref(term)
	if typ, ok := s.publicType(term); ok {
		return typ.String()
	}
	if term.kind == VarKind {
		return fmt.Sprintf("?%d", term.id)
	}
	if term.elem != nil {
		return fmt.Sprintf("%s<%s>", term.kind, s.describe(*term.elem))
	}
	return term.kind.String()
}

type inferResult struct {
	typ   typeTerm
	state *inferState
}

type inference struct {
	Params     []Parameter
	Result     Type
	NodeTypes  map[int]Type
	Selections map[int]string
}

type inferContext struct {
	args            map[string]typeTerm
	recursiveParams []typeTerm
	recursiveResult typeTerm
	registry        *Registry
}

type programCandidate struct {
	key       string
	result    inferResult
	params    []Parameter
	resultTyp Type
}

func inferProgram(expr Expr, registry *Registry, hints map[string]Type) (*inference, error) {
	names := collectVariables(expr)
	if err := validateHints(hints, names); err != nil {
		return nil, err
	}
	initial := newInferState()
	context := newInferContext(initial, names, registry)
	if err := applyHints(initial, context.args, hints); err != nil {
		return nil, err
	}
	results, err := inferExpr(expr, initial, context)
	if err != nil {
		return nil, err
	}
	candidates, unresolved := programCandidates(unifyRoot(results, context.recursiveResult), names, context.args)
	chosen, err := chooseCandidate(candidates, unresolved)
	if err != nil {
		return nil, err
	}
	return buildInference(chosen), nil
}

func validateHints(hints map[string]Type, names []string) error {
	known := make(map[string]bool, len(names))
	for _, name := range names {
		known[name] = true
	}
	for name := range hints {
		if !known[name] {
			return fmt.Errorf("type hint provided for unknown argument %q", name)
		}
		if !hints[name].IsConcrete() {
			return fmt.Errorf("type hint for %q is not concrete: %s", name, hints[name])
		}
	}
	return nil
}

// newInferContext allocates one type variable per free variable, in the order
// the variables appear, which is also the argument order of the program.
func newInferContext(state *inferState, names []string, registry *Registry) inferContext {
	args := make(map[string]typeTerm, len(names))
	for _, name := range names {
		args[name] = state.fresh()
	}
	recursiveParams := make([]typeTerm, len(names))
	for i, name := range names {
		recursiveParams[i] = args[name]
	}
	return inferContext{
		args:            args,
		recursiveParams: recursiveParams,
		recursiveResult: state.fresh(),
		registry:        registry,
	}
}

func applyHints(state *inferState, args map[string]typeTerm, hints map[string]Type) error {
	for name, hint := range hints {
		if err := state.unify(args[name], concreteTerm(hint)); err != nil {
			return fmt.Errorf("type hint for %q: %w", name, err)
		}
	}
	return nil
}

func unifyRoot(results []inferResult, recursiveResult typeTerm) []inferResult {
	out := make([]inferResult, 0, len(results))
	for _, result := range results {
		candidate := result.state.clone()
		if err := candidate.unify(recursiveResult, result.typ); err == nil {
			out = append(out, inferResult{typ: recursiveResult, state: candidate})
		}
	}
	return out
}

func programCandidates(results []inferResult, names []string, args map[string]typeTerm) (map[string]programCandidate, []string) {
	byKey := map[string]programCandidate{}
	var unresolved []string
	for _, result := range results {
		params, missing := candidateParams(result, names, args)
		if missing != "" {
			unresolved = append(unresolved, missing)
			continue
		}
		resultType, ok := result.state.publicType(result.typ)
		if !ok {
			unresolved = append(unresolved, "result")
			continue
		}
		key := candidateKey(params, resultType)
		if _, exists := byKey[key]; !exists {
			byKey[key] = programCandidate{key: key, result: result, params: params, resultTyp: resultType}
		}
	}
	return byKey, unresolved
}

// candidateParams returns the resolved parameters, or the name of the first
// argument whose type stayed open.
func candidateParams(result inferResult, names []string, args map[string]typeTerm) ([]Parameter, string) {
	params := make([]Parameter, len(names))
	for i, name := range names {
		typ, ok := result.state.publicType(args[name])
		if !ok {
			return nil, name
		}
		params[i] = Parameter{Name: name, Type: typ}
	}
	return params, ""
}

func candidateKey(params []Parameter, result Type) string {
	parts := make([]string, len(params))
	for i, param := range params {
		parts[i] = param.Name + ":" + param.Type.String()
	}
	return strings.Join(parts, ",") + "->" + result.String()
}

func chooseCandidate(byKey map[string]programCandidate, unresolved []string) (programCandidate, error) {
	if len(byKey) == 0 {
		sort.Strings(unresolved)
		name := "expression"
		if len(unresolved) > 0 {
			name = unresolved[0]
		}
		return programCandidate{}, fmt.Errorf("cannot infer a concrete type for %s; provide a compile-time type hint", name)
	}
	best := cheapestCandidates(byKey)
	if len(best) == 1 {
		return best[0], nil
	}
	keys := make([]string, 0, len(best))
	for _, item := range best {
		keys = append(keys, item.key)
	}
	sort.Strings(keys)
	return programCandidate{}, fmt.Errorf("ambiguous expression type (%s); use int(...), float(...), string(...), bool(...), or provide a type hint", strings.Join(keys, " | "))
}

func cheapestCandidates(byKey map[string]programCandidate) []programCandidate {
	bestScore := int(^uint(0) >> 1)
	var best []programCandidate
	for _, item := range byKey {
		score := implicitCandidateScore(item.params, item.resultTyp) + item.result.state.mixed*mixedPenalty
		if score < bestScore {
			bestScore = score
			best = []programCandidate{item}
		} else if score == bestScore {
			best = append(best, item)
		}
	}
	return best
}

func buildInference(chosen programCandidate) *inference {
	state := chosen.result.state
	nodeTypes := make(map[int]Type, len(state.nodeTypes))
	for id, term := range state.nodeTypes {
		if typ, ok := state.publicType(term); ok {
			nodeTypes[id] = typ
		}
	}
	return &inference{
		Params:     chosen.params,
		Result:     chosen.resultTyp,
		NodeTypes:  nodeTypes,
		Selections: state.selections,
	}
}

// mixedPenalty makes a mixed-numeric overload the last resort: for `risk < 0.5`
// the cheap-by-type choice would be risk:int promoted to float, which is almost
// never what the author meant. Writing the mixed form explicitly still works —
// it only loses when a same-type reading exists.
const mixedPenalty = 100

func implicitCandidateScore(params []Parameter, result Type) int {
	score := implicitTypeScore(result)
	for _, param := range params {
		score += implicitTypeScore(param.Type)
	}
	return score
}

func implicitTypeScore(typ Type) int {
	switch typ.Kind {
	case IntKind:
		return 0
	case BoolKind:
		return 1
	case FloatKind:
		return 10
	case StringKind:
		return 20
	case ArrayKind, DictKind:
		if typ.Elem == nil {
			return 100
		}
		return 2 + implicitTypeScore(*typ.Elem)
	default:
		return 100
	}
}
