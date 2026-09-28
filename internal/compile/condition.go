package compile

import (
	"github.com/nethinwei/funroute/internal/machine"
	"github.com/nethinwei/funroute/internal/syntax"
)

// A condition — an if's, a filter's, a case's — is tested by jumps, not by
// a bool made and then tested: a && b, a || b and !a, which the parser
// writes as ifs with a literal branch, are tested a part at a time, so a
// false a jumps past b and nothing of either is kept. The parts are
// evaluated exactly when the ifs would evaluate them.

// connective is which of the three a condition is.
type connective uint8

const (
	notConnective connective = iota
	and                      // if(a, b, false)
	or                       // if(a, true, b)
	not                      // if(a, false, true)
)

// compileCondition emits a test of cond that goes on past it when cond is
// true and, when it is false, jumps to the misses, for the caller to patch.
func (c *bytecodeCompiler) compileCondition(cond syntax.Expr) ([]int, error) {
	return c.jumpWhen(cond, false)
}

// jumpWhen emits a test of cond that jumps, to the jumps it returns, when
// cond is when, and goes on past it otherwise. A not is the other test of
// its operand; an and's first operand being false decides it, as an or's
// being true does.
func (c *bytecodeCompiler) jumpWhen(cond syntax.Expr, when bool) ([]int, error) {
	how, a, b := c.connectiveOf(cond)
	switch {
	case how == not:
		return c.jumpWhen(a, !when)
	case how == and && !when, how == or && when:
		jumps, err := c.jumpWhen(a, when)
		if err != nil {
			return nil, err
		}
		more, err := c.jumpWhen(b, when)
		return append(jumps, more...), err
	case how == and, how == or:
		decided, err := c.jumpWhen(a, !when)
		if err != nil {
			return nil, err
		}
		jumps, err := c.jumpWhen(b, when)
		c.patch(decided, len(c.instructions))
		return jumps, err
	}
	if err := c.compile(cond); err != nil {
		return nil, err
	}
	miss := c.emit(machine.Instruction{Op: machine.OpJumpIfFalse})
	if !when {
		return []int{miss}, nil
	}
	hit := c.emit(machine.Instruction{Op: machine.OpJump})
	c.patch([]int{miss}, len(c.instructions))
	return []int{hit}, nil
}

// connectiveOf reads cond as one of the three and its operands; an if whose
// value is kept for another use, or folds, is compiled as a value.
func (c *bytecodeCompiler) connectiveOf(cond syntax.Expr) (connective, syntax.Expr, syntax.Expr) {
	node, ok := cond.(*syntax.CallExpr)
	if !ok || len(node.Args) != 3 || c.folding {
		return notConnective, nil, nil
	}
	if _, hoisted := c.hoisted[node.ID]; hoisted || c.constantExpr(node) {
		return notConnective, nil, nil
	}
	if function, found := c.registry.Resolve(c.inferred.Selections[node.ID]); !found || !function.IsLazyIf() {
		return notConnective, nil, nil
	}
	whenTrue, trueLiteral := boolLiteral(node.Args[1])
	whenFalse, falseLiteral := boolLiteral(node.Args[2])
	switch {
	case falseLiteral && !whenFalse && trueLiteral && !whenTrue:
		return notConnective, nil, nil
	case trueLiteral && !whenTrue && falseLiteral && whenFalse:
		return not, node.Args[0], nil
	case falseLiteral && !whenFalse:
		return and, node.Args[0], node.Args[1]
	case trueLiteral && whenTrue:
		return or, node.Args[0], node.Args[2]
	}
	return notConnective, nil, nil
}

func boolLiteral(expr syntax.Expr) (bool, bool) {
	literal, ok := expr.(*syntax.LiteralExpr)
	if !ok {
		return false, false
	}
	return literal.Value.Bool()
}
