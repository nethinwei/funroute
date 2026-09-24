package machine

import (
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
)

// FxRate is an exchange rate, the Go form of fxrate: one unit of the base
// currency buys the rate's worth of the quote currency, exactly — a quote
// read from its decimal text, a rate implied by two amounts, or a chain of
// them multiplied out. A host gets one from a currency table
// (Currencies.FxRate, Implied) or a rate table (Rates.Rate), passes it to a
// rule and gets one back; the zero value is no rate. A rule marks a rate up
// or down (MulRate) and compares two of one pair, and otherwise hands rates
// to using and converts with ->: no rate is multiplied by an amount.
type FxRate struct {
	base, quote string
	// rate is shared between copies and never changed once made.
	rate *big.Rat
}

// Base is the currency one unit of which the rate prices.
func (f FxRate) Base() string { return f.base }

// Quote is the currency the rate prices it in.
func (f FxRate) Quote() string { return f.quote }

// String is the rate in the market's notation, "USD/JPY 150.25", exactly: a
// rate with no finite decimal, such as a third, is written as its fraction.
func (f FxRate) String() string {
	if f.rate == nil {
		return "no rate"
	}
	return f.base + "/" + f.quote + " " + rateText(f.rate)
}

// Decimal is the rate rounded half to even to places decimal places, for
// showing it: the rate itself stays exact.
func (f FxRate) Decimal(places int) (string, error) {
	if err := f.checked(); err != nil {
		return "", err
	}
	places = max(places, 0)
	scale := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(places)), nil)
	scaled, remainder := new(big.Int).QuoRem(new(big.Int).Mul(f.rate.Num(), scale), f.rate.Denom(), new(big.Int))
	if remainder.Sign() != 0 && bigRoundsAway(scaled, remainder, f.rate.Denom(), false, RoundHalfEven) {
		scaled.Add(scaled, big.NewInt(1))
	}
	return new(big.Rat).SetFrac(scaled, scale).FloatString(places), nil
}

func (f FxRate) checked() error {
	if f.rate == nil {
		return fmt.Errorf("%w: the zero FxRate is no exchange rate; make one with a currency table", ErrCurrency)
	}
	return nil
}

// Every exchange rate keeps two rules: it is positive, and from a currency
// to itself it is exactly 1, the identity. validRate says which one a rate
// breaks.
func validRate(base, quote string, rate *big.Rat) error {
	switch {
	case rate.Sign() <= 0:
		return fmt.Errorf("%w: exchange rate %s→%s of %s is not positive", ErrArithmetic, base, quote, rateText(rate))
	case base == quote && rate.Cmp(big.NewRat(1, 1)) != 0:
		return fmt.Errorf("%w: exchange rate %s→%s is %s, and from a currency to itself it is 1", ErrArithmetic, base, quote, rateText(rate))
	}
	return nil
}

// maxRateBits bounds either side of an exchange rate's fraction, about a
// thousand digits (maxRateDigits): far past any quote or product of quotes a
// market makes, and the size rateText's text is read back at. Every rate a
// Go method, a rule or a JSON document makes is held to it, so every rate
// that exists round-trips through its JSON.
const (
	maxRateBits   = 3322
	maxRateDigits = 1001
)

// boundedRate refuses a rate too large to write down and read back.
func boundedRate(rate *big.Rat) error {
	if rate.Num().BitLen() > maxRateBits || rate.Denom().BitLen() > maxRateBits {
		return fmt.Errorf("%w: an exchange rate past %d digits a side", ErrArithmetic, maxRateDigits)
	}
	return nil
}

// newFxRate is the one way an exchange rate is made: it keeps the rules
// (validRate) and the bound (boundedRate).
func newFxRate(base, quote string, rate *big.Rat) (FxRate, error) {
	if err := validRate(base, quote, rate); err != nil {
		return FxRate{}, err
	}
	if err := boundedRate(rate); err != nil {
		return FxRate{}, err
	}
	return FxRate{base: base, quote: quote, rate: rate}, nil
}

