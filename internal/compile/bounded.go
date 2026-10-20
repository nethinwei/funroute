package compile

import (
	"slices"

	"github.com/nethinwei/funroute/internal/machine"
	"github.com/nethinwei/funroute/internal/syntax"
)

// requireBoundedArgs holds a BoundedArgs function to its promise: what it
// produces is sized by its arguments, so those arguments must have a size the
// inputs already bound. Three things do: a literal, the length of a container
// (that length *is* part of the input size), and the two combined by
// arithmetic; a let's name for one of them is as good. An argument that could be any scalar at run time does not, and
// that is the one docs/termination.md rules out.
func (c *bytecodeCompiler) requireBoundedArgs(node *syntax.CallExpr) error {
	for i, arg := range node.Args {
		// Only an integer can stand for a length; the amount allocate splits
		// or anything else of another type says nothing about the size.
		if typ, ok := c.inferred.nodeType(arg.NodeID()); ok && typ.Kind() != machine.IntKind {
			continue
		}
		if c.boundedExpr(arg) {
			continue
		}
		return syntax.Around(arg,
			"%s needs arguments whose size the inputs already bound: a literal, len(...) of a container, or those combined by arithmetic — argument %d is neither",
			node.Name, i+1)
	}
	return nil
}

func (c *bytecodeCompiler) boundedExpr(expr syntax.Expr) bool {
	switch node := expr.(type) {
	case *syntax.LiteralExpr:
		return true
	case *syntax.VariableExpr:
		// A binding that folded to a constant is as good as a literal, and
		// a let's name as good as its value.
		bound, ok := c.names.top(node.Name)
		return ok && (bound.constant || bound.bounded)
	case *syntax.CallExpr:
		return c.boundedCall(node)
	}
	return false
}

// boundedCall knows two things about the kernel: len hands back a size the
// input already bound, and arithmetic on bounded sizes stays bounded (the
// polynomial in docs/termination.md is a polynomial for exactly this reason).
// Only the kernel's own functions count — a host may register another len.
func (c *bytecodeCompiler) boundedCall(node *syntax.CallExpr) bool {
	key, ok := c.inferred.Selections[node.ID]
	if !ok {
		return false
	}
	function, ok := machine.ResolveOf(c.registry, key)
	if !ok || !function.IsBuiltin() {
		return false
	}
	switch function.Name {
	case "len":
		return true
	case "add", "sub", "mul", "div", "mod":
		return !slices.ContainsFunc(node.Args, func(arg syntax.Expr) bool { return !c.boundedExpr(arg) })
	}
	return false
}
