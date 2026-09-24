package machine

import (
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
func moneyCodes(artifact *Artifact) []string {
	var codes []string
	add := func(code string) {
		if money.IsCurrencyCode(code) {
			codes = append(codes, code)
		}
	}
	for _, constant := range artifact.parts.Constants {
		constantCodes(constant, add)
	}
	slices.Sort(codes)
	return slices.Compact(codes)
}

// constantCodes adds the codes a constant names: an amount's or a
// currency's, both of an exchange rate's, and those of its items.
func constantCodes(constant Constant, add func(string)) {
	if IsMoneyKind(constant.Type) && constant.String != nil {
		add(*constant.String)
	}
	if constant.Type == FxRateKind && len(constant.Keys) > 0 {
		add(constant.Keys[0])
	}
	for _, item := range constant.Items {
		constantCodes(item, add)
	}
}

// ArtifactUsesMoney reports whether any type an artifact states — an
// argument, the result, an instruction's or a constant's — is a money type.
// Only then does it carry a MoneyStamp.
func ArtifactUsesMoney(artifact *Artifact) bool {
	types := []Type{artifact.parts.Result}
	for _, param := range artifact.parts.Args {
		types = append(types, param.typ)
	}
	for _, instruction := range artifact.parts.Instructions {
		if instruction.Type != nil {
			types = append(types, *instruction.Type)
		}
	}
	return slices.ContainsFunc(types, typeUsesMoney) || slices.ContainsFunc(artifact.parts.Constants, constantUsesMoney)
}

func typeUsesMoney(typ Type) bool {
	found := false
	_ = WalkTypes(typ, func(inner Type) error {
		found = found || IsMoneyKind(inner.kind)
		return nil
	})
	return found
}

func constantUsesMoney(constant Constant) bool {
	if IsMoneyKind(constant.Type) || (constant.Elem != nil && typeUsesMoney(*constant.Elem)) {
		return true
	}
	return slices.ContainsFunc(constant.Items, constantUsesMoney)
}

// checkMoneyStamp refuses an artifact whose money was compiled against
// another default rounding than the registry's, or against other places for
// a currency it names, or a currency the registry no longer declares.
func checkMoneyStamp(artifact *Artifact, registry *Registry) error {
	table := registry.currencies()
	stamp := artifact.parts.Money
	switch {
	case stamp == nil:
		if ArtifactUsesMoney(artifact) {
			return fmt.Errorf("artifact uses money but records no money stamp")
		}
		return nil
	case table == nil:
		return fmt.Errorf("artifact was compiled with money, and this registry declares none")
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
