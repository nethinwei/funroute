package compile

import (
	"github.com/nethinwei/funroute/internal/machine"
	"github.com/nethinwei/funroute/internal/syntax"
)

func (c *bytecodeCompiler) compileFor(node *syntax.ForExpr) error {
	if err := c.compile(node.Source); err != nil {
		return err
	}
	resultType, ok := c.inferred.nodeType(node.ID)
	if !ok || !hasElem(resultType) ||
		(resultType.Kind() != machine.ArrayKind && resultType.Kind() != machine.DictKind) {
		return syntax.Around(node, "cannot compile for with unresolved result type")
	}
	return c.compileLoop(loopShape{
		key: node.KeyVariable, value: node.Variable, where: node.Where, result: &resultType,
		prelude: c.hoistInnerSource(node),
		body:    func() error { return c.compileYield(node, resultType) },
	})
}

// loopShape is a loop to lay out: the names it binds — accumulator is the
// one a reduce folds into, and empty for a comprehension — its filter, its
// type, and what it runs: prelude once, before the first item, when there is
// one, and body for each item the filter keeps.
type loopShape struct {
	key, value, accumulator string
	where                   syntax.Expr
	result                  *machine.Type
	prelude, body           func() error
}

// compileLoop lays out a loop over the source on the stack: loop_init, the
// prelude, the condition, body, loop_next. A filtered item jumps straight to
// loop_next, which goes back past the prelude.
func (c *bytecodeCompiler) compileLoop(shape loopShape) error {
	slot := c.bindLocal(shape.value)
	defer c.unbindLocal(shape.value)
	keySlot := c.bindLocal(shape.key)
	defer c.unbindLocal(shape.key)
	// The accumulator's slot is the loop's from the start, its name the
	// body's alone: the condition reads the name as what is outside the
	// loop, as inference typed it.
	accSlot := machine.NoAccumulator
	if shape.accumulator != "" {
		accSlot = c.nextLocal
		c.nextLocal++
	}

	init := c.emit(machine.Instruction{Op: machine.OpLoopInit, B: slot, C: accSlot, D: keySlot, Type: shape.result})
	if shape.prelude != nil {
		if err := shape.prelude(); err != nil {
			return err
		}
	}
	loopStart := len(c.instructions)
	var filtered []int
	if shape.where != nil {
		misses, err := c.compileCondition(shape.where)
		if err != nil {
			return err
		}
		filtered = misses
	}
	if shape.accumulator != "" {
		c.names.push(shape.accumulator, nameSlot{index: accSlot})
		defer c.names.pop(shape.accumulator)
	}
	if err := shape.body(); err != nil {
		return err
	}
	next := c.emit(machine.Instruction{Op: machine.OpLoopNext, A: loopStart, Type: shape.result})
	c.patch(filtered, next)
	c.instructions[init].A = len(c.instructions)
	return nil
}

// compileYield emits the loop body and the instruction that takes its value.
// An outer clause of a nested comprehension spreads: what it yields is the
// inner loop's whole array, so the instruction checks it against the loop's own
// type and splices the elements.
func (c *bytecodeCompiler) compileYield(node *syntax.ForExpr, resultType machine.Type) error {
	// A dictionary comprehension pushes the key first, so loop_collect finds
	// the value on top and the key under it.
	pairs := 0
	if node.YieldKey != nil {
		if err := c.compile(node.YieldKey); err != nil {
			return err
		}
		pairs = 1
	}
	if err := c.compile(node.Yield); err != nil {
		return err
	}
	if node.Flatten {
		c.emit(machine.Instruction{Op: machine.OpLoopSpread, Type: &resultType})
		return nil
	}
	yieldType := elemOf(resultType)
	c.emit(machine.Instruction{Op: machine.OpLoopCollect, A: pairs, Type: &yieldType})
	return nil
}

// bindLocal reserves a local slot for a name bound by `for` or `reduce`. The
// slot is scoped: unbindLocal restores any outer binding of the same name. An
// empty name — the key of a loop over an array — takes no slot: it is NoKey.
func (c *bytecodeCompiler) bindLocal(name string) int {
	if name == "" {
		return machine.NoKey
	}
	slot := c.nextLocal
	c.nextLocal++
	c.names.push(name, nameSlot{index: slot})
	return slot
}

func (c *bytecodeCompiler) unbindLocal(name string) {
	if name == "" {
		return
	}
	c.names.pop(name)
}

// compileReduce lays out: source, init, loop_init, condition, body,
// loop_collect, loop_next. It shares the loop opcodes with for; the
// accumulator slot in C is what makes it a fold instead of a mapping, and a
// filtered item jumps straight to loop_next, leaving the accumulator alone.
func (c *bytecodeCompiler) compileReduce(node *syntax.ReduceExpr) error {
	resultType, ok := c.inferred.nodeType(node.ID)
	if !ok || !resultType.IsConcrete() {
		return syntax.Around(node, "cannot compile reduce with unresolved result type")
	}
	if err := c.compile(node.Source); err != nil {
		return err
	}
	if err := c.compile(node.Init); err != nil {
		return err
	}
	return c.compileLoop(loopShape{
		key: node.KeyVariable, value: node.Variable, accumulator: node.Accumulator, where: node.Where, result: &resultType,
		body: func() error {
			if err := c.compile(node.Body); err != nil {
				return err
			}
			c.emit(machine.Instruction{Op: machine.OpLoopCollect, Type: &resultType})
			return nil
		},
	})
}
