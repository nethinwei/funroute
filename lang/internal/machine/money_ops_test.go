package machine

import (
	"encoding/json"
	"errors"
	"math"
	"math/big"
	"strings"
	"testing"
)

func usd(minor int64) Money { return Money{currency: "USD", minor: minor} }

func TestMoneyAddsInOneCurrency(t *testing.T) {
	t.Parallel()
	if sum, err := usd(170).Add(usd(30)); err != nil || sum != usd(200) {
		t.Fatalf("USD 1.70 + USD 0.30 = %v, %v", sum, err)
	}
	if sum, err := (Money{}).Add(usd(5)); err != nil || sum != usd(5) {
		t.Fatalf("zero + USD 0.05 = %v, %v, want the zero to take the currency", sum, err)
	}
	if _, err := usd(1).Sub(Money{currency: "EUR", minor: 1}); !errors.Is(err, ErrCurrency) {
		t.Fatalf("dollars minus euros: error = %v, want ErrCurrency", err)
	}
	if _, err := usd(1).Cmp(Money{currency: "EUR", minor: 1}); !errors.Is(err, ErrCurrency) {
		t.Fatalf("comparing dollars with euros: error = %v, want ErrCurrency", err)
	}
	if _, err := usd(math.MinInt64).Neg(); err == nil {
		t.Fatal("negating the smallest amount succeeded, want an overflow")
	}
	if order, err := usd(-5).Cmp(Money{}); err != nil || order != -1 || usd(-5).Sign() != -1 {
		t.Fatalf("USD -0.05 against zero = %d, %v", order, err)
	}
}

func TestMoneyTimesARateRoundsOnce(t *testing.T) {
	t.Parallel()
	fee, err := usd(100).MulRate(newRate(250_000_000), RoundHalfEven) // 2.5%, half a cent
	if err != nil || fee != usd(2) {
		t.Fatalf("USD 1.00 * 2.5%% half even = %v, %v, want USD 0.02", fee, err)
	}
	fee, _ = usd(100).MulRate(newRate(250_000_000), RoundHalfUp)
	if fee != usd(3) {
		t.Fatalf("USD 1.00 * 2.5%% half up = %v, want USD 0.03", fee)
	}
	gross, err := usd(9710).DivRate(newRate(9_710_000_000), RoundHalfUp) // net / 97.1%
	if err != nil || gross != usd(10_000) {
		t.Fatalf("USD 97.10 / 97.1%% = %v, %v, want USD 100.00", gross, err)
	}
	if ratio, err := usd(290).Ratio(usd(10_000)); err != nil || ratio != newRate(290_000_000) {
		t.Fatalf("USD 2.90 / USD 100.00 = %s, %v, want 0.029", ratio, err)
	}
}

func TestRatesInTwoUnitsAdd(t *testing.T) {
	t.Parallel()
	fee, err := Percent("2.9")
	channel, _ := BasisPoints("25")
	if total, _ := fee.Add(channel); err != nil || total.String() != "0.0315" {
		t.Fatalf("2.9%% + 25bps = %s, %v", total, err)
	}
}

// Subtracting the smallest amount from a negative one does not overflow.
func TestSubtractingTheSmallestAmount(t *testing.T) {
	t.Parallel()
	difference, err := usd(-1).Sub(usd(math.MinInt64))
	if err != nil || difference != usd(math.MaxInt64) {
		t.Fatalf("-1 - MinInt64 = %v, %v, want MaxInt64", difference, err)
	}
	if _, err := usd(0).Sub(usd(math.MinInt64)); err == nil {
		t.Fatal("0 - MinInt64 succeeded, want an overflow")
	}
}

func eur(minor int64) Money { return Money{currency: "EUR", minor: minor} }

