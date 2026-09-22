package compile

import (
	"fmt"
	"funroute/lang/internal/machine"
	"funroute/lang/internal/syntax"
	"slices"
	"strings"
)

// record stamps the inferred type of expr into every candidate state.
func record(expr syntax.Expr, results []inferResult) []inferResult {
	for _, result := range results {
		result.state.nodeTypes[expr.NodeID()] = result.typ
	}
	return results
}

func inferExpr(expr syntax.Expr, state *inferState, context inferContext) ([]inferResult, error) {
	switch node := expr.(type) {
	case *syntax.LiteralExpr:
		return record(node, []inferResult{{typ: concreteTerm(node.Value.Type()), state: state}}), nil
	case *syntax.VariableExpr:
		return inferVariable(node, state, context)
	case *syntax.EnumExpr:
		return inferEnum(node, state, context)
	case *syntax.ArrayExpr:
		return inferHomogeneous(node, node.Items, machine.ArrayKind, node.Pos, "array elements must have one type", state, context)
	case *syntax.DictExpr:
		return inferDict(node, state, context)
	case *syntax.SwitchExpr:
		return inferSwitch(node, state, context)
	case *syntax.ForExpr:
		return inferFor(node, state, context)
	case *syntax.ReduceExpr:
		return inferReduce(node, state, context)
	case *syntax.LetExpr:
		return inferLet(node, state, context)
	case *syntax.CallExpr:
		return inferCall(node, state, context)
	default:
		return nil, fmt.Errorf("internal error: unsupported expression %T", expr)
	}
}

// inferEnum types @member from the contract's enum namespace, not from the
// surrounding expression, so a reference is typed wherever it appears.
func inferEnum(node *syntax.EnumExpr, state *inferState, context inferContext) ([]inferResult, error) {
	typ, err := resolveEnumReference(node, context.enums)
	if err != nil {
		return nil, err
	}
	return record(node, []inferResult{{typ: concreteTerm(typ), state: state}}), nil
}

func inferVariable(node *syntax.VariableExpr, state *inferState, context inferContext) ([]inferResult, error) {
	term, ok := context.args[node.Name]
	if !ok {
		return nil, fmt.Errorf("internal error: variable %q was not collected", node.Name)
	}
	return record(node, []inferResult{{typ: term, state: state}}), nil
}

func inferDict(node *syntax.DictExpr, state *inferState, context inferContext) ([]inferResult, error) {
	values := make([]syntax.Expr, len(node.Entries))
	for i, entry := range node.Entries {
		values[i] = entry.Value
	}
	return inferHomogeneous(node, values, machine.DictKind, node.Pos, "dictionary values must have one type", state, context)
}

// inferHomogeneous infers an array or dictionary node, whose elements all share
// one element type.
func inferHomogeneous(node syntax.Expr, items []syntax.Expr, kind machine.Kind, pos int, message string, state *inferState, context inferContext) ([]inferResult, error) {
	elem := state.fresh()
	states := []*inferState{state}
	for _, item := range items {
		next, err := unifyElement(item, elem, states, context)
		if err != nil {
			return nil, err
		}
		if len(next) == 0 {
			return nil, fmt.Errorf("type error at byte %d: %s", pos, message)
		}
		states = next
	}
	out := make([]inferResult, len(states))
	for i, partial := range states {
		out[i] = inferResult{typ: containerTerm(kind, elem), state: partial}
	}
	return record(node, out), nil
}

func unifyElement(item syntax.Expr, elem typeTerm, states []*inferState, context inferContext) ([]*inferState, error) {
	var next []*inferState
	for _, partial := range states {
		inferred, err := inferExpr(item, partial, context)
		if err != nil {
			return nil, err
		}
		for _, result := range inferred {
			candidate := result.state.clone()
			if err := candidate.unify(elem, result.typ); err == nil {
				next = append(next, candidate)
			}
		}
	}
	return next, nil
}

type partialSwitch struct {
	state   *inferState
	subject typeTerm
	result  typeTerm
}

