package money

import (
	"cmp"
	"fmt"
)

// Money and ratio arithmetic for Go. The kernel's operators are these methods,
// so a host computing a fee next to a rule gets exactly the rule's answer:
// the same currency checks, the same overflow errors, the same rounding. The
// operations that need a currency's decimal places are on Currencies.
//
// A currency-less zero — Money{} — goes with any currency, the way the
// literal 0 does in a rule. Only zero: an amount without a currency that is
// not zero is a host that forgot the currency, and is an ErrCurrency.

// meet is the currency two amounts combine in.
func (m Money) meet(other Money) (string, error) { return meet(m.currency, other.currency) }

// Add is m + other, in their one currency.
func (m Money) Add(other Money) (Money, error) { return m.plus(other, 1) }

// Sub is m - other, in their one currency.
func (m Money) Sub(other Money) (Money, error) { return m.plus(other, -1) }

func (m Money) plus(other Money, sign int64) (Money, error) {
	currency, err := m.meet(other)
	if err != nil {
		return Money{}, err
	}
	sum, err := addInt64(m.minor, other.minor, sign)
	return Money{currency: currency, minor: sum}, err
}

// Neg is -m.
func (m Money) Neg() (Money, error) { return Money{currency: m.currency}.Sub(m) }

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
	return cmp.Compare(m.minor, other.minor), nil
}

// Sign is -1, 0 or 1.
func (m Money) Sign() int { return cmp.Compare(m.minor, 0) }

// IsZero reports an amount of nothing, in any currency or none.
func (m Money) IsZero() bool { return m.minor == 0 }

// MulInt is m times a count, exactly.
func (m Money) MulInt(count int64) (Money, error) {
	minor, err := mulInt64(m.minor, count)
	return Money{currency: m.currency, minor: minor}, err
}

// Prorate is m × part / whole, exact in between and rounded once by mode:
// a fee refunded in proportion, prorate(fee, refund, paid). whole of zero
// is ErrArithmetic.
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

// MulRatio is m times a ratio, rounded once to the minor unit by mode.
func (m Money) MulRatio(r Ratio, mode Rounding) (Money, error) {
	minor, err := r.times(m.minor, mode)
	return Money{currency: m.currency, minor: minor}, err
}

// DivRatio is m divided by a ratio — the gross an amount is the net of —
// rounded once to the minor unit by mode.
func (m Money) DivRatio(r Ratio, mode Rounding) (Money, error) {
	inverse, err := r.inverse()
	if err != nil {
		return Money{}, err
	}
	minor, err := inverse.times(m.minor, mode)
	return Money{currency: m.currency, minor: minor}, err
}

// Ratio is m / other in one currency: the ratio a fee is of an amount,
// exactly.
func (m Money) Ratio(other Money) (Ratio, error) {
	if _, err := m.meet(other); err != nil {
		return Ratio{}, err
	}
	if other.minor == 0 {
		return Ratio{}, errDivisionByZero
	}
	return ratioOf(m.minor, other.minor)
}

// meet is the currency two amounts combine in: theirs when they agree, the
// other's when one is the currency-less zero.
func meet(a, b string) (string, error) {
	switch {
	case a == b || b == "":
		return a, nil
	case a == "":
		return b, nil
	default:
		return "", fmt.Errorf("%w: %s and %s", ErrCurrency, a, b)
	}
}

// addInt64 is a + sign·b, refusing to wrap.
func addInt64(a, b, sign int64) (int64, error) {
	sum, fits := add64(a, b, sign)
	if !fits {
		return 0, errFixedOverflow
	}
	return sum, nil
}

// mulInt64 is a·b, refusing to wrap.
func mulInt64(a, b int64) (int64, error) {
	return mulDivRound(a, b, 1, RoundDown)
}
