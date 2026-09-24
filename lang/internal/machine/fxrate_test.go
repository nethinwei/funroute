package machine

import (
	"encoding/json"
	"errors"
	"math/big"
	"strings"
	"testing"
)

// fxRate is rateCurrencies' rate from base to quote, read from its text.
func fxRate(t *testing.T, base, quote, rate string) FxRate {
	t.Helper()
	fx, err := rateCurrencies(t).FxRate(base, quote, rate)
	if err != nil {
		t.Fatalf("FxRate(%s, %s, %q) = %v", base, quote, rate, err)
	}
	return fx
}

// expectRate checks a rate's pair and its exact text.
func expectRate(t *testing.T, fx FxRate, err error, want string) {
	t.Helper()
	if err != nil || fx.String() != want {
		t.Fatalf("rate = %s, %v, want %s", fx, err, want)
	}
}

func TestFxRateReadsItsTextExactly(t *testing.T) {
	t.Parallel()
	for text, want := range map[string]string{
		"150.25":         "USD/JPY 150.25",
		"150":            "USD/JPY 150",
		"150.2500":       "USD/JPY 150.25",
		"0.0000000006":   "USD/JPY 0.0000000006",
		"007.10":         "USD/JPY 7.1",
		"123456789.0001": "USD/JPY 123456789.0001",
	} {
		t.Run(text, func(t *testing.T) {
			t.Parallel()
			fx, err := rateCurrencies(t).FxRate("USD", "JPY", text)
			expectRate(t, fx, err, want)
			if fx.Base() != "USD" || fx.Quote() != "JPY" {
				t.Fatalf("FxRate(USD, JPY, %q) is %s→%s, want USD→JPY", text, fx.Base(), fx.Quote())
			}
		})
	}
}

func TestFxRateRefusesWhatIsNoRate(t *testing.T) {
	t.Parallel()
	for name, test := range map[string]struct {
		base, quote, rate string
		want              error
	}{
		"one currency, not 1":  {"USD", "USD", "2", ErrArithmetic},
		"one currency, below":  {"USD", "USD", "0.5", ErrArithmetic},
		"an undeclared base":   {"CHF", "JPY", "150", ErrCurrency},
		"an undeclared quote":  {"USD", "CHF", "0.9", ErrCurrency},
		"not a code":           {"usd", "JPY", "150", ErrCurrency},
		"zero":                 {"USD", "JPY", "0", ErrArithmetic},
		"negative":             {"USD", "JPY", "-150", ErrArithmetic},
		"an exponent":          {"USD", "JPY", "1.5e2", ErrArithmetic},
		"a fraction":           {"USD", "JPY", "601/4", ErrArithmetic},
		"empty":                {"USD", "JPY", "", ErrArithmetic},
		"past the digit bound": {"USD", "JPY", "1.00000000000000000000000000000000000000001", ErrArithmetic},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fx, err := rateCurrencies(t).FxRate(test.base, test.quote, test.rate)
			if !errors.Is(err, test.want) {
				t.Fatalf("FxRate(%s, %s, %q) = %s, %v, want %v", test.base, test.quote, test.rate, fx, err, test.want)
			}
		})
	}
}

// A rate with no finite decimal is written as its fraction, so its text is
// always exact.
func TestFxRateStringIsExact(t *testing.T) {
	t.Parallel()
	for name, test := range map[string]struct {
		rate *big.Rat
		want string
	}{
		"an integer":        {big.NewRat(150, 1), "USD/JPY 150"},
		"halves and fifths": {big.NewRat(1, 40), "USD/JPY 0.025"},
		"a tenth":           {big.NewRat(1, 10), "USD/JPY 0.1"},
		"a third":           {big.NewRat(1, 3), "USD/JPY 1/3"},
		"a sixth":           {big.NewRat(7, 6), "USD/JPY 7/6"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := (FxRate{base: "USD", quote: "JPY", rate: test.rate}).String(); got != test.want {
				t.Fatalf("String(%s) = %s, want %s", test.rate, got, test.want)
			}
		})
	}
	if got := (FxRate{}).String(); got != "no rate" {
		t.Fatalf("String(FxRate{}) = %s, want no rate", got)
	}
}

