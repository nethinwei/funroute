package compile

import (
	"github.com/nethinwei/funroute/internal/machine"
	"github.com/nethinwei/funroute/internal/syntax"
)

// compileRoundingScope compiles the argument of round(expr, @mode): expr,
// inside which the steps that land between minor units are exact, and the
// mode after it. The call that rounds once is emitted like any other. The
// mode is written, not computed — which rounding a rule applies is part of
// the rule.
func (c *bytecodeCompiler) compileRoundingScope(node *syntax.CallExpr) error {
	if _, ok := node.Args[1].(*syntax.EnumExpr); !ok {
		return syntax.Around(node.Args[1], "type error: round needs its mode written out, such as @half_up")
	}
	// Each scope counts its own steps: a step inside a nested round is that
	// round's, so it does not make the outer one round anything.
	saved, outer := c.inRound, c.roundSteps
	c.inRound, c.roundSteps = true, 0
	err := c.compile(node.Args[0])
	mine := c.roundSteps
	c.inRound, c.roundSteps = saved, outer
	if err != nil {
		return err
	}
	if mine == 0 {
		return syntax.Around(node, "type error: nothing inside round(…) lands between minor units; only money times or over a ratio, a conversion and prorate do")
	}
	return c.compile(node.Args[1])
}

// exactStep checks a step that lands between minor units: written without
// its mode, it is exact, and only a round around it rounds what it makes.
func (c *bytecodeCompiler) exactStep(node *syntax.CallExpr, function *machine.RegisteredFunction) error {
	if !function.IsExactStep() {
		return nil
	}
	if !c.inRound {
		return outsideRound(node)
	}
	c.roundSteps++
	return nil
}

// outsideRound is a step that lands between minor units written where no
// round rounds it.
func outsideRound(node *syntax.CallExpr) error {
	return syntax.Around(node, "type error: %s lands between two minor units, so the rule says how it rounds: write it inside round(…, @half_up), or give its mode, %s(…, @half_up)", stepName(node), node.Name)
}

// stepName is how a step was written: the operator a rule writes for mul,
// div and convert.
func stepName(node *syntax.CallExpr) string {
	switch node.Name {
	case "mul":
		return "* (mul)"
	case "div":
		return "/ (div)"
	case "convert":
		return "-> (convert)"
	}
	return node.Name
}
