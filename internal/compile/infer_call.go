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
		return "two arrays are joined with concat(a, b)"
	case strings.HasPrefix(actual[0], "dict<"):
		return "two dictionaries are merged with merge(a, b)"
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
		return "two amounts do not multiply: an amount times a ratio (2.9%) or an integer; for the ratio of two amounts divide them, a / b"
	case (money(left) || money(right)) && (left == "float" || right == "float"):
		return "a float takes no part in money arithmetic: write a ratio as a literal such as 2.9%, or read one exactly from text with ratio(\"0.029\")"
	case (left == "ratio" && (right == "int" || right == "float")) || ((left == "int" || left == "float") && right == "ratio"):
		return "a ratio combines only with money or another ratio: write 1 - fee as 100% - fee, and in n * fee apply n to the amount first"
	case money(left) && right == "int" && name == "div":
		return "dividing an amount by an integer would drop minor units: allocate(m, n) splits it into n shares"
	case (money(left) && right == "int" || left == "int" && money(right)) && (name == "add" || name == "sub"):
		return "an amount adds to and subtracts from another amount only: write the number as an amount with its currency, or use money(n, USD) or money(n, currency(m)) for the currency of an amount m"
	}
	return ""
}
