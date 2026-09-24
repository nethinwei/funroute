package machine

import (
	"errors"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"sync/atomic"
)

// Exchange rates live in a rate table a currency table makes. A host adds the
// quotes it has, and every conversion — Rates.Convert in Go, amount -> JPY in
// a rule — is one hop: the quote between the two currencies, or the inverse
// of the one the other way. No route through a third currency is ever
// searched, so the rate a rule converts at is always the one its pair was
// given; a rule that goes through a third currency writes each hop, amount ->
// CNY -> USD, and each hop rounds. A conversion names the currency it wants,
// so no rate is applied to the wrong currency. using lays rates of the rule's
// own over the table for a stretch of it.
//
// Quotes are exact: "150.25" is the rational 15025/100, and only the
// converted amount is rounded, to its currency's minor unit. So a quote of 0.0000000006 or a conversion from BTC's satoshis to
// VND's dong loses nothing on the way.

// ErrNoRate is a conversion the rate table cannot make: no table was given,
// or it has no rate between the two currencies, either way. It is data not
// yet at hand, like a failed extension, so fallback takes it.
var ErrNoRate = errors.New("no exchange rate")

// maxQuoteDigits bounds a quote's text: far more places than any market
// quotes, and a bound on the work each one costs.
const maxQuoteDigits = 40

// Rates is a rate table: exchange rates between the currencies of one
// currency table. It is safe for concurrent use; Add publishes a new version
// of the table, and a conversion — a whole run, for a rule — uses the version
// it started with, so a rate that changes mid-run changes nothing in it.
type Rates struct {
	table *currencyTable
	mu    sync.Mutex // serialises Add
	graph atomic.Pointer[rateGraph]
}

// rateGraph is one version of the table: each currency's neighbours and the
// rate to each, the inverse of every quote included. It never changes once
// published, except for the conversions it has worked out, which it keeps.
type rateGraph struct {
	edges   map[string]map[string]rateEdge
	mu      sync.RWMutex
	factors map[[2]string]conversion
}

// rateEdge is one step: a quote as given, or the inverse of the quote the
// other way. A quote given in both directions keeps both as given.
type rateEdge struct {
	rate   *big.Rat
	quoted bool
	// meta is what the host said of a quote it added with AddQuote; nil for
	// an inverse, and for a quote added without it.
	meta *quoteMeta
}

// conversion is the factor that turns minor units of one currency into minor
// units of another: the chained rate rescaled between their places. The
// factor fits int64 as num/den nearly always; big holds it when it does not.
type conversion struct {
	num, den int64
	big      *big.Rat
	err      error
}

// NewRates is an empty rate table over this currency table.
func (c *Currencies) NewRates() *Rates {
	rates := &Rates{table: c.table}
	rates.graph.Store(newRateGraph())
	return rates
}

// newRateGraph is a table with no rates in it.
func newRateGraph() *rateGraph {
	return &rateGraph{edges: map[string]map[string]rateEdge{}, factors: map[[2]string]conversion{}}
}

// Add sets the rate one unit of base buys of quote: Add("USD", "JPY",
// "150.25"). Both currencies must be declared and different, and the rate a
// positive decimal, read exactly. Adding a pair again replaces its rate.
func (r *Rates) Add(base, quote, rate string) error {
	return r.AddQuote(QuoteSpec{Base: base, Quote: quote, Rate: rate})
}

