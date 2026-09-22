package compile

// Typing a call: expand one candidate per combination of argument overloads,
// score the signatures that unify, and say something useful when none does.
// This is where the language's only ambiguity lives, so it is kept apart from
// the per-node rules in infer_expr.go.

import (
	"fmt"
	"strings"

	"funroute/lang/internal/machine"
	"funroute/lang/internal/syntax"
)

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
		return nil, syntax.At(node.Pos, "unknown function %q", node.Name)
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
		return nil, syntax.At(node.Pos, "fallback requires at least 2 arguments")
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
	message := fmt.Sprintf("type error: no overload %s(%s)", node.Name, strings.Join(actual, ", "))
	if hint := overloadHint(node.Name, actual); hint != "" {
		message += "；" + hint
	}
	return syntax.At(node.Pos, "%s", message)
}

// overloadHint names what the writer probably wanted. `+` joins strings, so
// someone will try it on arrays and dictionaries, and "no overload
// add(array<int>, array<int>)" is a true statement that says nothing about
// where to go next. The check is on the printed type because that is what this
// function already holds, and being wrong about a hint costs nothing.
func overloadHint(name string, actual []string) string {
	if name != "add" || len(actual) != 2 || actual[0] != actual[1] {
		return ""
	}
	switch {
	case strings.HasPrefix(actual[0], "array<"):
		return "两个数组相接用标准包的 concat(a, b)"
	case strings.HasPrefix(actual[0], "dict<"):
		return "两个字典相叠用标准包的 merge(a, b)"
	}
	return ""
}
