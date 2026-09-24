package machine

import (
	"fmt"
)

// Money and rate arithmetic for Go. The kernel's operators are these methods,
// so a host computing a fee next to a rule gets exactly the rule's answer:
// the same currency checks, the same overflow errors, the same rounding. The
// operations that need a currency's decimal places are on Currencies.
//
// A currency-less zero — Money{} — goes with any currency, the way the
// literal 0 does in a rule. Only zero: an amount without a currency that is
// not zero is a host that forgot the currency, and is an ErrCurrency.

// meet is the currency two amounts combine in.
func (m Money) meet(other Money) (string, error) { return meet(m.currency, other.currency) }

// Like is minor units in m's currency: like(amount, n) in a rule. The
// currency-less zero has no currency to copy, so only 0 is like it.
func (m Money) Like(minor int64) (Money, error) {
	if m.currency == "" && minor != 0 {
		return Money{}, fmt.Errorf("%w: a currency-less zero has no currency to copy", ErrCurrency)
	}
	return newMoney(m.currency, minor), nil
}

// Add is m + other, in their one currency.
func (m Money) Add(other Money) (Money, error) {
	currency, err := m.meet(other)
	if err != nil {
		return Money{}, err
	}
	sum, err := addInt64(m.minor, other.minor, 1)
	return Money{currency: currency, minor: sum}, err
}

// Sub is m - other, in their one currency.
func (m Money) Sub(other Money) (Money, error) {
	currency, err := m.meet(other)
	if err != nil {
		return Money{}, err
	}
	difference, err := addInt64(m.minor, other.minor, -1)
	return Money{currency: currency, minor: difference}, err
}

// Neg is -m.
func (m Money) Neg() (Money, error) {
	negated, err := addInt64(0, m.minor, -1)
	return Money{currency: m.currency, minor: negated}, err
}

// Abs is m without its sign.
func (m Money) Abs() (Money, error) {
	if m.minor < 0 {
		return m.Neg()
	}
	return m, nil
}

// Cmp orders m against other: -1, 0 or 1. Amounts in two currencies have no
// order, and that is an ErrCurrency rather than a guess.
func (m Money) Cmp(other Money) (int, error) {
	if _, err := m.meet(other); err != nil {
		return 0, err
	}
	return compareOrdered(m.minor, other.minor), nil
}

// Sign is -1, 0 or 1.
func (m Money) Sign() int { return compareOrdered(m.minor, 0) }

// IsZero reports an amount of nothing, in any currency or none.
func (m Money) IsZero() bool { return m.minor == 0 }

// MulInt is m times a count, exactly.
func (m Money) MulInt(count int64) (Money, error) {
	minor, err := mulInt64(m.minor, count)
	return Money{currency: m.currency, minor: minor}, err
}

// Prorate is m × part / whole, exact in between and rounded once by mode:
// a fee refunded in proportion, prorate(fee, refund, paid), without the
// ratio refund / paid first rounding to a rate's ten places — which loses
// units once amounts reach the billions. whole of zero is ErrArithmetic.
func (m Money) Prorate(part, whole int64, mode Rounding) (Money, error) {
	minor, err := mulDivRound(m.minor, part, whole, mode)
	return Money{currency: m.currency, minor: minor}, err
}

// ProrateBy is Prorate with the proportion given as two amounts of one
// currency, the refund and what was paid: any currency, m's or another.
func (m Money) ProrateBy(part, whole Money, mode Rounding) (Money, error) {
	if _, err := part.meet(whole); err != nil {
		return Money{}, err
	}
	return m.Prorate(part.minor, whole.minor, mode)
}

// RoundTo is m to a whole number of steps, rounded by mode: cash rounding to
// CHF 0.05, or to whole kronor with SEK 1. The step is an amount in m's
// currency, and more than zero.
func (m Money) RoundTo(step Money, mode Rounding) (Money, error) {
	currency, err := m.meet(step)
	if err != nil {
		return Money{}, err
	}
	if step.minor <= 0 {
		return Money{}, fmt.Errorf("%w: round_to needs a step of more than zero, got %d minor units", ErrArithmetic, step.minor)
	}
	steps, err := mulDivRound(m.minor, 1, step.minor, mode)
	if err != nil {
		return Money{}, err
	}
	minor, err := mulInt64(steps, step.minor)
	return Money{currency: currency, minor: minor}, err
}

// MulRate is m times a rate, rounded once to the minor unit by mode.
func (m Money) MulRate(rate Rate, mode Rounding) (Money, error) {
	minor, err := mulDivRound(m.minor, rate.scaled, RateScale, mode)
	return Money{currency: m.currency, minor: minor}, err
}

// DivRate is m divided by a rate — the gross an amount is the net of —
// rounded once to the minor unit by mode.
func (m Money) DivRate(rate Rate, mode Rounding) (Money, error) {
	minor, err := mulDivRound(m.minor, RateScale, rate.scaled, mode)
	return Money{currency: m.currency, minor: minor}, err
}

// Ratio is m / other in one currency: the rate a fee is of an amount. Past
// ten places it rounds half to even.
func (m Money) Ratio(other Money) (Rate, error) {
	if _, err := m.meet(other); err != nil {
		return Rate{}, err
	}
	ratio, err := mulDivRound(m.minor, RateScale, other.minor, RoundHalfEven)
	return newRate(ratio), err
}

// Percent is a rate written in percent: Percent("2.9") is 2.9%.
func Percent(value string) (Rate, error) { return ParseRateIn(value, 2) }

// BasisPoints is a rate written in basis points: BasisPoints("25") is 0.25%.
func BasisPoints(value string) (Rate, error) { return ParseRateIn(value, 4) }

// Add is r + other.
func (r Rate) Add(other Rate) (Rate, error) {
	sum, err := addInt64(r.scaled, other.scaled, 1)
	return newRate(sum), err
}

// Sub is r - other.
func (r Rate) Sub(other Rate) (Rate, error) {
	difference, err := addInt64(r.scaled, other.scaled, -1)
	return newRate(difference), err
}

// Mul is r times other, a fee on a fee; past ten places it rounds half to
// even.
func (r Rate) Mul(other Rate) (Rate, error) {
	product, err := mulDivRound(r.scaled, other.scaled, RateScale, RoundHalfEven)
	return newRate(product), err
}

// Div is r divided by other, rounding half to even past ten places.
func (r Rate) Div(other Rate) (Rate, error) {
	quotient, err := mulDivRound(r.scaled, RateScale, other.scaled, RoundHalfEven)
	return newRate(quotient), err
}