func inferSwitch(node *syntax.SwitchExpr, state *inferState, context inferContext) ([]inferResult, error) {
	subjects, err := inferSwitchSubject(node, state, context)
	if err != nil {
		return nil, err
	}
	partials := make([]partialSwitch, 0, len(subjects))
	for _, subject := range subjects {
		partials = append(partials, partialSwitch{state: subject.state, subject: subject.typ, result: subject.state.fresh()})
	}
	for _, item := range node.Cases {
		next, err := inferSwitchCase(item, partials, context)
		if err != nil {
			return nil, err
		}
		if len(next) == 0 {
			return nil, fmt.Errorf("type error at byte %d: switch branches must match the subject and return one type%s",
				node.Pos, memberWrittenAsString(item, context.enums))
		}
		partials = next
	}
	if err := validateEnumSwitch(node, partials); err != nil {
		return nil, err
	}
	if node.Default == nil {
		out := make([]inferResult, len(partials))
		for i, partial := range partials {
			out[i] = inferResult{typ: partial.result, state: partial.state}
		}
		return record(node, out), nil
	}
	out, err := inferSwitchDefault(node.Default, partials, context)
	if err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("type error at byte %d: switch default must match the branch result type", node.Pos)
	}
	return record(node, out), nil
}

// memberWrittenAsString names the likely cause when a branch does not fit: a
// member spelled as a string. An enum is nominal, so "adyen" is not @adyen.
func memberWrittenAsString(item syntax.SwitchCaseExpr, enums map[string]machine.Type) string {
	for _, match := range item.Match {
		literal, ok := match.(*syntax.LiteralExpr)
		if !ok {
			continue
		}
		value, isString := literal.Value.String()
		if !isString {
			continue
		}
		for _, enum := range enums {
			if slices.Contains(enum.Values, value) {
				return fmt.Sprintf("; %q is a member of %s, written @%s", value, enum.Summary(), value)
			}
		}
	}
	return ""
}

func validateEnumSwitch(node *syntax.SwitchExpr, partials []partialSwitch) error {
	for _, partial := range partials {
		typ, ok := partial.state.publicType(partial.subject)
		if !ok || typ.Kind != machine.EnumKind {
			if node.Default == nil {
				return fmt.Errorf("type error at byte %d: switch without else requires a declared enum subject", node.Pos)
			}
			continue
		}
		if err := validateEnumCases(node, typ); err != nil {
			return err
		}
	}
	return nil
}

func validateEnumCases(node *syntax.SwitchExpr, enum machine.Type) error {
	seen := map[string]bool{}
	for _, item := range node.Cases {
		for _, match := range item.Match {
			if err := recordEnumMatch(node, enum, match, seen); err != nil {
				return err
			}
		}
	}
	if node.Default != nil || len(seen) == len(enum.Values) {
		return nil
	}
	missing := make([]string, 0, len(enum.Values)-len(seen))
	for _, value := range enum.Values {
		if !seen[value] {
			missing = append(missing, value)
		}
	}
	return fmt.Errorf("type error at byte %d: enum switch is not exhaustive; missing %s", node.Pos, strings.Join(missing, ", "))
}

func recordEnumMatch(node *syntax.SwitchExpr, enum machine.Type, match syntax.Expr, seen map[string]bool) error {
	member, ok := match.(*syntax.EnumExpr)
	if !ok {
		if node.Default == nil {
			return fmt.Errorf("type error at byte %d: an exhaustive enum switch matches enum members, such as @%s", match.Position(), enum.Values[0])
		}
		return nil
	}
	value := member.Member
	if !slices.Contains(enum.Values, value) {
		return fmt.Errorf("type error at byte %d: %q is not a member of %s", match.Position(), value, enum.Summary())
	}
	if seen[value] {
		return fmt.Errorf("type error at byte %d: enum member %q is matched more than once", match.Position(), value)
	}
	seen[value] = true
	return nil
}

// inferSwitchSubject types the subject. The subjectless form has none, and its
// matches are conditions — which is exactly a bool subject, so both forms share
// the unification below.
func inferSwitchSubject(node *syntax.SwitchExpr, state *inferState, context inferContext) ([]inferResult, error) {
	if node.Value != nil {
		return inferExpr(node.Value, state, context)
	}
	return []inferResult{{typ: scalarTerm(machine.BoolKind), state: state}}, nil
}

func inferSwitchCase(item syntax.SwitchCaseExpr, partials []partialSwitch, context inferContext) ([]partialSwitch, error) {
	var next []partialSwitch
	for _, partial := range partials {
		matched, err := inferMatches(item.Match, partial, context)
		if err != nil {
			return nil, err
		}
		for _, state := range matched {
			results, err := inferExpr(item.Result, state, context)
			if err != nil {
				return nil, err
			}
			next = append(next, unifyResults(results, partial)...)
		}
	}
	return next, nil
}

