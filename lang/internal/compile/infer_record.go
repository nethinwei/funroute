package compile

import (
	"slices"

	"funroute/lang/internal/machine"
)

// A record's shape is concrete wherever one appears, but the currencies in
// it follow the same rules as money on its own: every currency position in
// the record — a field, an item of a field, a field of a field — is a unit
// class of its own (typeTerm.units, in recordUnitNames order). Two records
// of one shape unify by merging those classes, so a known currency flows
// into a record declared as money, and one known only at run time into a
// record declared as money<USD> with a check where it arrives.

// recordUnitNames lists the units a type names, in the order a record term
// keeps its unit classes.
func recordUnitNames(t machine.Type) []string {
	return appendRecordUnits(nil, t)
}

func appendRecordUnits(out []string, t machine.Type) []string {
	switch t.Kind() {
	case machine.MoneyKind, machine.CurrencyKind, machine.FxRateKind:
		return append(out, t.Units()...)
	case machine.ArrayKind, machine.DictKind:
		if hasElem(t) {
			return appendRecordUnits(out, elemOf(t))
		}
	case machine.RecordKind:
		for _, field := range t.Fields() {
			out = appendRecordUnits(out, field.Type())
		}
	}
	return out
}

// withUnits is t with its units, in recordUnitNames order, replaced by names;
// it returns the names it did not use.
func withUnits(t machine.Type, names []string) (machine.Type, []string) {
	switch t.Kind() {
	case machine.MoneyKind:
		return machine.MoneyOf(names[0]), names[1:]
	case machine.CurrencyKind:
		return machine.CurrencyOf(names[0]), names[1:]
	case machine.FxRateKind:
		return machine.FxRateOf(names[0], names[1]), names[2:]
	case machine.ArrayKind, machine.DictKind:
		elem, rest := withUnits(elemOf(t), names)
		if t.Kind() == machine.ArrayKind {
			return machine.ArrayOf(elem), rest
		}
		return machine.DictOf(elem), rest
	case machine.RecordKind:
		fields := make([]machine.Field, len(t.Fields()))
		for i, field := range t.Fields() {
			var typ machine.Type
			typ, names = withUnits(field.Type(), names)
			fields[i] = machine.FieldOf(field.Name(), typ)
		}
		return machine.RecordOf(fields...), names
	default:
		return t, names
	}
}

// recordTerm is the term of a known record type, each unit a fresh class.
func (s *inferState) recordTerm(t machine.Type) typeTerm {
	names := recordUnitNames(t)
	units := make([]typeTerm, len(names))
	for i, name := range names {
		units[i] = s.freshUnit(unitFor(name))
	}
	return s.recordOver(t, units)
}

// recordOver is the term of a record type whose units are these classes.
func (s *inferState) recordOver(t machine.Type, units []typeTerm) typeTerm {
	cloned := machine.CloneType(t)
	return typeTerm{kind: machine.RecordKind, record: &cloned, units: units}
}

// unitTermsOf lists the unit classes of a settled term, in recordUnitNames
// order of its public type.
func (s *inferState) unitTermsOf(term typeTerm) []typeTerm {
	term = s.deref(term)
	switch term.kind {
	case machine.MoneyKind, machine.CurrencyKind:
		return []typeTerm{*term.elem}
	case machine.FxRateKind:
		return []typeTerm{*term.elem, *term.quote}
	case machine.ArrayKind, machine.DictKind:
		if term.elem != nil {
			return s.unitTermsOf(*term.elem)
		}
	case machine.RecordKind:
		return term.units
	}
	return nil
}

// publicRecord is a record term's type with what is known of its units.
func (s *inferState) publicRecord(term typeTerm) machine.Type {
	names := make([]string, len(term.units))
	for i, unit := range term.units {
		names[i] = s.unitOf(unit).published()
	}
	typ, _ := withUnits(*term.record, names)
	return typ
}

// unifyRecords unifies two records of one shape, class by class.
func (s *inferState) unifyRecords(left, right typeTerm) bool {
	if left.record == nil || right.record == nil || !machine.SameShape(*left.record, *right.record) || len(left.units) != len(right.units) {
		return false
	}
	for i := range left.units {
		s.unifyUnit(left.units[i], right.units[i])
	}
	return true
}

// fieldTerm is the term of a record's field, over the record's own classes:
// what the field's currency is known to be is what the record says.
func (s *inferState) fieldTerm(record typeTerm, index int) typeTerm {
	offset := 0
	for _, field := range record.record.Fields()[:index] {
		offset += len(recordUnitNames(field.Type()))
	}
	term, _ := s.termOver(record.record.Fields()[index].Type(), record.units[offset:])
	return term
}

// termOver builds the term of t over unit classes, returning the ones left.
func (s *inferState) termOver(t machine.Type, units []typeTerm) (typeTerm, []typeTerm) {
	switch t.Kind() {
	case machine.MoneyKind, machine.CurrencyKind:
		first := units[0]
		return typeTerm{kind: t.Kind(), elem: &first}, units[1:]
	case machine.FxRateKind:
		base, quote := units[0], units[1]
		return typeTerm{kind: t.Kind(), elem: &base, quote: &quote}, units[2:]
	case machine.ArrayKind, machine.DictKind:
		elem, rest := s.termOver(elemOf(t), units)
		return containerTerm(t.Kind(), elem), rest
	case machine.RecordKind:
		n := len(recordUnitNames(t))
		return s.recordOver(t, slices.Clone(units[:n])), units[n:]
	default:
		return s.concrete(t), units
	}
}
