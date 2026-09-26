package compile

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/nethinwei/funroute/internal/kit"
	"github.com/nethinwei/funroute/internal/machine"
	"github.com/nethinwei/funroute/internal/syntax"
)

// record stamps the inferred type of expr.
func record(expr syntax.Expr, state *inferState, term typeTerm) typeTerm {
	state.setType(expr.NodeID(), term)
	return term
}

func inferExpr(expr syntax.Expr, state *inferState, context inferContext) (typeTerm, error) {
	switch node := expr.(type) {
	case *syntax.LiteralExpr:
		return record(node, state, state.literalTerm(node.Value, context.money)), nil
	case *syntax.VariableExpr:
		return inferVariable(node, state, context)
	case *syntax.EnumExpr:
		return inferEnum(node, state, context)
	case *syntax.ArrayExpr:
		return inferHomogeneous(node, node.Items, machine.ArrayKind, "array elements must have one type", state, context)
	case *syntax.DictExpr:
		return inferHomogeneous(node, dictValues(node), machine.DictKind, "dictionary values must have one type", state, context)
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
		return typeTerm{}, fmt.Errorf("internal error: unsupported expression %T", expr)
	}
}

// inferEnum types @member from the contract's enum namespace, not from the
// surrounding expression, so a reference is typed wherever it appears.
func inferEnum(node *syntax.EnumExpr, state *inferState, context inferContext) (typeTerm, error) {
	typ, err := resolveEnumReference(node, context.enums)
	if err != nil {
		return typeTerm{}, err
	}
	return record(node, state, state.concrete(typ)), nil
}

func inferVariable(node *syntax.VariableExpr, state *inferState, context inferContext) (typeTerm, error) {
	term, ok := context.lookup(node.Name)
	if !ok {
		return typeTerm{}, fmt.Errorf("internal error: variable %q was not collected", node.Name)
	}
	return record(node, state, term), nil
}

// dictValues is a dictionary literal's values, in key order.
func dictValues(node *syntax.DictExpr) []syntax.Expr {
	return kit.Map(node.Entries, func(entry syntax.DictEntryExpr) syntax.Expr { return entry.Value })
}

// inferHomogeneous infers an array or dictionary node, whose elements all share
// one element type.
func inferHomogeneous(node syntax.Expr, items []syntax.Expr, kind machine.Kind, message string, state *inferState, context inferContext) (typeTerm, error) {
	elem := state.fresh()
	for _, item := range items {
		if err := inferAs(item, elem, state, context); err != nil {
			return typeTerm{}, typeErrorAt(node, err, message)
		}
	}
	return record(node, state, containerTerm(kind, elem)), nil
}

// mismatch is a sub-expression that typed but not as what its place asks.
type mismatch struct{ error }

// inferAs types expr and unifies it with want; a failing unification comes
// back as a mismatch, anything else as it was.
func inferAs(expr syntax.Expr, want typeTerm, state *inferState, context inferContext) error {
	term, err := inferExpr(expr, state, context)
	if err != nil {
		return err
	}
	if err := state.unify(want, term); err != nil {
		return mismatch{err}
	}
	return nil
}

// typeErrorAt is err, or the node's own message when err is a mismatch.
func typeErrorAt(node syntax.Expr, err error, message string) error {
	if _, ok := errors.AsType[mismatch](err); ok {
		return syntax.Around(node, "type error: %s", message)
	}
	return err
}

func inferSwitch(node *syntax.SwitchExpr, state *inferState, context inferContext) (typeTerm, error) {
	subject := scalarTerm(machine.BoolKind)
	if node.Value != nil {
		var err error
		if subject, err = inferExpr(node.Value, state, context); err != nil {
			return typeTerm{}, err
		}
	}
	result := state.fresh()
	for _, item := range node.Cases {
		if err := inferSwitchCase(item, subject, result, state, context); err != nil {
			return typeTerm{}, typeErrorAt(item.Result, err, "switch branches must match the subject and return one type"+memberWrittenAsString(item, context.enums))
		}
	}
	if node.Default != nil {
		if err := inferAs(node.Default, result, state, context); err != nil {
			return typeTerm{}, typeErrorAt(node, err, "switch default must match the branch result type")
		}
	}
	state.checks = append(state.checks, func() error { return validateEnumSwitch(node, state, subject) })
	return record(node, state, result), nil
}

// inferSwitchCase holds every value of a branch to the subject and its
// result to the switch's.
func inferSwitchCase(item syntax.SwitchCaseExpr, subject, result typeTerm, state *inferState, context inferContext) error {
	for _, match := range item.Match {
		if err := inferAs(match, subject, state, context); err != nil {
			return err
		}
	}
	return inferAs(item.Result, result, state, context)
}

