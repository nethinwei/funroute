package compile

import (
	"github.com/nethinwei/funroute/internal/machine"
	"github.com/nethinwei/funroute/internal/syntax"
)

// A record update, order with {amount: 1}, has its base's type: it replaces
// fields and never adds, removes or retypes one. That keeps the result the
// type it came from — an Order in, an Order out — so it can go back into the
// contract that declared it.

// inferRecordUpdate types the new values where they are written and, once
// the base's type is known, holds each to the type of the field it
// replaces; the update is the base's type.
func inferRecordUpdate(node *syntax.RecordUpdateExpr, state *inferState, context inferContext) (typeTerm, error) {
	base, err := inferExpr(node.Base, state, context)
	if err != nil {
		return typeTerm{}, err
	}
	values, err := inferArgs(updatedValues(node), state, context)
	if err != nil {
		return typeTerm{}, err
	}
	unknown := func() error {
		return syntax.Around(node.Base, "type error: with {…} updates something that is not a record with a known type")
	}
	err = state.waitFor(func() (bool, error) {
		base := state.deref(base)
		switch {
		case base.kind == machine.VarKind:
			return false, nil
		case base.kind != machine.RecordKind:
			return false, unknown()
		}
		return true, updateFields(node, base, values, state)
	}, unknown)
	return record(node, state, base), err
}

func updatedValues(node *syntax.RecordUpdateExpr) []syntax.Expr {
	values := make([]syntax.Expr, len(node.Fields))
	for i, field := range node.Fields {
		values[i] = field.Value
	}
	return values
}

// updateFields holds every new value to the type of the field it replaces.
func updateFields(node *syntax.RecordUpdateExpr, base typeTerm, values []typeTerm, state *inferState) error {
	for i, field := range node.Fields {
		index := fieldIndex(base, field.Name)
		if index < 0 {
			return syntax.Around(field.Value, "type error: %s has no field %q to update", state.describe(base), field.Name)
		}
		want := base.fields[index].term
		if err := state.unify(want, values[i]); err != nil {
			return syntax.Around(field.Value, "type error: field %q is %s, and an update keeps its type", field.Name, state.describe(want))
		}
	}
	return nil
}

// compileRecordUpdate pushes the base, then the new values in the order they
// were written, and replaces them in one instruction.
func (c *bytecodeCompiler) compileRecordUpdate(node *syntax.RecordUpdateExpr) error {
	values := make([]syntax.Expr, 0, len(node.Fields)+1)
	values = append(values, node.Base)
	names := make([]string, len(node.Fields))
	for i, field := range node.Fields {
		names[i] = field.Name
		values = append(values, field.Value)
	}
	return c.compileMake(node.ID, machine.RecordKind, values,
		machine.Instruction{Op: machine.OpRecordWith, Keys: names}, "internal error: record update with unresolved type")
}