// wellFormed checks a rate that did not come from a table: two codes, the
// rules and the bound.
func (f FxRate) wellFormed() error {
	if !IsCurrencyCode(f.base) || !IsCurrencyCode(f.quote) || f.rate == nil {
		return fmt.Errorf("%w: an exchange rate needs a \"base\" and a \"quote\" code and a rate", ErrCurrency)
	}
	_, err := newFxRate(f.base, f.quote, f.rate)
	return err
}

// identity reports the rate from a currency to itself.
func (f FxRate) identity() bool { return f.base == f.quote }

var rateOne = big.NewRat(1, 1)

// Inverse is the rate the other way round.
func (f FxRate) Inverse() (FxRate, error) {
	if err := f.checked(); err != nil {
		return FxRate{}, err
	}
	return newFxRate(f.quote, f.base, new(big.Rat).Inv(f.rate))
}

// Chain is f and then next: base to quote, then quote onward. The chain from
// a currency back to itself is no exchange rate.
func (f FxRate) Chain(next FxRate) (FxRate, error) {
	if err := f.checked(); err != nil {
		return FxRate{}, err
	}
	if err := next.checked(); err != nil {
		return FxRate{}, err
	}
	if f.quote != next.base {
		return FxRate{}, fmt.Errorf("%w: %s→%s cannot follow %s→%s", ErrCurrency, next.base, next.quote, f.base, f.quote)
	}
	if f.base == next.quote {
		// Back where it started: a currency to itself is 1, whatever the
		// spreads on the way cost.
		return FxRate{base: f.base, quote: f.base, rate: rateOne}, nil
	}
	return newFxRate(f.base, next.quote, new(big.Rat).Mul(f.rate, next.rate))
}

// MulRate is the rate with a markup or a discount: f × r, exactly, still the
// same pair — USD/JPY 150 times 101% is USD/JPY 151.5. Converting at it
// rounds once, where converting and then taking the rate of the result
// rounds twice. What comes out keeps the rules: a rate of zero or less, or
// one from a currency to itself that is not 1, is ErrArithmetic.
func (f FxRate) MulRate(r Rate) (FxRate, error) {
	if err := f.checked(); err != nil {
		return FxRate{}, err
	}
	scaled := new(big.Rat).SetFrac(big.NewInt(r.scaled), big.NewInt(RateScale))
	return newFxRate(f.base, f.quote, scaled.Mul(scaled, f.rate))
}

// Cmp orders two rates of one pair: which buys more of the quote currency.
func (f FxRate) Cmp(other FxRate) (int, error) {
	if err := f.checked(); err != nil {
		return 0, err
	}
	if err := other.checked(); err != nil {
		return 0, err
	}
	if f.base != other.base || f.quote != other.quote {
		return 0, fmt.Errorf("%w: %s→%s and %s→%s", ErrCurrency, f.base, f.quote, other.base, other.quote)
	}
	return f.rate.Cmp(other.rate), nil
}

func (f FxRate) MarshalJSON() ([]byte, error) {
	if err := f.checked(); err != nil {
		return nil, err
	}
	return json.Marshal(struct {
		Base  string `json:"base"`
		Quote string `json:"quote"`
		Rate  string `json:"rate"`
	}{f.base, f.quote, rateText(f.rate)})
}

// UnmarshalJSON reads the shape MarshalJSON writes. Whether the currencies
// are declared is the table's to say, where the rate is used.
func (f *FxRate) UnmarshalJSON(data []byte) error {
	var shape struct {
		Base  string `json:"base"`
		Quote string `json:"quote"`
		Rate  string `json:"rate"`
	}
	if err := json.Unmarshal(data, &shape); err != nil {
		return err
	}
	if !IsCurrencyCode(shape.Base) || !IsCurrencyCode(shape.Quote) {
		return fmt.Errorf("%w: an exchange rate needs a \"base\" and a \"quote\" currency code", ErrCurrency)
	}
	rate, err := parseRateText(shape.Rate)
	if err != nil {
		return err
	}
	fx, err := newFxRate(shape.Base, shape.Quote, rate)
	if err != nil {
		return err
	}
	*f = fx
	return nil
}

