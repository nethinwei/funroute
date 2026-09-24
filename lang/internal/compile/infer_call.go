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
		return nil, syntax.Around(node, "unknown function %q", node.Name)
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
		return nil, syntax.Around(node, "fallback requires at least 2 arguments")
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
			out = append(out, inferResult{typ: candidate.instantiateResult(function.Result, vars), state: candidate})
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
		if unitConflict(partial.state, partial.args, function.Params) != nil {
			continue
		}
		if comparesOperands[function.Name] && partial.state.sharedVariableConflict(partial.args, function.Params) != nil {
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
		resultType := candidate.instantiateResult(function.Result, vars)
		// money / money is a rate or an exchange rate; the one that pays
		// fxPenalty is the reading the operands do not suggest.
		distinct := partial.state.presumedDistinct(partial.args)
		switch {
		case producesExchangeRate(function) && candidate.convertsIntoItself(resultType):
			continue
		case producesExchangeRate(function) && !distinct, dividesIntoRate(function) && distinct:
			candidate.fx++
		}
		candidate.selections[node.ID] = function.Key()
		out = append(out, inferResult{typ: resultType, state: candidate})
	}
	return out
}

// producesExchangeRate is money / money read as an exchange rate.
func producesExchangeRate(function *machine.RegisteredFunction) bool {
	return function.Result.Kind() == machine.FxRateKind && len(function.Params) == 2 &&
		function.Params[0].Kind() == machine.MoneyKind && function.Params[1].Kind() == machine.MoneyKind
}

// dividesIntoRate is money / money read as a rate: one currency over itself.
func dividesIntoRate(function *machine.RegisteredFunction) bool {
	return function.Result.Kind() == machine.RateKind && len(function.Params) == 2 &&
		function.Params[0].Kind() == machine.MoneyKind && function.Params[1].Kind() == machine.MoneyKind
}

// isMixedNumeric reports a signature like (int, float) — one that only exists
// to allow safe promotion.
func isMixedNumeric(params []machine.Type) bool {
	if len(params) != 2 || params[0].Equal(params[1]) {
		return false
	}
	return numericKind(params[0].Kind()) && numericKind(params[1].Kind())
}

func numericKind(kind machine.Kind) bool {
	return kind == machine.IntKind || kind == machine.FloatKind || kind == machine.RateKind
}

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
	return syntax.Around(node, "%s", message)
}

// overloadHint names what the writer probably wanted. `+` joins strings, so
// someone will try it on arrays and dictionaries, and "no overload
// add(array<int>, array<int>)" is a true statement that says nothing about
// where to go next. The check is on the printed type because that is what this
// function already holds, and being wrong about a hint costs nothing.
func overloadHint(name string, actual []string) string {
	if hint := moneyHint(name, actual); hint != "" {
		return hint
	}
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

// combinesCurrencies names the operators that need their two amounts in one
// currency: the arithmetic that joins them and the comparisons.
var combinesCurrencies = map[string]bool{"add": true, "sub": true, "eq": true, "lt": true, "le": true, "gt": true, "ge": true}

// provenCurrency reports a printed amount type with its currency known.
func provenCurrency(text string) bool {
	return strings.HasPrefix(text, "money<") && !strings.HasPrefix(text, "money<?")
}

// moneyHint says where money meets a plain number the wrong way: amounts
// add only to amounts, a float enters as rate(x), and an amount splits with
// allocate rather than a division that would drop the cents.
func moneyHint(name string, actual []string) string {
	if len(actual) != 2 {
		return ""
	}
	money := func(text string) bool { return text == "money" || strings.HasPrefix(text, "money<") }
	left, right := actual[0], actual[1]
	switch {
	case combinesCurrencies[name] && provenCurrency(left) && provenCurrency(right) && left != right:
		return "两笔金额的币种不同：先用 amount -> USD 换成同一币种，或确认它们本应同币种"
	case money(left) && money(right) && name == "mul":
		return "两笔金额不能相乘：金额乘比例（2.9%）或整数；要两笔金额之比就用除法 a / b"
	case (money(left) || money(right)) && (left == "float" || right == "float"):
		return "浮点数不能参与金额运算：比例写成 2.9% 这样的字面量，或用 rate(\"0.029\") 从文本精确读入"
	case (left == "rate" && (right == "int" || right == "float")) || ((left == "int" || left == "float") && right == "rate"):
		return "比例只与金额或比例运算：1 - fee 写成 100% - fee，n * fee 先把 n 用在金额上"
	case money(left) && right == "int" && name == "div":
		return "金额除以整数会丢分，用 allocate(m, n) 平均分成 n 份"
	case (money(left) && right == "int" || left == "int" && money(right)) && (name == "add" || name == "sub"):
		return "金额只能与金额相加减：写成带币种的金额，或用 money(n, 币种)、like(同币种金额, n)"
	}
	return ""
}
