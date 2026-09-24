package compile

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/nethinwei/funroute/internal/machine"
	"github.com/nethinwei/funroute/internal/syntax"
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
		return record(node, []inferResult{{typ: state.literalTerm(node.Value, context.money), state: state}}), nil
	case *syntax.VariableExpr:
		return inferVariable(node, state, context)
	case *syntax.EnumExpr:
		return inferEnum(node, state, context)
	case *syntax.ArrayExpr:
		return inferHomogeneous(node, node.Items, machine.ArrayKind, "array elements must have one type", state, context)
	case *syntax.DictExpr:
		return inferDict(node, state, context)
	case *syntax.SwitchExpr:
		return inferSwitch(node, state, context)
	case *syntax.ForExpr:
		return inferFor(node, state, context)
	case *syntax.ReduceExpr:
		return inferReduce(node, state, context)
	case *syntax.RecordExpr:
		return inferRecord(node, state, context)
	case *syntax.FieldExpr:
		return inferField(node, state, context)
	case *syntax.RecordUpdateExpr:
		return inferRecordUpdate(node, state, context)
	case *syntax.LetExpr:
		return inferLet(node, state, context)
	case *syntax.UsingExpr:
		return inferUsing(node, state, context)
	case *syntax.CallExpr:
		return inferCall(node, state, context)
	case *syntax.MoneyExpr, *syntax.RatioExpr, *syntax.FxRateExpr, *syntax.CurrencyExpr:
		return inferMoneyLiteral(node, state, context)
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
	return record(node, []inferResult{{typ: state.concrete(typ), state: state}}), nil
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
	return inferHomogeneous(node, values, machine.DictKind, "dictionary values must have one type", state, context)
}

// inferHomogeneous infers an array or dictionary node, whose elements all share
// one element type.
func inferHomogeneous(node syntax.Expr, items []syntax.Expr, kind machine.Kind, message string, state *inferState, context inferContext) ([]inferResult, error) {
	elem := state.fresh()
	states := []*inferState{state}
	for _, item := range items {
		next, err := unifyElement(item, elem, states, context)
		if err != nil {
			return nil, err
		}
		if len(next) == 0 {
			return nil, syntax.Around(node, "type error: %s", message)
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
			return nil, syntax.Around(item.Result, "type error: switch branches must match the subject and return one type%s",
				memberWrittenAsString(item, context.enums))
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
		return nil, syntax.Around(node, "type error: switch default must match the branch result type")
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
			if slices.Contains(enum.Values(), value) {
				return fmt.Sprintf("; %q is a member of %s, written @%s", value, enum.Summary(), value)
			}
		}
	}
	return ""
}

