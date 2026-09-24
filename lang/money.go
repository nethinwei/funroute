package lang

import "funroute/lang/internal/machine"

// Money. A registry that calls DeclareMoney gets four more kinds of value:
// money in a currency's minor unit, a rate in fixed point, an exchange rate
// between two currencies, and a currency itself. Money and ordinary numbers meet only in the operators and a
// handful of functions; a registry that declares nothing compiles exactly
// what it did before. A rule's exchange rates live in a rate table
// (Currencies.NewRates), given as RunOptions.Rates, which amount -> JPY
// converts through.
type (
	// Money is an amount in a currency's minor unit, the Go form of money.
	// Only a currency table makes one (Currencies.Parse, Of, Minor); the zero
	// value is the currency-less zero. []Money is the backing of
	// array<money<…>>, handed over without a copy.
	Money = machine.Money
	// Rate is an exact ratio, made by ParseRate, Percent or BasisPoints; the
	// zero value is 0. It only works on money and on other rates, and
	// crosses JSON as a decimal string.
	Rate = machine.Rate
	// FxRate is an exact exchange rate, the Go form of fxrate: made by
	// Currencies.FxRate or Implied or read from a rate table with Rates.Rate,
	// passed to a rule as an argument, and returned from one. From a currency
	// to itself it is 1. Currencies.Convert converts at it and Rates.AddRate
	// adds it; a rule hands it to using, and converts with ->.
	FxRate = machine.FxRate
	// Rates is a rate table: the exchange rates between one currency
	// table's currencies, made by Currencies.NewRates and filled by Add. It
	// is safe for concurrent use; a conversion — a whole run, for a rule —
	// uses the version the table had when it started.
	Rates = machine.Rates
	// Quote is a quote a rate table holds, as the host added it, with the
	// source and times it recorded: Rates.Quote reads it. QuoteSpec is one to
	// add with them, by Rates.AddQuote; the table records them and never acts
	// on them, so what is stale is the host's to decide.
	Quote     = machine.Quote
	QuoteSpec = machine.QuoteSpec
	// Currency is a declared currency as a value, made by
	// Currencies.Currency.
	Currency = machine.Currency
)

// Money and Rate carry the language's arithmetic as methods — Add, Sub,
// Cmp, MulRate, Allocate and the rest — and the kernel's
// operators are those methods, so a host computing beside a rule gets the
// rule's answer. They return an error where the language would: currencies
// that do not match (ErrCurrency), an int64 overflow, a division by zero.
var (
	// Percent("2.9") is 2.9%, BasisPoints("25") is 0.25%, exactly.
	Percent     = machine.Percent
	BasisPoints = machine.BasisPoints
	// AverageMoney and MedianMoney are std's avg and median: amounts in one
	// currency, rounded once by mode.
	AverageMoney = machine.AverageMoney
	MedianMoney  = machine.MedianMoney
)

// The money types. A unit is a currency code (money<USD>), a contract's
// currency variable (money<c>, lower case), or "" for a currency only the run
// knows (money<?>). An exchange rate has two, fxrate<USD,JPY>.
var (
	RateType   = machine.RateType
	MoneyOf    = machine.MoneyOf
	CurrencyOf = machine.CurrencyOf
	FxRateOf   = machine.FxRateOf
)

// A money value becomes a Value through ToValue, like any Go value.
var (
	// ParseRate reads a decimal ratio exactly.
	ParseRate = machine.ParseRate
)