// inferMatches unifies every value of a multi-value branch with the subject.
func inferMatches(matches []syntax.Expr, partial partialSwitch, context inferContext) ([]*inferState, error) {
	states := []*inferState{partial.state}
	for _, match := range matches {
		candidates, err := unifyElement(match, partial.subject, states, context)
		if err != nil {
			return nil, err
		}
		if len(candidates) == 0 {
			return nil, nil
		}
		states = candidates
	}
	return states, nil
}

func unifyResults(results []inferResult, partial partialSwitch) []partialSwitch {
	var next []partialSwitch
	for _, result := range results {
		candidate := result.state.clone()
		if err := candidate.unify(partial.result, result.typ); err == nil {
			next = append(next, partialSwitch{state: candidate, subject: partial.subject, result: partial.result})
		}
	}
	return next
}

func inferSwitchDefault(fallback syntax.Expr, partials []partialSwitch, context inferContext) ([]inferResult, error) {
	var out []inferResult
	for _, partial := range partials {
		fallbacks, err := inferExpr(fallback, partial.state, context)
		if err != nil {
			return nil, err
		}
		for _, result := range fallbacks {
			candidate := result.state.clone()
			if err := candidate.unify(partial.result, result.typ); err == nil {
				out = append(out, inferResult{typ: partial.result, state: candidate})
			}
		}
	}
	return out, nil
}

func inferFor(node *syntax.ForExpr, state *inferState, context inferContext) ([]inferResult, error) {
	sources, err := inferExpr(node.Source, state, context)
	if err != nil {
		return nil, err
	}
	var out []inferResult
	for _, source := range sources {
		yielded, err := inferForSource(node, source, context)
		if err != nil {
			return nil, err
		}
		out = append(out, yielded...)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("type error at byte %d: %s and the condition must be bool", node.Pos, loopSourceHint(node.KeyVariable))
	}
	return record(node, out), nil
}

func inferForSource(node *syntax.ForExpr, source inferResult, context inferContext) ([]inferResult, error) {
	candidate := source.state.clone()
	elem := candidate.fresh()
	if err := candidate.unify(source.typ, containerTerm(loopKind(node.KeyVariable), elem)); err != nil {
		return nil, nil
	}
	local := withLoopLocals(context, node.KeyVariable, node.Variable, elem)
	states, err := filterCondition(node.Where, []*inferState{candidate}, local)
	if err != nil {
		return nil, err
	}
	return inferYield(node.Yield, states, local)
}

// loopKind is what the source must be: two loop variables mean a dictionary.
func loopKind(key string) machine.Kind {
	if key == "" {
		return machine.ArrayKind
	}
	return machine.DictKind
}

// withLoopLocals binds the value variable, plus the key variable (always
// string) when the loop walks a dictionary.
func withLoopLocals(context inferContext, key, value string, elem typeTerm) inferContext {
	local := withLocal(context, value, elem)
	if key == "" {
		return local
	}
	return withLocal(local, key, scalarTerm(machine.StringKind))
}

// withLocal binds a `for` local name without touching the outer arguments.
func withLocal(context inferContext, name string, term typeTerm) inferContext {
	local := context
	local.args = make(map[string]typeTerm, len(context.args)+1)
	for key, typ := range context.args {
		local.args[key] = typ
	}
	local.args[name] = term
	return local
}

func filterCondition(where syntax.Expr, states []*inferState, context inferContext) ([]*inferState, error) {
	if where == nil {
		return states, nil
	}
	var filtered []*inferState
	for _, partial := range states {
		conditions, err := inferExpr(where, partial, context)
		if err != nil {
			return nil, err
		}
		for _, condition := range conditions {
			candidate := condition.state.clone()
			if err := candidate.unify(condition.typ, scalarTerm(machine.BoolKind)); err == nil {
				filtered = append(filtered, candidate)
			}
		}
	}
	return filtered, nil
}

func inferYield(yield syntax.Expr, states []*inferState, context inferContext) ([]inferResult, error) {
	var out []inferResult
	for _, partial := range states {
		yields, err := inferExpr(yield, partial, context)
		if err != nil {
			return nil, err
		}
		for _, result := range yields {
			out = append(out, inferResult{typ: containerTerm(machine.ArrayKind, result.typ), state: result.state})
		}
	}
	return out, nil
}

