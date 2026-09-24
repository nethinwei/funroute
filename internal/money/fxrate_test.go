package money

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// rateCurrencies is the currency table the exchange rate tests use.
func rateCurrencies(t testing.TB) *Currencies {
	t.Helper()
	table, err := NewCurrencies(MoneySpec{Currencies: []CurrencySpec{
		{Code: "USD", Digits: 2}, {Code: "EUR", Digits: 2}, {Code: "GBP", Digits: 2}, {Code: "JPY", Digits: 0},
		{Code: "KWD", Digits: 3}, {Code: "BTC", Digits: 8}, {Code: "VND", Digits: 0},
	}})
	if err != nil {
		t.Fatal(err)
	}
	return table
}

// pairRate is num/den of quote for one base, made without a table.
func pairRate(base, quote string, num, den int64) FxRate {
	return FxRate{pair: &Pair{Base: base, Quote: quote}, rate: ratio(num, den)}
}

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
		"150.25":         "150.25 JPY / USD",
		"150":            "150 JPY / USD",
		"150.2500":       "150.25 JPY / USD",
		"0.0000000006":   "0.0000000006 JPY / USD",
		"007.10":         "7.1 JPY / USD",
		"123456789.0001": "123456789.0001 JPY / USD",
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
		rate Ratio
		want string
	}{
		"an integer":        {ratio(150, 1), "150 JPY / USD"},
		"halves and fifths": {ratio(1, 40), "0.025 JPY / USD"},
		"a tenth":           {ratio(1, 10), "0.1 JPY / USD"},
		"a third":           {ratio(1, 3), "1/3 JPY / USD"},
		"a sixth":           {ratio(7, 6), "7/6 JPY / USD"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := (FxRate{pair: &Pair{Base: "USD", Quote: "JPY"}, rate: test.rate}).String(); got != test.want {
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
	third := pairRate("USD", "JPY", 1, 3)
	for places, want := range map[int]string{-1: "0", 0: "0", 2: "0.33", 4: "0.3333"} {
		if got, err := third.Decimal(places); err != nil || got != want {
			t.Errorf("Decimal(%d) of 1/3 = %q, %v, want %q", places, got, err, want)
		}
	}
	if got, err := fxRate(t, "USD", "JPY", "150.26").Decimal(1); err != nil || got != "150.3" {
		t.Errorf("Decimal(1) of 150.26 = %q, %v, want 150.3", got, err)
	}
	if third.String() != "1/3 JPY / USD" {
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
	for name, err := range map[string]error{
		"Decimal": decimal, "Inverse": inverse, "Chain from": chainFrom, "Chain to": chainTo,
		"Cmp from": cmpFrom, "Cmp to": cmpTo, "MarshalJSON": marshal, "Convert": convert,
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
	for rate, want := range map[string]string{"150": "1/150 USD / JPY", "0.8": "1.25 USD / JPY", "150.25": "4/601 USD / JPY"} {
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
	expectRate(t, chained, err, "165.275 JPY / EUR")
	if _, err := usdJPY.Chain(eurUSD); !errors.Is(err, ErrCurrency) {
		t.Errorf("USD/JPY then EUR/USD = %v, want ErrCurrency: it does not follow on", err)
	}
	back, err := usdJPY.Inverse()
	if err != nil {
		t.Fatal(err)
	}
	// Back where it started, the chain is the currency's own rate: 1.
	home, err := usdJPY.Chain(back)
	expectRate(t, home, err, "1 USD / USD")
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
		"a fraction": {pairRate("JPY", "USD", 1, 3), `{"base":"JPY","quote":"USD","rate":"1/3"}`},
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
		"yen for dollars":    {Money{"JPY", 15000}, Money{"USD", 10000}, "150 JPY / USD"},
		"dollars for yen":    {Money{"USD", 100}, Money{"JPY", 150}, "1/150 USD / JPY"},
		"dinars for dollars": {Money{"KWD", 307}, Money{"USD", 100}, "0.307 KWD / USD"},
		"satoshis for dong":  {Money{"BTC", 1}, Money{"VND", 1}, "0.00000001 BTC / VND"},
		"two refunds":        {Money{"JPY", -15025}, Money{"USD", -10000}, "150.25 JPY / USD"},
		"a rate with no end": {Money{"EUR", 100}, Money{"USD", 300}, "1/3 EUR / USD"},
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
		"one currency, unequal": {Money{"USD", 150}, Money{"USD", 100}, ErrCurrency},
		"one currency, equal":   {Money{"USD", 100}, Money{"USD", 100}, ErrCurrency},
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

// A currency is worth itself: its rate against itself is exactly 1, however
// it is made, and any other figure is no rate.
func TestTheRateOfACurrencyToItselfIsOne(t *testing.T) {
	t.Parallel()
	table := rateCurrencies(t)
	quoted, errQuoted := table.FxRate("USD", "USD", "1")
	padded, errPadded := table.FxRate("JPY", "JPY", "1.000")
	var read FxRate
	errRead := json.Unmarshal([]byte(`{"base":"KWD","quote":"KWD","rate":"1"}`), &read)
	for name, test := range map[string]struct {
		fx   FxRate
		err  error
		want string
	}{
		"FxRate 1":           {quoted, errQuoted, "1 USD / USD"},
		"FxRate 1.000":       {padded, errPadded, "1 JPY / JPY"},
		"UnmarshalJSON of 1": {read, errRead, "1 KWD / KWD"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			expectRate(t, test.fx, test.err, test.want)
		})
	}
}

// MulRatio marks a rate up or down, exactly, keeping its pair; the identity
// cannot be marked, and nothing can take a rate to zero.
func TestFxRateMulRateKeepsThePair(t *testing.T) {
	t.Parallel()
	fx := fxRate(t, "USD", "JPY", "150")
	marked, err := fx.MulRatio(ratio(101, 100))
	if err != nil || marked.String() != "151.5 JPY / USD" {
		t.Fatalf("150 JPY / USD × 101%% = %v, %v, want 151.5 JPY / USD", marked, err)
	}
	if _, err := fx.MulRatio(Ratio{}); !errors.Is(err, ErrArithmetic) {
		t.Fatalf("× 0%% error = %v, want ErrArithmetic", err)
	}
	if _, err := fxRate(t, "USD", "USD", "1").MulRatio(ratio(2, 1)); !errors.Is(err, ErrArithmetic) {
		t.Fatalf("USD/USD × 200%% error = %v, want ErrArithmetic", err)
	}
	if _, err := (FxRate{}).MulRatio(ratio(1, 1)); err == nil {
		t.Fatal("the zero FxRate marked up, want an error")
	}
}

// A rate past int64 a side is ErrArithmetic wherever it would be made: a
// quote's text, a chain of quotes multiplied out, a markup. Nothing is ever
// rounded to fit.
func TestAnFxRatePastInt64IsRefused(t *testing.T) {
	t.Parallel()
	if _, err := rateCurrencies(t).FxRate("USD", "JPY", "1"+strings.Repeat("0", 19)); !errors.Is(err, ErrArithmetic) {
		t.Fatalf("a quote of 10^19 = %v, want ErrArithmetic", err)
	}
	if _, err := rateCurrencies(t).FxRate("USD", "JPY", "0."+strings.Repeat("0", 18)+"1"); !errors.Is(err, ErrArithmetic) {
		t.Fatalf("a quote of 19 places = %v, want ErrArithmetic", err)
	}
	a, b := pairRate("USD", "EUR", 1<<40, 3), pairRate("EUR", "JPY", 1<<40, 7)
	if _, err := a.Chain(b); !errors.Is(err, ErrArithmetic) {
		t.Fatalf("a chain past int64 = %v, want ErrArithmetic", err)
	}
	if _, err := a.MulRatio(ratio(1<<40, 1)); !errors.Is(err, ErrArithmetic) {
		t.Fatalf("a markup past int64 = %v, want ErrArithmetic", err)
	}
}