func TestFxRateDecimalRoundsOnlyTheText(t *testing.T) {
	t.Parallel()
	third := FxRate{base: "USD", quote: "JPY", rate: big.NewRat(1, 3)}
	for places, want := range map[int]string{-1: "0", 0: "0", 2: "0.33", 4: "0.3333"} {
		if got, err := third.Decimal(places); err != nil || got != want {
			t.Errorf("Decimal(%d) of 1/3 = %q, %v, want %q", places, got, err, want)
		}
	}
	if got, err := fxRate(t, "USD", "JPY", "150.26").Decimal(1); err != nil || got != "150.3" {
		t.Errorf("Decimal(1) of 150.26 = %q, %v, want 150.3", got, err)
	}
	if third.String() != "USD/JPY 1/3" {
		t.Errorf("Decimal changed the rate to %s", third)
	}
}

// Every method of the zero FxRate is ErrCurrency: it is no rate at all.
func TestTheZeroFxRateIsNoRate(t *testing.T) {
	t.Parallel()
	var zero FxRate
	rate := fxRate(t, "USD", "JPY", "150")
	_, decimal := zero.Decimal(2)
	_, inverse := zero.Inverse()
	_, chainFrom := zero.Chain(rate)
	_, chainTo := rate.Chain(zero)
	_, cmpFrom := zero.Cmp(rate)
	_, cmpTo := rate.Cmp(zero)
	_, marshal := json.Marshal(zero)
	_, convert := rateCurrencies(t).Convert(Money{"USD", 100}, zero, RoundHalfEven)
	add := rateCurrencies(t).NewRates().AddRate(zero)
	for name, err := range map[string]error{
		"Decimal": decimal, "Inverse": inverse, "Chain from": chainFrom, "Chain to": chainTo,
		"Cmp from": cmpFrom, "Cmp to": cmpTo, "MarshalJSON": marshal, "Convert": convert, "AddRate": add,
	} {
		if !errors.Is(err, ErrCurrency) {
			t.Errorf("%s of the zero FxRate = %v, want ErrCurrency", name, err)
		}
	}
	if zero.Base() != "" || zero.Quote() != "" {
		t.Errorf("the zero FxRate is %q→%q, want no currencies", zero.Base(), zero.Quote())
	}
}

func TestFxRateInverseIsExact(t *testing.T) {
	t.Parallel()
	for rate, want := range map[string]string{"150": "JPY/USD 1/150", "0.8": "JPY/USD 1.25", "150.25": "JPY/USD 4/601"} {
		t.Run(rate, func(t *testing.T) {
			t.Parallel()
			inverse, err := fxRate(t, "USD", "JPY", rate).Inverse()
			expectRate(t, inverse, err, want)
			back, err := inverse.Inverse()
			expectRate(t, back, err, fxRate(t, "USD", "JPY", rate).String())
		})
	}
}

func TestFxRateChain(t *testing.T) {
	t.Parallel()
	eurUSD, usdJPY := fxRate(t, "EUR", "USD", "1.1"), fxRate(t, "USD", "JPY", "150.25")
	chained, err := eurUSD.Chain(usdJPY)
	expectRate(t, chained, err, "EUR/JPY 165.275")
	if _, err := usdJPY.Chain(eurUSD); !errors.Is(err, ErrCurrency) {
		t.Errorf("USD/JPY then EUR/USD = %v, want ErrCurrency: it does not follow on", err)
	}
	back, err := usdJPY.Inverse()
	if err != nil {
		t.Fatal(err)
	}
	// Back where it started, the chain is the currency's own rate: 1.
	home, err := usdJPY.Chain(back)
	expectRate(t, home, err, "USD/USD 1")
}

