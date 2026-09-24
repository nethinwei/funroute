package compile

import (
	"github.com/nethinwei/funroute/internal/machine"
	"github.com/nethinwei/funroute/internal/syntax"
)

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
