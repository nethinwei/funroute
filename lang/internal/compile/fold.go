package compile

import (
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
func (c *bytecodeCompiler) tryFold(expr syntax.Expr) bool {
	if !c.foldable(expr) {
		return false
	}
	value, ok := c.evaluate(expr)
	if !ok {
		return false
	}
	index, ok := c.intern(value)
	if !ok {
		return false
	}
	c.emit(machine.Instruction{Op: machine.OpConstant, A: index})
	c.folded++
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
func (c *bytecodeCompiler) foldBinding(value syntax.Expr) (int, bool) {
	if literal, isLiteral := value.(*syntax.LiteralExpr); isLiteral {
		return c.intern(literal.Value)
	}
	if !c.foldable(value) {
		return 0, false
	}
	folded, ok := c.evaluate(value)
	if !ok {
		return 0, false
	}
	return c.intern(folded)
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
	return c.constantExpr(expr)
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
func (c *bytecodeCompiler) evaluate(expr syntax.Expr) (machine.Value, bool) {
	// The runtime checks the value it produced against the declared result
	// type, so a node whose type inference left open is not folded.
	result, ok := c.inferred.NodeTypes[expr.NodeID()]
	if !ok {
		return machine.Value{}, false
	}
	sub := newBytecodeCompiler(c.registry, c.inferred)
	sub.folding = true
	// The nested compiler inherits the constant pool and the constant bindings,
	// so a name that already folded resolves to its value here too.
	sub.constants = c.constants
	sub.constIndex = c.constIndex
	if err := sub.compile(expr); err != nil {
		return machine.Value{}, false
	}
	value, err := machine.EvaluateClosed(sub.artifact(result), c.registry, foldFuel, foldStack)
	if err != nil {
		return machine.Value{}, false
	}
	return value, true
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