// validateEnumSwitch proves a switch over a declared enum exhaustive, and
// refuses one without else over anything else.
func validateEnumSwitch(node *syntax.SwitchExpr, state *inferState, subject typeTerm) error {
	typ, ok := state.publicType(subject)
	if ok && typ.Kind() == machine.EnumKind {
		return validateEnumCases(node, typ)
	}
	if node.Default == nil {
		return syntax.Around(node, "type error: switch without else requires a declared enum subject")
	}
	return nil
}

func inferFor(node *syntax.ForExpr, state *inferState, context inferContext) (typeTerm, error) {
	message := loopSourceHint(node.KeyVariable) + ", the condition must be bool and a dictionary comprehension needs a string key"
	local, err := loopHead(node.Source, node.Where, node.KeyVariable, node.Variable, state, context)
	if err == nil {
		var result typeTerm
		result, err = inferForYield(node, state, local)
		if err == nil {
			return record(node, state, result), nil
		}
	}
	return typeTerm{}, typeErrorAt(node, err, message)
}

// inferForYield is the comprehension's type: an array or a dictionary of
// what it yields, or, for the outer clause of a nested one, the inner
// clause's array spliced in.
func inferForYield(node *syntax.ForExpr, state *inferState, local inferContext) (typeTerm, error) {
	if node.YieldKey != nil {
		if err := inferAs(node.YieldKey, scalarTerm(machine.StringKind), state, local); err != nil {
			return typeTerm{}, err
		}
	}
	yield, err := inferExpr(node.Yield, state, local)
	switch {
	case err != nil:
		return typeTerm{}, err
	case node.YieldKey != nil:
		return containerTerm(machine.DictKind, yield), nil
	case !node.Flatten:
		return containerTerm(machine.ArrayKind, yield), nil
	}
	spliced := containerTerm(machine.ArrayKind, state.fresh())
	if err := state.unify(spliced, yield); err != nil {
		return typeTerm{}, mismatch{err}
	}
	return spliced, nil
}

// loopHead types a loop's source as a container of the loop's kind, binds
// the loop's variables to its elements, and holds the condition to bool; it
// is the context the rest of the loop sees.
func loopHead(source, where syntax.Expr, key, value string, state *inferState, context inferContext) (inferContext, error) {
	elem := state.fresh()
	if err := inferAs(source, containerTerm(loopKind(key), elem), state, context); err != nil {
		return inferContext{}, err
	}
	local := withLoopLocals(context, key, value, elem)
	if where == nil {
		return local, nil
	}
	return local, inferAs(where, scalarTerm(machine.BoolKind), state, local)
}

func inferReduce(node *syntax.ReduceExpr, state *inferState, context inferContext) (typeTerm, error) {
	message := loopSourceHint(node.KeyVariable) + ", the condition must be bool and the body must return the accumulator type"
	local, err := loopHead(node.Source, node.Where, node.KeyVariable, node.Variable, state, context)
	if err != nil {
		return typeTerm{}, typeErrorAt(node, err, message)
	}
	// The initial value is typed outside the loop: the accumulator starts
	// from something the loop variables cannot see.
	init, err := inferExpr(node.Init, state, context)
	if err != nil {
		return typeTerm{}, err
	}
	if err := inferAs(node.Body, init, state, withLocal(local, node.Accumulator, init)); err != nil {
		return typeTerm{}, typeErrorAt(node, err, message)
	}
	return record(node, state, init), nil
}

func inferLet(node *syntax.LetExpr, state *inferState, context inferContext) (typeTerm, error) {
	local := context
	for _, binding := range node.Bindings {
		term, err := inferExpr(binding.Value, state, local)
		if err != nil {
			return typeTerm{}, err
		}
		local = withLocal(local, binding.Name, term)
	}
	body, err := inferExpr(node.Body, state, local)
	if err != nil {
		return typeTerm{}, err
	}
	return record(node, state, body), nil
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

// localName is a name a loop or a let binds, and the ones bound around it.
type localName struct {
	name  string
	term  typeTerm
	outer *localName
}

// withLocal binds a local name in front of the ones around it, leaving the
// context it came from as it was.
func withLocal(context inferContext, name string, term typeTerm) inferContext {
	context.locals = &localName{name: name, term: term, outer: context.locals}
	return context
}

// lookup is the term of a name: the innermost local by that name, or else
// the argument.
func (c inferContext) lookup(name string) (typeTerm, bool) {
	for local := c.locals; local != nil; local = local.outer {
		if local.name == name {
			return local.term, true
		}
	}
	return c.args.find(name)
}

// loopSourceHint explains which source shape the variable count asks for.
func loopSourceHint(key string) string {
	if key == "" {
		return "the source must be an array (use two variables, [e for k, v in d], to walk a dictionary)"
	}
	return "two loop variables walk a dictionary, so the source must be a dict (use one variable for an array)"
}
