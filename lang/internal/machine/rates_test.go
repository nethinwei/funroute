package machine

import (
	"errors"
	"math"
	"math/big"
	"math/rand/v2"
	"strings"
	"sync"
	"testing"
)

// rateCurrencies is a table wide enough for every path the rate tests take:
// places 0, 2, 3 and 8.
func rateCurrencies(t *testing.T) *Currencies {
	t.Helper()
	table, err := NewCurrencies(MoneySpec{Rounding: RoundHalfEven, Currencies: []CurrencySpec{
		{Code: "USD", Digits: 2}, {Code: "EUR", Digits: 2}, {Code: "GBP", Digits: 2}, {Code: "JPY", Digits: 0},
		{Code: "KWD", Digits: 3}, {Code: "BTC", Digits: 8}, {Code: "VND", Digits: 0},
	}})
	if err != nil {
		t.Fatal(err)
	}
	return table
}

// ratesOf is a rate table over rateCurrencies holding quotes, each written
// base, quote, rate.
func ratesOf(t *testing.T, quotes ...[3]string) *Rates {
	t.Helper()
	rates := rateCurrencies(t).NewRates()
	for _, quote := range quotes {
		if err := rates.Add(quote[0], quote[1], quote[2]); err != nil {
			t.Fatalf("Add(%s, %s, %q) = %v", quote[0], quote[1], quote[2], err)
		}
	}
	return rates
}

// expectConverted checks one conversion's answer.
func expectConverted(t *testing.T, rates *Rates, m Money, to string, mode Rounding, want Money) {
	t.Helper()
	got, err := rates.Convert(m, to, mode)
	if err != nil || got != want {
		t.Fatalf("Convert(%v, %s, %s) = %v, %v, want %v", m, to, mode, got, err, want)
	}
}

func TestRatesConvertThroughTheirQuotes(t *testing.T) {
	t.Parallel()
	rates := ratesOf(t, [3]string{"USD", "JPY", "150.25"}, [3]string{"EUR", "USD", "1.1"})
	for name, test := range map[string]struct {
		m    Money
		to   string
		want Money
	}{
		"a direct quote":         {Money{"USD", 100}, "JPY", Money{"JPY", 150}},
		"a half to even":         {Money{"USD", 1000}, "JPY", Money{"JPY", 1502}},
		"a negative amount":      {Money{"USD", -1000}, "JPY", Money{"JPY", -1502}},
		"the inverse of a quote": {Money{"JPY", 15025}, "USD", Money{"USD", 10_000}},
		"an inverse that rounds": {Money{"JPY", 1}, "USD", Money{"USD", 1}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			expectConverted(t, rates, test.m, test.to, RoundHalfEven, test.want)
		})
	}
}

// A conversion is one hop: the pair's quote or the inverse of the one the
// other way. The table never goes through a third currency, however many
// routes it holds; a pair quoted both ways uses each way's own quote.
func TestRatesConvertInOneHop(t *testing.T) {
	t.Parallel()
	rates := ratesOf(t, [3]string{"EUR", "USD", "1"}, [3]string{"USD", "JPY", "100"}, [3]string{"EUR", "GBP", "1"}, [3]string{"GBP", "JPY", "200"})
	for _, pair := range [][2]string{{"EUR", "JPY"}, {"JPY", "EUR"}} {
		if got, err := rates.Convert(Money{pair[0], 100}, pair[1], RoundHalfEven); !errors.Is(err, ErrNoRate) {
			t.Errorf("Convert(%s -> %s) = %v, %v, want ErrNoRate: no route is searched", pair[0], pair[1], got, err)
		}
	}
	both := ratesOf(t, [3]string{"EUR", "USD", "1.1"}, [3]string{"USD", "EUR", "0.9"})
	expectConverted(t, both, Money{"EUR", 100}, "USD", RoundHalfEven, Money{"USD", 110})
	expectConverted(t, both, Money{"USD", 100}, "EUR", RoundHalfEven, Money{"EUR", 90})
}

