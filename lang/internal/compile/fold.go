package compile

import (
	"errors"
	"funroute/lang/internal/machine"
	"funroute/lang/internal/syntax"
)

// Constant folding: a subexpression that reads no argument and no loop
// variable has a value that is fixed at compile time, so the bytecode should
// carry the answer rather than the work.
//
// The fold runs the subexpression on the real VM instead of a second
// evaluator, so there is exactly one set of semantics in the codebase. If it
// cannot be folded — it reads an argument, it produces a container the
// constant pool cannot hold, it runs out of fold fuel, or it fails — the
// compiler falls back to emitting the work, and the program behaves exactly as
// it did before. That fallback is what keeps folding safe inside a lazy `if`:
// a branch that would fail is simply not folded.

// foldFuel bounds compile-time evaluation. A closed loop over a large literal
// array is legal but not worth stalling a compile for.
const foldFuel = 10_000

// foldStack bounds the compile-time stack. Closed subexpressions are shallow.
const foldStack = 256

// tryFold evaluates expr at compile time and emits its value as a constant.
// It reports whether it did.
func (c *bytecodeCompiler) tryFold(expr syntax.Expr) (bool, error) {
	if !c.foldable(expr) {
		return false, nil
	}
	value, err := c.evaluate(expr)
	if err != nil {
		// Folding only ran this because every call in it is constexpr and it
		// reads no argument, so it computes the same thing every time: the
		// failure is a certainty, including inside a branch that happens not
		// to be taken — the way a constant division by zero is an error in Go
		// even under `if false`.
		return false, err
	}
	if value == nil {
		return false, nil
	}
	index, ok := c.intern(*value)
	if !ok {
		return false, nil
	}
	c.emit(machine.Instruction{Op: machine.OpConstant, A: index})
	c.folded++
	return true, nil
}

// constexprOnly reports whether every call in expr may run while compiling.
// The kernel's functions always may; a host function only if it says so. This
// is what keeps folding from reaching an engine or a clock, and it is also
// what makes a failure found here a certainty rather than a circumstance.
func constexprOnly(expr syntax.Expr, inferred *inference, registry *machine.Registry) bool {
	if call, ok := expr.(*syntax.CallExpr); ok {
		key, found := inferred.Selections[call.ID]
		if !found {
			return false
		}
		function, found := registry.Resolve(key)
		if !found || !function.IsConstexpr() {
			return false
		}
	}
	for _, child := range syntax.Children(expr) {
		if !constexprOnly(child, inferred, registry) {
			return false
		}
	}
	return true
}

// intern puts a compile-time value in the constant pool. Arrays and
// dictionaries have no constant form, so they report false and the caller
// emits the work instead.
func (c *bytecodeCompiler) intern(value machine.Value) (int, bool) {
	constant, err := machine.ConstantFromValue(value)
	if err != nil {
		return 0, false
	}
	index := len(c.constants)
	c.constants = append(c.constants, constant)
	return index, true
}

// foldBinding evaluates a let binding at compile time and interns its value,
// without emitting anything: a constant binding needs no local slot.
func (c *bytecodeCompiler) foldBinding(value syntax.Expr) (int, bool, error) {
	if literal, isLiteral := value.(*syntax.LiteralExpr); isLiteral {
		index, ok := c.intern(literal.Value)
		return index, ok, nil
	}
	if !c.foldable(value) {
		return 0, false, nil
	}
	folded, err := c.evaluate(value)
	if err != nil {
		return 0, false, err
	}
	if folded == nil {
		return 0, false, nil
	}
	index, ok := c.intern(*folded)
	return index, ok, nil
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
	for _, name := range syntax.FreeVariables(expr) {
		if len(c.constIndex[name]) == 0 {
			return false
		}
	}
	return true
}

// evaluate compiles expr on its own and runs it. The nested compiler has
// folding disabled, so a fold never recurses into another fold.
// evaluate returns the value expr has at compile time, a nil value when it
// cannot be folded for a reason that is not a failure, or the failure itself.
func (c *bytecodeCompiler) evaluate(expr syntax.Expr) (*machine.Value, error) {
	// The runtime checks the value it produced against the declared result
	// type, so a node whose type inference left open is not folded.
	result, ok := c.inferred.NodeTypes[expr.NodeID()]
	if !ok {
		return nil, nil
	}
	sub := newBytecodeCompiler(c.registry, c.inferred)
	sub.folding = true
	// The nested compiler inherits the constant pool and the constant bindings,
	// so a name that already folded resolves to its value here too.
	sub.constants = c.constants
	sub.constIndex = c.constIndex
	if err := sub.compile(expr); err != nil {
		return nil, nil
	}
	value, err := machine.EvaluateClosed(sub.artifact(result), c.registry, foldFuel, foldStack)
	if err != nil {
		// Running out of the fold budget says nothing about the program: it is
		// this pass that stopped, not the expression that failed.
		if errors.Is(err, machine.ErrFuel) {
			return nil, nil
		}
		return nil, err
	}
	return &value, nil
}

// artifact wraps what the compiler has emitted so far, for a compile-time run.
func (c *bytecodeCompiler) artifact(result machine.Type) *machine.Artifact {
	return &machine.Artifact{
		Version:      machine.ArtifactVersion,
		Result:       result,
		Constants:    c.constants,
		Calls:        c.calls,
		Locals:       c.nextLocal,
		MaxStack:     maxStackDepth(c.instructions),
		Instructions: c.instructions,
	}
}
