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
// rather than once an item. Both answer and fail as the program written
// does: a step of the fold that fails waits for the items after it, which
// the array built computes first (the machine's frame.failed).

// foldAnswer is the name a fused fold's answer is bound to: no program can
// write it, so it hides nothing.
const foldAnswer = " answer"

// compileAggregate lays out a call of a function that declares a Fold on a
// comprehension as that comprehension's loop, folding, and reports false for
// any other call. A fold that stops — an any, an all, a first — is laid out
// so whatever the comprehension's clauses and items are: that it stops is
// what it means (docs/semantics.md S4). One that folds every item is laid
// out so for a comprehension of one clause, and is otherwise the call.
func (c *bytecodeCompiler) compileAggregate(node *syntax.CallExpr, function *machine.RegisteredFunction) (bool, error) {
	fold := function.Fold
	if fold == nil || c.plain || len(node.Args) != 1 {
		return false, nil
	}
	comprehension, ok := node.Args[0].(*syntax.ForExpr)
	if !ok || comprehension.YieldKey != nil {
		return false, nil
	}
	answer := c.inferred.typeOf(node.ID)
	switch {
	case fold.First:
		return true, c.compileFirst(function, comprehension, answer)
	case fold.Stops:
		seed := func() error { return c.emitConstant(fold.Init, answer) }
		stops, err := c.compileStopping(comprehension, answer, seed, func(shape *loopShape, yield syntax.Expr) *[]int {
			return c.stopBody(shape, yield, fold, answer)
		})
		// A stop leaves the loop where loop_next's last iteration does.
		c.patch(stops, len(c.instructions))
		return true, err
	case comprehension.Flatten || fold.Counts && !plainValue(comprehension.Yield):
		return false, nil
	}
	item := elemOf(c.inferred.typeOf(comprehension.ID))
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
	return true, c.compileLoop(loopShape{
		key: comprehension.KeyVariable, value: comprehension.Variable, accumulator: foldAnswer,
		where: comprehension.Where, result: &answer,
		body: func() error { return c.foldItem(comprehension.Yield, fold, answer, item) },
	})
}

// compileStopping lays out the loops of a comprehension a fold stops in, a
// loop a clause, each with an answer of its own that starts as the seed: the
// outermost clause's source, then the seed, then the loops. innermost fills
// in the innermost loop, whose loop_breaks it is; a loop outside it that
// its clause stops stops too, with that answer, and goes on when the clause
// runs out. The loop_breaks returned are the outermost loop's, the answer on
// the stack where they go: the seed when no item stopped it.
func (c *bytecodeCompiler) compileStopping(clause *syntax.ForExpr, answer machine.Type, seed func() error, innermost func(*loopShape, syntax.Expr) *[]int) ([]int, error) {
	if err := c.compile(clause.Source); err != nil {
		return nil, err
	}
	if err := seed(); err != nil {
		return nil, err
	}
	shape := loopShape{
		key: clause.KeyVariable, value: clause.Variable, accumulator: foldAnswer,
		where: clause.Where, result: &answer,
	}
	inner, nested := clause.Yield.(*syntax.ForExpr)
	if !clause.Flatten || !nested {
		stops := innermost(&shape, clause.Yield)
		err := c.compileLoop(shape)
		return *stops, err
	}
	var stops []int
	shape.prelude = c.hoistInnerSource(clause)
	shape.body = func() error {
		stopped, err := c.compileStopping(inner, answer, seed, innermost)
		if err != nil {
			return err
		}
		// The clause ran out: its answer is the seed, as this loop's is.
		c.emit(machine.Instruction{Op: machine.OpLoopCollect, Type: &answer})
		onward := c.emit(machine.Instruction{Op: machine.OpJump})
		c.patch(stopped, len(c.instructions))
		stops = append(stops, c.emit(machine.Instruction{Op: machine.OpLoopBreak, Type: &answer}))
		c.patch([]int{onward}, len(c.instructions))
		return nil
	}
	err := c.compileLoop(shape)
	return stops, err
}

// compileFirst lays out a first on a comprehension as the loops that stop
// at the first item yielded, which is the answer. With a seed to stand for
// the answer until then, the answer is the loops' own; a type with none — an
// array, a handle — keeps the item in a local, and the loops answer whether
// there is one.
func (c *bytecodeCompiler) compileFirst(function *machine.RegisteredFunction, comprehension *syntax.ForExpr, answer machine.Type) error {
	seed, ok := seedOf(answer)
	if _, constant := machine.ConstantOf(seed, answer); !ok || !constant {
		return c.compileFirstKept(function, comprehension, answer)
	}
	found, err := c.compileStopping(comprehension, answer, func() error { return c.emitConstant(seed, answer) },
		func(shape *loopShape, yield syntax.Expr) *[]int {
			stops := &[]int{}
			shape.body = func() error {
				if err := c.compile(yield); err != nil {
					return err
				}
				*stops = append(*stops, c.emit(machine.Instruction{Op: machine.OpLoopBreak, Type: &answer}))
				return nil
			}
			return stops
		})
	if err != nil {
		return err
	}
	// No item: the seed is dropped, and the call on an empty array fails as
	// the call on the array built would have.
	c.drop()
	empty, err := machine.Array(answer, nil)
	if err != nil {
		return err
	}
	if err := c.emitConstant(empty, machine.ArrayOf(answer)); err != nil {
		return err
	}
	c.emitCall(function, 1, answer)
	c.patch(found, len(c.instructions))
	return nil
}

