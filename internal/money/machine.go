package money

// The entries the machine builds values from. They are package functions,
// not methods, so no host reaches them through a type it holds: they trust
// their arguments to be in canonical form, which only the machine, copying
// a value it made, can promise.

// Make is money of minor units in a currency, unchecked: the machine's, for
// money it already holds as a value.
func Make(currency string, minor int64) Money { return newMoney(currency, minor) }

// CurrencyOf is a currency code as a Currency, unchecked.
func CurrencyOf(code string) Currency { return Currency{code: code} }

// RatioParts is a ratio's numerator and denominator as it holds them: the
// zero value's denominator is 0.
func RatioParts(r Ratio) (num, den int64) { return r.num, r.den }

// RatioFromParts is the ratio RatioParts gave.
func RatioFromParts(num, den int64) Ratio { return Ratio{num: num, den: den} }

// RatioOf is num/den reduced; a denominator of 0 is no ratio.
func RatioOf(num, den int64) (Ratio, error) {
	if den == 0 {
		return Ratio{}, errDivisionByZero
	}
	return ratioOf(num, den)
}

// FxRateParts is an exchange rate's shared pair and its rate.
func FxRateParts(fx FxRate) (*Pair, Ratio) { return fx.pair, fx.rate }

// FxRateFrom is the exchange rate FxRateParts gave.
func FxRateFrom(pair *Pair, r Ratio) FxRate { return FxRate{pair: pair, rate: r} }

// ExactFrom is exact money of minor units in a currency, unchecked.
func ExactFrom(currency string, minor Ratio) ExactMoney {
	return ExactMoney{currency: currency, minor: minor}
}

// ReadFxRate is a rate from its parts as text, the JSON's: two codes and the
// rate, a decimal or a fraction.
func ReadFxRate(base, quote, text string) (FxRate, error) { return readFxRate(base, quote, text) }

// PairOf is the table's one Pair for two declared currencies.
func PairOf(c *Currencies, base, quote string) (*Pair, error) { return c.pairOf(base, quote) }

// Meet is the currency two amounts combine in: theirs when they agree, the
// other's when one is the currency-less zero.
func Meet(a, b string) (string, error) { return meet(a, b) }

// MulDivRound is a·b/d rounded once by mode, 128 bits wide in between.
func MulDivRound(a, b, d int64, mode Rounding) (int64, error) { return mulDivRound(a, b, d, mode) }

// CheckFxRate refuses the zero FxRate, which is no exchange rate.
func CheckFxRate(fx FxRate) error { return fx.checked() }

// WellFormedFxRate checks a rate that did not come from a table: two codes
// and the rules every exchange rate keeps.
func WellFormedFxRate(fx FxRate) error { return fx.wellFormed() }

// ConvertAt is Convert for a rate whose currencies the machine already
// holds declared: it checks nothing the machine checked.
func ConvertAt(c *Currencies, m Money, fx FxRate, mode Rounding) (Money, error) {
	return c.convertAt(m, fx, mode)
}

// ConvertExactAt is ConvertExact for a rate the machine already checked.
func ConvertExactAt(c *Currencies, m ExactMoney, fx FxRate) (ExactMoney, error) {
	return c.convertExactAt(m, fx)
}
