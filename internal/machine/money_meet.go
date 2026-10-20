package machine

import (
	"fmt"

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

// meetAll checks that values of typ meet. A type that holds no money has
// nothing to check.
func meetAll(typ Type, values ...Value) error {
	if !containsUnits(typ) {
		return nil
	}
	plan, places := planUnits(typ, 0)
	m := unitsMeet{at: make([]string, places)}
	for _, v := range values {
		if err := m.meet(&plan, v); err != nil {
			return err
		}
	}
	return nil
}

// meetIn checks that item meets the items of array, of its type.
func meetIn(item, array Value) error {
	typ := array.Type()
	if !containsUnits(typ) {
		return nil
	}
	plan, places := planUnits(typ, 0)
	m := unitsMeet{at: make([]string, places)}
	if err := m.meet(plan.elem, item); err != nil {
		return err
	}
	return m.meet(&plan, array)
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

// see adds one unit at place: the currency-less zero's, "", meets any.
func (m *unitsMeet) see(place int, unit string, pair bool) error {
	seen := m.at[place]
	switch {
	case unit == "" || seen == unit:
		return nil
	case seen == "":
		m.at[place] = unit
		return nil
	case pair:
		return fmt.Errorf("%w: cannot compare an exchange rate for %s with one for %s", ErrCurrency, seen, unit)
	}
	return fmt.Errorf("%w: cannot compare %s with %s", ErrCurrency, seen, unit)
}