// compileFirstKept is compileFirst for an answer with no seed: the item
// found goes into a local, and the loops answer true. When they answer
// false there is no item, and the call is laid out as written — on an array
// built of none, which costs the sources and the filters again, and fails as
// it does.
func (c *bytecodeCompiler) compileFirstKept(function *machine.RegisteredFunction, comprehension *syntax.ForExpr, answer machine.Type) error {
	kept := c.nextLocal
	c.nextLocal++
	flag := machine.BoolType
	found, err := c.compileStopping(comprehension, flag, func() error { return c.emitConstant(machine.Bool(false), flag) },
		func(shape *loopShape, yield syntax.Expr) *[]int {
			stops := &[]int{}
			shape.body = func() error {
				if err := c.compile(yield); err != nil {
					return err
				}
				c.emit(machine.Instruction{Op: machine.OpStoreLocal, A: kept})
				if err := c.emitConstant(machine.Bool(true), flag); err != nil {
					return err
				}
				*stops = append(*stops, c.emit(machine.Instruction{Op: machine.OpLoopBreak, Type: &flag}))
				return nil
			}
			return stops
		})
	if err != nil {
		return err
	}
	c.drop()
	if err := c.compileFor(comprehension); err != nil {
		return err
	}
	c.emitCall(function, 1, answer)
	done := c.emit(machine.Instruction{Op: machine.OpJump})
	c.patch(found, len(c.instructions))
	c.drop()
	c.emit(machine.Instruction{Op: machine.OpLoadLocal, A: kept})
	c.patch([]int{done}, len(c.instructions))
	return nil
}

// drop takes the value on top of the stack off it, into a local nothing
// reads.
func (c *bytecodeCompiler) drop() {
	c.emit(machine.Instruction{Op: machine.OpStoreLocal, A: c.nextLocal})
	c.nextLocal++
}

// seedOf is a value of typ to stand for a first's answer until an item is
// found: a zero, which no run answers with, for the plain types, an amount,
// and a record of them — the channel a first(…) of channels finds, built
// only once it is found rather than every one that might be.
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
	case machine.MoneyKind:
		return machine.MoneyValue(0, ""), true
	case machine.RecordKind:
		fields := typ.Fields()
		seeds := make([]machine.Value, len(fields))
		for i, field := range fields {
			seed, ok := seedOf(field.Type())
			if !ok {
				return machine.Value{}, false
			}
			seeds[i] = seed
		}
		record, err := machine.Record(typ, seeds)
		return record, err == nil
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

// stopBody fills in what the innermost loop of an any or an all does with
// each item, and returns the loop_break instructions it will lay out.
func (c *bytecodeCompiler) stopBody(shape *loopShape, yield syntax.Expr, fold *machine.Fold, answer machine.Type) *[]int {
	breaks := &[]int{}
	stop := func() error {
		if err := c.emitConstant(machine.Bool(fold.Stop), answer); err != nil {
			return err
		}
		*breaks = append(*breaks, c.emit(machine.Instruction{Op: machine.OpLoopBreak, Type: &answer}))
		return nil
	}
	if fold.Stop {
		// any: a false item goes on, a true one is the answer.
		shape.body = func() error {
			onward, err := c.compileCondition(yield)
			if err != nil {
				return err
			}
			defer func() { c.patch(onward, len(c.instructions)) }()
			return stop()
		}
		return breaks
	}
	// all: the stop is laid out ahead of the items, where the first jumps
	// over it, and a false item jumps back to it.
	stopAt := 0
	shape.prelude = func() error {
		skip := c.emit(machine.Instruction{Op: machine.OpJump})
		stopAt = len(c.instructions)
		defer func() { c.patch([]int{skip}, len(c.instructions)) }()
		return stop()
	}
	shape.body = func() error {
		misses, err := c.compileCondition(yield)
		c.patch(misses, stopAt)
		return err
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
		// A comprehension laid out a second time (compileFirstKept)
		// computes its source again: the slot of the first is bound only
		// where that loop ran.
		delete(c.hoisted, inner.Source.NodeID())
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
	return c.callsConstexpr(expr)
}