// The currency-less zero meets every currency, two currencies meet only
// themselves, and the answer names the one they met in.
func TestMoneyMeetsInOneCurrency(t *testing.T) {
	t.Parallel()
	for name, test := range map[string]struct {
		left, right    Money
		sum, diff      Money
		order          int
		currencyErrors bool
	}{
		"two zeros with no currency": {Money{}, Money{}, Money{}, Money{}, 0, false},
		"zero then dollars":          {Money{}, usd(5), usd(5), usd(-5), -1, false},
		"dollars then zero":          {usd(5), Money{}, usd(5), usd(5), 1, false},
		"euros and zero":             {eur(-3), Money{}, eur(-3), eur(-3), -1, false},
		"dollars and dollars":        {usd(7), usd(7), usd(14), usd(0), 0, false},
		"dollars and euros":          {usd(1), eur(1), Money{}, Money{}, 0, true},
		"a dollar zero and euros":    {usd(0), eur(1), Money{}, Money{}, 0, true},
		"lower-case is another code": {usd(1), Money{currency: "usd", minor: 1}, Money{}, Money{}, 0, true},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			sum, sumErr := test.left.Add(test.right)
			diff, diffErr := test.left.Sub(test.right)
			order, orderErr := test.left.Cmp(test.right)
			if test.currencyErrors {
				assertCurrencyErrors(t, test.left, test.right, sumErr, diffErr, orderErr)
				return
			}
			if sumErr != nil || diffErr != nil || orderErr != nil || sum != test.sum || diff != test.diff || order != test.order {
				t.Fatalf("%v with %v: + = %v, %v; - = %v, %v; cmp = %d, %v; want %v, %v, %d",
					test.left, test.right, sum, sumErr, diff, diffErr, order, orderErr, test.sum, test.diff, test.order)
			}
		})
	}
}

func assertCurrencyErrors(t *testing.T, left, right Money, errs ...error) {
	t.Helper()
	for _, err := range errs {
		if !errors.Is(err, ErrCurrency) {
			t.Fatalf("%v with %v: error = %v, want ErrCurrency", left, right, err)
		}
	}
}

// Only nothing is in no currency: an amount a host forgot to label cannot
// be made at all, not even read from JSON, so it never takes on the other
// side's currency.
func TestACurrencyLessAmountIsOnlyZero(t *testing.T) {
	t.Parallel()
	for _, text := range []string{`{"currency":"","minor":500}`, `{"currency":"usd","minor":1}`, `{"minor":1}`, `{"currency":"USD"}`} {
		var m Money
		if err := json.Unmarshal([]byte(text), &m); err == nil {
			t.Fatalf("json.Unmarshal(%s) = %v, want an error", text, m)
		}
	}
	var zero Money
	if err := json.Unmarshal([]byte(`{"currency":"","minor":0}`), &zero); err != nil || zero != (Money{}) {
		t.Fatalf("the currency-less zero from JSON = %v, %v", zero, err)
	}
}

// Sums and differences refuse to wrap at either end of int64.
func TestMoneyAdditionRefusesToWrap(t *testing.T) {
	t.Parallel()
	for name, test := range map[string]struct {
		left, right int64
		add         bool
		want        int64
		overflow    bool
	}{
		"MaxInt64 + 1":        {math.MaxInt64, 1, true, 0, true},
		"MinInt64 + -1":       {math.MinInt64, -1, true, 0, true},
		"MaxInt64 + MinInt64": {math.MaxInt64, math.MinInt64, true, -1, false},
		"MaxInt64 + 0":        {math.MaxInt64, 0, true, math.MaxInt64, false},
		"MinInt64 - 1":        {math.MinInt64, 1, false, 0, true},
		"MaxInt64 - -1":       {math.MaxInt64, -1, false, 0, true},
		"MinInt64 - MinInt64": {math.MinInt64, math.MinInt64, false, 0, false},
		"-2 - MaxInt64":       {-2, math.MaxInt64, false, 0, true},
		"-1 - MaxInt64":       {-1, math.MaxInt64, false, math.MinInt64, false},
		"MaxInt64 - MaxInt64": {math.MaxInt64, math.MaxInt64, false, 0, false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			operation := usd(test.left).Sub
			if test.add {
				operation = usd(test.left).Add
			}
			got, err := operation(usd(test.right))
			if test.overflow != errors.Is(err, errFixedOverflow) || (!test.overflow && (err != nil || got != usd(test.want))) {
				t.Fatalf("%d with %d = %v, %v, want %d (overflow %v)", test.left, test.right, got, err, test.want, test.overflow)
			}
		})
	}
}

