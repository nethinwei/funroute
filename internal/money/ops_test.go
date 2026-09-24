package money

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

func TestMoneyTimesARatioRoundsOnce(t *testing.T) {
	t.Parallel()
	fee, err := usd(100).MulRatio(ratio(1, 40), RoundHalfEven) // 2.5%, half a cent
	if err != nil || fee != usd(2) {
		t.Fatalf("USD 1.00 * 2.5%% half even = %v, %v, want USD 0.02", fee, err)
	}
	fee, _ = usd(100).MulRatio(ratio(1, 40), RoundHalfUp)
	if fee != usd(3) {
		t.Fatalf("USD 1.00 * 2.5%% half up = %v, want USD 0.03", fee)
	}
	gross, err := usd(9710).DivRatio(ratio(971, 1000), RoundHalfUp) // net / 97.1%
	if err != nil || gross != usd(10_000) {
		t.Fatalf("USD 97.10 / 97.1%% = %v, %v, want USD 100.00", gross, err)
	}
	if got, err := usd(290).Ratio(usd(10_000)); err != nil || got != ratio(29, 1000) {
		t.Fatalf("USD 2.90 / USD 100.00 = %s, %v, want 0.029", got, err)
	}
}

func TestRatiosInTwoUnitsAdd(t *testing.T) {
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

// Money times a ratio lands on the minor unit once, by the mode the caller
// names, and a negative amount rounds by the same rule as a positive one.
func TestMoneyTimesARatioByEveryMode(t *testing.T) {
	t.Parallel()
	twoAndAHalf := ratio(1, 40)
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
				assertRatioProduct(t, usd(amount), twoAndAHalf, mode, usd(want[i]))
			}
		})
	}
}

func assertRatioProduct(t *testing.T, m Money, r Ratio, mode Rounding, want Money) {
	t.Helper()
	if got, err := m.MulRatio(r, mode); err != nil || got != want {
		t.Fatalf("%v.MulRatio(%s, %s) = %v, %v, want %v", m, r, mode, got, err, want)
	}
}

func TestMoneyAndRatioEdges(t *testing.T) {
	t.Parallel()
	for name, test := range map[string]struct {
		run  func() (Money, error)
		want Money
		fail string // "" succeeds, "overflow" or an error text
	}{
		"times a negative rate": {func() (Money, error) { return usd(100).MulRatio(ratio(-1, 40), RoundHalfEven) }, usd(-2), ""},
		"times zero":            {func() (Money, error) { return usd(100).MulRatio(ratio(0, 1), RoundUp) }, usd(0), ""},
		"zero with no unit":     {func() (Money, error) { return Money{}.MulRatio(ratio(1, 1), RoundUp) }, Money{}, ""},
		"times the finest rate": {func() (Money, error) { return usd(1).MulRatio(ratio(1, 10_000_000_000), RoundUp) }, usd(1), ""},
		"past MaxInt64":         {func() (Money, error) { return usd(math.MaxInt64).MulRatio(ratio(2, 1), RoundDown) }, Money{}, "overflow"},
		"MinInt64 times one":    {func() (Money, error) { return usd(math.MinInt64).MulRatio(ratio(1, 1), RoundDown) }, usd(math.MinInt64), ""},
		"MinInt64 times -1":     {func() (Money, error) { return usd(math.MinInt64).MulRatio(ratio(-1, 1), RoundDown) }, Money{}, "overflow"},
		"over a zero rate":      {func() (Money, error) { return usd(100).DivRatio(ratio(0, 1), RoundUp) }, Money{}, "division by zero"},
		"over a negative rate":  {func() (Money, error) { return usd(100).DivRatio(ratio(-1, 2), RoundUp) }, usd(-200), ""},
		"over a third":          {func() (Money, error) { return usd(100).DivRatio(ratio(3_333_333_333, 10_000_000_000), RoundHalfEven) }, usd(300), ""},
		"over the finest rate":  {func() (Money, error) { return usd(1_000_000_000).DivRatio(ratio(1, 10_000_000_000), RoundDown) }, Money{}, "overflow"},
		"zero over a rate":      {func() (Money, error) { return Money{}.DivRatio(ratio(7, 10_000_000_000), RoundUp) }, Money{}, ""},
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

func TestMoneyRatioIsARatio(t *testing.T) {
	t.Parallel()
	for name, test := range map[string]struct {
		left, right Money
		want        Ratio
		fail        string
	}{
		"a third":                {usd(1), usd(3), ratio(1, 3), ""},
		"two thirds":             {usd(2), usd(3), ratio(2, 3), ""},
		"a sliver":               {usd(1), usd(20_000_000_000), ratio(1, 20_000_000_000), ""},
		"a negative fee":         {usd(-290), usd(10_000), ratio(-29, 1000), ""},
		"both negative":          {usd(-1), usd(-4), ratio(1, 4), ""},
		"zero with no unit":      {Money{}, usd(7), ratio(0, 1), ""},
		"over zero":              {usd(1), usd(0), ratio(0, 1), "division by zero"},
		"over a currency-less 0": {usd(1), Money{}, ratio(0, 1), "division by zero"},
		"two currencies":         {usd(1), eur(1), ratio(0, 1), "currency"},
		"the largest amount":     {usd(math.MaxInt64), usd(1), ratio(math.MaxInt64, 1), ""},
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
			_, err := (Money{currency: "USD", minor: 5}).MulRatio(ratio(1, 3), Rounding(99))
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
// refunded by a third is USD 10 billion to the cent, and so is taking the
// ratio first: a ratio is exact, so a third is a third.
func TestProrateRoundsOnce(t *testing.T) {
	t.Parallel()
	fee := usd(3_000_000_000_000)
	share, err := fee.ProrateBy(usd(100), usd(300), RoundHalfUp)
	if err != nil || share.minor != 1_000_000_000_000 {
		t.Fatalf("prorate(USD 30 billion, 1, 3) = %v, %v, want USD 10 billion", share, err)
	}
	ratio, _ := usd(100).Ratio(usd(300))
	viaRatio, _ := fee.MulRatio(ratio, RoundHalfUp)
	if viaRatio.minor != share.minor {
		t.Fatalf("the rate first gives %v, want %v: a third is exact", viaRatio, share)
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