// AddQuote is Add with what the host knows of the quote: its source, when it
// was quoted and until when. The table keeps them for Quote to hand back;
// it never judges them, so a quote past its Until converts all the same.
func (r *Rates) AddQuote(spec QuoteSpec) error {
	if _, err := r.table.places(spec.Base); err != nil {
		return err
	}
	if _, err := r.table.places(spec.Quote); err != nil {
		return err
	}
	parsed, err := parseQuote(spec.Rate)
	if err != nil {
		return fmt.Errorf("%s→%s: %w", spec.Base, spec.Quote, err)
	}
	if err := validRate(spec.Base, spec.Quote, parsed); err != nil {
		return err
	}
	if spec.Base == spec.Quote {
		return nil // a currency to itself needs no rate
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.graph.Store(r.graph.Load().with(spec.Base, spec.Quote, parsed, spec.meta()))
	return nil
}

// parseQuote reads a quote a host or a rule wrote: a positive plain decimal
// of at most maxQuoteDigits digits.
func parseQuote(text string) (*big.Rat, error) {
	return parseDecimalRate(text, maxQuoteDigits)
}

// parseDecimalRate reads a positive plain decimal exactly: digits with at
// most one point, no sign, no exponent, at most limit digits.
func parseDecimalRate(text string, limit int) (*big.Rat, error) {
	whole, fraction, _ := strings.Cut(text, ".")
	digits := whole + fraction
	if digits == "" || len(digits) > limit || strings.Trim(digits, "0123456789") != "" {
		return nil, fmt.Errorf("%w: exchange rate %q is not a plain decimal of at most %d digits", ErrArithmetic, text, limit)
	}
	rate, ok := new(big.Rat).SetString(text)
	if !ok || rate.Sign() <= 0 {
		return nil, fmt.Errorf("%w: exchange rate %q is not positive", ErrArithmetic, text)
	}
	return rate, nil
}

// with is the graph with base→quote set: a copy, since the graph in hand may
// be in use. The inverse goes the other way unless that way was quoted.
func (g *rateGraph) with(base, quote string, rate *big.Rat, meta *quoteMeta) *rateGraph {
	next := g.copied()
	next.set(base, quote, rateEdge{rate: rate, quoted: true, meta: meta})
	if back, ok := next.edges[quote][base]; !ok || !back.quoted {
		next.set(quote, base, rateEdge{rate: new(big.Rat).Inv(rate)})
	}
	return next
}

func (g *rateGraph) set(from, to string, edge rateEdge) {
	if g.edges[from] == nil {
		g.edges[from] = map[string]rateEdge{}
	}
	g.edges[from][to] = edge
}

// Convert is m in the currency to, at the table's rate between the two: the
// quote from m's currency to to, or the inverse of the one the other way,
// with the amount rounded once by mode. It is one hop; no route through a
// third currency is searched, and a pair the table has no rate for is
// ErrNoRate. Money already in to, and the currency-less zero, need no rate.
func (r *Rates) Convert(m Money, to string, mode Rounding) (Money, error) {
	if r == nil {
		return Money{}, fmt.Errorf("%w: no rate table was given", ErrNoRate)
	}
	return r.graph.Load().convert(r.table, m, to, mode)
}

func (g *rateGraph) convert(table *currencyTable, m Money, to string, mode Rounding) (Money, error) {
	if _, err := table.places(to); err != nil {
		return Money{}, err
	}
	if m.currency == "" {
		if err := noCurrencyIsZero(MoneyValue(m.minor, "")); err != nil {
			return Money{}, err
		}
		return Money{currency: to}, nil
	}
	if _, err := table.places(m.currency); err != nil {
		return Money{}, err
	}
	if m.currency == to {
		return m, nil
	}
	factor := g.conversion(table, m.currency, to)
	if factor.err != nil {
		return Money{}, factor.err
	}
	minor, err := factor.apply(m.minor, mode)
	return Money{currency: to, minor: minor}, err
}

// conversion is the factor from one currency to another, worked out once
// per version of the table.
func (g *rateGraph) conversion(table *currencyTable, from, to string) conversion {
	key := [2]string{from, to}
	g.mu.RLock()
	found, ok := g.factors[key]
	g.mu.RUnlock()
	if ok {
		return found
	}
	found = g.resolve(table, from, to)
	g.mu.Lock()
	g.factors[key] = found
	g.mu.Unlock()
	return found
}

func (g *rateGraph) resolve(table *currencyTable, from, to string) conversion {
	edge, err := g.hop(from, to)
	if err != nil {
		return conversion{err: err}
	}
	return factorOf(table, from, to, edge.rate)
}

// hop is the one step from one currency to another: the quote between them,
// or the inverse of the quote the other way. A conversion is never more than
// that — no route is searched through a third currency, so which rate a rule
// converts at is always the one its pair was given. A rule that means to go
// through a third currency writes both steps, amount -> CNY -> USD.
func (g *rateGraph) hop(from, to string) (rateEdge, error) {
	edge, ok := g.edges[from][to]
	if !ok {
		return rateEdge{}, fmt.Errorf("%w: the table has no rate between %s and %s, either way", ErrNoRate, from, to)
	}
	return edge, nil
}

// factorOf is what minor units of from are multiplied by to be minor units
// of to at rate: the rate rescaled between the two currencies' places.
func factorOf(table *currencyTable, from, to string, rate *big.Rat) conversion {
	fromDigits, _ := table.places(from)
	toDigits, _ := table.places(to)
	scale := new(big.Rat).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(abs(toDigits-fromDigits))), nil))
	factor := new(big.Rat).Set(rate)
	if toDigits >= fromDigits {
		factor.Mul(factor, scale)
	} else {
		factor.Quo(factor, scale)
	}
	if factor.Num().IsInt64() && factor.Denom().IsInt64() {
		return conversion{num: factor.Num().Int64(), den: factor.Denom().Int64()}
	}
	return conversion{big: factor}
}

// apply is minor times the factor, rounded once by mode.
func (c conversion) apply(minor int64, mode Rounding) (int64, error) {
	if c.big == nil {
		return mulDivRound(minor, c.num, c.den, mode)
	}
	return bigMulDivRound(minor, c.big, mode)
}

func abs(n int) int { return max(n, -n) }

// bigMulDivRound is minor × factor rounded once by mode, for a factor past
// int64: the rules of mulDivRound, in arbitrary precision.
func bigMulDivRound(minor int64, factor *big.Rat, mode Rounding) (int64, error) {
	if mode < RoundHalfEven || mode > RoundFloor {
		return 0, fmt.Errorf("%w: rounding %d is not a mode", ErrArithmetic, mode)
	}
	numerator := new(big.Int).Mul(big.NewInt(minor), factor.Num())
	quotient, remainder := new(big.Int).QuoRem(numerator, factor.Denom(), new(big.Int))
	negative := numerator.Sign() < 0
	if remainder.Sign() != 0 && bigRoundsAway(quotient, remainder, factor.Denom(), negative, mode) {
		step := big.NewInt(1)
		if negative {
			step.Neg(step)
		}
		quotient.Add(quotient, step)
	}
	if !quotient.IsInt64() {
		return 0, errFixedOverflow
	}
	return quotient.Int64(), nil
}

// bigRoundsAway is roundsAway for big magnitudes: whether the quotient moves
// one step away from zero.
func bigRoundsAway(quotient, remainder, divisor *big.Int, negative bool, mode Rounding) bool {
	switch mode {
	case RoundUp:
		return true
	case RoundCeiling:
		return !negative
	case RoundFloor:
		return negative
	case RoundDown:
		return false
	}
	twice := new(big.Int).Abs(remainder)
	half := twice.Lsh(twice, 1).Cmp(divisor)
	switch mode {
	case RoundHalfUp:
		return half >= 0
	case RoundHalfDown:
		return half > 0
	default: // RoundHalfEven
		return half > 0 || (half == 0 && quotient.Bit(0) == 1)
	}
}
