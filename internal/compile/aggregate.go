package compile

import (
	"github.com/nethinwei/funroute/internal/machine"
	"github.com/nethinwei/funroute/internal/syntax"
)

// Two rewrites make a comprehension cheaper without changing what it
// computes. A function that declares a Fold, called on a comprehension, is
// the comprehension's loop folding each item as it is made: no array is
// built, and none handed over. And the inner source of a nested
// comprehension, when it does not depend on the outer item, is computed once
// rather than once an item. Both keep the order things are computed in, and
// what fails.

// foldAnswer is the name a fused fold's answer is bound to: no program can
// write it, so it hides nothing.
const foldAnswer = " answer"

// compileAggregate lays out a call of a function that declares a Fold on a
// comprehension of one clause as that comprehension's loop, folding, and
// reports false for any other call.
func (c *bytecodeCompiler) compileAggregate(node *syntax.CallExpr, function *machine.RegisteredFunction) (bool, error) {
	fold := function.Fold
	if fold == nil || c.plain || len(node.Args) != 1 {
		return false, nil
	}
	comprehension, ok := node.Args[0].(*syntax.ForExpr)
	if !ok || comprehension.Flatten || comprehension.YieldKey != nil || fold.Counts && !plainValue(comprehension.Yield) {
		return false, nil
	}
	if fold.First {
		return c.compileFirst(node, function, comprehension)
	}
	answer, item := c.inferred.NodeTypes[node.ID], elemOf(c.inferred.NodeTypes[comprehension.ID])
	init := fold.Init
	if fold.Counts {
		init = machine.Int(0)
	}
	if err := c.compile(comprehension.Source); err != nil {
		return true, err
	}
	if err := c.emitConstant(init, answer); err != nil {
		return true, err
	}
	shape := loopShape{
		key: comprehension.KeyVariable, value: comprehension.Variable, accumulator: foldAnswer,
		where: comprehension.Where, result: &answer,
	}
	breaks := c.foldBody(&shape, comprehension.Yield, fold, answer, item)
	err := c.compileLoop(shape)
	// A stop leaves the loop where loop_next's last iteration does.
	c.patch(*breaks, len(c.instructions))
	return true, err
}

// compileFirst lays out a first on a comprehension as the loop that stops at
// the first item it yields, which is the answer. A loop that ends with none
// drops its seed and calls the function on an empty array, which fails as
// the call on the array built would have. It reports false, compiling
// nothing, for an item type with no seed to stand for the answer until then.
func (c *bytecodeCompiler) compileFirst(node *syntax.CallExpr, function *machine.RegisteredFunction, comprehension *syntax.ForExpr) (bool, error) {
	answer := c.inferred.NodeTypes[node.ID]
	seed, ok := seedOf(answer)
	if !ok {
		return false, nil
	}
	if err := c.compile(comprehension.Source); err != nil {
		return true, err
	}
	if err := c.emitConstant(seed, answer); err != nil {
		return true, err
	}
	var found []int
	err := c.compileLoop(loopShape{
		key: comprehension.KeyVariable, value: comprehension.Variable, accumulator: foldAnswer,
		where: comprehension.Where, result: &answer,
		body: func() error {
			if err := c.compile(comprehension.Yield); err != nil {
				return err
			}
			found = append(found, c.emit(machine.Instruction{Op: machine.OpLoopBreak, Type: &answer}))
			return nil
		},
	})
	if err != nil {
		return true, err
	}
	dropped := c.nextLocal
	c.nextLocal++
	c.emit(machine.Instruction{Op: machine.OpStoreLocal, A: dropped})
	empty, err := machine.Array(answer, nil)
	if err != nil {
		return true, err
	}
	if err := c.emitConstant(empty, machine.ArrayOf(answer)); err != nil {
		return true, err
	}
	c.emitCall(function, 1, answer)
	c.patch(found, len(c.instructions))
	return true, nil
}

// seedOf is a value of typ to stand for a first's answer until an item is
// found: a zero, which no run answers with, for the plain types.
func seedOf(typ machine.Type) (machine.Value, bool) {
	switch typ.Kind() {
	case machine.BoolKind:
		return machine.Bool(false), true
	case machine.IntKind:
		return machine.Int(0), true
	case machine.FloatKind:
		return machine.Float(0), true
	case machine.StringKind:
		return machine.String(""), true
	}
	return machine.Value{}, false
}

