package money

import (
	"errors"
	"math"
	"math/big"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"
)

// mulDivRound agrees with exact rational arithmetic in every mode, including
// the values at the edges of int64.
func TestMulDivRoundIsExactThenRoundedOnce(t *testing.T) {
	t.Parallel()
	edges := make([]int64, 0, 17+2000)
	edges = append(edges, 0, 1, -1, 2, -2, 3, 5, -5, 7, 10, 999, 1_000_000_007, 10_000_000_000, math.MaxInt64, math.MinInt64, math.MaxInt64/3, math.MinInt64/7)
	random := rand.New(rand.NewPCG(1, 2))
	for range 2000 {
		value := random.Int64() >> random.IntN(63)
		if random.IntN(2) == 0 {
			value = -value
		}
		edges = append(edges, value)
	}
	for mode := RoundHalfEven; mode <= RoundFloor; mode++ {
		t.Run(mode.String(), func(t *testing.T) {
			t.Parallel()
			for i := 0; i+2 < len(edges); i++ {
				assertMulDivRound(t, edges[i], edges[i+1], edges[i+2], mode)
			}
		})
	}
}

func assertMulDivRound(t *testing.T, a, b, d int64, mode Rounding) {
	t.Helper()
	got, err := mulDivRound(a, b, d, mode)
	want, fits := refMulDivRound(a, b, d, mode)
	switch {
	case d == 0:
		if err == nil {
			t.Fatalf("mulDivRound(%d, %d, 0) = %d, want division by zero", a, b, got)
		}
	case !fits:
		if !errors.Is(err, errFixedOverflow) {
			t.Fatalf("mulDivRound(%d, %d, %d, %s) = %d, %v, want overflow", a, b, d, mode, got, err)
		}
	case err != nil || got != want:
		t.Fatalf("mulDivRound(%d, %d, %d, %s) = %d, %v, want %d", a, b, d, mode, got, err, want)
	}
}

// refMulDivRound is the reference: a·b/d as a rational, rounded by mode.
func refMulDivRound(a, b, d int64, mode Rounding) (int64, bool) {
	if d == 0 {
		return 0, false
	}
	return bigRoundFrac(new(big.Int).Mul(big.NewInt(a), big.NewInt(b)), big.NewInt(d), mode)
}

// bigRoundFrac rounds num/den by mode and reports whether it fits int64: the
// reference every exact money operation is checked against.
func bigRoundFrac(num, den *big.Int, mode Rounding) (int64, bool) {
	exact := new(big.Rat).SetFrac(num, den)
	floor := new(big.Int).Div(exact.Num(), exact.Denom()) // Euclidean: floor for a positive denominator
	remainder := new(big.Rat).Sub(exact, new(big.Rat).SetInt(floor))
	half := big.NewRat(1, 2)
	up := false
	switch mode {
	case RoundFloor:
	case RoundCeiling:
		up = remainder.Sign() > 0
	case RoundDown:
		up = remainder.Sign() > 0 && exact.Sign() < 0
	case RoundUp:
		up = remainder.Sign() > 0 && exact.Sign() > 0
	default:
		comparison := remainder.Cmp(half)
		up = comparison > 0 || (comparison == 0 && halfGoesUp(floor, exact, mode))
	}
	if up {
		floor.Add(floor, big.NewInt(1))
	}
	return floor.Int64(), floor.IsInt64()
}

// halfGoesUp settles an exact half between floor and floor+1.
func halfGoesUp(floor *big.Int, exact *big.Rat, mode Rounding) bool {
	switch mode {
	case RoundHalfUp:
		return exact.Sign() > 0
	case RoundHalfDown:
		return exact.Sign() < 0
	default:
		return floor.Bit(0) == 1
	}
}

func TestRoundingNamesRoundTrip(t *testing.T) {
	t.Parallel()
	for _, name := range RoundingModes() {
		mode, err := ParseRounding(name)
		if err != nil || mode.String() != name {
			t.Fatalf("ParseRounding(%q) = %s, %v", name, mode, err)
		}
	}
	if _, err := ParseRounding("nearest"); err == nil {
		t.Fatal(`ParseRounding("nearest") succeeded, want an error`)
	}
}

// Every mode settles the same ten tenths, the ties at ±0.5, ±1.5 and ±2.5
// included, and the sign may come from any of the three operands.
func TestEveryRoundingModeSettlesTiesItsOwnWay(t *testing.T) {
	t.Parallel()
	tenths := []int64{-25, -15, -6, -5, -4, 4, 5, 6, 15, 25}
	for mode, want := range map[Rounding][]int64{
		RoundHalfEven: {-2, -2, -1, 0, 0, 0, 0, 1, 2, 2},
		RoundHalfUp:   {-3, -2, -1, -1, 0, 0, 1, 1, 2, 3},
		RoundHalfDown: {-2, -1, -1, 0, 0, 0, 0, 1, 1, 2},
		RoundDown:     {-2, -1, 0, 0, 0, 0, 0, 0, 1, 2},
		RoundUp:       {-3, -2, -1, -1, -1, 1, 1, 1, 2, 3},
		RoundCeiling:  {-2, -1, 0, 0, 0, 1, 1, 1, 2, 3},
		RoundFloor:    {-3, -2, -1, -1, -1, 0, 0, 0, 1, 2},
	} {
		t.Run(mode.String(), func(t *testing.T) {
			t.Parallel()
			for i, n := range tenths {
				assertTenthRounds(t, n, mode, want[i])
			}
		})
	}
}