// rateText writes a rate exactly: its decimal when it has a finite one of at
// most maxRateDigits places, its fraction otherwise.
func rateText(rate *big.Rat) string {
	if rate.IsInt() {
		return rate.Num().String()
	}
	denominator := new(big.Int).Set(rate.Denom())
	places := 0
	for _, prime := range []int64{2, 5} {
		p, count := big.NewInt(prime), 0
		for new(big.Int).Mod(denominator, p).Sign() == 0 {
			denominator.Quo(denominator, p)
			count++
		}
		places = max(places, count)
	}
	if denominator.Cmp(big.NewInt(1)) != 0 || places > maxRateDigits {
		return rate.String()
	}
	return strings.TrimRight(strings.TrimRight(rate.FloatString(places), "0"), ".")
}

// parseRateText reads what rateText writes — a plain decimal or a fraction of
// two positive integers — for any rate boundedRate lets through: that is the
// size its limits are set at, not a quote's.
func parseRateText(text string) (*big.Rat, error) {
	numerator, denominator, fraction := strings.Cut(text, "/")
	if !fraction {
		rate, err := parseDecimalRate(text, 2*maxRateDigits)
		if err != nil {
			return nil, err
		}
		return rate, boundedRate(rate)
	}
	top, err := parseDecimalRate(numerator, maxRateDigits)
	if err != nil || !top.IsInt() {
		return nil, fmt.Errorf("%w: exchange rate %q is not a decimal or a fraction of integers", ErrArithmetic, text)
	}
	bottom, err := parseDecimalRate(denominator, maxRateDigits)
	if err != nil || !bottom.IsInt() {
		return nil, fmt.Errorf("%w: exchange rate %q is not a decimal or a fraction of integers", ErrArithmetic, text)
	}
	rate := top.Quo(top, bottom)
	return rate, boundedRate(rate)
}

// FxRate is an exchange rate between two declared currencies, read exactly
// from its decimal text: FxRate("USD", "JPY", "150.25"). From a currency to
// itself it must be 1.
func (c *Currencies) FxRate(base, quote, rate string) (FxRate, error) {
	if err := c.pair(base, quote); err != nil {
		return FxRate{}, err
	}
	parsed, err := parseQuote(rate)
	if err != nil {
		return FxRate{}, fmt.Errorf("%s→%s: %w", base, quote, err)
	}
	return newFxRate(base, quote, parsed)
}

// Implied is the rate two amounts of one exchange imply: over was paid for
// under, so one unit of under's currency bought this much of over's —
// Implied(JPY 15000, USD 100) is USD/JPY 150. It is what over / under is in
// a rule. The amounts must be in declared currencies, not zero, and
// of one sign; in one currency they imply its rate to itself, 1, only when
// they are equal.
func (c *Currencies) Implied(over, under Money) (FxRate, error) {
	if err := c.pair(under.currency, over.currency); err != nil {
		return FxRate{}, err
	}
	rate, err := impliedRate(c.table, over, under)
	if err != nil {
		return FxRate{}, err
	}
	return newFxRate(under.currency, over.currency, rate)
}

