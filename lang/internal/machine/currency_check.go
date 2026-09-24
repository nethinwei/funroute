package machine

import (
	"fmt"
	"slices"
)

// The currencies of one run. A contract's currency variable — the c of
// money<c> — is bound by the arguments each time the program runs: they must
// agree with each other, every currency must be declared, and a unit written
// as a code must be that code. Arrays and records of money are walked for
// this, the one place the boundary reads a container's elements: a value
// carries its currency, and nothing else can say which one it is.

// UnitVariables lists the currency variables a contract's parameters use,
// sorted: the order the frame keeps their bindings in. The compiler numbers
// the variables the same way, so a check names one by index.
func UnitVariables(params []Parameter) []string {
	var names []string
	for _, param := range params {
		_ = WalkTypes(param.typ, func(inner Type) error {
			names = appendUnitVariables(names, inner.Units())
			return nil
		})
	}
	slices.Sort(names)
	return names
}

func appendUnitVariables(names, units []string) []string {
	for _, unit := range units {
		if IsUnitVariable(unit) && !slices.Contains(names, unit) {
			names = append(names, unit)
		}
	}
	return names
}

// moneyPlan is what a runtime precomputes about the money in its arguments.
type moneyPlan struct {
	table *currencyTable
	units []string
	// scan marks the parameters whose type holds a unit-carrying kind.
	scan []bool
	any  bool
	// result marks a result type that holds money, whose currency-less zeros
	// the frame fills in.
	result bool
	// results marks, by program counter, the calls whose result type names
	// a currency somewhere in it; nil when no call's does.
	results []bool
}

func newMoneyPlan(artifact *Artifact, registry *Registry) moneyPlan {
	plan := moneyPlan{table: registry.currencies(), units: UnitVariables(artifact.parts.Args)}
	plan.result = TypeContains(artifact.parts.Result, MoneyKind)
	plan.scan = make([]bool, len(artifact.parts.Args))
	for i, param := range artifact.parts.Args {
		plan.scan[i] = containsUnits(param.typ)
		plan.any = plan.any || plan.scan[i]
	}
	plan.results = unitResults(artifact)
	return plan
}

// unitResults marks the calls whose result type states a currency: the ones
// a host function's result is checked against, all the way in.
func unitResults(artifact *Artifact) []bool {
	var marks []bool
	for pc, instruction := range artifact.parts.Instructions {
		if instruction.Op != OpCall || instruction.Type == nil || !statesUnits(*instruction.Type) {
			continue
		}
		if marks == nil {
			marks = make([]bool, len(artifact.parts.Instructions))
		}
		marks[pc] = true
	}
	return marks
}

// statesUnits reports a type naming a currency — a code or a variable —
// anywhere in it; a unit left empty is decided by the run and checks nothing.
func statesUnits(typ Type) bool {
	found := false
	_ = WalkTypes(typ, func(inner Type) error {
		for _, unit := range inner.Units() {
			found = found || unit != ""
		}
		return nil
	})
	return found
}

// checksResult reports a call whose result the frame holds to its type's
// currencies. Small enough to inline: a program without such a call pays one
// nil check per call.
func (p *moneyPlan) checksResult(pc int) bool {
	return p.results != nil && p.results[pc]
}

func containsUnits(typ Type) bool {
	found := false
	_ = WalkTypes(typ, func(inner Type) error {
		found = found || IsUnitKind(inner.kind)
		return nil
	})
	return found
}

// bindUnits checks the arguments' currencies and binds the contract's
// currency variables for this run.
func (f *frame) bindUnits(args []Value) error {
	count := len(f.runtime.money.units)
	f.units = f.unitsArray[:0]
	if count > len(f.unitsArray) {
		if cap(f.unitsSpill) < count {
			f.unitsSpill = make([]string, count)
		}
		f.units = f.unitsSpill[:0]
	}
	f.units = f.units[:count]
	clear(f.units)
	if err := f.scanArgs(args, false); err != nil {
		// A dictionary is walked in the map's order, which differs from run
		// to run; the error is said the same every time, from a walk in key
		// order. Only a refused run pays for it.
		clear(f.units)
		return f.scanArgs(args, true)
	}
	return nil
}

// scanArgs scans the arguments whose types hold units, dictionaries in key
// order when ordered.
func (f *frame) scanArgs(args []Value, ordered bool) error {
	plan := &f.runtime.money
	for i, param := range f.runtime.artifact.parts.Args {
		// An argument of another kind is the empty slot a Codec leaves for
		// one the program never reads (runTyped); the other doors checked
		// every argument's kind already.
		if !plan.scan[i] || args[i].kind != param.typ.kind {
			continue
		}
		if err := f.scanUnits(args[i], param.typ, ordered); err != nil {
			return fmt.Errorf("%w: argument %q: %w", ErrContract, param.name, err)
		}
	}
	return nil
}

// scanUnits walks a value along its declared type, binding and checking
// every currency it holds.
func (f *frame) scanUnits(value Value, typ Type, ordered bool) error {
	switch typ.kind {
	case MoneyKind:
		if value.s == "" {
			return noCurrencyIsZero(value)
		}
		return f.unitHolds(typ.name, value.s)
	case CurrencyKind:
		return f.unitHolds(typ.name, value.s)
	case FxRateKind:
		return f.scanRate(value, typ)
	case ArrayKind:
		for i := 0; i < value.length(); i++ {
			if err := f.scanUnits(value.at(i), *typ.elem, ordered); err != nil {
				return fmt.Errorf("item %d: %w", i, err)
			}
		}
	case DictKind:
		return f.scanEntries(value, *typ.elem, ordered)
	case RecordKind:
		for i, field := range typ.fields {
			if err := f.scanUnits(value.Field(i), field.typ, ordered); err != nil {
				return fmt.Errorf("field %q: %w", field.name, err)
			}
		}
	}
	return nil
}

