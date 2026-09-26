package compile

import (
	"errors"

	"github.com/nethinwei/funroute/internal/machine"
	"github.com/nethinwei/funroute/internal/syntax"
)

// Constant folding: a subexpression that reads no argument and no loop
// variable has a value that is fixed at compile time, so the bytecode should
// carry the answer rather than the work.
//
// The fold runs the subexpression on the real VM instead of a second
// evaluator, so there is exactly one set of semantics in the codebase. If it
// cannot be folded — it reads an argument, it produces a container the
// constant pool cannot hold, its loops turn past the fold budget, or it fails — the
// compiler falls back to emitting the work, and the program behaves exactly as
// it did before. That fallback is what keeps folding safe inside a lazy `if`:
// a branch that would fail is simply not folded.

// foldTurns bounds compile-time evaluation: how many turns its loops may take
// in all. A closed loop over a large literal array is legal but not worth
// stalling a compile for, and a count, unlike a clock, stops at the same place
// on every machine — so the same source always compiles to the same artifact.
const foldTurns = 10_000

// tryFold evaluates expr at compile time and emits its value as a constant.
// It reports whether it did.
func (c *bytecodeCompiler) tryFold(expr syntax.Expr) (bool, error) {
	// Folding only runs expr when every call in it is constexpr and it reads
	// no argument, so it computes the same thing every time: a failure is a
	// certainty, including inside a branch that happens not to be taken — the
	// way a constant division by zero is an error in Go even under `if false`.
	value, ok, err := c.foldValue(expr)
	if err != nil || !ok {
		return false, err
	}
	// A value with no constant form — exact money inside a round — is left
	// for the caller to emit as the work that computes it.
	index, ok := c.addConstant(value, c.inferred.NodeTypes[expr.NodeID()])
	if ok {
		c.emit(machine.Instruction{Op: machine.OpConstant, A: index})
		c.folded++
	}
	return ok, nil
}

// constexprOnly reports whether every call in expr may run while compiling.
// The kernel's functions always may; a host function only if it says so. This
// is what keeps folding from reaching an engine or a clock, and it is also
// what makes a failure found here a certainty rather than a circumstance.
func constexprOnly(expr syntax.Expr, inferred *inference, registry *machine.Registry) bool {
	for node := range syntax.Nodes(expr) {
		switch node := node.(type) {
		case *syntax.UsingExpr:
			// A using is only there for the conversions in it, which read the run.
			return false
		case *syntax.CallExpr:
			if function, found := registry.Resolve(inferred.Selections[node.ID]); !found || !function.IsConstexpr() {
				return false
			}
		}
	}
	return true
}

// addConstant puts value, of the static type typ, in the constant pool and
// returns its index, or false for a value with no constant form.
func (c *bytecodeCompiler) addConstant(value machine.Value, typ machine.Type) (int, bool) {
	constant, ok := machine.ConstantOf(value, typ)
	if !ok {
		return 0, false
	}
	c.constants = append(c.constants, constant)
	return len(c.constants) - 1, true
}

// foldBinding evaluates a let binding at compile time and puts its value in
// the constant pool, without emitting anything: a constant binding needs no
// local slot. It reports false for a value that does not fold or has no
// constant form.
func (c *bytecodeCompiler) foldBinding(value syntax.Expr) (int, bool, error) {
	var folded machine.Value
	var ok bool
	var err error
	if literal, isLiteral := value.(*syntax.LiteralExpr); isLiteral {
		folded, err = c.literalValue(literal)
		ok = err == nil
	} else {
		folded, ok, err = c.foldValue(value)
	}
	if err != nil || !ok {
		return 0, false, err
	}
	index, ok := c.addConstant(folded, c.inferred.NodeTypes[value.NodeID()])
	return index, ok, nil
}

// foldValue is expr's value at compile time and true, false when it is not
// folded for a reason that is not a failure, or the failure itself.
func (c *bytecodeCompiler) foldValue(expr syntax.Expr) (machine.Value, bool, error) {
	if !c.foldable(expr) {
		return machine.Value{}, false, nil
	}
	return c.evaluate(expr)
}

