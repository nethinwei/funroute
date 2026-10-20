package compile

import "github.com/nethinwei/funroute/internal/syntax"

// compileRoundingScope compiles the argument of round(expr, @mode): expr,
// inside which the steps that land between minor units are exact, and the
// mode after it. The call that rounds once is emitted like any other. The
// mode is written, not computed — which rounding a rule applies is part of
// the rule.
func (c *bytecodeCompiler) compileRoundingScope(node *syntax.CallExpr) error {
	if _, ok := node.Args[1].(*syntax.EnumExpr); !ok {
		return syntax.Around(node.Args[1], "type error: round needs its mode written out, such as @half_up")
	}
	// Where a step may be and what a round must be handed is checkExact's
	// (exact.go), which has walked the program before it compiles.
	if err := c.compile(node.Args[0]); err != nil {
		return err
	}
	return c.compile(node.Args[1])
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
