package compile

import (
	"funroute/lang/internal/machine"
	"funroute/lang/internal/syntax"
)

// compileUsing leaves the exchange rates on the stack, opens the scope with
// them — over the named table when there is one — and closes it after the
// body.
func (c *bytecodeCompiler) compileUsing(node *syntax.UsingExpr) error {
	for _, rate := range node.Quotes {
		if err := c.compile(rate); err != nil {
			return err
		}
	}
	// A named table is no value on the stack: the instruction names it, and
	// the run looks it up among the ones the host handed in.
	var keys []string
	if table, ok := node.Table.(*syntax.EnumExpr); ok {
		keys = []string{table.Member}
	}
	c.emit(machine.Instruction{Op: machine.OpFxPush, B: len(node.Quotes), Keys: keys})
	if err := c.compile(node.Body); err != nil {
		return err
	}
	c.emit(machine.Instruction{Op: machine.OpFxPop})
	return nil
}
