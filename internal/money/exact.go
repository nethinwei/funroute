package money

import (
	"encoding/json"
	"errors"
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
	places   uint8 // as Money's: decimal places + 1, 0 not known
}

// Exact is m as exact money, to compute with before rounding once.
func (m Money) Exact() ExactMoney {
	if m.minor == 0 {
		return ExactMoney{currency: m.currency, places: m.places}
	}
	return ExactMoney{currency: m.currency, minor: Ratio{num: m.minor, den: 1}, places: m.places}
}

// unit is e's currency and places as an amount of nothing.
func (e ExactMoney) unit() Money { return Money{currency: e.currency, places: e.places} }

// of is minor units in e's currency and places.
func (e ExactMoney) of(minor Ratio) ExactMoney {
	e.minor = minor
	return e
}

// Digits is how many decimal places the amount's currency has, as Money's.
func (e ExactMoney) Digits() (digits int, ok bool) { return e.unit().Digits() }

// Currency is the amount's currency, "" for the currency-less zero.
func (e ExactMoney) Currency() string { return e.currency }

// Minor is the amount in minor units, exactly.
func (e ExactMoney) Minor() Ratio { return e.minor }

// String is the currency and the minor units: "USD 17/2 minor units".
func (e ExactMoney) String() string {
	return fmt.Sprintf("%s %s minor units", e.currency, e.minor)
}

func (e ExactMoney) MarshalJSON() ([]byte, error) {
	var digits *int
	if d, ok := e.Digits(); ok {
		digits = &d
	}
	return json.Marshal(struct {
		Currency string `json:"currency"`
		Minor    string `json:"minor"`
		Digits   *int   `json:"digits,omitempty"`
	}{e.currency, e.minor.String(), digits})
}

// UnmarshalJSON reads the shape MarshalJSON writes: the minor units as a
// ratio's text, "17/2", and a currency, which only a zero may go without.
func (e *ExactMoney) UnmarshalJSON(data []byte) error {
	var shape struct {
		Currency *string `json:"currency"`
		Minor    *string `json:"minor"`
		Digits   *int    `json:"digits"`
	}
	if err := json.Unmarshal(data, &shape); err != nil {
		return err
	}
	if shape.Currency == nil || shape.Minor == nil {
		return errors.New("exact money needs a \"currency\" and a \"minor\"")
	}
	minor, err := ParseRatio(*shape.Minor)
	if err != nil {
		return err
	}
	unit, err := fromJSON(*shape.Currency, int64(minor.Sign()), shape.Digits)
	if err != nil {
		return err
	}
	*e = unit.Exact().of(minor)
	return nil
}

// Add is e + other, in their one currency.
func (e ExactMoney) Add(other ExactMoney) (ExactMoney, error) { return e.plus(other, 1) }

// Sub is e - other, in their one currency.
func (e ExactMoney) Sub(other ExactMoney) (ExactMoney, error) { return e.plus(other, -1) }

func (e ExactMoney) plus(other ExactMoney, sign int64) (ExactMoney, error) {
	unit, err := e.unit().meet(other.unit())
	if err != nil {
		return ExactMoney{}, err
	}
	sum, err := e.minor.plus(other.minor, sign)
	return unit.Exact().of(sum), err
}

// Neg is -e.
func (e ExactMoney) Neg() (ExactMoney, error) { return e.of(Ratio{}).Sub(e) }

// MulInt is e times a count.
func (e ExactMoney) MulInt(count int64) (ExactMoney, error) {
	return e.MulRatio(Ratio{num: count, den: 1})
}

// MulRatio is e times a ratio, exactly.
func (e ExactMoney) MulRatio(r Ratio) (ExactMoney, error) {
	product, err := e.minor.Mul(r)
	return e.of(product), err
}

// MulPart is e times part/whole, exactly: prorate's step. The proportion is
// never a ratio of its own, which might not fit where the product does.
func (e ExactMoney) MulPart(part, whole int64) (ExactMoney, error) {
	if whole == 0 {
		return ExactMoney{}, errDivisionByZero
	}
	p, w := magnitude(part), magnitude(whole)
	g := gcd(p, w)
	a, b := e.minor.parts()
	product, err := fraction((a < 0) != ((part < 0) != (whole < 0)), []uint64{magnitude(a), p / g}, []uint64{uint64(b), w / g})
	return e.of(product), err
}

// ProrateBy is e times part/whole, two amounts of one currency, exactly: as
// Money.ProrateBy, rounded nowhere.
func (e ExactMoney) ProrateBy(part, whole Money) (ExactMoney, error) {
	if _, err := part.meet(whole); err != nil {
		return ExactMoney{}, err
	}
	return e.MulPart(part.minor, whole.minor)
}

// DivRatio is e divided by a ratio, exactly; by zero it is ErrArithmetic.
func (e ExactMoney) DivRatio(r Ratio) (ExactMoney, error) {
	quotient, err := e.minor.Div(r)
	return e.of(quotient), err
}

// Cmp orders two amounts of one currency.
func (e ExactMoney) Cmp(other ExactMoney) (int, error) {
	if _, err := e.unit().meet(other.unit()); err != nil {
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
	return e.unit().of(minor), err
}

// ConvertExact is m, in fx's base currency, in its quote currency, exactly:
// Convert without the rounding, for a conversion inside round(…).
func (c *Currencies) ConvertExact(m ExactMoney, fx FxRate) (ExactMoney, error) {
	if err := c.declared(fx); err != nil {
		return ExactMoney{}, err
	}
	return c.convertExactAt(m, fx)
}

// convertExactAt is m converted at fx, whose currencies are declared.
func (c *Currencies) convertExactAt(m ExactMoney, fx FxRate) (ExactMoney, error) {
	if err := c.inPlaces(m.unit()); err != nil {
		return ExactMoney{}, err
	}
	up, down, err := c.rescaling(m.currency, fx)
	if err != nil {
		return ExactMoney{}, err
	}
	a, b := m.minor.parts()
	num, den := fx.rate.parts()
	nums, dens := [3]uint64{magnitude(a), uint64(num), up}, [3]uint64{uint64(b), uint64(den), down}
	converted, err := fraction(a < 0, nums[:], dens[:])
	return c.unitOf(fx.pair.Quote).Exact().of(converted), err
}
