package compile

import (
	"slices"

	"github.com/nethinwei/funroute/internal/machine"
	"github.com/nethinwei/funroute/internal/syntax"
)

// inferRecord types a record literal: every field is typed on its own, and
// the record's type is those types in the order they were written.
func inferRecord(node *syntax.RecordExpr, state *inferState, context inferContext) (typeTerm, error) {
	fields := make([]fieldTerm, len(node.Fields))
	for i, field := range node.Fields {
		term, err := inferExpr(field.Value, state, context)
		if err != nil {
			return typeTerm{}, err
		}
		fields[i] = fieldTerm{name: field.Name, term: term}
	}
	term := typeTerm{kind: machine.RecordKind, fields: fields}
	state.checks = append(state.checks, func() error {
		if _, ok := state.publicType(term); !ok {
			return syntax.Around(node, "type error: every record field needs a type of its own")
		}
		return nil
	})
	return record(node, state, term), nil
}

// fieldIndex is the position of the named field in a record term, or -1.
func fieldIndex(record typeTerm, name string) int {
	return slices.IndexFunc(record.fields, func(field fieldTerm) bool { return field.name == name })
}

// inferField reads one field off a record, once the record's type is known
// — from the contract, from a literal, from a let binding or from a call
// decided later — because the field's own type comes from it.
func inferField(node *syntax.FieldExpr, state *inferState, context inferContext) (typeTerm, error) {
	base, err := inferExpr(node.Value, state, context)
	if err != nil {
		return typeTerm{}, err
	}
	result := state.fresh()
	unknown := func() error {
		return syntax.Around(node, "type error: %q is read off something that is not a record with a known type", node.Field)
	}
	err = state.waitFor(func() (bool, error) {
		found := state.deref(base)
		switch {
		case found.kind == machine.VarKind:
			return false, nil
		case found.kind != machine.RecordKind:
			return false, unknown()
		}
		index := fieldIndex(found, node.Field)
		if index < 0 {
			return false, syntax.Around(node, "type error: %s has no field %q", state.describe(found), node.Field)
		}
		return true, state.unify(result, found.fields[index].term)
	}, unknown)
	return record(node, state, result), err
}