func TestFxRateCmp(t *testing.T) {
	t.Parallel()
	low, high := fxRate(t, "USD", "JPY", "150"), fxRate(t, "USD", "JPY", "150.25")
	for name, test := range map[string]struct {
		a, b FxRate
		want int
	}{
		"lower":            {low, high, -1},
		"higher":           {high, low, 1},
		"equal":            {low, fxRate(t, "USD", "JPY", "150.00"), 0},
		"an implied equal": {low, implied(t, Money{"JPY", 15000}, Money{"USD", 10000}), 0},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got, err := test.a.Cmp(test.b); err != nil || got != test.want {
				t.Fatalf("Cmp(%s, %s) = %d, %v, want %d", test.a, test.b, got, err, test.want)
			}
		})
	}
	inverse, err := low.Inverse()
	if err != nil {
		t.Fatal(err)
	}
	for _, other := range []FxRate{inverse, fxRate(t, "USD", "EUR", "0.9")} {
		if _, err := low.Cmp(other); !errors.Is(err, ErrCurrency) {
			t.Errorf("Cmp(%s, %s) = %v, want ErrCurrency", low, other, err)
		}
	}
}

// implied is rateCurrencies' Implied(over, under).
func implied(t *testing.T, over, under Money) FxRate {
	t.Helper()
	fx, err := rateCurrencies(t).Implied(over, under)
	if err != nil {
		t.Fatalf("Implied(%v, %v) = %v", over, under, err)
	}
	return fx
}

func TestFxRateJSONRoundTrips(t *testing.T) {
	t.Parallel()
	for name, test := range map[string]struct {
		fx   FxRate
		want string
	}{
		"a decimal":  {fxRate(t, "USD", "JPY", "150.25"), `{"base":"USD","quote":"JPY","rate":"150.25"}`},
		"an integer": {fxRate(t, "USD", "JPY", "150"), `{"base":"USD","quote":"JPY","rate":"150"}`},
		"a fraction": {FxRate{base: "JPY", quote: "USD", rate: big.NewRat(1, 3)}, `{"base":"JPY","quote":"USD","rate":"1/3"}`},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			data, err := json.Marshal(test.fx)
			if err != nil || string(data) != test.want {
				t.Fatalf("Marshal(%s) = %s, %v, want %s", test.fx, data, err, test.want)
			}
			var back FxRate
			if err := json.Unmarshal(data, &back); err != nil {
				t.Fatalf("Unmarshal(%s) = %v", data, err)
			}
			if same, err := back.Cmp(test.fx); err != nil || same != 0 {
				t.Fatalf("Unmarshal(%s) = %s, want %s", data, back, test.fx)
			}
		})
	}
}

func TestFxRateJSONRefusesWhatIsNoRate(t *testing.T) {
	t.Parallel()
	for name, test := range map[string]struct {
		json string
		want error
	}{
		"one currency, not 1":    {`{"base":"USD","quote":"USD","rate":"2"}`, ErrArithmetic},
		"no quote currency":      {`{"base":"USD","rate":"150"}`, ErrCurrency},
		"not a code":             {`{"base":"usd","quote":"JPY","rate":"150"}`, ErrCurrency},
		"zero":                   {`{"base":"USD","quote":"JPY","rate":"0"}`, ErrArithmetic},
		"negative":               {`{"base":"USD","quote":"JPY","rate":"-150"}`, ErrArithmetic},
		"no rate":                {`{"base":"USD","quote":"JPY"}`, ErrArithmetic},
		"a zero fraction":        {`{"base":"USD","quote":"JPY","rate":"0/3"}`, ErrArithmetic},
		"a fraction over zero":   {`{"base":"USD","quote":"JPY","rate":"1/0"}`, ErrArithmetic},
		"a decimal fraction":     {`{"base":"USD","quote":"JPY","rate":"1.5/3"}`, ErrArithmetic},
		"a fraction of decimals": {`{"base":"USD","quote":"JPY","rate":"3/1.5"}`, ErrArithmetic},
		"two fraction lines":     {`{"base":"USD","quote":"JPY","rate":"1/3/4"}`, ErrArithmetic},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var fx FxRate
			if err := json.Unmarshal([]byte(test.json), &fx); !errors.Is(err, test.want) {
				t.Fatalf("Unmarshal(%s) = %s, %v, want %v", test.json, fx, err, test.want)
			}
		})
	}
	var fx FxRate
	for _, shape := range []string{`[1]`, `{"base":1}`, `{"rate":150}`} {
		if err := json.Unmarshal([]byte(shape), &fx); err == nil {
			t.Errorf("Unmarshal(%s) = %s, nil, want an error", shape, fx)
		}
	}
}