// Adding a pair again replaces its rate, and the inverse with it.
func TestRatesAddAgainReplaces(t *testing.T) {
	t.Parallel()
	rates := ratesOf(t, [3]string{"USD", "JPY", "150"})
	expectConverted(t, rates, Money{"USD", 100}, "JPY", RoundHalfEven, Money{"JPY", 150})
	expectConverted(t, rates, Money{"JPY", 150}, "USD", RoundHalfEven, Money{"USD", 100})
	if err := rates.Add("USD", "JPY", "160"); err != nil {
		t.Fatal(err)
	}
	expectConverted(t, rates, Money{"USD", 100}, "JPY", RoundHalfEven, Money{"JPY", 160})
	expectConverted(t, rates, Money{"JPY", 160}, "USD", RoundHalfEven, Money{"USD", 100})
}

// A quote given in both directions keeps both as given: the direct quote is
// used before the inverse of the other, whichever came first.
func TestRatesPreferADirectQuoteToAnInverse(t *testing.T) {
	t.Parallel()
	usdJPY, jpyUSD := [3]string{"USD", "JPY", "150"}, [3]string{"JPY", "USD", "0.0067"}
	for name, rates := range map[string]*Rates{
		"the dollar quote first": ratesOf(t, usdJPY, jpyUSD),
		"the yen quote first":    ratesOf(t, jpyUSD, usdJPY),
		"the dollar quote again": ratesOf(t, usdJPY, jpyUSD, usdJPY),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			// 1/150 would make JPY 1000 USD 6.67; the quote says 6.70.
			expectConverted(t, rates, Money{"JPY", 1000}, "USD", RoundHalfEven, Money{"USD", 670})
			expectConverted(t, rates, Money{"USD", 100}, "JPY", RoundHalfEven, Money{"JPY", 150})
		})
	}
}

func TestRatesAddRefusesWhatIsNoRate(t *testing.T) {
	t.Parallel()
	forty := "1" + strings.Repeat("0", 39)
	for name, test := range map[string]struct {
		base, quote, rate string
		want              error
	}{
		"an undeclared base":     {"XXX", "USD", "1", ErrCurrency},
		"an undeclared quote":    {"USD", "XXX", "1", ErrCurrency},
		"one currency, not 1":    {"USD", "USD", "2", ErrArithmetic},
		"zero":                   {"USD", "JPY", "0", ErrArithmetic},
		"zero with places":       {"USD", "JPY", "0.000", ErrArithmetic},
		"a negative rate":        {"USD", "JPY", "-1", ErrArithmetic},
		"a signed rate":          {"USD", "JPY", "+1", ErrArithmetic},
		"an exponent":            {"USD", "JPY", "1e3", ErrArithmetic},
		"a fraction":             {"USD", "JPY", "1/3", ErrArithmetic},
		"two points":             {"USD", "JPY", "1.2.3", ErrArithmetic},
		"a space":                {"USD", "JPY", " 1", ErrArithmetic},
		"nothing":                {"USD", "JPY", "", ErrArithmetic},
		"a point alone":          {"USD", "JPY", ".", ErrArithmetic},
		"a word":                 {"USD", "JPY", "abc", ErrArithmetic},
		"forty-one digits":       {"USD", "JPY", forty + "0", ErrArithmetic},
		"forty-one with a point": {"USD", "JPY", "0." + forty, ErrArithmetic},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if err := rateCurrencies(t).NewRates().Add(test.base, test.quote, test.rate); !errors.Is(err, test.want) {
				t.Fatalf("Add(%s, %s, %q) = %v, want %v", test.base, test.quote, test.rate, err, test.want)
			}
		})
	}
	for _, rate := range []string{forty, "0." + strings.Repeat("0", 38) + "1", ".5", "1.", "007"} {
		if err := rateCurrencies(t).NewRates().Add("USD", "JPY", rate); err != nil {
			t.Fatalf("Add(USD, JPY, %q) = %v, want it taken", rate, err)
		}
	}
}