func TestMoneySignsAndMagnitudes(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		m         Money
		abs, neg  Money
		sign      int
		zero      bool
		absErrors bool
	}{
		{Money{}, Money{}, Money{}, 0, true, false},
		{usd(0), usd(0), usd(0), 0, true, false},
		{eur(0), eur(0), eur(0), 0, true, false},
		{usd(5), usd(5), usd(-5), 1, false, false},
		{usd(-5), usd(5), usd(5), -1, false, false},
		{usd(math.MaxInt64), usd(math.MaxInt64), usd(-math.MaxInt64), 1, false, false},
		{usd(math.MinInt64 + 1), usd(math.MaxInt64), usd(math.MaxInt64), -1, false, false},
		{usd(math.MinInt64), Money{}, Money{}, -1, false, true},
	} {
		abs, absErr := test.m.Abs()
		neg, negErr := test.m.Neg()
		if test.m.Sign() != test.sign || test.m.IsZero() != test.zero {
			t.Fatalf("%v: Sign = %d, IsZero = %v, want %d, %v", test.m, test.m.Sign(), test.m.IsZero(), test.sign, test.zero)
		}
		if test.absErrors {
			if !errors.Is(absErr, errFixedOverflow) || !errors.Is(negErr, errFixedOverflow) {
				t.Fatalf("%v: Abs error = %v, Neg error = %v, want overflow", test.m, absErr, negErr)
			}
			continue
		}
		if absErr != nil || negErr != nil || abs != test.abs || neg != test.neg {
			t.Fatalf("%v: Abs = %v, %v; Neg = %v, %v; want %v, %v", test.m, abs, absErr, neg, negErr, test.abs, test.neg)
		}
	}
}

func TestMoneyTimesACountIsExact(t *testing.T) {
	t.Parallel()
	for name, test := range map[string]struct {
		m        Money
		count    int64
		want     Money
		overflow bool
	}{
		"a negative count":     {usd(3), -2, usd(-6), false},
		"times zero":           {usd(math.MaxInt64), 0, usd(0), false},
		"a zero with no unit":  {Money{}, 7, Money{}, false},
		"MinInt64 times one":   {usd(math.MinInt64), 1, usd(math.MinInt64), false},
		"MinInt64 negated":     {usd(math.MinInt64), -1, Money{}, true},
		"MaxInt64 doubled":     {usd(math.MaxInt64), 2, Money{}, true},
		"half of MinInt64 x2":  {usd(math.MinInt64 / 2), 2, usd(math.MinInt64), false},
		"past MinInt64 by one": {usd(math.MinInt64/2 - 1), 2, Money{}, true},
		"MaxInt64 times -1":    {usd(math.MaxInt64), -1, usd(-math.MaxInt64), false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, err := test.m.MulInt(test.count)
			if test.overflow != errors.Is(err, errFixedOverflow) || (!test.overflow && (err != nil || got != test.want)) {
				t.Fatalf("%v.MulInt(%d) = %v, %v, want %v (overflow %v)", test.m, test.count, got, err, test.want, test.overflow)
			}
		})
	}
}