// Implied(over, under) is the rate one unit of under's currency bought of
// over's, in major units: the currencies' places do not show in it.
func TestImpliedIsTheRateTwoAmountsImply(t *testing.T) {
	t.Parallel()
	for name, test := range map[string]struct {
		over, under Money
		want        string
	}{
		"yen for dollars":    {Money{"JPY", 15000}, Money{"USD", 10000}, "USD/JPY 150"},
		"dollars for yen":    {Money{"USD", 100}, Money{"JPY", 150}, "JPY/USD 1/150"},
		"dinars for dollars": {Money{"KWD", 307}, Money{"USD", 100}, "USD/KWD 0.307"},
		"satoshis for dong":  {Money{"BTC", 1}, Money{"VND", 1}, "VND/BTC 0.00000001"},
		"two refunds":        {Money{"JPY", -15025}, Money{"USD", -10000}, "USD/JPY 150.25"},
		"a rate with no end": {Money{"EUR", 100}, Money{"USD", 300}, "USD/EUR 1/3"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fx, err := rateCurrencies(t).Implied(test.over, test.under)
			expectRate(t, fx, err, test.want)
		})
	}
	inverse, err := implied(t, Money{"JPY", 15000}, Money{"USD", 10000}).Inverse()
	if err != nil {
		t.Fatal(err)
	}
	if same, err := implied(t, Money{"USD", 100}, Money{"JPY", 150}).Cmp(inverse); err != nil || same != 0 {
		t.Fatalf("Implied(USD 1.00, JPY 150) is not the inverse of Implied(JPY 15000, USD 100.00): %d, %v", same, err)
	}
}

func TestImpliedRefusesAmountsThatImplyNoRate(t *testing.T) {
	t.Parallel()
	for name, test := range map[string]struct {
		over, under Money
		want        error
	}{
		"zero over":             {Money{"JPY", 0}, Money{"USD", 100}, ErrArithmetic},
		"zero under":            {Money{"JPY", 150}, Money{"USD", 0}, ErrArithmetic},
		"opposite signs":        {Money{"JPY", -150}, Money{"USD", 100}, ErrArithmetic},
		"one currency, unequal": {Money{"USD", 150}, Money{"USD", 100}, ErrArithmetic},
		"a currency-less zero":  {Money{"JPY", 150}, Money{}, ErrCurrency},
		"an undeclared one":     {Money{"CHF", 150}, Money{"USD", 100}, ErrCurrency},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fx, err := rateCurrencies(t).Implied(test.over, test.under)
			if !errors.Is(err, test.want) {
				t.Fatalf("Implied(%v, %v) = %s, %v, want %v", test.over, test.under, fx, err, test.want)
			}
		})
	}
}

