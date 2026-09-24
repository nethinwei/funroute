package money

import (
	"fmt"
	"math"
	"math/bits"
)

// Fixed-point arithmetic for money. Every product and quotient is
// taken exactly in 128 bits and then rounded once, the way the rule's author
// wrote it; nothing here goes through a float.

// Rounding is how a result that falls between two representable values is
// settled. The registry declares the default a console uses, round(expr,
// mode) overrides it where it is written, and the names are what both the
// declaration and the rounding enum spell.
type Rounding uint8

const (
	// RoundHalfEven takes the nearer value and the even one on a tie
	// (banker's rounding).
	RoundHalfEven Rounding = iota + 1
	// RoundHalfUp takes the nearer value and the one away from zero on a
	// tie, which is what the euro conversion rules and most card schemes do.
	RoundHalfUp
	// RoundHalfDown takes the nearer value and the one toward zero on a tie.
	RoundHalfDown
	// RoundDown truncates toward zero.
	RoundDown
	// RoundUp rounds away from zero.
	RoundUp
	// RoundCeiling rounds toward positive infinity.
	RoundCeiling
	// RoundFloor rounds toward negative infinity.
	RoundFloor
)

var roundingNames = [...]string{"", "half_even", "half_up", "half_down", "down", "up", "ceiling", "floor"}

// RoundingModes lists every mode by name, in declaration order: the members
// of the rounding enum.
func RoundingModes() []string { return append([]string(nil), roundingNames[1:]...) }

func (r Rounding) String() string {
	if int(r) < len(roundingNames) && r != 0 {
		return roundingNames[r]
	}
	return "invalid"
}

// ParseRounding reads a mode's name.
func ParseRounding(name string) (Rounding, error) {
	for i, known := range roundingNames {
		if i > 0 && known == name {
			return Rounding(i), nil
		}
	}
	return 0, fmt.Errorf("unknown rounding %q (want one of half_even, half_up, half_down, down, up, ceiling, floor)", name)
}

var errFixedOverflow = fmt.Errorf("%w: amount overflows int64", ErrArithmetic)

// mulDivRound is a·b/d rounded by mode, exact in between: the product is 128
// bits wide, so nothing is lost before the one rounding step.
func mulDivRound(a, b, d int64, mode Rounding) (int64, error) {
	if d == 0 {
		return 0, errDivisionByZero
	}
	if mode < RoundHalfEven || mode > RoundFloor {
		return 0, fmt.Errorf("%w: rounding %d is not a mode", ErrArithmetic, mode)
	}
	negative := (a < 0) != (b < 0)
	if d < 0 {
		negative = !negative
	}
	if a == 0 || b == 0 {
		return 0, nil
	}
	ud := magnitude(d)
	q, r, err := mulDivParts(magnitude(a), magnitude(b), ud)
	if err != nil {
		return 0, err
	}
	if roundsAway(q, r, ud, negative, mode) {
		if q == math.MaxUint64 {
			return 0, errFixedOverflow
		}
		q++
	}
	return signed(q, negative)
}

// mulDivParts is a·b/d on magnitudes, 128 bits wide in between: the quotient
// and the remainder, before any rounding. d is not zero.
func mulDivParts(a, b, d uint64) (quotient, remainder uint64, err error) {
	hi, lo := bits.Mul64(a, b)
	if hi >= d {
		return 0, 0, errFixedOverflow
	}
	quotient, remainder = bits.Div64(hi, lo, d)
	return quotient, remainder, nil
}

// magnitude is |x| as unsigned, which holds MinInt64's too.
func magnitude(x int64) uint64 {
	if x < 0 {
		return uint64(-(x + 1)) + 1
	}
	return uint64(x)
}

func signed(q uint64, negative bool) (int64, error) {
	if negative {
		if q > 1<<63 {
			return 0, errFixedOverflow
		}
		return int64(-q), nil // q ≤ 2^63, so -q wraps to MinInt64 at worst
	}
	if q > math.MaxInt64 {
		return 0, errFixedOverflow
	}
	return int64(q), nil
}

// roundsAway decides whether the magnitude q with remainder r (of d) moves
// one step away from zero.
func roundsAway(q, r, d uint64, negative bool, mode Rounding) bool {
	if r == 0 {
		return false
	}
	switch mode {
	case RoundUp:
		return true
	case RoundCeiling:
		return !negative
	case RoundFloor:
		return negative
	case RoundHalfUp:
		return r >= d-r
	case RoundHalfDown:
		return r > d-r
	case RoundHalfEven:
		return r > d-r || (r == d-r && q%2 == 1)
	default:
		return false
	}
}