// A rate lands on the minor unit once, by the mode the caller names, and a
// negative amount rounds by the same rule as a positive one.
func TestMoneyTimesARateByEveryMode(t *testing.T) {
	t.Parallel()
	twoAndAHalf := newRate(250_000_000)
	for mode, want := range map[Rounding][4]int64{ // USD 1.00, USD -1.00, USD 3.00, USD -3.00 times 2.5%
		RoundHalfEven: {2, -2, 8, -8},
		RoundHalfUp:   {3, -3, 8, -8},
		RoundHalfDown: {2, -2, 7, -7},
		RoundDown:     {2, -2, 7, -7},
		RoundUp:       {3, -3, 8, -8},
		RoundCeiling:  {3, -2, 8, -7},
		RoundFloor:    {2, -3, 7, -8},
	} {
		t.Run(mode.String(), func(t *testing.T) {
			t.Parallel()
			for i, amount := range []int64{100, -100, 300, -300} {
				assertRateProduct(t, usd(amount), twoAndAHalf, mode, usd(want[i]))
			}
		})
	}
}

func assertRateProduct(t *testing.T, m Money, rate Rate, mode Rounding, want Money) {
	t.Helper()
	if got, err := m.MulRate(rate, mode); err != nil || got != want {
		t.Fatalf("%v.MulRate(%s, %s) = %v, %v, want %v", m, rate, mode, got, err, want)
	}
}

func TestMoneyAndRateEdges(t *testing.T) {
	t.Parallel()
	for name, test := range map[string]struct {
		run  func() (Money, error)
		want Money
		fail string // "" succeeds, "overflow" or an error text
	}{
		"times a negative rate": {func() (Money, error) { return usd(100).MulRate(newRate(-250_000_000), RoundHalfEven) }, usd(-2), ""},
		"times zero":            {func() (Money, error) { return usd(100).MulRate(newRate(0), RoundUp) }, usd(0), ""},
		"zero with no unit":     {func() (Money, error) { return Money{}.MulRate(newRate(RateScale), RoundUp) }, Money{}, ""},
		"times the finest rate": {func() (Money, error) { return usd(1).MulRate(newRate(1), RoundUp) }, usd(1), ""},
		"past MaxInt64":         {func() (Money, error) { return usd(math.MaxInt64).MulRate(newRate(2*RateScale), RoundDown) }, Money{}, "overflow"},
		"MinInt64 times one":    {func() (Money, error) { return usd(math.MinInt64).MulRate(newRate(RateScale), RoundDown) }, usd(math.MinInt64), ""},
		"MinInt64 times -1":     {func() (Money, error) { return usd(math.MinInt64).MulRate(newRate(-RateScale), RoundDown) }, Money{}, "overflow"},
		"over a zero rate":      {func() (Money, error) { return usd(100).DivRate(newRate(0), RoundUp) }, Money{}, "division by zero"},
		"over a negative rate":  {func() (Money, error) { return usd(100).DivRate(newRate(-RateScale/2), RoundUp) }, usd(-200), ""},
		"over a third":          {func() (Money, error) { return usd(100).DivRate(newRate(3_333_333_333), RoundHalfEven) }, usd(300), ""},
		"over the finest rate":  {func() (Money, error) { return usd(1_000_000_000).DivRate(newRate(1), RoundDown) }, Money{}, "overflow"},
		"zero over a rate":      {func() (Money, error) { return Money{}.DivRate(newRate(7), RoundUp) }, Money{}, ""},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, err := test.run()
			assertMoneyOutcome(t, got, err, test.want, test.fail)
		})
	}
}

func assertMoneyOutcome(t *testing.T, got Money, err error, want Money, fail string) {
	t.Helper()
	switch {
	case fail == "overflow":
		if !errors.Is(err, errFixedOverflow) {
			t.Fatalf("got %v, %v, want an overflow", got, err)
		}
	case fail != "":
		if err == nil || !strings.Contains(err.Error(), fail) {
			t.Fatalf("got %v, %v, want an error containing %q", got, err, fail)
		}
	case err != nil || got != want:
		t.Fatalf("got %v, %v, want %v", got, err, want)
	}
}