// assertTenthRounds checks n/10 written with the sign on a, on b and on d.
func assertTenthRounds(t *testing.T, n int64, mode Rounding, want int64) {
	t.Helper()
	for _, operands := range [][3]int64{{n, 1, 10}, {-n, 1, -10}, {1, n, 10}, {-n, -1, 10}, {n, -1, -10}} {
		got, err := mulDivRound(operands[0], operands[1], operands[2], mode)
		if err != nil || got != want {
			t.Fatalf("mulDivRound(%d, %d, %d, %s) = %d, %v, want %d", operands[0], operands[1], operands[2], mode, got, err, want)
		}
	}
}

// The edges of int64: MinInt64 is reachable, its negation is not, and a
// quotient of exactly 2^64-1 that rounds up does not wrap to zero.
func TestMulDivRoundAtTheEdgesOfInt64(t *testing.T) {
	t.Parallel()
	for name, test := range map[string]struct {
		a, b, d  int64
		mode     Rounding
		want     int64
		overflow bool
	}{
		"MinInt64 times one":          {math.MinInt64, 1, 1, RoundDown, math.MinInt64, false},
		"MinInt64 negated":            {math.MinInt64, -1, 1, RoundDown, 0, true},
		"MinInt64 over minus one":     {math.MinInt64, 1, -1, RoundDown, 0, true},
		"MinInt64 twice negated":      {math.MinInt64, -1, -1, RoundDown, math.MinInt64, false},
		"MaxInt64 over minus one":     {math.MaxInt64, 1, -1, RoundDown, -math.MaxInt64, false},
		"MaxInt64 squared over it":    {math.MaxInt64, math.MaxInt64, math.MaxInt64, RoundUp, math.MaxInt64, false},
		"MinInt64 squared over it":    {math.MinInt64, math.MinInt64, math.MinInt64, RoundUp, math.MinInt64, false},
		"MaxInt64 plus a rounding":    {math.MaxInt64, 3, 2, RoundDown, 0, true},
		"one past MaxInt64 by up":     {math.MaxInt64, 2, 2, RoundUp, math.MaxInt64, false},
		"a quotient of 2^64-1 up":     {31, 1190112520884487201, 2, RoundUp, 0, true},
		"a quotient of 2^64-1 down":   {31, 1190112520884487201, 2, RoundDown, 0, true},
		"zero times anything":         {0, math.MinInt64, -7, RoundUp, 0, false},
		"anything times zero":         {math.MaxInt64, 0, 3, RoundCeiling, 0, false},
		"MinInt64 halved, rounded up": {math.MinInt64, 1, 2, RoundUp, math.MinInt64 / 2, false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, err := mulDivRound(test.a, test.b, test.d, test.mode)
			if test.overflow != errors.Is(err, errFixedOverflow) || (!test.overflow && (err != nil || got != test.want)) {
				t.Fatalf("mulDivRound(%d, %d, %d, %s) = %d, %v, want %d (overflow %v)", test.a, test.b, test.d, test.mode, got, err, test.want, test.overflow)
			}
		})
	}
}

// Dividing by zero is its own error, whatever the numerator: a zero product
// does not hide it.
func TestMulDivRoundRefusesAZeroDivisor(t *testing.T) {
	t.Parallel()
	for _, a := range []int64{0, 1, -1, math.MinInt64} {
		got, err := mulDivRound(a, 5, 0, RoundHalfEven)
		if err == nil || errors.Is(err, errFixedOverflow) || !strings.Contains(err.Error(), "division by zero") {
			t.Fatalf("mulDivRound(%d, 5, 0) = %d, %v, want division by zero", a, got, err)
		}
	}
}

// A mode outside the seven has no name, and no other text reads as one.
func TestRoundingRejectsWhatIsNotAMode(t *testing.T) {
	t.Parallel()
	for _, mode := range []Rounding{0, RoundFloor + 1, 255} {
		if mode.String() != "invalid" {
			t.Fatalf("Rounding(%d).String() = %q, want invalid", mode, mode.String())
		}
	}
	for _, name := range []string{"", "invalid", "HALF_UP", "half-up", " half_up", "halfeven", "0", "1"} {
		if mode, err := ParseRounding(name); err == nil {
			t.Fatalf("ParseRounding(%q) = %s, want an error", name, mode)
		}
	}
}

// RoundingModes hands out a copy: a caller cannot rename a mode.
func TestRoundingModesIsACopy(t *testing.T) {
	t.Parallel()
	modes := RoundingModes()
	if !slices.Equal(modes, []string{"half_even", "half_up", "half_down", "down", "up", "ceiling", "floor"}) {
		t.Fatalf("RoundingModes() = %v, want the seven in declaration order", modes)
	}
	modes[0] = "nearest"
	if RoundingModes()[0] != "half_even" || RoundHalfEven.String() != "half_even" {
		t.Fatal("writing to RoundingModes()'s result renamed a mode")
	}
}