func TestRatesWithoutAChainAreErrNoRate(t *testing.T) {
	t.Parallel()
	rates := ratesOf(t, [3]string{"USD", "JPY", "150"})
	if got, err := rates.Convert(Money{"EUR", 100}, "JPY", RoundHalfEven); !errors.Is(err, ErrNoRate) {
		t.Fatalf("Convert(EUR 1.00, JPY) with only USD/JPY = %v, %v, want ErrNoRate", got, err)
	}
	if got, err := rates.Convert(Money{"USD", 100}, "EUR", RoundHalfEven); !errors.Is(err, ErrNoRate) {
		t.Fatalf("Convert(USD 1.00, EUR) with only USD/JPY = %v, %v, want ErrNoRate", got, err)
	}
	var none *Rates
	if got, err := none.Convert(Money{"USD", 100}, "JPY", RoundHalfEven); !errors.Is(err, ErrNoRate) {
		t.Fatalf("a nil table's Convert = %v, %v, want ErrNoRate", got, err)
	}
	if got, err := rates.Convert(Money{"USD", 100}, "XXX", RoundHalfEven); !errors.Is(err, ErrCurrency) {
		t.Fatalf("Convert(USD 1.00, XXX) = %v, %v, want ErrCurrency", got, err)
	}
}

// Money already in the currency, and the currency-less zero, need no rate.
func TestRatesNeedNoQuoteForOneCurrency(t *testing.T) {
	t.Parallel()
	empty := rateCurrencies(t).NewRates()
	expectConverted(t, empty, Money{"USD", 12_345}, "USD", RoundHalfEven, Money{"USD", 12_345})
	expectConverted(t, empty, Money{}, "JPY", RoundHalfEven, Money{currency: "JPY"})
}

// USD 100.00, -100.00 and 140.00 at 0.025 are JPY 2.5, -2.5 and 3.5: every
// mode tells them apart differently.
func TestRatesRoundByEveryMode(t *testing.T) {
	t.Parallel()
	rates := ratesOf(t, [3]string{"USD", "JPY", "0.025"})
	for mode, want := range map[Rounding][3]int64{
		RoundHalfEven: {2, -2, 4}, RoundHalfUp: {3, -3, 4}, RoundHalfDown: {2, -2, 3}, RoundUp: {3, -3, 4},
		RoundDown: {2, -2, 3}, RoundCeiling: {3, -2, 4}, RoundFloor: {2, -3, 3},
	} {
		t.Run(mode.String(), func(t *testing.T) {
			t.Parallel()
			for i, minor := range []int64{10_000, -10_000, 14_000} {
				expectConverted(t, rates, Money{"USD", minor}, "JPY", mode, Money{"JPY", want[i]})
			}
		})
	}
	if got, err := rates.Convert(Money{"USD", 100}, "JPY", Rounding(99)); !errors.Is(err, ErrArithmetic) {
		t.Fatalf("Convert with rounding 99 = %v, %v, want ErrArithmetic", got, err)
	}
}

// BTC has eight places and VND none: at VND 1,600,000,000 to the bitcoin a
// satoshi is VND 16 and a dong is 0.0625 satoshi, and nothing is lost on the
// way, whichever way the quote is written.
func TestRatesSpanEightPlacesExactly(t *testing.T) {
	t.Parallel()
	for name, rates := range map[string]*Rates{
		"quoted per bitcoin": ratesOf(t, [3]string{"BTC", "VND", "1600000000"}),
		"quoted per dong":    ratesOf(t, [3]string{"VND", "BTC", "0.000000000625"}),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			expectSatoshisAndDong(t, rates)
		})
	}
}

