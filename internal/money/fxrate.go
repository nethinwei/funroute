package money

import (
	"cmp"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/nethinwei/funroute/internal/kit"
)

// FxRate is an exchange rate, the Go form of fxrate: one unit of the base
// currency buys the rate's worth of the quote currency, exactly — a quote
// read from its decimal text, a rate implied by two amounts, or a chain of
// them multiplied out. A host gets one from a currency table
// (Currencies.FxRate, Implied), passes it to a rule — alone or as a
// []FxRate, which crosses without a copy — and gets one back; the zero value
// is no rate. A rule marks a rate up or down (MulRatio) and compares two of
// one pair, and otherwise hands rates to using and converts with ->: no rate
// is multiplied by an amount.
type FxRate struct {
	// pair is shared by every rate of one pair a table makes, so a rate
	// becomes a value without an allocation.
	pair *Pair
	rate Ratio // positive; exactly 1 from a currency to itself
}

// Pair is a base and a quote currency, shared by the rates of the pair.
type Pair struct{ Base, Quote string }

// Base is the currency one unit of which the rate prices.
func (f FxRate) Base() string {
	if f.pair == nil {
		return ""
	}
	return f.pair.Base
}

// Quote is the currency the rate prices it in.
func (f FxRate) Quote() string {
	if f.pair == nil {
		return ""
	}
	return f.pair.Quote
}

// String is the rate as a rule writes it, "150.25 JPY / USD", exactly: a
// rate with no finite decimal, such as a third, is written as its fraction.
func (f FxRate) String() string {
	if f.pair == nil {
		return "no rate"
	}
	return f.rate.String() + " " + f.pair.Quote + " / " + f.pair.Base
}

// Decimal is the rate rounded half to even to places decimal places, at
// most 18, for showing it: the rate itself stays exact.
func (f FxRate) Decimal(places int) (string, error) {
	if err := f.checked(); err != nil {
		return "", err
	}
	places = max(places, 0)
	if places > 18 {
		return "", fmt.Errorf("%w: %d decimal places, at most 18 fit", ErrArithmetic, places)
	}
	scaled, err := mulDivRound(f.rate.num, pow10(places), f.rate.den, RoundHalfEven)
	if err != nil {
		return "", err
	}
	return FormatDecimal(scaled, places, false), nil
}

func (f FxRate) checked() error {
	if f.pair == nil {
		return fmt.Errorf("%w: the zero FxRate is no exchange rate; make one with a currency table", ErrCurrency)
	}
	return nil
}

// Every exchange rate keeps two rules: it is positive, and from a currency
// to itself it is exactly 1, the identity. validRate says which one a rate
// breaks.
func validRate(base, quote string, r Ratio) error {
	switch {
	case r.Sign() <= 0:
		return fmt.Errorf("%w: exchange rate %s %s / %s is not positive", ErrArithmetic, r, quote, base)
	case base == quote && r != ratioOne:
		return fmt.Errorf("%w: exchange rate %s %s / %s: from a currency to itself it is 1", ErrArithmetic, r, quote, base)
	}
	return nil
}

var ratioOne = Ratio{num: 1, den: 1}

// newFxRate is the one way an exchange rate is made: it keeps the rules.
func newFxRate(pair *Pair, r Ratio) (FxRate, error) {
	if err := validRate(pair.Base, pair.Quote, r); err != nil {
		return FxRate{}, err
	}
	return FxRate{pair: pair, rate: r}, nil
}

// wellFormed checks a rate that did not come from a table: two codes and the
// rules.
func (f FxRate) wellFormed() error {
	if f.pair == nil || !IsCurrencyCode(f.pair.Base) || !IsCurrencyCode(f.pair.Quote) {
		return fmt.Errorf("%w: an exchange rate needs a \"base\" and a \"quote\" code and a rate", ErrCurrency)
	}
	return validRate(f.pair.Base, f.pair.Quote, f.rate)
}

// Inverse is the rate the other way round.
func (f FxRate) Inverse() (FxRate, error) {
	if err := f.checked(); err != nil {
		return FxRate{}, err
	}
	inverse, err := f.rate.inverse()
	if err != nil {
		return FxRate{}, err
	}
	return newFxRate(&Pair{Base: f.pair.Quote, Quote: f.pair.Base}, inverse)
}