func TestMoneyRatioIsARate(t *testing.T) {
	t.Parallel()
	for name, test := range map[string]struct {
		left, right Money
		want        Rate
		fail        string
	}{
		"a third rounds down":      {usd(1), usd(3), newRate(3_333_333_333), ""},
		"two thirds round up":      {usd(2), usd(3), newRate(6_666_666_667), ""},
		"half of the finest, even": {usd(1), usd(20_000_000_000), newRate(0), ""},
		"three halves, even":       {usd(3), usd(20_000_000_000), newRate(2), ""},
		"a negative fee":           {usd(-290), usd(10_000), newRate(-290_000_000), ""},
		"both negative":            {usd(-1), usd(-4), newRate(RateScale / 4), ""},
		"zero with no unit":        {Money{}, usd(7), newRate(0), ""},
		"over zero":                {usd(1), usd(0), newRate(0), "division by zero"},
		"over a currency-less 0":   {usd(1), Money{}, newRate(0), "division by zero"},
		"two currencies":           {usd(1), eur(1), newRate(0), "currency"},
		"past the largest rate":    {usd(math.MaxInt64), usd(1), newRate(0), "overflows"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, err := test.left.Ratio(test.right)
			if (test.fail == "") != (err == nil) || (err != nil && !strings.Contains(err.Error(), test.fail)) || got != test.want && err == nil {
				t.Fatalf("%v.Ratio(%v) = %s, %v, want %s (error %q)", test.left, test.right, got, err, test.want, test.fail)
			}
		})
	}
	if _, err := usd(1).Ratio(eur(1)); !errors.Is(err, ErrCurrency) {
		t.Fatalf("USD.Ratio(EUR) error = %v, want ErrCurrency", err)
	}
}

func assertFairShares(t *testing.T, m Money, weights []int64, shares []Money) {
	t.Helper()
	total, sum := new(big.Int), new(big.Int)
	for _, weight := range weights {
		total.Add(total, big.NewInt(weight))
	}
	for i, share := range shares {
		sum.Add(sum, big.NewInt(share.minor))
		// |share·total − m·weight| < total: within one unit of the exact part.
		gap := new(big.Int).Sub(new(big.Int).Mul(big.NewInt(share.minor), total), new(big.Int).Mul(big.NewInt(m.minor), big.NewInt(weights[i])))
		if gap.Abs(gap).Cmp(total) >= 0 || share.Sign()*m.Sign() < 0 || (weights[i] == 0 && share.minor != 0) {
			t.Fatalf("%v.Allocate(%v)[%d] = %v, not within a unit of its part", m, weights, i, share)
		}
	}
	if !sum.IsInt64() || sum.Int64() != m.minor {
		t.Fatalf("%v.Allocate(%v) = %v, adds up to %s", m, weights, shares, sum)
	}
}

