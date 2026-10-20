package machine

import (
	"strings"

	"github.com/nethinwei/funroute/internal/money"
)

// Whether values that hold money are equal is only a question where they
// meet (docs/semantics.md): every amount at one place of their type is in
// one currency, or is the currency-less zero, and every exchange rate at one
// place is for one pair. A place is a path of fields; an array's items, and
// a dictionary's values, are one place. That is checked over every value an
// equality reads — ==, in, switch, index_of, unique, intersect, except — in
// any order, before any two are compared: USD 1 in [EUR 1, USD 1] fails as
// USD 1 in [USD 1, EUR 1] does, whatever else differs first.

// unitsMeet is the unit seen so far at each place of a type: a currency, or
// an exchange rate's pair as its literal writes it; "" while none is.
type unitsMeet struct {
	at []string
}

// fewPlaces is how many places a caller's own array holds, so a type of
// that many asks without allocating.
type fewPlaces [4]string

// meeting is a unitsMeet of n places, in few when they fit.
func meeting(few *fewPlaces, n int) unitsMeet {
	if n <= len(few) {
		return unitsMeet{at: few[:n]}
	}
	return unitsMeet{at: make([]string, n)}
}

// meetValue checks that the money in v meets: unique's question, and a
// sort's by amounts. A type that holds no money has nothing to check.
func meetValue(v Value) error {
	plan, places, units := planOfValue(v)
	if !units {
		return nil
	}
	var few fewPlaces
	m := meeting(&few, places)
	return m.meet(plan, v)
}

// meetPair checks that two values of one type, right's, meet: what == asks,
// and intersect and except.
func meetPair(left, right Value) error {
	plan, places, units := planOfValue(right)
	if !units {
		return nil
	}
	var few fewPlaces
	m := meeting(&few, places)
	if err := m.meet(plan, left); err != nil {
		return err
	}
	return m.meet(plan, right)
}

// meetIn checks that item meets the items of array, of its type.
func meetIn(item, array Value) error {
	plan, places, units := planOfValue(array)
	if !units {
		return nil
	}
	var few fewPlaces
	m := meeting(&few, places)
	if err := m.meet(plan.elem, item); err != nil {
		return err
	}
	return m.meet(plan, array)
}

// The plans of the types of one place that equalities read most — an
// amount, a rate, an array or a dictionary of either — made once.
var (
	amountPlan = unitPlan{kind: MoneyKind}
	ratePlan   = unitPlan{kind: FxRateKind}
	itemPlans  = [...][2]unitPlan{
		ArrayKind: {{kind: ArrayKind, elem: &amountPlan}, {kind: ArrayKind, elem: &ratePlan}},
		DictKind:  {{kind: DictKind, elem: &amountPlan}, {kind: DictKind, elem: &ratePlan}},
	}
)

// itemsPlan is the plan made once of an array or a dictionary, kind, of
// amounts or rates, elem; nil for any other.
func itemsPlan(kind, elem Kind) *unitPlan {
	switch {
	case kind != ArrayKind && kind != DictKind:
		return nil
	case elem == MoneyKind:
		return &itemPlans[kind][0]
	case elem == FxRateKind:
		return &itemPlans[kind][1]
	}
	return nil
}

// plansOf is typ's plan and how many places it has: one made once for the
// common types, a new one for any other.
func plansOf(typ Type) (*unitPlan, int) {
	switch {
	case typ.kind == MoneyKind:
		return &amountPlan, 1
	case typ.kind == FxRateKind:
		return &ratePlan, 1
	case typ.elem != nil:
		if plan := itemsPlan(typ.kind, typ.elem.kind); plan != nil {
			return plan, 1
		}
	}
	plan, places := planUnits(typ, 0)
	return &plan, places
}

