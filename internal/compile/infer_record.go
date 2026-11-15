package compile

import (
	"fmt"
	"slices"
	"strings"

	"github.com/nethinwei/funroute/internal/machine"
	"github.com/nethinwei/funroute/internal/syntax"
)

// inferRecord types a record literal: every field is typed on its own, and
// the record's type is those types in the order they were written, or in
// the order of a record it unifies with that has the same fields.
func inferRecord(node *syntax.RecordExpr, state *inferState, context inferContext) (typeTerm, error) {
	fields := make([]fieldTerm, len(node.Fields))
	for i, field := range node.Fields {
		term, err := inferExpr(field.Value, state, context)
		if err != nil {
			return typeTerm{}, err
		}
		fields[i] = fieldTerm{name: field.Name, term: term}
	}
	// The literal's term sits behind a variable of its own: its fields are in
	// the order they were written until it meets a record that settles
	// another, and that order is bound to the variable (unifyRecords).
	literal := state.fresh()
	state.bind(literal.id, typeTerm{kind: machine.RecordKind, fields: fields, loose: true})
	state.checks = append(state.checks, func() error {
		if _, ok := state.publicType(literal); !ok {
			return syntax.Around(node, "type error: every record field needs a type of its own")
		}
		return nil
	})
	return record(node, state, literal), nil
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

// branchMismatch says how a branch's result, the node at, differs from what
// the branches before it return: the two types, and for two records which
// fields one has that the other lacks, or which field's type differs.
func (s *inferState) branchMismatch(branch string, at syntax.Expr, want typeTerm) string {
	got := s.typeAt(at.NodeID())
	message := fmt.Sprintf(": %s returns %s, the branches before it %s", branch, s.describe(got), s.describe(want))
	if hint := s.recordDifference(want, got); hint != "" {
		message += "; " + hint
	}
	return message
}

// typeAt is the term inference stamped on node id.
func (s *inferState) typeAt(id int) typeTerm {
	if id < len(s.nodeTypes) {
		return s.nodeTypes[id]
	}
	return typeTerm{}
}

// recordDifference says how record got differs from record want, in the
// words a rule's author fixes it in: the fields it lacks, the fields it has
// besides, the first field of another type, or the same fields in another
// order. Empty when either is not a record, or nothing tells them apart.
func (s *inferState) recordDifference(want, got typeTerm) string {
	want, got = s.deref(want), s.deref(got)
	if want.kind != machine.RecordKind || got.kind != machine.RecordKind {
		return ""
	}
	var missing, extra []string
	for _, field := range want.fields {
		if fieldIndex(got, field.name) < 0 {
			missing = append(missing, field.name)
		}
	}
	for _, field := range got.fields {
		if fieldIndex(want, field.name) < 0 {
			extra = append(extra, field.name)
		}
	}
	var parts []string
	if len(missing) > 0 {
		parts = append(parts, "it lacks "+strings.Join(missing, ", "))
	}
	if len(extra) > 0 {
		parts = append(parts, "it has "+strings.Join(extra, ", ")+" besides")
	}
	if len(parts) > 0 {
		return strings.Join(parts, ", and ")
	}
	for _, field := range want.fields {
		other := got.fields[fieldIndex(got, field.name)]
		if a, b := s.describe(field.term), s.describe(other.term); a != b {
			return fmt.Sprintf("field %s is %s in one and %s in the other", field.name, a, b)
		}
	}
	if !sameFieldOrder(want, got) {
		return "the fields are the same in another order: both record types are fixed, and the order of its fields is part of a type"
	}
	return ""
}

// unifyRecords unifies two records: the same fields, each of one type. The
// order of a record type's fields is part of the type, but a literal's is
// only how it was written: where the names are the same in another order, a
// literal takes the other record's order. Two records that unify around a
// literal become one term, so an order settled later, by a third, reaches
// every literal that met it.
func (s *inferState) unifyRecords(left, right typeTerm, leftVar, rightVar int) error {
	if len(left.fields) != len(right.fields) {
		return s.cannotUnify(left, right)
	}
	if !sameFieldOrder(left, right) {
		switch {
		case right.loose && rightVar != 0:
			return s.adopt(right, rightVar, left, leftVar)
		case left.loose && leftVar != 0:
			return s.adopt(left, leftVar, right, rightVar)
		default:
			return s.cannotUnify(left, right)
		}
	}
	if err := s.unifyFields(left, right); err != nil {
		return err
	}
	switch {
	case leftVar == rightVar:
	case right.loose && rightVar != 0:
		s.bind(rightVar, termVia(left, leftVar))
	case left.loose && leftVar != 0:
		s.bind(leftVar, termVia(right, rightVar))
	}
	return nil
}

// adopt unifies literal, the term of variable id, with other in other's
// order, and binds the variable to other: the literal's fields are now in
// the order other's type has them.
func (s *inferState) adopt(literal typeTerm, id int, other typeTerm, otherVar int) error {
	aligned, ok := alignedTo(literal, other)
	if !ok {
		return s.cannotUnify(other, literal)
	}
	if err := s.unifyFields(other, aligned); err != nil {
		return err
	}
	s.bind(id, termVia(other, otherVar))
	return nil
}

// unifyFields unifies two records' fields, which are the same names in the
// same order, pairwise.
func (s *inferState) unifyFields(left, right typeTerm) error {
	for i, field := range left.fields {
		if err := s.unify(field.term, right.fields[i].term); err != nil {
			return err
		}
	}
	return nil
}

// sameFieldOrder reports whether two records of as many fields name them
// in one order.
func sameFieldOrder(left, right typeTerm) bool {
	for i, field := range left.fields {
		if field.name != right.fields[i].name {
			return false
		}
	}
	return true
}

// alignedTo is record's fields in the order other names them; false when
// the two do not have the same names.
func alignedTo(record, other typeTerm) (typeTerm, bool) {
	fields := make([]fieldTerm, len(other.fields))
	for i, field := range other.fields {
		at := fieldIndex(record, field.name)
		if at < 0 {
			return typeTerm{}, false
		}
		fields[i] = record.fields[at]
	}
	record.fields = fields
	return record, true
}

// termVia is term as a variable reaches it, the variable when there is one.
func termVia(term typeTerm, id int) typeTerm {
	if id != 0 {
		return varTerm(id)
	}
	return term
}