type partialArgs struct {
	state *inferState
	args  []typeTerm
}

// inferArgs expands one candidate state per combination of argument overloads.
//
// A candidate whose sub-expression does not type is *dropped*, not fatal: in
// `x > 1 && x == 99` the comparison leaves two candidates (x:int from gt(int,int)
// and x:float from the mixed gt(float,int)), and only the int one survives the
// equality. Failing the whole inference on the first dead branch would reject a
// perfectly good program — and which branch dies depends on the order the
// operators were written, so it would fail inconsistently too.
func inferArgs(args []syntax.Expr, state *inferState, context inferContext) ([]partialArgs, error) {
	partials := []partialArgs{{state: state}}
	for _, arg := range args {
		var next []partialArgs
		var dropped error
		for _, partial := range partials {
			inferred, err := inferExpr(arg, partial.state, context)
			if err != nil {
				dropped = err
				continue
			}
			for _, result := range inferred {
				argTypes := append([]typeTerm(nil), partial.args...)
				next = append(next, partialArgs{state: result.state, args: append(argTypes, result.typ)})
			}
		}
		if len(next) == 0 {
			if dropped != nil {
				return nil, dropped
			}
			return nil, nil
		}
		partials = next
	}
	return partials, nil
}

func inferCall(node *syntax.CallExpr, state *inferState, context inferContext) ([]inferResult, error) {
	functions := context.registry.Overloads(node.Name)
	if len(functions) == 0 {
		return nil, fmt.Errorf("unknown function %q at byte %d", node.Name, node.Pos)
	}
	if functions[0].IsLazyFallback() {
		return inferFallback(node, state, context, functions[0])
	}
	partials, err := inferArgs(node.Args, state, context)
	if err != nil {
		return nil, err
	}
	var out []inferResult
	for _, partial := range partials {
		out = append(out, selectOverloads(node, partial, functions)...)
	}
	if len(out) == 0 {
		return nil, noOverloadError(node, partials)
	}
	return record(node, out), nil
}

func inferFallback(node *syntax.CallExpr, state *inferState, context inferContext, function *machine.RegisteredFunction) ([]inferResult, error) {
	if len(node.Args) < 2 {
		return nil, fmt.Errorf("fallback requires at least 2 arguments at byte %d", node.Pos)
	}
	partials, err := inferArgs(node.Args, state, context)
	if err != nil {
		return nil, err
	}
	params := make([]machine.Type, len(node.Args))
	for i := range params {
		params[i] = function.Params[0]
	}
	var out []inferResult
	for _, partial := range partials {
		candidate := partial.state.clone()
		vars := map[string]typeTerm{}
		if unifyParams(candidate, partial.args, params, vars) {
			candidate.selections[node.ID] = function.Key()
			out = append(out, inferResult{typ: candidate.instantiate(function.Result, vars), state: candidate})
		}
	}
	if len(out) == 0 {
		return nil, noOverloadError(node, partials)
	}
	return record(node, out), nil
}

func selectOverloads(node *syntax.CallExpr, partial partialArgs, functions []*machine.RegisteredFunction) []inferResult {
	var out []inferResult
	for _, function := range functions {
		if len(function.Params) != len(node.Args) {
			continue
		}
		candidate := partial.state.clone()
		vars := map[string]typeTerm{}
		if !unifyParams(candidate, partial.args, function.Params, vars) {
			continue
		}
		if isMixedNumeric(function.Params) {
			candidate.mixed++
		}
		resultType := candidate.instantiate(function.Result, vars)
		candidate.selections[node.ID] = function.Key()
		out = append(out, inferResult{typ: resultType, state: candidate})
	}
	return out
}

// isMixedNumeric reports a signature like (int, float) — one that only exists
// to allow safe promotion.
func isMixedNumeric(params []machine.Type) bool {
	if len(params) != 2 || params[0].Equal(params[1]) {
		return false
	}
	return numericKind(params[0].Kind) && numericKind(params[1].Kind)
}

func numericKind(kind machine.Kind) bool { return kind == machine.IntKind || kind == machine.FloatKind }

func unifyParams(state *inferState, args []typeTerm, params []machine.Type, vars map[string]typeTerm) bool {
	for i, param := range params {
		if err := state.unify(args[i], state.instantiate(param, vars)); err != nil {
			return false
		}
	}
	return true
}