func TestRateArithmetic(t *testing.T) {
	t.Parallel()
	half := newRate(RateScale / 2)
	for name, test := range map[string]struct {
		run  func() (Rate, error)
		want Rate
		fail string
	}{
		"a sum":                    {func() (Rate, error) { return newRate(1).Add(newRate(2)) }, newRate(3), ""},
		"a sum past int64":         {func() (Rate, error) { return newRate(math.MaxInt64).Add(newRate(1)) }, newRate(0), "overflows"},
		"a sum below int64":        {func() (Rate, error) { return newRate(math.MinInt64).Add(newRate(-1)) }, newRate(0), "overflows"},
		"a difference":             {func() (Rate, error) { return newRate(1).Sub(newRate(3)) }, newRate(-2), ""},
		"a difference below int64": {func() (Rate, error) { return newRate(math.MinInt64).Sub(newRate(1)) }, newRate(0), "overflows"},
		"a difference past int64":  {func() (Rate, error) { return newRate(0).Sub(newRate(math.MinInt64)) }, newRate(0), "overflows"},
		"a product":                {func() (Rate, error) { return newRate(290_000_000).Mul(half) }, newRate(145_000_000), ""},
		"half a unit, even down":   {func() (Rate, error) { return newRate(1).Mul(half) }, newRate(0), ""},
		"a unit and a half, up":    {func() (Rate, error) { return newRate(3).Mul(half) }, newRate(2), ""},
		"a negative half, even":    {func() (Rate, error) { return newRate(-3).Mul(half) }, newRate(-2), ""},
		"a product past int64":     {func() (Rate, error) { return newRate(math.MaxInt64).Mul(newRate(2 * RateScale)) }, newRate(0), "overflows"},
		"times one":                {func() (Rate, error) { return newRate(math.MinInt64).Mul(newRate(RateScale)) }, newRate(math.MinInt64), ""},
		"a quotient":               {func() (Rate, error) { return newRate(RateScale).Div(newRate(3 * RateScale)) }, newRate(3_333_333_333), ""},
		"a half quotient, even":    {func() (Rate, error) { return newRate(1).Div(newRate(2 * RateScale)) }, newRate(0), ""},
		"a quotient rounding up":   {func() (Rate, error) { return newRate(3).Div(newRate(2 * RateScale)) }, newRate(2), ""},
		"over zero":                {func() (Rate, error) { return newRate(1).Div(newRate(0)) }, newRate(0), "division by zero"},
		"over the finest rate":     {func() (Rate, error) { return newRate(RateScale).Div(newRate(1)) }, newRate(0), "overflows"},
		"a negative quotient":      {func() (Rate, error) { return newRate(RateScale).Div(newRate(-4 * RateScale)) }, newRate(-RateScale / 4), ""},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, err := test.run()
			if (test.fail == "") != (err == nil) || (err != nil && !strings.Contains(err.Error(), test.fail)) || (err == nil && got != test.want) {
				t.Fatalf("got %d, %v, want %d (error %q)", got.scaled, err, test.want.scaled, test.fail)
			}
		})
	}
}

func TestRatesInTheirUnits(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		percent bool
		value   string
		want    Rate
	}{
		{true, "2.9", newRate(290_000_000)}, {true, "-1", newRate(-100_000_000)}, {true, "100", newRate(RateScale)},
		{true, "0.00000001", newRate(1)}, {true, "+0.5", newRate(50_000_000)}, {false, "25", newRate(25_000_000)},
		{false, "0.5", newRate(500_000)}, {false, "0.000001", newRate(1)}, {false, "10000", newRate(RateScale)}, {false, "-3", newRate(-3_000_000)},
	} {
		parse, unit := BasisPoints, "BasisPoints"
		if test.percent {
			parse, unit = Percent, "Percent"
		}
		if got, err := parse(test.value); err != nil || got != test.want {
			t.Fatalf("%s(%q) = %d, %v, want %d", unit, test.value, got.scaled, err, test.want.scaled)
		}
	}
	for _, value := range []string{"0.000000001", "", "2.9%", "abc", "1e2"} {
		if got, err := Percent(value); err == nil {
			t.Fatalf("Percent(%q) = %s, want an error", value, got)
		}
	}
	for _, value := range []string{"0.0000001", "25bps", " 25"} {
		if got, err := BasisPoints(value); err == nil {
			t.Fatalf("BasisPoints(%q) = %s, want an error", value, got)
		}
	}
}

// Go gets the errors a rule gets: a method checks what a host hands it the
// way the boundary checks what a rule is given.
func TestMethodsCheckWhatTheyAreHanded(t *testing.T) {
	t.Parallel()
	for name, test := range map[string]struct {
		call func() error
		want error
	}{
		"money split too finely":   {func() error { _, err := (Money{currency: "USD", minor: 5}).Split(10_001); return err }, ErrArithmetic},
		"money split into nothing": {func() error { _, err := (Money{currency: "USD", minor: 5}).Split(0); return err }, ErrArithmetic},
		"a negative weight":        {func() error { _, err := (Money{currency: "USD", minor: 5}).Allocate(1, -1); return err }, ErrArithmetic},
		"no weight":                {func() error { _, err := (Money{currency: "USD", minor: 5}).Allocate(0); return err }, ErrArithmetic},
		"an unknown rounding": {func() error {
			_, err := (Money{currency: "USD", minor: 5}).MulRate(newRate(RateScale/3), Rounding(99))
			return err
		}, ErrArithmetic},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if err := test.call(); !errors.Is(err, test.want) {
				t.Fatalf("%s: error = %v, want %v", name, err, test.want)
			}
		})
	}
}

