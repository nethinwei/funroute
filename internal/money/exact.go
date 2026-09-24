package money

import (
	"encoding/json"
	"fmt"
)

// ExactMoney is money between minor units: what a step inside round(…)
// computes before the one rounding at its end — amount × 2.9% × 50%, or a
// conversion — held exactly as a ratio of minor units. Round makes Money of
// it. A host gets one from Money.Exact and computes with the methods a rule
// computes with, so the two get one answer; the zero value is the
// currency-less zero. Like Ratio it is two int64, reduced: a result that does
// not fit is ErrArithmetic, never a rounding.
type ExactMoney struct {
	currency string
	minor    Ratio
}

// Exact is m as exact money, to compute with before rounding once.
func (m Money) Exact() ExactMoney {
	if m.minor == 0 {
		return ExactMoney{currency: m.currency}
	}
	return ExactMoney{currency: m.currency, minor: Ratio{num: m.minor, den: 1}}
}

// Currency is the amount's currency, "" for the currency-less zero.
func (e ExactMoney) Currency() string { return e.currency }

// Minor is the amount in minor units, exactly.
func (e ExactMoney) Minor() Ratio { return e.minor }

// String is the currency and the minor units: "USD 17/2 minor units".
func (e ExactMoney) String() string {
	return fmt.Sprintf("%s %s minor units", e.currency, e.minor)
}

func (e ExactMoney) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Currency string `json:"currency"`
		Minor    string `json:"minor"`
	}{e.currency, e.minor.String()})
}

// Add is e + other, in their one currency.
func (e ExactMoney) Add(other ExactMoney) (ExactMoney, error) { return e.plus(other, 1) }

// Sub is e - other, in their one currency.
func (e ExactMoney) Sub(other ExactMoney) (ExactMoney, error) { return e.plus(other, -1) }

func (e ExactMoney) plus(other ExactMoney, sign int64) (ExactMoney, error) {
	currency, err := meet(e.currency, other.currency)
	if err != nil {
		return ExactMoney{}, err
	}
	sum, err := e.minor.plus(other.minor, sign)
	return ExactMoney{currency: currency, minor: sum}, err
}

// Neg is -e.
func (e ExactMoney) Neg() (ExactMoney, error) { return ExactMoney{currency: e.currency}.Sub(e) }

// MulInt is e times a count.
func (e ExactMoney) MulInt(count int64) (ExactMoney, error) {
	return e.MulRatio(Ratio{num: count, den: 1})
}

// MulRatio is e times a ratio, exactly.
func (e ExactMoney) MulRatio(r Ratio) (ExactMoney, error) {
	product, err := e.minor.Mul(r)
	return ExactMoney{currency: e.currency, minor: product}, err
}

// DivRatio is e divided by a ratio, exactly; by zero it is ErrArithmetic.
func (e ExactMoney) DivRatio(r Ratio) (ExactMoney, error) {
	quotient, err := e.minor.Div(r)
	return ExactMoney{currency: e.currency, minor: quotient}, err
}

// Cmp orders two amounts of one currency.
func (e ExactMoney) Cmp(other ExactMoney) (int, error) {
	if _, err := meet(e.currency, other.currency); err != nil {
		return 0, err
	}
	return e.minor.Cmp(other.minor), nil
}

// Sign is -1, 0 or 1.
func (e ExactMoney) Sign() int { return e.minor.Sign() }

// Round is e in whole minor units, rounded once by mode.
func (e ExactMoney) Round(mode Rounding) (Money, error) {
	num, den := e.minor.parts()
	minor, err := mulDivRound(num, 1, den, mode)
	return Money{currency: e.currency, minor: minor}, err
}

// ConvertExact is m, in fx's base currency, in its quote currency, exactly:
// Convert without the rounding, for a conversion inside round(…).
func (c *Currencies) ConvertExact(m ExactMoney, fx FxRate) (ExactMoney, error) {
	if err := fx.checked(); err != nil {
		return ExactMoney{}, err
	}
	if _, err := c.pairOf(fx.pair.Base, fx.pair.Quote); err != nil {
		return ExactMoney{}, err
	}
	return c.convertExactAt(m, fx)
}

// convertExactAt is m converted at fx, whose currencies are declared.
func (c *Currencies) convertExactAt(m ExactMoney, fx FxRate) (ExactMoney, error) {
	if m.currency != "" && m.currency != fx.pair.Base {
		return ExactMoney{}, fmt.Errorf("%w: %s converted at %s→%s", ErrCurrency, m.currency, fx.pair.Base, fx.pair.Quote)
	}
	factor, err := c.factor(fx.pair.Base, fx.pair.Quote, fx.rate)
	if err != nil {
		return ExactMoney{}, err
	}
	converted, err := m.minor.Mul(factor)
	return ExactMoney{currency: fx.pair.Quote, minor: converted}, err
}
