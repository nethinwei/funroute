package compile

// Typing a call: its arguments, then a choice among the overloads that fit
// (infer_solve.go), and something useful to say when none does. This is
// where the language's only ambiguity lives, so it is kept apart from the
// per-node rules in infer_expr.go.

import (
	"strings"

	"github.com/nethinwei/funroute/internal/machine"
	"github.com/nethinwei/funroute/internal/syntax"
)

// inferArgs types a call's arguments in order.
func inferArgs(args []syntax.Expr, state *inferState, context inferContext) ([]typeTerm, error) {
	terms := make([]typeTerm, len(args))
	for i, arg := range args {
		term, err := inferExpr(arg, state, context)
		if err != nil {
			return nil, err
		}
		terms[i] = term
	}
	return terms, nil
}

func inferCall(node *syntax.CallExpr, state *inferState, context inferContext) (typeTerm, error) {
	functions := machine.OverloadsOf(context.registry, node.Name)
	if len(functions) == 0 {
		return typeTerm{}, syntax.Around(node, "unknown function %q", node.Name)
	}
	if functions[0].IsLazyFallback() {
		return inferFallback(node, state, context, functions[0])
	}
	args, err := inferArgs(node.Args, state, context)
	if err != nil {
		return typeTerm{}, err
	}
	functions = kept(functions, func(function *machine.RegisteredFunction) bool { return len(function.Params) == len(node.Args) })
	c := &choice{node: node, args: args, result: state.fresh()}
	if err := state.offer(c, functions); err != nil {
		return typeTerm{}, err
	}
	return record(node, state, c.result), nil
}

// inferFallback types fallback(a, b, …): every candidate is the one
// parameter's type, however many there are.
func inferFallback(node *syntax.CallExpr, state *inferState, context inferContext, function *machine.RegisteredFunction) (typeTerm, error) {
	if len(node.Args) < 2 {
		return typeTerm{}, syntax.Around(node, "fallback requires at least 2 arguments")
	}
	args, err := inferArgs(node.Args, state, context)
	if err != nil {
		return typeTerm{}, err
	}
	var vars namedTerms
	for _, arg := range args {
		if state.unify(arg, state.instantiate(function.Params[0], &vars)) != nil {
			return typeTerm{}, noOverloadError(node, state, args)
		}
	}
	state.selectKey(node.ID, function.Key())
	return record(node, state, state.instantiate(function.Result, &vars)), nil
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

// moneyHint says where money meets a plain number the wrong way: amounts
// add only to amounts, a float never meets money, and an amount splits with
// allocate rather than a division that would drop the cents.
func moneyHint(name string, actual []string) string {
	if len(actual) != 2 {
		return ""
	}
	money := func(text string) bool { return text == "money" }
	left, right := actual[0], actual[1]
	switch {
	case money(left) && money(right) && name == "mul":
		return "两笔金额不能相乘：金额乘比例（2.9%）或整数；要两笔金额之比就用除法 a / b"
	case (money(left) || money(right)) && (left == "float" || right == "float"):
		return "浮点数不能参与金额运算：比例写成 2.9% 这样的字面量，或用 ratio(\"0.029\") 从文本精确读入"
	case (left == "ratio" && (right == "int" || right == "float")) || ((left == "int" || left == "float") && right == "ratio"):
		return "比例只与金额或比例运算：1 - fee 写成 100% - fee，n * fee 先把 n 用在金额上"
	case money(left) && right == "int" && name == "div":
		return "金额除以整数会丢分，用 allocate(m, n) 平均分成 n 份"
	case (money(left) && right == "int" || left == "int" && money(right)) && (name == "add" || name == "sub"):
		return "金额只能与金额相加减：写成带币种的金额，或用 money(n, 币种)、money(n, currency(同币种金额))"
	}
	return ""
}