func TestCurrenciesConvertAtARate(t *testing.T) {
	t.Parallel()
	chained, err := fxRate(t, "EUR", "USD", "1.1").Chain(fxRate(t, "USD", "JPY", "150.25"))
	if err != nil {
		t.Fatal(err)
	}
	third := implied(t, Money{"EUR", 100}, Money{"USD", 300})
	for name, test := range map[string]struct {
		m    Money
		fx   FxRate
		mode Rounding
		want Money
	}{
		"dollars into yen":     {Money{"USD", 100}, fxRate(t, "USD", "JPY", "150.25"), RoundHalfEven, Money{"JPY", 150}},
		"rounded up":           {Money{"USD", 100}, fxRate(t, "USD", "JPY", "150.25"), RoundUp, Money{"JPY", 151}},
		"a half to even":       {Money{"USD", 100}, fxRate(t, "USD", "KWD", "0.3075"), RoundHalfEven, Money{"KWD", 308}},
		"a currency-less zero": {Money{}, fxRate(t, "USD", "JPY", "150.25"), RoundHalfEven, Money{"JPY", 0}},
		"dong into satoshis":   {Money{"VND", 150}, implied(t, Money{"BTC", 1}, Money{"VND", 1}), RoundHalfEven, Money{"BTC", 150}},
		"a third, half even":   {Money{"USD", 100}, third, RoundHalfEven, Money{"EUR", 33}},
		"a third, up":          {Money{"USD", 100}, third, RoundUp, Money{"EUR", 34}},
		// Rounded once: EUR 0.05 is JPY 8.26375 exactly, where rounding at
		// the dollar would give USD 0.06, JPY 9.015, JPY 9.
		"a chain rounded once": {Money{"EUR", 5}, chained, RoundHalfEven, Money{"JPY", 8}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, err := rateCurrencies(t).Convert(test.m, test.fx, test.mode)
			if err != nil || got != test.want {
				t.Fatalf("Convert(%v, %s, %s) = %v, %v, want %v", test.m, test.fx, test.mode, got, err, test.want)
			}
		})
	}
}

func TestCurrenciesConvertRefusesTheWrongCurrency(t *testing.T) {
	t.Parallel()
	usdJPY := fxRate(t, "USD", "JPY", "150")
	if got, err := rateCurrencies(t).Convert(Money{"EUR", 100}, usdJPY, RoundHalfEven); !errors.Is(err, ErrCurrency) {
		t.Errorf("Convert(EUR 1.00, %s) = %v, %v, want ErrCurrency", usdJPY, got, err)
	}
	// A rate over a currency this table does not declare.
	eurUSD := fxRate(t, "EUR", "USD", "1.1")
	if got, err := testCurrencies(t).Convert(Money{"EUR", 100}, eurUSD, RoundHalfEven); !errors.Is(err, ErrCurrency) {
		t.Errorf("Convert(EUR 1.00, %s) on a table without EUR = %v, %v, want ErrCurrency", eurUSD, got, err)
	}
}

// ratesHolding is a rate table over rateCurrencies holding fxs, added as
// rates.
func ratesHolding(t *testing.T, fxs ...FxRate) *Rates {
	t.Helper()
	rates := rateCurrencies(t).NewRates()
	for _, fx := range fxs {
		if err := rates.AddRate(fx); err != nil {
			t.Fatalf("AddRate(%s) = %v", fx, err)
		}
	}
	return rates
}

// Rate is the exact product of the chain a conversion takes, whichever way
// round the quotes were given — one hop, never a chain through a third.
func TestRatesRateIsTheOneHop(t *testing.T) {
	t.Parallel()
	rates := ratesHolding(t, fxRate(t, "USD", "JPY", "150.25"), fxRate(t, "EUR", "USD", "1.1"),
		implied(t, Money{"GBP", 100}, Money{"KWD", 3000}))
	for name, test := range map[string]struct {
		base, quote, want string
	}{
		"a direct quote":     {"USD", "JPY", "USD/JPY 150.25"},
		"its inverse":        {"JPY", "USD", "JPY/USD 4/601"},
		"a fraction added":   {"KWD", "GBP", "KWD/GBP 1/3"},
		"its inverse, exact": {"GBP", "KWD", "GBP/KWD 3"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fx, err := rates.Rate(test.base, test.quote)
			expectRate(t, fx, err, test.want)
		})
	}
	if _, err := rates.Rate("EUR", "JPY"); !errors.Is(err, ErrNoRate) {
		t.Fatalf("Rate(EUR, JPY) through dollars = %v, want ErrNoRate", err)
	}
	// The rate Rate gives converts as the table does.
	hop, err := rates.Rate("JPY", "USD")
	if err != nil {
		t.Fatal(err)
	}
	direct, errDirect := rateCurrencies(t).Convert(Money{"JPY", 5}, hop, RoundHalfEven)
	through, errThrough := rates.Convert(Money{"JPY", 5}, "USD", RoundHalfEven)
	if errDirect != nil || errThrough != nil || direct != through {
		t.Fatalf("Convert at Rate = %v, %v; the table's Convert = %v, %v; want the same", direct, errDirect, through, errThrough)
	}
}