// Prorate is exact in between and rounds once: a fee of USD 30 billion
// refunded by a third is USD 10 billion to the cent, where taking the ratio
// first, as a rate of ten places, loses a dollar.
func TestProrateRoundsOnce(t *testing.T) {
	t.Parallel()
	fee := usd(3_000_000_000_000)
	share, err := fee.ProrateBy(usd(100), usd(300), RoundHalfUp)
	if err != nil || share.minor != 1_000_000_000_000 {
		t.Fatalf("prorate(USD 30 billion, 1, 3) = %v, %v, want USD 10 billion", share, err)
	}
	ratio, _ := usd(100).Ratio(usd(300))
	viaRate, _ := fee.MulRate(ratio, RoundHalfUp)
	if viaRate.minor == share.minor {
		t.Fatalf("the rate first gives %v too; the case shows nothing", viaRate)
	}
	for _, test := range []struct {
		part, whole int64
		mode        Rounding
		want        int64
	}{{1, 3, RoundHalfUp, 3333}, {2, 3, RoundHalfUp, 6667}, {2, 3, RoundDown, 6666}, {1, 1, RoundHalfUp, 10000}} {
		if got, err := usd(10000).Prorate(test.part, test.whole, test.mode); err != nil || got.minor != test.want {
			t.Errorf("USD 100 × %d/%d by %s = %v, %v, want %d", test.part, test.whole, test.mode, got, err, test.want)
		}
	}
	if _, err := usd(1).Prorate(1, 0, RoundHalfUp); !errors.Is(err, ErrArithmetic) {
		t.Fatalf("prorate over zero: error = %v, want ErrArithmetic", err)
	}
	if _, err := usd(1).ProrateBy(usd(1), Money{currency: "EUR", minor: 2}, RoundHalfUp); !errors.Is(err, ErrCurrency) {
		t.Fatalf("a proportion of two currencies: error = %v, want ErrCurrency", err)
	}
}

// RoundTo takes an amount to a whole number of steps of its currency: cash
// rounding. A step of zero or less, or in another currency, is refused.
func TestRoundToTakesWholeSteps(t *testing.T) {
	t.Parallel()
	chf := func(minor int64) Money { return Money{currency: "CHF", minor: minor} }
	for _, test := range []struct {
		m, step Money
		mode    Rounding
		want    int64
	}{
		{chf(103), chf(5), RoundHalfUp, 105},
		{chf(103), chf(5), RoundDown, 100},
		{chf(102), chf(5), RoundHalfUp, 100},
		{chf(-103), chf(5), RoundFloor, -105},
		{chf(-103), chf(5), RoundCeiling, -100},
		{chf(1234), chf(100), RoundHalfEven, 1200},
		{Money{}, chf(5), RoundHalfUp, 0},
	} {
		if got, err := test.m.RoundTo(test.step, test.mode); err != nil || got.minor != test.want || got.currency != "CHF" {
			t.Errorf("%v.RoundTo(%v, %s) = %v, %v, want CHF %d", test.m, test.step, test.mode, got, err, test.want)
		}
	}
	for _, step := range []Money{chf(0), chf(-5)} {
		if _, err := chf(100).RoundTo(step, RoundHalfUp); !errors.Is(err, ErrArithmetic) {
			t.Errorf("RoundTo(%v) error = %v, want ErrArithmetic", step, err)
		}
	}
	if _, err := chf(100).RoundTo(usd(5), RoundHalfUp); !errors.Is(err, ErrCurrency) {
		t.Fatalf("a step in another currency: error = %v, want ErrCurrency", err)
	}
}