// planOfValue is plansOf of v's type, and false when it holds no money. An
// array's or a dictionary's type is built only when its items hold money
// and are not amounts or rates.
func planOfValue(v Value) (*unitPlan, int, bool) {
	if v.kind == ArrayKind || v.kind == DictKind {
		elem := v.elemType()
		if !containsUnits(elem) {
			return nil, 0, false
		}
		if plan := itemsPlan(v.kind, elem.kind); plan != nil {
			return plan, 1, true
		}
	}
	typ := v.Type()
	if !containsUnits(typ) {
		return nil, 0, false
	}
	plan, places := plansOf(typ)
	return plan, places, true
}

// unitPlan is where a type holds money, worked out once for every value
// of it: the place of an amount or a rate, an array's or a dictionary's
// items, and only the fields of a record that hold any.
type unitPlan struct {
	kind   Kind
	place  int
	elem   *unitPlan
	fields []unitField
}

// unitField is a record's field that holds money, by its index.
type unitField struct {
	at   int
	plan unitPlan
}

// planUnits is typ's plan, its places numbered from place on, and how many
// places it has.
func planUnits(typ Type, place int) (unitPlan, int) {
	plan := unitPlan{kind: typ.kind, place: place}
	switch typ.kind {
	case MoneyKind, FxRateKind:
		return plan, 1
	case ArrayKind, DictKind:
		elem, places := planUnits(*typ.elem, place)
		plan.elem = &elem
		return plan, places
	}
	places := 0
	for i, field := range typ.fields {
		if containsUnits(field.typ) {
			sub, n := planUnits(field.typ, place+places)
			plan.fields = append(plan.fields, unitField{at: i, plan: sub})
			places += n
		}
	}
	return plan, places
}

// meet adds the units of v by its type's plan.
func (m *unitsMeet) meet(plan *unitPlan, v Value) error {
	switch plan.kind {
	case MoneyKind:
		return m.see(plan.place, v.s, false)
	case FxRateKind:
		return m.see(plan.place, pairText(v), true)
	case ArrayKind:
		return m.meetItems(plan, v)
	case DictKind:
		if amounts, ok := v.box.(map[string]money.Money); ok && m.amountsMeet(plan.elem.place, amounts) {
			return nil
		}
		// In key order, so a failure names the same two every run.
		return v.eachPart(true, func(entry Value) error { return m.meet(plan.elem, entry) })
	case RecordKind:
		record, _ := v.box.(*recordValue)
		for i := range plan.fields {
			if err := m.meet(&plan.fields[i].plan, record.fields[plan.fields[i].at]); err != nil {
				return err
			}
		}
	}
	return nil
}

// meetItems adds the units of an array's items: a native array of amounts
// read as it is.
func (m *unitsMeet) meetItems(plan *unitPlan, v Value) error {
	if amounts, ok := nativeItems[money.Money](v); ok {
		for _, amount := range amounts {
			if err := m.see(plan.place, amount.Currency(), false); err != nil {
				return err
			}
		}
		return nil
	}
	for i := range v.length() {
		if err := m.meet(plan.elem, v.at(i)); err != nil {
			return err
		}
	}
	return nil
}

// amountsMeet adds the currencies of a dictionary of amounts, in the map's
// order, when they meet; when they do not it changes nothing, and the
// dictionary is walked again in key order to say which two.
func (m *unitsMeet) amountsMeet(place int, amounts map[string]money.Money) bool {
	seen := m.at[place]
	for _, amount := range amounts {
		switch unit := amount.Currency(); {
		case unit == "" || unit == seen:
		case seen == "":
			seen = unit
		default:
			return false
		}
	}
	m.at[place] = seen
	return true
}

// see adds one unit at place: the currency-less zero's, "", meets any.
func (m *unitsMeet) see(place int, unit string, pair bool) error {
	seen := m.at[place]
	switch {
	case unit == "" || seen == unit:
		return nil
	case seen == "":
		m.at[place] = unit
		return nil
	}
	// A copy of the unit seen, so the error holds nothing of m: what m
	// holds would otherwise be taken to leave with it, and m, on the
	// caller's stack, would be moved to the heap on every call.
	return unitsDiffer(strings.Clone(seen), unit, pair)
}