func validateEnumSwitch(node *syntax.SwitchExpr, partials []partialSwitch) error {
	for _, partial := range partials {
		typ, ok := partial.state.publicType(partial.subject)
		if !ok || typ.Kind() != machine.EnumKind {
			if node.Default == nil {
				return syntax.Around(node, "type error: switch without else requires a declared enum subject")
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
	if node.Default != nil || len(seen) == len(enum.Values()) {
		return nil
	}
	missing := make([]string, 0, len(enum.Values())-len(seen))
	for _, value := range enum.Values() {
		if !seen[value] {
			missing = append(missing, value)
		}
	}
	return syntax.Around(node, "type error: enum switch is not exhaustive; missing %s", strings.Join(missing, ", "))
}

func recordEnumMatch(node *syntax.SwitchExpr, enum machine.Type, match syntax.Expr, seen map[string]bool) error {
	member, ok := match.(*syntax.EnumExpr)
	if !ok {
		if node.Default == nil {
			return syntax.Around(match, "type error: an exhaustive enum switch matches enum members, such as @%s", enum.Values()[0])
		}
		return nil
	}
	value := member.Member
	if !slices.Contains(enum.Values(), value) {
		return syntax.Around(match, "type error: %q is not a member of %s", value, enum.Summary())
	}
	if seen[value] {
		return syntax.Around(match, "type error: enum member %q is matched more than once", value)
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
		candidates, err := unifyMatch(match, partial.subject, states, context)
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

// unifyMatch is unifyElement for a branch value, which is compared with the
// subject.
func unifyMatch(match syntax.Expr, subject typeTerm, states []*inferState, context inferContext) ([]*inferState, error) {
	var next []*inferState
	for _, partial := range states {
		inferred, err := inferExpr(match, partial, context)
		if err != nil {
			return nil, err
		}
		for _, result := range inferred {
			candidate := result.state.clone()
			if err := candidate.unify(subject, result.typ); err == nil {
				next = append(next, candidate)
			}
		}
	}
	return next, nil
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
		return nil, syntax.Around(node, "type error: %s, the condition must be bool and a dictionary comprehension needs a string key", loopSourceHint(node.KeyVariable))
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
	states, err := constrain(node.Where, []*inferState{candidate}, local, machine.BoolKind)
	if err != nil {
		return nil, err
	}
	if node.YieldKey == nil {
		if node.Flatten {
			return spliceYield(node.Yield, states, local)
		}
		return inferYield(node.Yield, states, local, machine.ArrayKind)
	}
	keyed, err := constrain(node.YieldKey, states, local, machine.StringKind)
	if err != nil {
		return nil, err
	}
	return inferYield(node.Yield, keyed, local, machine.DictKind)
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
	maps.Copy(local.args, context.args)
	local.args[name] = term
	if _, shadowed := context.hints[name]; shadowed {
		local.hints = maps.Clone(context.hints)
		delete(local.hints, name)
	}
	return local
}

// constrain types expr in every state and keeps the ones where it came out the
// wanted kind: a comprehension's condition has to be bool, and the key of a
// dictionary comprehension has to be a string. A nil expression constrains
// nothing, which is how the optional clauses opt out.
func constrain(expr syntax.Expr, states []*inferState, context inferContext, kind machine.Kind) ([]*inferState, error) {
	if expr == nil {
		return states, nil
	}
	var filtered []*inferState
	for _, partial := range states {
		conditions, err := inferExpr(expr, partial, context)
		if err != nil {
			return nil, err
		}
		for _, condition := range conditions {
			candidate := condition.state.clone()
			if err := candidate.unify(condition.typ, scalarTerm(kind)); err == nil {
				filtered = append(filtered, candidate)
			}
		}
	}
	return filtered, nil
}

// spliceYield types the outer loop of a nested comprehension: its yield is the
// inner loop's array and the elements go straight into the output, so the loop
// has the type of what it yields rather than an array of it.
func spliceYield(yield syntax.Expr, states []*inferState, context inferContext) ([]inferResult, error) {
	var out []inferResult
	for _, partial := range states {
		yields, err := inferExpr(yield, partial, context)
		if err != nil {
			return nil, err
		}
		for _, result := range yields {
			candidate := result.state.clone()
			elem := candidate.fresh()
			if err := candidate.unify(result.typ, containerTerm(machine.ArrayKind, elem)); err != nil {
				continue
			}
			out = append(out, inferResult{typ: containerTerm(machine.ArrayKind, elem), state: candidate})
		}
	}
	return out, nil
}

func inferYield(yield syntax.Expr, states []*inferState, context inferContext, kind machine.Kind) ([]inferResult, error) {
	var out []inferResult
	for _, partial := range states {
		yields, err := inferExpr(yield, partial, context)
		if err != nil {
			return nil, err
		}
		for _, result := range yields {
			out = append(out, inferResult{typ: containerTerm(kind, result.typ), state: result.state})
		}
	}
	return out, nil
}

// inferRecord types a record literal: every field is typed on its own, and
// the record's type is those types in the order they were written. A field
// whose type does not settle — an empty array, say — has to be written with a
// type the way any other literal does.
func inferRecord(node *syntax.RecordExpr, state *inferState, context inferContext) ([]inferResult, error) {
	results := []inferResult{{typ: recordTerm(machine.RecordOf()), state: state}}
	for _, field := range node.Fields {
		next, err := inferRecordField(field, results, context)
		if err != nil {
			return nil, err
		}
		results = next
	}
	if len(results) == 0 {
		return nil, syntax.Around(node, "type error: every record field needs a type of its own")
	}
	return record(node, results), nil
}

// inferRecordField extends each record built so far with one more field.
func inferRecordField(field syntax.RecordFieldExpr, sofar []inferResult, context inferContext) ([]inferResult, error) {
	var out []inferResult
	for _, partial := range sofar {
		values, err := inferExpr(field.Value, partial.state, context)
		if err != nil {
			return nil, err
		}
		for _, value := range values {
			out = append(out, growRecord(partial, field.Name, value)...)
		}
	}
	return out, nil
}

// growRecord adds one typed field to a record built so far. A literal still
// open between kinds is settled once per kind: the field's type is part of
// the record's, and needs to be known now.
func growRecord(partial inferResult, name string, value inferResult) []inferResult {
	var out []inferResult
	for _, state := range value.state.forkLiteral(value.typ) {
		typ, ok := state.publicType(value.typ)
		if !ok {
			continue
		}
		grown := machine.RecordOf(append(partial.typ.record.Fields(), machine.FieldOf(name, typ))...)
		out = append(out, inferResult{typ: recordTerm(grown), state: state})
	}
	return out
}

// inferField reads one field off a record. The record's type has to be known
// here — from the contract, from a literal or from a let binding — because the
// field's own type comes from it.
func inferField(node *syntax.FieldExpr, state *inferState, context inferContext) ([]inferResult, error) {
	values, err := inferExpr(node.Value, state, context)
	if err != nil {
		return nil, err
	}
	var out []inferResult
	for _, value := range values {
		typ, ok := value.state.publicType(value.typ)
		if !ok || typ.Kind() != machine.RecordKind {
			continue
		}
		index := typ.FieldIndex(node.Field)
		if index < 0 {
			return nil, syntax.Around(node, "type error: %s has no field %q", typ.Summary(), node.Field)
		}
		out = append(out, inferResult{typ: value.state.fieldTerm(value.state.deref(value.typ), index), state: value.state})
	}
	if len(out) == 0 {
		return nil, syntax.Around(node, "type error: %q is read off something that is not a record with a known type", node.Field)
	}
	return record(node, out), nil
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
		return nil, syntax.Around(node, "type error: %s, the condition must be bool and the body must return the accumulator type", loopSourceHint(node.KeyVariable))
	}
	return record(node, out), nil
}

func inferReduceSource(node *syntax.ReduceExpr, source inferResult, context inferContext) ([]inferResult, error) {
	candidate := source.state.clone()
	elem := candidate.fresh()
	if err := candidate.unify(source.typ, containerTerm(loopKind(node.KeyVariable), elem)); err != nil {
		return nil, nil
	}
	local := withLoopLocals(context, node.KeyVariable, node.Variable, elem)
	states, err := constrain(node.Where, []*inferState{candidate}, local, machine.BoolKind)
	if err != nil {
		return nil, err
	}
	var out []inferResult
	for _, state := range states {
		folded, err := inferReduceInit(node, state, elem, context)
		if err != nil {
			return nil, err
		}
		out = append(out, folded...)
	}
	return out, nil
}

// inferReduceInit types the initial value outside the loop: the accumulator
// starts from something the loop variables cannot see.
func inferReduceInit(node *syntax.ReduceExpr, state *inferState, elem typeTerm, context inferContext) ([]inferResult, error) {
	inits, err := inferExpr(node.Init, state, context)
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
		return nil, syntax.Around(node, "type error: the let body is not typeable")
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
			return nil, syntax.Around(node, "type error: let binding %q is not typeable", binding.Name)
		}
		scopes = next
	}
	return scopes, nil
}
