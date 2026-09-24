package compile

import (
	"slices"

	"github.com/nethinwei/funroute/internal/machine"
	"github.com/nethinwei/funroute/internal/syntax"
)

// Exact money — what a step inside round(…) makes before the round rounds
// it — stays inside the round. It may flow through the kernel's sums,
// counts and comparisons, the branches of if, switch and fallback, and let,
// to the round around it; it may not reach a host function, a container, a
// record, a comprehension or the program's result, none of which has a
// place for money between minor units. checkExact walks the program once,
// after inference has chosen every overload.

type exactness struct {
	inferred *inference
	registry *machine.Registry
	// locals is whether each let-bound name holds exact money, innermost
	// last; a loop's names hold none.
	locals scoped[bool]
	// rounds is how many rounds the walk is inside.
	rounds *int
}

func checkExact(expr syntax.Expr, inferred *inference, registry *machine.Registry) error {
	walk := exactness{inferred: inferred, registry: registry, locals: scoped[bool]{}, rounds: new(int)}
	return walk.plain(expr, "the program's result")
}

// plain requires expr's value to be whole minor units where it goes.
func (w exactness) plain(expr syntax.Expr, where string) error {
	exact, err := w.of(expr)
	if err != nil || !exact {
		return err
	}
	return syntax.Around(expr, "type error: this amount is exact, between two minor units, and only a round takes it out: write round(…, @half_up) around it before it goes into %s", where)
}

// of reports whether expr's value may be exact money.
func (w exactness) of(expr syntax.Expr) (bool, error) {
	switch node := expr.(type) {
	case *syntax.CallExpr:
		return w.call(node)
	case *syntax.VariableExpr:
		exact, _ := w.locals.top(node.Name)
		return exact, nil
	case *syntax.LetExpr:
		return w.let(node)
	case *syntax.SwitchExpr:
		return w.switchOf(node)
	case *syntax.UsingExpr:
		for _, quote := range node.Quotes {
			if err := w.plain(quote, "a using's quotes"); err != nil {
				return false, err
			}
		}
		return w.of(node.Body)
	case *syntax.ForExpr:
		return false, w.loop([]syntax.Expr{node.Source}, []string{node.Variable, node.KeyVariable}, "a comprehension", node.Where, node.YieldKey, node.Yield)
	case *syntax.ReduceExpr:
		// The accumulator is bound in the body alone.
		if err := w.loop([]syntax.Expr{node.Source, node.Init}, []string{node.Variable, node.KeyVariable}, "a reduce", node.Where); err != nil {
			return false, err
		}
		return false, w.loop(nil, []string{node.Variable, node.KeyVariable, node.Accumulator}, "a reduce", node.Body)
	}
	return false, w.children(expr, containerName(expr))
}

// children requires every child of expr to be plain.
func (w exactness) children(expr syntax.Expr, where string) error {
	return w.plainAll(syntax.Children(expr), where)
}

// plainAll requires every one of exprs to be plain; a nil one is a part the
// node does not have.
func (w exactness) plainAll(exprs []syntax.Expr, where string) error {
	for _, expr := range exprs {
		if expr == nil {
			continue
		}
		if err := w.plain(expr, where); err != nil {
			return err
		}
	}
	return nil
}

// loop requires the parts of a loop to be plain: outside, the ones the loop
// reads before it binds anything, then inside, the ones it runs with names
// bound — names that hold no exact money however a let outside binds them.
func (w exactness) loop(outside []syntax.Expr, names []string, where string, inside ...syntax.Expr) error {
	if err := w.plainAll(outside, where); err != nil {
		return err
	}
	for _, name := range names {
		if name != "" {
			w.locals.push(name, false)
			defer w.locals.pop(name)
		}
	}
	return w.plainAll(inside, where)
}

func (w exactness) let(node *syntax.LetExpr) (bool, error) {
	for _, binding := range node.Bindings {
		exact, err := w.of(binding.Value)
		if err != nil {
			return false, err
		}
		w.locals.push(binding.Name, exact)
		defer w.locals.pop(binding.Name)
	}
	return w.of(node.Body)
}