// foldable reports whether expr is worth trying to fold. It must read nothing
// that varies at run time: collectVariables returns the free variables, so
// locals bound inside expr do not count, and a free name is still fine when it
// resolves to a binding that folded to a constant itself. That is what makes
// folding transitive — with `@let a = 250`, `a * 4` folds to 1000.
//
// Literals and plain variables are skipped because they already compile to a
// single instruction; folding them would only cost a compile-time run.
func (c *bytecodeCompiler) foldable(expr syntax.Expr) bool {
	if c.folding {
		return false
	}
	switch expr.(type) {
	case *syntax.LiteralExpr, *syntax.VariableExpr:
		return false
	}
	return c.constantExpr(expr) && constexprOnly(expr, c.inferred, c.registry)
}

// constantExpr reports whether expr's value is fixed at compile time: it reads
// no argument and no loop variable, so every free name it has resolves to a
// binding that folded to a constant itself.
func (c *bytecodeCompiler) constantExpr(expr syntax.Expr) bool {
	if _, literal := expr.(*syntax.LiteralExpr); literal {
		return true
	}
	if c.readsArgument[expr.NodeID()] {
		return false
	}
	for _, name := range syntax.FreeVariables(expr) {
		if !c.foldedName(name) {
			return false
		}
	}
	return true
}

// evaluate compiles expr on its own and runs it. The nested compiler has
// folding disabled, so a fold never recurses into another fold.
// evaluate returns the value expr has at compile time and true, false when it
// cannot be folded for a reason that is not a failure, or the failure itself.
func (c *bytecodeCompiler) evaluate(expr syntax.Expr) (machine.Value, bool, error) {
	// The runtime checks the value it produced against the declared result
	// type, so a node whose type inference left open is not folded.
	result, ok := c.inferred.NodeTypes[expr.NodeID()]
	if !ok {
		return machine.Value{}, false, nil
	}
	sub, ok := c.compileNested(expr)
	if !ok {
		return machine.Value{}, false, nil
	}
	value, err := machine.EvaluateClosed(sub.artifact(result), c.registry, foldTurns)
	if err != nil {
		// Running out of the fold budget says nothing about the program: it is
		// this pass that stopped, not the expression that failed.
		if errors.Is(err, machine.ErrFoldBudget) {
			return machine.Value{}, false, nil
		}
		return machine.Value{}, false, syntax.AroundError(expr, err)
	}
	// Exact money has no constant form, and the round around it still has
	// its rounding to do: the steps are left to run.
	if machine.IsExact(value) {
		return machine.Value{}, false, nil
	}
	c.roundSteps += sub.roundSteps
	return value, true, nil
}

// compileNested compiles expr on a nested compiler with folding disabled. It
// reports false when that compiler rejects expr: such an expression is simply
// not folded, and the real compile reports whatever is wrong with it.
func (c *bytecodeCompiler) compileNested(expr syntax.Expr) (*bytecodeCompiler, bool) {
	sub := newBytecodeCompiler(c.registry, c.inferred)
	sub.folding = true
	// The nested compiler inherits the constant pool and the names in scope,
	// so a name that already folded resolves to its value here too — and the
	// round(…) it sits in. Only a closed expression gets here, so every free
	// name it reads is a constant; the local slots it binds are its own.
	sub.constants = c.constants
	sub.names = c.names
	sub.readsArgument = c.readsArgument
	sub.inRound = c.inRound
	sub.plain = c.plain
	return sub, sub.compile(expr) == nil
}

// artifact wraps what the compiler has emitted so far, for a compile-time run.
func (c *bytecodeCompiler) artifact(result machine.Type) machine.ArtifactParts {
	return machine.ArtifactParts{
		Version:      machine.ArtifactVersion,
		Result:       result,
		Constants:    c.constants,
		Calls:        c.calls,
		Locals:       c.nextLocal,
		Instructions: c.instructions,
	}
}