func expectSatoshisAndDong(t *testing.T, rates *Rates) {
	t.Helper()
	expectConverted(t, rates, Money{"BTC", 100_000_000}, "VND", RoundHalfEven, Money{"VND", 1_600_000_000})
	expectConverted(t, rates, Money{"BTC", 1}, "VND", RoundHalfEven, Money{"VND", 16})
	expectConverted(t, rates, Money{"VND", 1}, "BTC", RoundHalfEven, Money{"BTC", 0})
	expectConverted(t, rates, Money{"VND", 1}, "BTC", RoundUp, Money{"BTC", 1})
	expectConverted(t, rates, Money{"VND", 8}, "BTC", RoundHalfEven, Money{"BTC", 0})
	expectConverted(t, rates, Money{"VND", 8}, "BTC", RoundHalfUp, Money{"BTC", 1})
	expectConverted(t, rates, Money{"VND", 16}, "BTC", RoundDown, Money{"BTC", 1})
	expectConverted(t, rates, Money{"VND", 1_600_000_000}, "BTC", RoundHalfEven, Money{"BTC", 100_000_000})
	expectConverted(t, rates, Money{"VND", 9_000_000_000_000_000_000}, "BTC", RoundHalfEven, Money{"BTC", 562_500_000_000_000_000})
	if got, err := rates.Convert(Money{"BTC", math.MaxInt64}, "VND", RoundHalfEven); !errors.Is(err, ErrArithmetic) {
		t.Fatalf("Convert(the most satoshis, VND) = %v, %v, want an overflow", got, err)
	}
}

// A factor past int64 — a quote of many places, or a chain of them — is
// carried in arbitrary precision.
func TestRatesCarryFactorsPastInt64(t *testing.T) {
	t.Parallel()
	rates := ratesOf(t, [3]string{"USD", "JPY", "150.1234567890123456789"})
	// USD 100.00 is JPY 15012.34567890123456789.
	expectConverted(t, rates, Money{"USD", 10_000}, "JPY", RoundHalfEven, Money{"JPY", 15_012})
	expectConverted(t, rates, Money{"USD", 10_000}, "JPY", RoundCeiling, Money{"JPY", 15_013})
	expectConverted(t, rates, Money{"USD", -10_000}, "JPY", RoundFloor, Money{"JPY", -15_013})
	if got, err := rates.Convert(Money{"USD", 10_000}, "JPY", Rounding(0)); !errors.Is(err, ErrArithmetic) {
		t.Fatalf("Convert with rounding 0 through big = %v, %v, want ErrArithmetic", got, err)
	}
	if got, err := rates.Convert(Money{"USD", math.MaxInt64}, "JPY", RoundHalfEven); !errors.Is(err, errFixedOverflow) {
		t.Fatalf("Convert(the most cents, JPY) through big = %v, %v, want an overflow", got, err)
	}
}

// bigMulDivRound agrees with exact rational arithmetic in every mode, and
// with mulDivRound wherever the factor fits int64.
func TestBigMulDivRoundIsExactThenRoundedOnce(t *testing.T) {
	t.Parallel()
	random := rand.New(rand.NewPCG(5, 6))
	for mode := RoundHalfEven; mode <= RoundFloor; mode++ {
		for range 2000 {
			minor := random.Int64() >> random.IntN(63)
			if random.IntN(2) == 0 {
				minor = -minor
			}
			assertBigMulDivRound(t, minor, randomFactor(random), mode)
		}
	}
}

// randomFactor is a positive rational of up to about 128 bits on each side.
func randomFactor(random *rand.Rand) *big.Rat {
	part := func() *big.Int {
		n := new(big.Int).SetUint64(random.Uint64() >> random.IntN(64))
		if random.IntN(2) == 0 {
			n.Lsh(n, 64).Add(n, new(big.Int).SetUint64(random.Uint64()))
		}
		return n.Add(n, big.NewInt(1))
	}
	return new(big.Rat).SetFrac(part(), part())
}

