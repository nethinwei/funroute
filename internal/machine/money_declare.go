package machine

import (
	"errors"
	"fmt"

	"github.com/nethinwei/funroute/internal/money"
)

// DeclareMoney turns the money feature on for a registry: the currencies it
// accepts and their decimal places. Until it is called a registry admits no
// money type at all, and everything it compiles is what it was before the
// feature existed — digests included. It can be called once, before any
// function that takes or returns money is registered.
func (r *Registry) DeclareMoney(spec money.MoneySpec) error {
	table, err := money.NewCurrencies(spec)
	if err != nil {
		return err
	}
	r.mu.Lock()
	if r.money != nil {
		r.mu.Unlock()
		return errors.New("money is already declared")
	}
	r.money = table
	r.mu.Unlock()
	return registerMoneyKernel(r, table)
}

// Money reports the declared money feature, if any.
func (r *Registry) Money() (money.MoneySpec, bool) {
	table := r.currencies()
	if table == nil {
		return money.MoneySpec{}, false
	}
	return table.Spec(), true
}

// Currencies is the table the registry declared, if it declared one.
func (r *Registry) Currencies() (*money.Currencies, bool) {
	table := r.currencies()
	return table, table != nil
}

func (r *Registry) currencies() *money.Currencies {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.money
}

// RoundingEnum is the enum @half_up is resolved in: an enum that comes from
// the registry rather than the contract, so a contract may not reuse its
// name. A currency is no enum; it is written as its code.
const RoundingEnum = "rounding"

// RoundingEnumType is the enum a rounding mode is written in.
func RoundingEnumType() Type { return EnumOf(RoundingEnum, money.RoundingModes()...) }

// AllocationEnum is the enum @last is resolved in, the second enum that comes
// from the registry: where allocate hands out the units rounding leaves.
const AllocationEnum = "allocation"

// AllocationEnumType is the enum an allocation strategy is written in.
func AllocationEnumType() Type { return EnumOf(AllocationEnum, money.AllocationStrategies()...) }

// ParseMoneyAmount reads an amount written in a currency's major unit — the
// "1.70" of USD 1.70 — into a money value, exactly.
func ParseMoneyAmount(r *Registry, code, amount string) (Value, error) {
	table := r.currencies()
	if table == nil {
		return Value{}, errors.New("money is not declared for this registry")
	}
	m, err := table.Of(code, amount)
	return moneyValueOf(m), err
}

// FxRateLiteral is the exchange rate 150 JPY / USD stands for in a rule: one
// base buys figure of quote, read exactly. The currencies must be declared,
// and from one to itself the figure is 1.
func FxRateLiteral(r *Registry, figure, quote, base string) (Value, error) {
	table := r.currencies()
	if table == nil {
		return Value{}, fmt.Errorf("%w: this registry declares no money", ErrCurrency)
	}
	fx, err := table.FxRate(base, quote, figure)
	if err != nil {
		return Value{}, err
	}
	return FxRateValue(fx), nil
}
