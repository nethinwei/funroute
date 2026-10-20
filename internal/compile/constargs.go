package compile

import (
	"github.com/nethinwei/funroute/internal/machine"
	"github.com/nethinwei/funroute/internal/syntax"
)

// requireConstantArgs holds a call to the arguments its function wants
// fixed at compile time (Doc.ConstArgs): each must be a constant expression,
// and the function must take its value. A nested compiler checks as well:
// a call it folds away is never compiled again, and its argument is held to
// the rule as one that is — matches("abc", "(") as matches(s, "(").
func (c *bytecodeCompiler) requireConstantArgs(node *syntax.CallExpr, function *machine.RegisteredFunction) error {
	for _, at := range function.Doc.ConstArgs {
		arg := node.Args[at]
		// A clock or an engine is not fixed when the rule is compiled, and
		// is not called to find out.
		fixed := c.constantExpr(arg) && c.callsConstexpr(arg)
		var value machine.Value
		var err error
		if fixed {
			// One that does not fold — past the folding budget — is not
			// known when the rule is compiled either.
			if value, fixed, err = c.evaluate(arg); err != nil {
				return err
			}
		}
		if !fixed {
			return syntax.Around(arg, "%s needs argument %d fixed when the rule is compiled: a literal, or what folds to one", node.Name, at+1)
		}
		if err := machine.CheckConstantArg(function, value); err != nil {
			return syntax.AroundError(arg, err)
		}
	}
	return nil
}