// Chain is f and then next: base to quote, then quote onward. The chain from
// a currency back to itself is no exchange rate.
func (f FxRate) Chain(next FxRate) (FxRate, error) {
	if f.pair == nil || next.pair == nil {
		return FxRate{}, cmp.Or(f.checked(), next.checked())
	}
	if f.pair.Quote != next.pair.Base {
		return FxRate{}, fmt.Errorf("%w: %s→%s cannot follow %s→%s", ErrCurrency, next.pair.Base, next.pair.Quote, f.pair.Base, f.pair.Quote)
	}
	pair := &Pair{Base: f.pair.Base, Quote: next.pair.Quote}
	if pair.Base == pair.Quote {
		// Back where it started: a currency to itself is 1, whatever the
		// spreads on the way cost.
		return FxRate{pair: pair, rate: ratioOne}, nil
	}
	product, err := f.rate.Mul(next.rate)
	if err != nil {
		return FxRate{}, err
	}
	return newFxRate(pair, product)
}

// MulRatio is the rate with a markup or a discount: f × r, exactly, still the
// same pair — 150 JPY / USD times 101% is 151.5 JPY / USD. Converting at it
// rounds once, where converting and then taking the rate of the result
// rounds twice. What comes out keeps the rules: a rate of zero or less, or
// one from a currency to itself that is not 1, is ErrArithmetic.
func (f FxRate) MulRatio(r Ratio) (FxRate, error) {
	if err := f.checked(); err != nil {
		return FxRate{}, err
	}
	product, err := f.rate.Mul(r)
	if err != nil {
		return FxRate{}, err
	}
	return newFxRate(f.pair, product)
}

// Cmp orders two rates of one pair: which buys more of the quote currency.
func (f FxRate) Cmp(other FxRate) (int, error) {
	if f.pair == nil || other.pair == nil {
		return 0, cmp.Or(f.checked(), other.checked())
	}
	if !f.samePair(other) {
		return 0, fmt.Errorf("%w: %s→%s and %s→%s", ErrCurrency, f.pair.Base, f.pair.Quote, other.pair.Base, other.pair.Quote)
	}
	return f.rate.Cmp(other.rate), nil
}

func (f FxRate) samePair(other FxRate) bool {
	return f.pair == other.pair || *f.pair == *other.pair
}

func (f FxRate) MarshalJSON() ([]byte, error) {
	if err := f.checked(); err != nil {
		return nil, err
	}
	return json.Marshal(fxRateJSON{f.pair.Base, f.pair.Quote, f.rate.String()})
}

type fxRateJSON struct {
	Base  string `json:"base"`
	Quote string `json:"quote"`
	Rate  string `json:"rate"`
}

// UnmarshalJSON reads the shape MarshalJSON writes. Whether the currencies
// are declared is the table's to say, where the rate is used.
func (f *FxRate) UnmarshalJSON(data []byte) error {
	var shape fxRateJSON
	if err := json.Unmarshal(data, &shape); err != nil {
		return err
	}
	fx, err := readFxRate(shape.Base, shape.Quote, shape.Rate)
	if err != nil {
		return err
	}
	*f = fx
	return nil
}

// readFxRate is a rate from its parts as text: two codes and the rate, a
// decimal or a fraction.
func readFxRate(base, quote, text string) (FxRate, error) {
	if !IsCurrencyCode(base) || !IsCurrencyCode(quote) {
		return FxRate{}, fmt.Errorf("%w: an exchange rate needs a \"base\" and a \"quote\" currency code", ErrCurrency)
	}
	r, err := parseRatio(text)
	if err != nil {
		return FxRate{}, kit.Classify(ErrArithmetic, fmt.Sprintf("exchange rate %q ", text), err)
	}
	return newFxRate(&Pair{Base: base, Quote: quote}, r)
}

// FxRate is an exchange rate between two declared currencies, read exactly
// from its decimal text: FxRate("USD", "JPY", "150.25"). From a currency to
// itself it must be 1.
func (c *Currencies) FxRate(base, quote, rate string) (FxRate, error) {
	pair, err := c.pairOf(base, quote)
	if err != nil {
		return FxRate{}, err
	}
	if strings.Contains(rate, "/") || strings.HasPrefix(rate, "-") || strings.HasPrefix(rate, "+") {
		return FxRate{}, fmt.Errorf("%w: %s→%s: exchange rate %q is not a plain decimal", ErrArithmetic, base, quote, rate)
	}
	parsed, err := parseRatio(rate)
	if err != nil {
		return FxRate{}, kit.Classify(ErrArithmetic, fmt.Sprintf("%s→%s: exchange rate %q ", base, quote, rate), err)
	}
	return newFxRate(pair, parsed)
}

