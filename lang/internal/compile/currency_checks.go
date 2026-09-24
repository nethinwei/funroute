package compile

// The currency checks a program carries. The kernel's money operators check
// the currencies they are handed themselves, so a call to one needs nothing
// emitted. A host's or a pack's function cannot be trusted to: where its
// signature names one unit variable in several parameters, or a currency
// code, and inference could not prove the operands' currencies, the operands
// are checked just before the call. The result is checked at the end when
// the contract declares a currency the expression did not prove.

import (
	"fmt"

	"funroute/lang/internal/machine"
	"funroute/lang/internal/syntax"
)

// unitSlot is one place an operand holds a currency, as the signature names it.
type unitSlot struct {
	depth int    // how far below the stack top the operand sits
	quote bool   // the exchange rate's second side
	unit  string // the signature's unit: a variable or a code
	proof string // the currency inference proved there, "" when none
}

// emitUnitChecks checks the operands a host function's signature ties
// together, when inference left their currency to the run. Operands sharing
// a unit variable share a unit class too, so they are proven together or not
// at all; the check does not lean on that — one unproven operand puts every
// operand of its variable into the group.
func (c *bytecodeCompiler) emitUnitChecks(node *syntax.CallExpr, function *machine.RegisteredFunction) {
	if function.IsBuiltin() {
		return
	}
	slots := c.unitSlots(node, function)
	shared, unproven := map[string]int{}, map[string]bool{}
	for _, slot := range slots {
		shared[slot.unit]++
		unproven[slot.unit] = unproven[slot.unit] || slot.proof == ""
	}
	groups := map[string]int{}
	first := true
	for _, slot := range slots {
		instruction, needed := checkFor(slot, shared[slot.unit] > 1 && unproven[slot.unit], groups)
		if !needed {
			continue
		}
		if first {
			instruction.B |= machine.CheckNewCall
			first = false
		}
		c.emit(instruction)
	}
}

// checkFor is the instruction one slot needs, if any: a code the operand did
// not prove, or a place in the group of a variable that needs checking.
func checkFor(slot unitSlot, grouped bool, groups map[string]int) (machine.Instruction, bool) {
	instruction := machine.Instruction{Op: machine.OpCurrencyCheck, A: slot.depth}
	if slot.quote {
		instruction.B = machine.CheckQuote
	}
	switch {
	case machine.IsCurrencyCode(slot.unit) && slot.proof == "":
		instruction.C, instruction.Keys = machine.CheckCode, []string{slot.unit}
		return instruction, true
	case machine.IsUnitVariable(slot.unit) && grouped:
		if _, ok := groups[slot.unit]; !ok {
			groups[slot.unit] = len(groups)
		}
		instruction.C, instruction.D = machine.CheckGroup, groups[slot.unit]
		return instruction, true
	default:
		return instruction, false
	}
}

// unitSlots lists every currency the call's scalar operands hold, as their
// parameters name it. Money inside a container is the function's own to
// check: it walks the container anyway.
func (c *bytecodeCompiler) unitSlots(node *syntax.CallExpr, function *machine.RegisteredFunction) []unitSlot {
	var slots []unitSlot
	for i, param := range function.Params {
		if !machine.IsUnitKind(param.Kind()) {
			continue
		}
		depth := len(function.Params) - 1 - i
		proven := c.inferred.NodeTypes[node.Args[i].NodeID()]
		units, proofs := param.Units(), proven.Units()
		for side := range units {
			proof := ""
			if side < len(proofs) {
				proof = proofs[side]
			}
			slots = append(slots, unitSlot{depth: depth, quote: side == 1, unit: units[side], proof: proof})
		}
	}
	return slots
}

// checkResult checks the program's result against the contract's currency
// where the expression did not prove it.
func (c *bytecodeCompiler) checkResult(expr syntax.Expr) { c.checkValue(expr, c.inferred.Result) }

// checkValue checks the value expr just left on the stack against the units
// declared says, unless inference proved them.
func (c *bytecodeCompiler) checkValue(expr syntax.Expr, declared machine.Type) {
	if !carriesUnits(declared) {
		return
	}
	proven, ok := c.inferred.NodeTypes[expr.NodeID()]
	if ok && provesUnits(proven, declared) {
		return
	}
	c.emit(machine.Instruction{Op: machine.OpCurrencyCheck, C: machine.CheckPattern, Type: &declared})
}

// provesUnits reports whether every unit the declared type names is the
// one the proven type states.
func provesUnits(proven, declared machine.Type) bool {
	if proven.Kind() != declared.Kind() {
		return false
	}
	units, proofs := declared.Units(), proven.Units()
	for i, unit := range units {
		if unit != "" && (i >= len(proofs) || proofs[i] != unit) {
			return false
		}
	}
	for i, field := range declared.Fields() {
		if i >= len(proven.Fields()) || !provesUnits(proven.Fields()[i].Type(), field.Type()) {
			return false
		}
	}
	if hasElem(declared) {
		return hasElem(proven) && provesUnits(elemOf(proven), elemOf(declared))
	}
	return true
}

// compileRoundingScope compiles round(expr, @mode): expr, with every
// rounding step inside it taking mode. The mode is written, not computed —
// which rounding a rule applies is part of the rule.
func (c *bytecodeCompiler) compileRoundingScope(node *syntax.CallExpr) error {
	mode, ok := node.Args[1].(*syntax.EnumExpr)
	if !ok {
		return syntax.Around(node.Args[1], "type error: round needs its mode written out, such as @half_up")
	}
	// Each scope counts its own steps: a step inside a nested round takes the
	// inner mode, so it does not make the outer one round anything.
	saved, outer := c.rounding, c.roundSteps
	c.rounding, c.roundSteps = &mode.Member, 0
	err := c.compile(node.Args[0])
	mine := c.roundSteps
	c.rounding, c.roundSteps = saved, outer
	if err != nil {
		return err
	}
	if mine == 0 {
		return syntax.Around(node, "type error: nothing inside round(…) rounds; only money times a rate or an exchange rate, and money divided by one, do")
	}
	return nil
}

// roundedStep is the call a rounding step inside round(…) makes instead: the
// variant with the mode as its last argument, which is pushed here.
func (c *bytecodeCompiler) roundedStep(function *machine.RegisteredFunction) (*machine.RegisteredFunction, int, error) {
	if c.rounding == nil {
		return function, 0, nil
	}
	variant, ok := machine.RoundingVariant(c.registry, function)
	if !ok {
		return function, 0, nil
	}
	if err := c.emitConstant(machine.String(*c.rounding)); err != nil {
		return nil, 0, fmt.Errorf("rounding mode: %w", err)
	}
	c.roundSteps++
	return variant, 1, nil
}
