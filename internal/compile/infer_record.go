package compile

import "github.com/nethinwei/funroute/internal/machine"

// concrete is the term of a known type.
func (s *inferState) concrete(t machine.Type) typeTerm {
	switch t.Kind() {
	case machine.ArrayKind, machine.DictKind:
		return containerTerm(t.Kind(), s.concrete(elemOf(t)))
	case machine.RecordKind:
		return recordTerm(t)
	default:
		return typeTerm{kind: t.Kind(), name: t.Name(), values: append([]string(nil), t.Values()...)}
	}
}

// recordTerm is the term of a record type. A record's shape is concrete
// wherever one appears — it comes from the contract or from a literal whose
// field values have types — so it unifies as a whole.
func recordTerm(t machine.Type) typeTerm {
	cloned := machine.CloneType(t)
	return typeTerm{kind: machine.RecordKind, record: &cloned}
}

// unifyRecords reports whether two record terms are one type.
func (s *inferState) unifyRecords(left, right typeTerm) bool {
	return left.record != nil && right.record != nil && left.record.Equal(*right.record)
}

// fieldTerm is the term of a record's field.
func (s *inferState) fieldTerm(record typeTerm, index int) typeTerm {
	return s.concrete(record.record.Fields()[index].Type())
}