// Implied is the rate two amounts of one exchange imply: over was paid for
// under, so one unit of under's currency bought this much of over's —
// Implied(JPY 15000, USD 100) is 150 JPY / USD. It is implied(over, under)
// in a rule. The amounts must be in two different declared currencies — two
// amounts in one currency have a ratio, over / under, not an exchange rate —
// not zero, and of one sign.
func (c *Currencies) Implied(over, under Money) (FxRate, error) {
	pair, err := c.pairOf(under.currency, over.currency)
	if err != nil {
		return FxRate{}, err
	}
	if over.currency == under.currency {
		return FxRate{}, fmt.Errorf("%w: %s and %s imply no exchange rate: two amounts in one currency have a ratio, over / under", ErrCurrency, over.currency, under.currency)
	}
	if over.minor == 0 || under.minor == 0 || (over.minor < 0) != (under.minor < 0) {
		return FxRate{}, fmt.Errorf("%w: %s %d and %s %d minor units imply no exchange rate: the amounts must be non-zero and of one sign",
			ErrArithmetic, over.currency, over.minor, under.currency, under.minor)
	}
	ratio, err := ratioOf(over.minor, under.minor)
	if err != nil {
		return FxRate{}, err
	}
	// Minor units to major ones: over / 10^over's places, under / 10^under's,
	// which is the factor from over's minor units to under's.
	r, err := c.factor(over.currency, under.currency, ratio)
	if err != nil {
		return FxRate{}, err
	}
	return newFxRate(pair, r)
}

// Convert is m, in fx's base currency, in its quote currency: rescaled
// between their places and rounded once by mode. The currency-less zero is
// zero in the quote currency.
func (c *Currencies) Convert(m Money, fx FxRate, mode Rounding) (Money, error) {
	if err := c.declared(fx); err != nil {
		return Money{}, err
	}
	return c.convertAt(m, fx, mode)
}

// declared refuses the zero FxRate and a rate between currencies the table
// does not declare.
func (c *Currencies) declared(fx FxRate) error {
	if err := fx.checked(); err != nil {
		return err
	}
	_, err := c.pairOf(fx.pair.Base, fx.pair.Quote)
	return err
}

// convertAt is m converted at fx, whose currencies are declared.
func (c *Currencies) convertAt(m Money, fx FxRate, mode Rounding) (Money, error) {
	if err := m.wellFormed(); err != nil {
		return Money{}, err
	}
	factor, err := c.rateFactor(m.currency, fx)
	if err != nil {
		return Money{}, err
	}
	minor, err := factor.times(m.minor, mode)
	return Money{currency: fx.pair.Quote, minor: minor}, err
}

// rateFactor is the factor fx converts money in currency by: currency is
// fx's base, or none for the currency-less zero.
func (c *Currencies) rateFactor(currency string, fx FxRate) (Ratio, error) {
	if currency != "" && currency != fx.pair.Base {
		return Ratio{}, fmt.Errorf("%w: %s converted at %s→%s", ErrCurrency, currency, fx.pair.Base, fx.pair.Quote)
	}
	return c.factor(fx.pair.Base, fx.pair.Quote, fx.rate)
}

// factor is what minor units of from are multiplied by to be minor units of
// to at rate: the rate rescaled between the two currencies' places.
func (c *Currencies) factor(from, to string, r Ratio) (Ratio, error) {
	fromDigits, _ := c.Places(from)
	toDigits, _ := c.Places(to)
	return r.scaleTen(toDigits - fromDigits)
}

// pairOf is the table's one fxPair for two declared currencies, made the
// first time it is asked for: a rate of a pair the table knows becomes a
// value without an allocation.
func (c *Currencies) pairOf(base, quote string) (*Pair, error) {
	if _, err := c.Places(base); err != nil {
		return nil, err
	}
	if _, err := c.Places(quote); err != nil {
		return nil, err
	}
	key := [2]string{base, quote}
	c.pairsMu.RLock()
	pair, ok := c.pairs[key]
	c.pairsMu.RUnlock()
	if ok {
		return pair, nil
	}
	c.pairsMu.Lock()
	defer c.pairsMu.Unlock()
	if pair, ok = c.pairs[key]; !ok {
		pair = &Pair{Base: base, Quote: quote}
		c.pairs[key] = pair
	}
	return pair, nil
}

func abs(n int) int { return max(n, -n) }