func (w exactness) switchOf(node *syntax.SwitchExpr) (bool, error) {
	if node.Value != nil {
		if err := w.plain(node.Value, "a switch's subject"); err != nil {
			return false, err
		}
	}
	results := []syntax.Expr{node.Default}
	for _, branch := range node.Cases {
		for _, match := range branch.Match {
			if err := w.plain(match, "a switch's match"); err != nil {
				return false, err
			}
		}
		results = append(results, branch.Result)
	}
	return w.any(results)
}

// any reports whether any of exprs may be exact.
func (w exactness) any(exprs []syntax.Expr) (bool, error) {
	exact := false
	for _, expr := range exprs {
		if expr == nil {
			continue
		}
		one, err := w.of(expr)
		if err != nil {
			return false, err
		}
		exact = exact || one
	}
	return exact, nil
}

func (w exactness) call(node *syntax.CallExpr) (bool, error) {
	function, ok := w.registry.Resolve(w.inferred.Selections[node.ID])
	switch {
	case !ok:
		return false, w.children(node, "a call")
	case function.IsLazyIf():
		if err := w.plain(node.Args[0], "a condition"); err != nil {
			return false, err
		}
		fallthrough
	case function.IsLazyFallback():
		return w.any(lazyResults(node, function))
	case !function.TakesExact():
		return false, w.children(node, node.Name)
	case function.IsExactStep() && *w.rounds == 0:
		return false, outsideRound(node)
	case function.IsRoundingScope():
		*w.rounds++
		defer func() { *w.rounds-- }()
	}
	exact, err := w.operands(node, function)
	if err != nil {
		return false, err
	}
	return resultStaysExact(function, exact), nil
}

// operands checks what a function that takes exact money is handed: any
// operand of a sum or a comparison, only the amount of a step or a rounding
// variant — the other amounts of prorate are its proportion.
func (w exactness) operands(node *syntax.CallExpr, function *machine.RegisteredFunction) (bool, error) {
	amount := -1
	if function.IsExactStep() || roundsAtOnce(function) {
		amount = firstMoney(function.Params)
	}
	if amount < 0 {
		return w.any(node.Args)
	}
	where := node.Name + "'s other operands"
	if err := w.plainAll(node.Args[:amount], where); err != nil {
		return false, err
	}
	exact, err := w.of(node.Args[amount])
	if err != nil {
		return false, err
	}
	return exact, w.plainAll(node.Args[amount+1:], where)
}

// resultStaysExact reports whether what function makes of exact operands is
// exact: a step's always is, a sum's or a count's is when an operand is, and
// a round's, a rounding variant's or a comparison's never is.
func resultStaysExact(function *machine.RegisteredFunction, exactOperand bool) bool {
	switch {
	case function.IsExactStep():
		return true
	case function.IsRoundingScope(), roundsAtOnce(function):
		return false
	}
	return exactOperand && function.Result.Kind() == machine.MoneyKind
}

// roundsAtOnce reports a rounding variant: the mode is its last argument.
func roundsAtOnce(function *machine.RegisteredFunction) bool {
	params := function.Params
	return len(params) > 0 && params[len(params)-1].Kind() == machine.EnumKind && params[len(params)-1].Name() == machine.RoundingEnum
}

func firstMoney(params []machine.Type) int {
	return slices.IndexFunc(params, func(param machine.Type) bool { return param.Kind() == machine.MoneyKind })
}

// containerName is what the error says exact money cannot go into.
func containerName(expr syntax.Expr) string {
	switch expr.(type) {
	case *syntax.ArrayExpr:
		return "an array"
	case *syntax.DictExpr:
		return "a dictionary"
	case *syntax.RecordExpr, *syntax.RecordUpdateExpr:
		return "a record"
	case *syntax.FieldExpr:
		return "a field read"
	}
	return "this"
}

// lazyResults is the arguments a lazy form returns its value from: an if's
// two branches, every candidate of a fallback. It is nil for any other call.
func lazyResults(node *syntax.CallExpr, function *machine.RegisteredFunction) []syntax.Expr {
	switch {
	case function.IsLazyIf():
		return node.Args[1:]
	case function.IsLazyFallback():
		return node.Args
	}
	return nil
}