// plainValue reports an expression that is only read — a name or a
// literal — which a count need not compute: it cannot fail.
func plainValue(expr syntax.Expr) bool {
	switch expr.(type) {
	case *syntax.VariableExpr, *syntax.LiteralExpr:
		return true
	}
	return false
}

// foldBody fills in what a fused fold does with each item, and returns the
// loop_break instructions it will lay out, whose target is the loop's exit.
// Each item costs what collecting it did: one instruction after the item.
func (c *bytecodeCompiler) foldBody(shape *loopShape, yield syntax.Expr, fold *machine.Fold, answer, item machine.Type) *[]int {
	breaks := &[]int{}
	stop := func() error {
		if err := c.emitConstant(machine.Bool(fold.Stop), answer); err != nil {
			return err
		}
		*breaks = append(*breaks, c.emit(machine.Instruction{Op: machine.OpLoopBreak, Type: &answer}))
		return nil
	}
	switch {
	case fold.Stops && fold.Stop:
		// any: a false item goes on, a true one is the answer.
		shape.body = func() error {
			if err := c.compile(yield); err != nil {
				return err
			}
			onward := c.emit(machine.Instruction{Op: machine.OpJumpIfFalse})
			defer func() { c.patch([]int{onward}, len(c.instructions)) }()
			return stop()
		}
	case fold.Stops:
		// all: the stop is laid out ahead of the items, where the first
		// jumps over it, and a false item jumps back to it.
		stopAt := 0
		shape.prelude = func() error {
			skip := c.emit(machine.Instruction{Op: machine.OpJump})
			stopAt = len(c.instructions)
			defer func() { c.patch([]int{skip}, len(c.instructions)) }()
			return stop()
		}
		shape.body = func() error {
			if err := c.compile(yield); err != nil {
				return err
			}
			c.emit(machine.Instruction{Op: machine.OpJumpIfFalse, A: stopAt})
			return nil
		}
	default:
		shape.body = func() error { return c.foldItem(yield, fold, answer, item) }
	}
	return breaks
}

// foldItem folds one item into the answer with the fold's step, or counts
// it.
func (c *bytecodeCompiler) foldItem(yield syntax.Expr, fold *machine.Fold, answer, item machine.Type) error {
	stepName, stepItem := fold.Step, item
	if fold.Counts {
		stepName, stepItem = "add", machine.IntType
		if err := c.emitConstant(machine.Int(1), machine.IntType); err != nil {
			return err
		}
	} else if err := c.compile(yield); err != nil {
		return err
	}
	step, ok := machine.FoldStep(c.registry, stepName, answer, stepItem)
	if !ok {
		return syntax.Around(yield, "internal error: no fold step %s", stepName)
	}
	c.emit(machine.Instruction{Op: machine.OpLoopFold, A: c.callRef(step), Type: &answer})
	return nil
}

// hoistInnerSource is the prelude of a nested comprehension's outer clause:
// the inner clause's source, computed once, when it reads nothing the outer
// clause binds and calls only what may be folded — then every item would
// compute the same value. Without a filter the source is the first thing an
// item computes, so the first item computes it where it always did, and the
// rest read it. Nil when there is nothing to hoist.
func (c *bytecodeCompiler) hoistInnerSource(outer *syntax.ForExpr) func() error {
	inner, ok := outer.Yield.(*syntax.ForExpr)
	if !ok || !outer.Flatten || outer.Where != nil || c.plain || plainValue(inner.Source) || !c.invariant(inner.Source, outer) {
		return nil
	}
	return func() error {
		if err := c.compile(inner.Source); err != nil {
			return err
		}
		slot := c.nextLocal
		c.nextLocal++
		c.emit(machine.Instruction{Op: machine.OpStoreLocal, A: slot})
		c.hoisted[inner.Source.NodeID()] = slot
		return nil
	}
}

// invariant reports an expression every item of the loop computes the same:
// it reads none of the loop's names, and calls only what may be folded.
func (c *bytecodeCompiler) invariant(expr syntax.Expr, loop *syntax.ForExpr) bool {
	for _, name := range syntax.FreeVariables(expr) {
		if name == loop.Variable || name == loop.KeyVariable {
			return false
		}
	}
	return constexprOnly(expr, c.inferred, c.registry)
}
