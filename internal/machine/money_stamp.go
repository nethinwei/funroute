package machine

import (
	"errors"
	"fmt"
	"slices"

	"github.com/nethinwei/funroute/internal/money"
)

// MoneyStamp is what an artifact records about the money it was compiled
// against: the places of each currency the compiled program names in a
// literal or a constant, which its minor units depend on. A currency the program only meets at run time is the
// registry's to know then, and a code folding took
// out of the program left only its result behind, so a registry that
// declares another currency, or changes one the program does not name,
// still loads it.
type MoneyStamp struct {
	Currencies []money.CurrencySpec `json:"currencies,omitempty"`
}

// stampFor is the stamp of a program naming codes, under this table.
func stampFor(t *money.Currencies, codes []string) MoneyStamp {
	var stamp MoneyStamp
	for _, code := range codes {
		if digits, err := t.Places(code); err == nil {
			stamp.Currencies = append(stamp.Currencies, money.CurrencySpec{Code: code, Digits: digits})
		}
	}
	return stamp
}

// moneyCodes is every currency code an artifact names, sorted: the ones its
// constants state. A code folding took out of the
// program, as minor(USD 1.70) folds to 170, is no fact the run depends on:
// what was computed from it is a constant now, whatever the table says.
func moneyCodes(artifact *Artifact) ([]string, error) {
	var codes []string
	add := func(code string) {
		if money.IsCurrencyCode(code) {
			codes = append(codes, code)
		}
	}
	for i, constant := range artifact.parts.Constants {
		value, err := constant.value()
		if err == nil {
			err = valueCodes(value, add)
		}
		if err != nil {
			return nil, fmt.Errorf("constant %d: %w", i, err)
		}
	}
	slices.Sort(codes)
	return slices.Compact(codes), nil
}

// valueCodes adds the codes a value names: an amount's or a currency's,
// both of an exchange rate's, and those of its parts.
func valueCodes(value Value, add func(string)) error {
	switch value.kind {
	case MoneyKind, CurrencyKind:
		add(value.s)
	case FxRateKind:
		add(value.s)
		add(value.quote())
	}
	return value.eachPart(false, func(part Value) error { return valueCodes(part, add) })
}

// ArtifactUsesMoney reports whether any type an artifact states — an
// argument, the result, an instruction's or a constant's — is a money type.
// Only then does it carry a MoneyStamp.
func ArtifactUsesMoney(artifact *Artifact) bool {
	parts := &artifact.parts
	return typeUsesMoney(parts.Result) ||
		slices.ContainsFunc(parts.Args, func(param Parameter) bool { return typeUsesMoney(param.typ) }) ||
		slices.ContainsFunc(parts.Instructions, func(in Instruction) bool { return in.Type != nil && typeUsesMoney(*in.Type) }) ||
		slices.ContainsFunc(parts.Constants, func(constant Constant) bool { return typeUsesMoney(constant.Type) })
}

func typeUsesMoney(typ Type) bool { return typeHas(typ, IsMoneyKind) }

// checkMoneyStamp refuses an artifact whose money was compiled against
// another default rounding than the registry's, or against other places for
// a currency it names, or a currency the registry no longer declares.
func checkMoneyStamp(artifact *Artifact, registry *Registry) error {
	table := registry.currencies()
	stamp := artifact.parts.Money
	switch {
	case stamp == nil:
		if ArtifactUsesMoney(artifact) {
			return errors.New("artifact uses money but records no money stamp")
		}
		return nil
	case table == nil:
		return errors.New("artifact was compiled with money, and this registry declares none")
	}
	for _, currency := range stamp.Currencies {
		digits, err := table.Places(currency.Code)
		switch {
		case err != nil:
			return fmt.Errorf("money changed: the artifact names %s, which this registry does not declare", currency.Code)
		case digits != currency.Digits:
			return fmt.Errorf("money changed: %s has %d decimal places in the artifact and %d in this registry", currency.Code, currency.Digits, digits)
		}
	}
	return nil
}
