package compile

import (
	"github.com/nethinwei/funroute/internal/kit"
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

func updatedValues(node *syntax.RecordUpdateExpr) []syntax.Expr { return fieldValues(node.Fields) }

func fieldValues(fields []syntax.RecordFieldExpr) []syntax.Expr {
	return kit.Map(fields, func(field syntax.RecordFieldExpr) syntax.Expr { return field.Value })
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
	values := append([]syntax.Expr{node.Base}, fieldValues(node.Fields)...)
	names := kit.Map(node.Fields, func(field syntax.RecordFieldExpr) string { return field.Name })
	return c.compileMake(node.ID, machine.RecordKind, values,
		machine.Instruction{Op: machine.OpRecordWith, Keys: names}, "internal error: record update with unresolved type")
}