func TestRatesRateRefusesWhatHasNoRate(t *testing.T) {
	t.Parallel()
	rates := ratesHolding(t, fxRate(t, "USD", "JPY", "150.25"))
	for name, test := range map[string]struct {
		base, quote string
		want        error
	}{
		"no chain":      {"GBP", "JPY", ErrNoRate},
		"an undeclared": {"CHF", "USD", ErrCurrency},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if fx, err := rates.Rate(test.base, test.quote); !errors.Is(err, test.want) {
				t.Fatalf("Rate(%s, %s) = %s, %v, want %v", test.base, test.quote, fx, err, test.want)
			}
		})
	}
}

func TestRatesAddRate(t *testing.T) {
	t.Parallel()
	rates := rateCurrencies(t).NewRates()
	if err := rates.Add("USD", "JPY", "100"); err != nil {
		t.Fatal(err)
	}
	if err := rates.AddRate(fxRate(t, "USD", "JPY", "150.25")); err != nil {
		t.Fatal(err)
	}
	fx, err := rates.Rate("USD", "JPY")
	expectRate(t, fx, err, "USD/JPY 150.25")
	for name, test := range map[string]struct {
		fx   FxRate
		want error
	}{
		"an undeclared currency": {FxRate{base: "USD", quote: "CHF", rate: big.NewRat(9, 10)}, ErrCurrency},
	} {
		if err := rates.AddRate(test.fx); !errors.Is(err, test.want) {
			t.Errorf("AddRate(%s: %s) = %v, want %v", name, test.fx, err, test.want)
		}
	}
}

// A currency is worth itself: its rate against itself is exactly 1, however
// it is made, and any other figure is no rate.
func TestTheRateOfACurrencyToItselfIsOne(t *testing.T) {
	t.Parallel()
	table := rateCurrencies(t)
	quoted, errQuoted := table.FxRate("USD", "USD", "1")
	padded, errPadded := table.FxRate("JPY", "JPY", "1.000")
	equal, errEqual := table.Implied(Money{"USD", 100}, Money{"USD", 100})
	refunds, errRefunds := table.Implied(Money{"JPY", -5}, Money{"JPY", -5})
	var read FxRate
	errRead := json.Unmarshal([]byte(`{"base":"KWD","quote":"KWD","rate":"1"}`), &read)
	for name, test := range map[string]struct {
		fx   FxRate
		err  error
		want string
	}{
		"FxRate 1":           {quoted, errQuoted, "USD/USD 1"},
		"FxRate 1.000":       {padded, errPadded, "JPY/JPY 1"},
		"Implied, equal":     {equal, errEqual, "USD/USD 1"},
		"Implied, refunds":   {refunds, errRefunds, "JPY/JPY 1"},
		"UnmarshalJSON of 1": {read, errRead, "KWD/KWD 1"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			expectRate(t, test.fx, test.err, test.want)
		})
	}
}

