package compile

import (
	"github.com/nethinwei/funroute/internal/machine"
	"github.com/nethinwei/funroute/internal/syntax"
)

// using(rate, …, body) types as its body. Every quote is an exchange rate —
// implied(settled, paid), 150 JPY / USD, an argument — or an array of them,
// between any two currencies.

func inferUsing(node *syntax.UsingExpr, state *inferState, context inferContext) (typeTerm, error) {
	for _, rate := range node.Quotes {
		if err := inferQuotedRate(rate, state, context); err != nil {
			return typeTerm{}, err
		}
	}
	body, err := inferExpr(node.Body, state, context)
	if err != nil {
		return typeTerm{}, err
	}
	return record(node, state, body), nil
}

// inferQuotedRate holds one quote to an exchange rate or an array of them,
// once its type is known; a quote nothing else decides is one rate.
func inferQuotedRate(rate syntax.Expr, state *inferState, context inferContext) error {
	quote, err := inferExpr(rate, state, context)
	if err != nil {
		return err
	}
	rateTerm := scalarTerm(machine.FxRateKind)
	return state.waitFor(func() (bool, error) {
		found := state.deref(quote)
		if found.kind == machine.VarKind {
			return false, nil
		}
		if found.kind == machine.ArrayKind {
			found = state.deref(*found.elem)
			if found.kind == machine.VarKind {
				return true, state.unify(found, rateTerm)
			}
		}
		if found.kind != machine.FxRateKind {
			return false, syntax.Around(rate, "type error: using quotes exchange rates — an fxrate such as 150 JPY / USD or implied(settled, paid), or an array<fxrate> — and this is %s", state.describe(quote))
		}
		return true, nil
	}, func() error { return state.unify(quote, rateTerm) })
}

// compileUsing leaves the exchange rates on the stack, opens the scope with
// them and closes it after the body.
func (c *bytecodeCompiler) compileUsing(node *syntax.UsingExpr) error {
	for _, rate := range node.Quotes {
		if err := c.compile(rate); err != nil {
			return err
		}
	}
	c.emit(machine.Instruction{Op: machine.OpFxPush, B: len(node.Quotes)})
	c.usingDepth++
	err := c.compile(node.Body)
	c.usingDepth--
	if err != nil {
		return err
	}
	c.emit(machine.Instruction{Op: machine.OpFxPop})
	return nil
}

// operatorName is how a call that reads rates was written: amount -> JPY is
// a convert call the parser made, and the error should say ->.
func operatorName(node *syntax.CallExpr) string {
	if node.Name == "convert" {
		return "convert (->)"
	}
	return node.Name
}
