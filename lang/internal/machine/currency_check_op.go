package machine

import "fmt"

// The currency_check instruction: what the compiler emits where it could not
// prove a currency and a function that is not the kernel's needs one, and at
// the end of a program whose contract declares a currency the expression did
// not prove.

// maxCheckGroups is how many unit variables one call's checks can track.
const maxCheckGroups = 8

// The modes of a currency check, in its C operand: a group slot D that the
// first currency seen binds, the code Keys[0], or the units of Type walked.
const (
	CheckGroup = iota
	CheckCode
	CheckPattern
)

// The flags of a currency check, in its B operand: the first check before a
// call clears the group slots, and a check may read an exchange rate's quote.
const (
	CheckNewCall = 1 << iota
	CheckQuote
)

func validateCurrencyCheck(in Instruction, _ *Artifact, fail failFunc) error {
	if in.A < 0 || in.B < 0 || in.B > CheckNewCall|CheckQuote {
		return fail("malformed currency check")
	}
	switch in.C {
	case CheckGroup:
		if in.D < 0 || in.D >= maxCheckGroups {
			return fail("currency check group %d", in.D)
		}
	case CheckCode:
		if len(in.Keys) != 1 || !IsCurrencyCode(in.Keys[0]) {
			return fail("currency check needs one currency code")
		}
	case CheckPattern:
		if in.Type == nil || !in.Type.IsConcrete() {
			return fail("currency check needs a type")
		}
	default:
		return fail("currency check mode %d", in.C)
	}
	return nil
}

func (f *frame) checkCurrency(in Instruction) error {
	if in.A >= len(f.stack) {
		return fmt.Errorf("currency check below the stack")
	}
	if in.B&CheckNewCall != 0 {
		clear(f.groups[:])
	}
	value := f.stack[len(f.stack)-1-in.A]
	if in.C == CheckPattern {
		return f.checkPattern(value, *in.Type)
	}
	currency := value.s
	if in.B&CheckQuote != 0 {
		currency = value.quote()
	}
	if currency == "" && value.kind == MoneyKind {
		return nil
	}
	if in.C == CheckCode {
		return expectCurrency(in.Keys[0], currency)
	}
	switch bound := f.groups[in.D]; bound {
	case "":
		f.groups[in.D] = currency
		return nil
	default:
		return expectCurrency(bound, currency)
	}
}

func expectCurrency(want, got string) error {
	if want != got {
		return fmt.Errorf("%w: %s where %s is required", ErrCurrency, got, want)
	}
	return nil
}

// checkPattern walks a result along the contract's type: every currency must
// be the code the type names, or what this run bound the variable to.
func (f *frame) checkPattern(value Value, typ Type) error {
	switch typ.kind {
	case MoneyKind:
		if value.s == "" {
			return nil
		}
		return f.unitMatches(typ.name, value.s)
	case CurrencyKind:
		return f.unitMatches(typ.name, value.s)
	case FxRateKind:
		if err := f.unitMatches(typ.values[0], value.s); err != nil {
			return err
		}
		return f.unitMatches(typ.values[1], value.quote())
	case ArrayKind:
		for i := 0; i < value.length(); i++ {
			if err := f.checkPattern(value.at(i), *typ.elem); err != nil {
				return err
			}
		}
	case DictKind:
		return value.eachEntry(func(_ string, entry Value) error { return f.checkPattern(entry, *typ.elem) })
	case RecordKind:
		for i, field := range typ.fields {
			if err := f.checkPattern(value.Field(i), field.typ); err != nil {
				return err
			}
		}
	}
	return nil
}

// unitMatches is unitHolds without the registry check: a value the program
// computed is in a currency that entered it declared.
func (f *frame) unitMatches(unit, currency string) error {
	switch {
	case IsCurrencyCode(unit):
		return expectCurrency(unit, currency)
	case IsUnitVariable(unit):
		return f.bindUnit(unit, currency)
	default:
		return nil
	}
}

// declaredResult refuses a host function's result holding a currency the
// registry did not declare, or an amount with no currency that is not zero,
// at any depth. The kernel's results are its operands' currencies and need
// no look.
func (f *frame) declaredResult(function *RegisteredFunction, value Value) error {
	if function.IsBuiltin() || f.runtime.money.table == nil {
		return nil
	}
	return f.declaredUnits(value)
}

func (f *frame) declaredUnits(value Value) error {
	return declaredValue(f.runtime.money.table, value)
}

// declaredValue refuses a value holding a currency table does not declare,
// or an amount with no currency that is not zero, at any depth: a host
// function's result, and at load an artifact's constants.
func declaredValue(table *currencyTable, value Value) error {
	switch value.kind {
	case MoneyKind:
		if value.s == "" {
			return noCurrencyIsZero(value)
		}
		_, err := table.places(value.s)
		return err
	case CurrencyKind:
		_, err := table.places(value.s)
		return err
	case FxRateKind:
		return declaredRate(table, value)
	case ArrayKind, DictKind, RecordKind:
		return declaredItems(table, value)
	default:
		return nil
	}
}

// declaredItems looks at the items of a container or the fields of a record
// that can hold a currency.
func declaredItems(table *currencyTable, value Value) error {
	if value.kind == RecordKind {
		record, ok := value.box.(*recordValue)
		if !ok {
			return nil
		}
		for _, field := range record.fields {
			if err := declaredValue(table, field); err != nil {
				return err
			}
		}
		return nil
	}
	if !containsUnits(value.elemType()) {
		return nil
	}
	if value.kind == DictKind {
		return value.eachEntry(func(_ string, entry Value) error { return declaredValue(table, entry) })
	}
	for i := range value.length() {
		if err := declaredValue(table, value.at(i)); err != nil {
			return err
		}
	}
	return nil
}

// declaredRate checks an exchange rate a host function returned: declared
// currencies and the rules.
func declaredRate(table *currencyTable, value Value) error {
	rate, _ := value.FxRate()
	if err := rate.wellFormed(); err != nil {
		return err
	}
	return (&Currencies{table: table}).pair(rate.base, rate.quote)
}