// scanEntries scans a dictionary's entries: in the map's order, which costs
// nothing, or in key order, which sorts the keys, for a stable error.
func (f *frame) scanEntries(value Value, elem Type, ordered bool) error {
	scan := func(key string, entry Value) error {
		if err := f.scanUnits(entry, elem, ordered); err != nil {
			return fmt.Errorf("entry %q: %w", key, err)
		}
		return nil
	}
	if !ordered {
		return value.eachEntry(scan)
	}
	for _, key := range value.keys() {
		entry, _ := value.lookup(key)
		if err := scan(key, entry); err != nil {
			return err
		}
	}
	return nil
}

// unitHolds checks one currency against the unit its type names: a declared
// currency always, the code itself for a code, and for a variable the
// currency this run bound it to — binding it if this is the first.
func (f *frame) unitHolds(unit, currency string) error {
	table := f.runtime.money.table
	if table == nil {
		return fmt.Errorf("%w: %s, but this registry declares no money", ErrCurrency, currency)
	}
	if _, err := table.places(currency); err != nil {
		return err
	}
	switch {
	case IsCurrencyCode(unit):
		if unit != currency {
			return fmt.Errorf("%w: %s where %s is declared", ErrCurrency, currency, unit)
		}
	case IsUnitVariable(unit):
		return f.bindUnit(unit, currency)
	}
	return nil
}

func (f *frame) bindUnit(unit, currency string) error {
	index, found := slices.BinarySearch(f.runtime.money.units, unit)
	if !found {
		return nil
	}
	switch bound := f.units[index]; bound {
	case "":
		f.units[index] = currency
		return nil
	case currency:
		return nil
	default:
		return fmt.Errorf("%w: %s is %s here but %s elsewhere", ErrCurrency, unit, currency, bound)
	}
}

// fillUnits gives a result's currency-less zeros the currency its type
// names: the code, or what this run bound the variable to. A zero whose unit
// is unknown stays currency-less — there is nothing to call it. It reports
// whether anything changed, so a container is copied only then.
func (f *frame) fillUnits(value Value, typ Type) (Value, bool) {
	switch typ.kind {
	case MoneyKind:
		if value.s != "" {
			return value, false
		}
		value.s = f.unitCurrency(typ.name)
		return value, value.s != ""
	case ArrayKind:
		return f.fillArray(value, *typ.elem)
	case RecordKind:
		return f.fillRecord(value, typ)
	case DictKind:
		return f.fillDict(value, *typ.elem)
	default:
		return value, false
	}
}

// noCurrencyIsZero refuses an amount with no currency that is not zero: a
// host that forgot the currency must not have its amount taken for any.
func noCurrencyIsZero(value Value) error {
	if value.i != 0 {
		return fmt.Errorf("%w: %d minor units with no currency", ErrCurrency, value.i)
	}
	return nil
}

func (f *frame) fillDict(value Value, elem Type) (Value, bool) {
	if !containsUnits(elem) {
		return value, false
	}
	entries := make(map[string]Value, value.length())
	changed := false
	for _, key := range value.keys() {
		entry, _ := value.lookup(key)
		filled, entryChanged := f.fillUnits(entry, elem)
		changed = changed || entryChanged
		entries[key] = filled
	}
	if !changed {
		return value, false
	}
	return packDict(elem, entries), true
}

func (f *frame) unitCurrency(unit string) string {
	if IsCurrencyCode(unit) {
		return unit
	}
	if index, found := slices.BinarySearch(f.runtime.money.units, unit); found {
		return f.units[index]
	}
	return ""
}

func (f *frame) fillArray(value Value, elem Type) (Value, bool) {
	if !containsUnits(elem) {
		return value, false
	}
	builder := newArrayBuilder(elem, value.length())
	changed := false
	for i := 0; i < value.length(); i++ {
		filled, itemChanged := f.fillUnits(value.at(i), elem)
		changed = changed || itemChanged
		builder.add(filled)
	}
	if !changed {
		return value, false
	}
	return builder.finish(), true
}

func (f *frame) fillRecord(value Value, typ Type) (Value, bool) {
	record, ok := value.box.(*recordValue)
	if !ok || !containsUnits(typ) {
		return value, false
	}
	fields := slices.Clone(record.fields)
	changed := false
	for i, field := range typ.fields {
		var fieldChanged bool
		fields[i], fieldChanged = f.fillUnits(fields[i], field.typ)
		changed = changed || fieldChanged
	}
	if !changed {
		return value, false
	}
	return Value{kind: RecordKind, box: &recordValue{typ: record.typ, fields: fields}}, true
}

// scanRate binds and checks an exchange rate's two currencies, and holds the
// rate to the rules a host's Go value may not have kept.
func (f *frame) scanRate(value Value, typ Type) error {
	rate, _ := value.FxRate()
	if err := rate.wellFormed(); err != nil {
		return err
	}
	if err := f.unitHolds(typ.values[0], rate.base); err != nil {
		return err
	}
	return f.unitHolds(typ.values[1], rate.quote)
}