func assertBigMulDivRound(t *testing.T, minor int64, factor *big.Rat, mode Rounding) {
	t.Helper()
	got, err := bigMulDivRound(minor, factor, mode)
	want, fits := bigRoundFrac(new(big.Int).Mul(big.NewInt(minor), factor.Num()), factor.Denom(), mode)
	switch {
	case !fits:
		if !errors.Is(err, errFixedOverflow) {
			t.Fatalf("bigMulDivRound(%d, %s, %s) = %d, %v, want an overflow", minor, factor, mode, got, err)
		}
	case err != nil || got != want:
		t.Fatalf("bigMulDivRound(%d, %s, %s) = %d, %v, want %d", minor, factor, mode, got, err, want)
	}
	if factor.Num().IsInt64() && factor.Denom().IsInt64() {
		small, smallErr := mulDivRound(minor, factor.Num().Int64(), factor.Denom().Int64(), mode)
		if (err == nil) != (smallErr == nil) || small != got {
			t.Fatalf("mulDivRound(%d, %s, %s) = %d, %v, bigMulDivRound = %d, %v", minor, factor, mode, small, smallErr, got, err)
		}
	}
}

// Quotes are added while conversions run: every conversion sees one version
// of the table, whole, so it is one of the rates added and never a mix.
func TestRatesAddAndConvertConcurrently(t *testing.T) {
	t.Parallel()
	rates := ratesOf(t, [3]string{"USD", "JPY", "100"})
	var group sync.WaitGroup
	group.Go(func() {
		for i := range 200 {
			if err := rates.Add("USD", "JPY", []string{"100", "200"}[i%2]); err != nil {
				t.Error(err)
			}
		}
	})
	for range 4 {
		group.Go(func() { convertWhileAdding(t, rates) })
	}
	group.Wait()
}

func convertWhileAdding(t *testing.T, rates *Rates) {
	t.Helper()
	for range 200 {
		got, err := rates.Convert(Money{"USD", 100}, "JPY", RoundHalfEven)
		if err != nil || (got != Money{"JPY", 100} && got != Money{"JPY", 200}) {
			t.Errorf("Convert(USD 1.00, JPY) while adding = %v, %v, want JPY 100 or 200", got, err)
			return
		}
	}
}

// A table converts only what it can say something about: a currency-less
// amount that is not zero, a currency it does not declare, to or from, are
// errors; a rate from a currency to itself is no quote and changes nothing.
func TestRatesRefuseWhatTheyCannotConvert(t *testing.T) {
	t.Parallel()
	rates := rateCurrencies(t).NewRates()
	if err := rates.Add("USD", "USD", "1"); err != nil {
		t.Fatalf("Add(USD, USD, 1) = %v, want nothing to do", err)
	}
	if _, ok := rates.Quote("USD", "USD"); ok {
		t.Fatal("Add(USD, USD, 1) left a quote")
	}
	for name, test := range map[string]struct {
		m  Money
		to string
	}{
		"a currency-less amount": {Money{minor: 5}, "JPY"},
		"an undeclared source":   {Money{currency: "XYZ", minor: 5}, "JPY"},
		"an undeclared target":   {Money{currency: "USD", minor: 5}, "XYZ"},
	} {
		if _, err := rates.Convert(test.m, test.to, RoundHalfUp); !errors.Is(err, ErrCurrency) {
			t.Errorf("%s: Convert error = %v, want ErrCurrency", name, err)
		}
	}
	if zero, err := rates.Convert(Money{}, "JPY", RoundHalfUp); err != nil || zero.currency != "JPY" {
		t.Fatalf("Convert(zero) = %v, %v, want JPY 0", zero, err)
	}
	var none *Rates
	if _, err := none.Convert(Money{currency: "USD", minor: 1}, "JPY", RoundHalfUp); !errors.Is(err, ErrNoRate) {
		t.Fatalf("a nil table's Convert error = %v, want ErrNoRate", err)
	}
}
