package money

import (
	"errors"
	"math"
	"math/big"
	"math/rand/v2"
	"testing"
)

// An average or an even count's median rounds once, by the mode: a tie by
// half_even goes to the even neighbour on either side of zero, and the ends
// of int64 average without overflowing.
func TestAveragesRoundOnceByTheMode(t *testing.T) {
	t.Parallel()
	for name, test := range map[string]struct {
		minors []int64
		mode   Rounding
		want   int64
	}{
		"1.5 to even":           {[]int64{1, 2}, RoundHalfEven, 2},
		"2.5 to even":           {[]int64{2, 3}, RoundHalfEven, 2},
		"-1.5 to even":          {[]int64{-1, -2}, RoundHalfEven, -2},
		"-2.5 to even":          {[]int64{-2, -3}, RoundHalfEven, -2},
		"0.5 to even":           {[]int64{0, 1}, RoundHalfEven, 0},
		"-0.5 to even":          {[]int64{0, -1}, RoundHalfEven, 0},
		"2.5 half up":           {[]int64{2, 3}, RoundHalfUp, 3},
		"-2.5 half up":          {[]int64{-2, -3}, RoundHalfUp, -3},
		"2.5 half down":         {[]int64{2, 3}, RoundHalfDown, 2},
		"-1/3 floor":            {[]int64{0, 0, -1}, RoundFloor, -1},
		"-1/3 ceiling":          {[]int64{0, 0, -1}, RoundCeiling, 0},
		"2/3 down":              {[]int64{1, 1, 0}, RoundDown, 0},
		"2/3 up":                {[]int64{1, 1, 0}, RoundUp, 1},
		"the largest twice":     {[]int64{math.MaxInt64, math.MaxInt64}, RoundHalfEven, math.MaxInt64},
		"the smallest twice":    {[]int64{math.MinInt64, math.MinInt64}, RoundHalfEven, math.MinInt64},
		"both ends, to even":    {[]int64{math.MaxInt64, math.MinInt64}, RoundHalfEven, 0},
		"both ends, floor":      {[]int64{math.MaxInt64, math.MinInt64}, RoundFloor, -1},
		"both ends, half up":    {[]int64{math.MaxInt64, math.MinInt64}, RoundHalfUp, -1},
		"both ends, half down":  {[]int64{math.MaxInt64, math.MinInt64}, RoundHalfDown, 0},
		"near the top, ceiling": {[]int64{math.MaxInt64, math.MaxInt64 - 1}, RoundCeiling, math.MaxInt64},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			amounts := make([]Money, len(test.minors))
			for i, minor := range test.minors {
				amounts[i] = usd(minor)
			}
			if got, err := AverageMoney(amounts, test.mode); err != nil || got != usd(test.want) {
				t.Errorf("AverageMoney(%v, %s) = %v, %v, want %v", test.minors, test.mode, got, err, usd(test.want))
			}
		})
	}
}

// A median of an odd count is the middle amount; of an even count, the mean
// of the middle two rounded by the mode; in any order.
func TestMediansTakeTheMiddle(t *testing.T) {
	t.Parallel()
	for name, test := range map[string]struct {
		minors []int64
		mode   Rounding
		want   int64
	}{
		"odd":                 {[]int64{5, 1, 3}, RoundHalfEven, 3},
		"one":                 {[]int64{-7}, RoundFloor, -7},
		"even, to even":       {[]int64{4, 1, 3, 2}, RoundHalfEven, 2},
		"even, half up":       {[]int64{4, 1, 3, 2}, RoundHalfUp, 3},
		"even, negative":      {[]int64{-4, -1, -3, -2}, RoundHalfEven, -2},
		"even, at both ends":  {[]int64{math.MinInt64, math.MaxInt64}, RoundCeiling, 0},
		"even, equal middles": {[]int64{9, 2, 2, 0}, RoundUp, 2},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			amounts := make([]Money, len(test.minors))
			for i, minor := range test.minors {
				amounts[i] = usd(minor)
			}
			if got, err := MedianMoney(amounts, test.mode); err != nil || got != usd(test.want) {
				t.Errorf("MedianMoney(%v, %s) = %v, %v, want %v", test.minors, test.mode, got, err, usd(test.want))
			}
		})
	}
}

// Nothing has no average; two currencies have none either, while the
// currency-less zero goes with any.
func TestAveragesNeedAmountsInOneCurrency(t *testing.T) {
	t.Parallel()
	if _, err := AverageMoney(nil, RoundHalfEven); !errors.Is(err, ErrDomain) {
		t.Errorf("AverageMoney(none) error = %v, want ErrDomain", err)
	}
	if _, err := MedianMoney([]Money{}, RoundHalfEven); !errors.Is(err, ErrDomain) {
		t.Errorf("MedianMoney(none) error = %v, want ErrDomain", err)
	}
	if _, err := AverageMoney([]Money{usd(1), {currency: "EUR", minor: 1}}, RoundHalfEven); !errors.Is(err, ErrCurrency) {
		t.Errorf("AverageMoney(USD and EUR) error = %v, want ErrCurrency", err)
	}
	if got, err := MedianMoney([]Money{{}, usd(4), usd(2)}, RoundHalfEven); err != nil || got != usd(2) {
		t.Errorf("MedianMoney(0, USD 0.04, USD 0.02) = %v, %v, want USD 0.02", got, err)
	}
}

// Whatever the amounts and the mode, the average is the exact mean rounded
// once, as big.Rat rounds it.
func TestAverageAgreesWithBigRat(t *testing.T) {
	t.Parallel()
	rng := rand.New(rand.NewPCG(5, 8))
	edges := []int64{math.MaxInt64, math.MinInt64, math.MaxInt64 - 1, math.MinInt64 + 1, 0, 1, -1}
	modes := []Rounding{RoundHalfEven, RoundHalfUp, RoundHalfDown, RoundDown, RoundUp, RoundCeiling, RoundFloor}
	for range 20000 {
		minors := make([]int64, 1+rng.IntN(6))
		for i := range minors {
			if rng.IntN(3) == 0 {
				minors[i] = edges[rng.IntN(len(edges))]
			} else {
				minors[i] = rng.Int64N(2001) - 1000
			}
		}
		amounts := make([]Money, len(minors))
		sum := new(big.Int)
		for i, minor := range minors {
			amounts[i] = usd(minor)
			sum.Add(sum, big.NewInt(minor))
		}
		mode := modes[rng.IntN(len(modes))]
		want, _ := bigRoundFrac(sum, big.NewInt(int64(len(minors))), mode)
		if got, err := AverageMoney(amounts, mode); err != nil || got.minor != want {
			t.Fatalf("AverageMoney(%v, %s) = %v, %v, want %d minor units", minors, mode, got, err, want)
		}
	}
}