// Every rate that can be made writes a JSON its UnmarshalJSON reads back
// equal: a fraction with a thousand-digit side, a decimal with a thousand
// places, and the rates Implied and a chain of quotes make, whose text has
// no bound of its own but the one newFxRate keeps.
func TestEveryFxRateRoundTripsThroughJSON(t *testing.T) {
	t.Parallel()
	pow := func(base, exp int64) *big.Int { return new(big.Int).Exp(big.NewInt(base), big.NewInt(exp), nil) }
	implied, err := rateCurrencies(t).Implied(Money{currency: "USD", minor: 1}, Money{currency: "JPY", minor: 1 << 62})
	if err != nil {
		t.Fatal(err)
	}
	for name, rate := range map[string]*big.Rat{
		"a thousand-digit fraction": new(big.Rat).SetFrac(pow(3, 600), pow(7, 500)),
		"a thousand decimal places": new(big.Rat).SetFrac(big.NewInt(1), pow(2, 1000)),
		"past a thousand places":    new(big.Rat).SetFrac(big.NewInt(1), pow(2, 3000)),
		"implied by 2^62 yen":       implied.rate,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fx, err := newFxRate("JPY", "USD", rate)
			if err != nil {
				t.Fatalf("newFxRate(%s) = %v", name, err)
			}
			data, err := json.Marshal(fx)
			if err != nil {
				t.Fatal(err)
			}
			var back FxRate
			if err := json.Unmarshal(data, &back); err != nil || back.rate.Cmp(fx.rate) != 0 {
				t.Fatalf("%s: %.80s… reads back as %v, %v, want the same rate", name, data, back, err)
			}
		})
	}
}

// A rate past maxRateBits a side is ErrArithmetic wherever it would be made:
// a chain of quotes multiplied out, its inverse, its text. A quote a host or
// a rule writes stays within maxQuoteDigits.
func TestAnFxRateTooLargeToWriteIsRefused(t *testing.T) {
	t.Parallel()
	huge := new(big.Rat).SetInt(new(big.Int).Lsh(big.NewInt(1), maxRateBits))
	if _, err := newFxRate("USD", "JPY", huge); !errors.Is(err, ErrArithmetic) {
		t.Fatalf("newFxRate(2^%d) = %v, want ErrArithmetic", maxRateBits, err)
	}
	half := new(big.Rat).SetInt(new(big.Int).Lsh(big.NewInt(1), maxRateBits/2+8))
	a, b := FxRate{base: "USD", quote: "EUR", rate: half}, FxRate{base: "EUR", quote: "JPY", rate: half}
	if _, err := a.Chain(b); !errors.Is(err, ErrArithmetic) {
		t.Fatalf("a chain past the bound = %v, want ErrArithmetic", err)
	}
	if _, err := parseRateText(huge.RatString()); !errors.Is(err, ErrArithmetic) {
		t.Fatalf("parseRateText(2^%d) = %v, want ErrArithmetic", maxRateBits, err)
	}
	if _, err := rateCurrencies(t).FxRate("USD", "JPY", "1"+strings.Repeat("0", maxQuoteDigits)); !errors.Is(err, ErrArithmetic) {
		t.Fatalf("a quote of %d digits = %v, want ErrArithmetic", maxQuoteDigits+1, err)
	}
}

// MulRate marks a rate up or down, exactly, keeping its pair; the identity
// cannot be marked, and nothing can take a rate to zero.
func TestFxRateMulRateKeepsThePair(t *testing.T) {
	t.Parallel()
	fx := fxRate(t, "USD", "JPY", "150")
	marked, err := fx.MulRate(Rate{scaled: RateScale + RateScale/100})
	if err != nil || marked.String() != "USD/JPY 151.5" {
		t.Fatalf("USD/JPY 150 × 101%% = %v, %v, want USD/JPY 151.5", marked, err)
	}
	if _, err := fx.MulRate(Rate{}); !errors.Is(err, ErrArithmetic) {
		t.Fatalf("× 0%% error = %v, want ErrArithmetic", err)
	}
	if _, err := fxRate(t, "USD", "USD", "1").MulRate(Rate{scaled: 2 * RateScale}); !errors.Is(err, ErrArithmetic) {
		t.Fatalf("USD/USD × 200%% error = %v, want ErrArithmetic", err)
	}
	if _, err := (FxRate{}).MulRate(Rate{scaled: RateScale}); err == nil {
		t.Fatal("the zero FxRate marked up, want an error")
	}
}