// impliedRate is over / under in major units, exactly.
func impliedRate(table *currencyTable, over, under Money) (*big.Rat, error) {
	if over.minor == 0 || under.minor == 0 || (over.minor < 0) != (under.minor < 0) {
		return nil, fmt.Errorf("%w: %s %d and %s %d minor units imply no exchange rate: the amounts must be non-zero and of one sign",
			ErrArithmetic, over.currency, over.minor, under.currency, under.minor)
	}
	if over.currency == under.currency {
		if over.minor != under.minor {
			return nil, fmt.Errorf("%w: %s %d and %d minor units in one currency imply no exchange rate; a currency to itself is 1",
				ErrArithmetic, over.currency, over.minor, under.minor)
		}
		return rateOne, nil
	}
	// Minor units to major ones: over / 10^over's places, under / 10^under's,
	// which is the factor from over's minor units to under's.
	rate := new(big.Rat).SetFrac(big.NewInt(over.minor), big.NewInt(under.minor))
	return rate.Mul(rate, factorRat(factorOf(table, over.currency, under.currency, big.NewRat(1, 1)))), nil
}

// factorRat is a conversion's factor as a rational.
func factorRat(c conversion) *big.Rat {
	if c.big != nil {
		return c.big
	}
	return big.NewRat(c.num, c.den)
}

// Convert is m, in fx's base currency, in its quote currency: rescaled
// between their places and rounded once by mode. The currency-less zero is
// zero in the quote currency.
func (c *Currencies) Convert(m Money, fx FxRate, mode Rounding) (Money, error) {
	if err := fx.checked(); err != nil {
		return Money{}, err
	}
	if err := c.pair(fx.base, fx.quote); err != nil {
		return Money{}, err
	}
	if m.currency == "" {
		if err := noCurrencyIsZero(MoneyValue(m.minor, "")); err != nil {
			return Money{}, err
		}
	}
	if m.currency != "" && m.currency != fx.base {
		return Money{}, fmt.Errorf("%w: %s converted at %s→%s", ErrCurrency, m.currency, fx.base, fx.quote)
	}
	minor, err := factorOf(c.table, fx.base, fx.quote, fx.rate).apply(m.minor, mode)
	return Money{currency: fx.quote, minor: minor}, err
}

// pair checks two declared currencies.
func (c *Currencies) pair(base, quote string) error {
	if _, err := c.table.places(base); err != nil {
		return err
	}
	_, err := c.table.places(quote)
	return err
}

// AddRate sets fx in the table, as Add does its decimal text.
func (r *Rates) AddRate(fx FxRate) error {
	if err := fx.checked(); err != nil {
		return err
	}
	if err := (&Currencies{table: r.table}).pair(fx.base, fx.quote); err != nil {
		return err
	}
	if fx.identity() {
		return nil // a currency to itself needs no rate
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.graph.Store(r.graph.Load().with(fx.base, fx.quote, fx.rate, nil))
	return nil
}

// Rate is the exchange rate from base to quote the table converts at: the
// quote between them or its inverse, one hop, as Convert takes it.
func (r *Rates) Rate(base, quote string) (FxRate, error) {
	if err := (&Currencies{table: r.table}).pair(base, quote); err != nil {
		return FxRate{}, err
	}
	return r.graph.Load().fxRate(base, quote)
}

// fxRate is the rate from base to quote in this version of a table: the
// identity for one currency, otherwise the one hop between them.
func (g *rateGraph) fxRate(base, quote string) (FxRate, error) {
	if base == quote {
		return FxRate{base: base, quote: base, rate: rateOne}, nil
	}
	edge, err := g.hop(base, quote)
	if err != nil {
		return FxRate{}, err
	}
	return newFxRate(base, quote, edge.rate)
}

// FxRateLiteral is the exchange rate 150 JPY / USD stands for in a rule: one
// base buys figure of quote, read exactly, so it takes any number of places.
// The currencies must be declared, and from one to itself the figure is 1.
func FxRateLiteral(r *Registry, figure, quote, base string) (Value, error) {
	table := r.currencies()
	if table == nil {
		return Value{}, fmt.Errorf("%w: this registry declares no money", ErrCurrency)
	}
	fx, err := (&Currencies{table: table}).FxRate(base, quote, figure)
	if err != nil {
		return Value{}, err
	}
	return FxRateValue(fx), nil
}
