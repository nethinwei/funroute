package machine

import (
	"fmt"

	"github.com/nethinwei/funroute/internal/money"
)

// The currencies a value holds, checked where one enters a run: an argument,
// a host function's result, an artifact's constant at load. Every currency
// must be declared, an amount with no currency must be zero, and an exchange
// rate must keep the rules. A currency is the value's own — money is one
// type — so this is the whole of it; the rules between currencies are the
// operations' to check where they meet. Arrays, dictionaries and records are
// walked, the one place the boundary reads a container's elements: a value
// carries its currency, and nothing else can say which one it is.

// moneyPlan is what a runtime precomputes about the money in its arguments.
type moneyPlan struct {
	table *money.Currencies
	// scan marks the parameters whose type holds a currency-carrying kind.
	scan []bool
	any  bool
}

func newMoneyPlan(artifact *Artifact, registry *Registry) moneyPlan {
	plan := moneyPlan{table: registry.currencies()}
	plan.scan = make([]bool, len(artifact.parts.Args))
	for i, param := range artifact.parts.Args {
		plan.scan[i] = containsUnits(param.typ)
		plan.any = plan.any || plan.scan[i]
	}
	return plan
}

func containsUnits(typ Type) bool { return typeHas(typ, IsUnitKind) }

// checkUnits checks the arguments' currencies.
func (r *Runtime) checkUnits(args []Value) error {
	if err := r.scanArgs(args, false); err != nil {
		// A dictionary is walked in the map's order, which differs from run
		// to run; the error is said the same every time, from a walk in key
		// order. Only a refused run pays for it.
		return r.scanArgs(args, true)
	}
	return nil
}

// scanArgs scans the arguments whose types hold currencies, dictionaries in
// key order when ordered.
func (r *Runtime) scanArgs(args []Value, ordered bool) error {
	for i, param := range r.artifact.parts.Args {
		// An argument of another kind is the empty slot a Codec leaves for
		// one the program never reads (runTyped); the other doors checked
		// every argument's kind already.
		if !r.money.scan[i] || args[i].kind != param.typ.kind {
			continue
		}
		if err := declaredValue(r.money.table, args[i], ordered); err != nil {
			return money.Classify(ErrContract, fmt.Sprintf("argument %q: ", param.name), err)
		}
	}
	return nil
}

// declaredValue walks a value and checks every currency it holds: declared,
// and a currency-less amount only as zero. Dictionaries are walked in the
// map's order, which costs nothing, or in key order for a stable error.
func declaredValue(table *money.Currencies, value Value, ordered bool) error {
	switch value.kind {
	case MoneyKind:
		if value.s == "" {
			return noCurrencyIsZero(value)
		}
		return declaredCode(table, value.s)
	case CurrencyKind:
		return declaredCode(table, value.s)
	case FxRateKind:
		return declaredRate(table, value)
	case ArrayKind, DictKind, RecordKind:
		if value.kind != RecordKind && !containsUnits(value.elemType()) {
			return nil
		}
		return value.eachPart(ordered, func(part Value) error { return declaredValue(table, part, ordered) })
	}
	return nil
}

func declaredCode(table *money.Currencies, currency string) error {
	if table == nil {
		return fmt.Errorf("%w: %s, but this registry declares no money", ErrCurrency, currency)
	}
	_, err := table.Places(currency)
	return err
}

// declaredRate checks an exchange rate: declared currencies, and the rules a
// host's Go value may not have kept.
func declaredRate(table *money.Currencies, value Value) error {
	rate, _ := value.FxRate()
	if err := money.WellFormedFxRate(rate); err != nil {
		return err
	}
	if table == nil {
		return fmt.Errorf("%w: %s, but this registry declares no money", ErrCurrency, rate)
	}
	_, err := money.PairOf(table, rate.Base(), rate.Quote())
	return err
}

// noCurrencyIsZero refuses an amount with no currency that is not zero: a
// host that forgot the currency must not have its amount taken for any.
func noCurrencyIsZero(value Value) error {
	if value.i != 0 {
		return fmt.Errorf("%w: %d minor units with no currency", ErrCurrency, value.i)
	}
	return nil
}