func noOverloadError(node *syntax.CallExpr, partials []partialArgs) error {
	actual := make([]string, len(node.Args))
	if len(partials) > 0 {
		for i, arg := range partials[0].args {
			actual[i] = partials[0].state.describe(arg)
		}
	}
	return fmt.Errorf("type error at byte %d: no overload %s(%s)", node.Pos, node.Name, strings.Join(actual, ", "))
}

func inferReduce(node *syntax.ReduceExpr, state *inferState, context inferContext) ([]inferResult, error) {
	sources, err := inferExpr(node.Source, state, context)
	if err != nil {
		return nil, err
	}
	var out []inferResult
	for _, source := range sources {
		folded, err := inferReduceSource(node, source, context)
		if err != nil {
			return nil, err
		}
		out = append(out, folded...)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("type error at byte %d: %s and the body must return the accumulator type", node.Pos, loopSourceHint(node.KeyVariable))
	}
	return record(node, out), nil
}

func inferReduceSource(node *syntax.ReduceExpr, source inferResult, context inferContext) ([]inferResult, error) {
	candidate := source.state.clone()
	elem := candidate.fresh()
	if err := candidate.unify(source.typ, containerTerm(loopKind(node.KeyVariable), elem)); err != nil {
		return nil, nil
	}
	inits, err := inferExpr(node.Init, candidate, context)
	if err != nil {
		return nil, err
	}
	var out []inferResult
	for _, init := range inits {
		folded, err := inferReduceBody(node, init, elem, context)
		if err != nil {
			return nil, err
		}
		out = append(out, folded...)
	}
	return out, nil
}

// inferReduceBody binds the item and the accumulator locally; the body must
// unify with the accumulator so the fold keeps one type.
func inferReduceBody(node *syntax.ReduceExpr, init inferResult, elem typeTerm, context inferContext) ([]inferResult, error) {
	local := withLocal(withLoopLocals(context, node.KeyVariable, node.Variable, elem), node.Accumulator, init.typ)
	bodies, err := inferExpr(node.Body, init.state, local)
	if err != nil {
		return nil, err
	}
	var out []inferResult
	for _, body := range bodies {
		candidate := body.state.clone()
		if err := candidate.unify(init.typ, body.typ); err == nil {
			out = append(out, inferResult{typ: init.typ, state: candidate})
		}
	}
	return out, nil
}

// loopSourceHint explains which source shape the variable count asks for.
func loopSourceHint(key string) string {
	if key == "" {
		return "the source must be an array (use two variables, [e for k, v in d], to walk a dictionary)"
	}
	return "two loop variables walk a dictionary, so the source must be a dict (use one variable for an array)"
}

// letScope is one candidate while walking the bindings: the inference state
// plus the context the following bindings and the body will see.
type letScope struct {
	state   *inferState
	context inferContext
}

func inferLet(node *syntax.LetExpr, state *inferState, context inferContext) ([]inferResult, error) {
	scopes, err := inferLetBindings(node, state, context)
	if err != nil {
		return nil, err
	}
	var out []inferResult
	var dropped error
	for _, scope := range scopes {
		bodies, err := inferExpr(node.Body, scope.state, scope.context)
		if err != nil {
			dropped = err
			continue
		}
		out = append(out, bodies...)
	}
	if len(out) == 0 {
		if dropped != nil {
			return nil, dropped
		}
		return nil, fmt.Errorf("type error at byte %d: the let body is not typeable", node.Pos)
	}
	return record(node, out), nil
}

// inferLetBindings types the bindings in order, each one visible to the next.
func inferLetBindings(node *syntax.LetExpr, state *inferState, context inferContext) ([]letScope, error) {
	scopes := []letScope{{state: state, context: context}}
	for _, binding := range node.Bindings {
		var next []letScope
		var dropped error
		for _, scope := range scopes {
			values, err := inferExpr(binding.Value, scope.state, scope.context)
			if err != nil {
				dropped = err
				continue
			}
			for _, value := range values {
				next = append(next, letScope{
					state:   value.state,
					context: withLocal(scope.context, binding.Name, value.typ),
				})
			}
		}
		if len(next) == 0 {
			if dropped != nil {
				return nil, dropped
			}
			return nil, fmt.Errorf("type error at byte %d: let binding %q is not typeable", node.Pos, binding.Name)
		}
		scopes = next
	}
	return scopes, nil
}
