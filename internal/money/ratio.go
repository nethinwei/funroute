package money

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/bits"
	"strconv"
	"strings"
)

// Ratio is an exact ratio: 2.9% is a ratio, so are 1.01, -0.5 and a third. A
// host gets one from ParseRatio, Percent or BasisPoints; the zero value is 0.
// Arithmetic between ratios never rounds, and a ratio crosses JSON as its text
// — its decimal where it has a finite one, p/q otherwise — so no float ever
// stands in for one.
//
// A ratio is num/den, both int64, reduced and with den > 0, so two equal ratios
// hold the same fields and making or combining one allocates nothing. A
// result whose numerator or denominator does not fit int64 is ErrArithmetic,
// never a rounding.
type Ratio struct {
	num, den int64 // den == 0 is the zero value, 0
}

// errRatioOverflow is a ratio past int64 on either side.
var errRatioOverflow = fmt.Errorf("%w: a ratio's numerator or denominator overflows int64", ErrArithmetic)

// ratioOf is num/den reduced. MinInt64 on either side has no negation, so it
// is an overflow; den is not zero.
func ratioOf(num, den int64) (Ratio, error) {
	if num == math.MinInt64 || den == math.MinInt64 {
		return Ratio{}, errRatioOverflow
	}
	if den < 0 {
		num, den = -num, -den
	}
	if num == 0 {
		return Ratio{}, nil
	}
	g := int64(gcd(magnitude(num), uint64(den)))
	return Ratio{num: num / g, den: den / g}, nil
}

// parts is the ratio as num/den, the zero value as 0/1.
func (r Ratio) parts() (num, den int64) {
	if r.den == 0 {
		return 0, 1
	}
	return r.num, r.den
}

// String is the ratio's decimal when it has one of at most 18 places, and
// its fraction p/q otherwise.
func (r Ratio) String() string {
	num, den := r.parts()
	if den == 1 {
		return strconv.FormatInt(num, 10)
	}
	if places, ok := decimalPlaces(den); ok {
		if scaled, fits := mul64(num, pow10(places)/den); fits {
			return FormatDecimal(scaled, places, true)
		}
	}
	return strconv.FormatInt(num, 10) + "/" + strconv.FormatInt(den, 10)
}

// decimalPlaces is how many places 1/den takes, when it is a finite decimal
// that fits int64: den's only prime factors are 2 and 5.
func decimalPlaces(den int64) (int, bool) {
	twos, fives := 0, 0
	for den%2 == 0 {
		den, twos = den/2, twos+1
	}
	for den%5 == 0 {
		den, fives = den/5, fives+1
	}
	places := max(twos, fives)
	return places, den == 1 && places <= 18
}

// Cmp orders two ratios: -1, 0 or 1, comparing a·d with c·b in 128 bits.
func (r Ratio) Cmp(other Ratio) int {
	a, b := r.parts()
	c, d := other.parts()
	return compareProducts(a, d, c, b)
}

// compareProducts orders a·b against c·d exactly.
func compareProducts(a, b, c, d int64) int {
	left, right := productSign(a, b), productSign(c, d)
	if left != right || left == 0 {
		return cmp.Compare(left, right)
	}
	hi1, lo1 := bits.Mul64(magnitude(a), magnitude(b))
	hi2, lo2 := bits.Mul64(magnitude(c), magnitude(d))
	if hi1 != hi2 {
		return cmp.Compare(hi1, hi2) * left
	}
	return cmp.Compare(lo1, lo2) * left
}

func productSign(a, b int64) int {
	switch {
	case a == 0 || b == 0:
		return 0
	case (a < 0) != (b < 0):
		return -1
	default:
		return 1
	}
}

// Sign is -1, 0 or 1.
func (r Ratio) Sign() int { return cmp.Compare(r.num, 0) }

// IsZero reports the ratio 0.
func (r Ratio) IsZero() bool { return r.num == 0 }

func (r Ratio) MarshalJSON() ([]byte, error) { return json.Marshal(r.String()) }

// UnmarshalJSON takes the text of a ratio, or a JSON number read as the
// decimal text it was written as.
func (r *Ratio) UnmarshalJSON(data []byte) error {
	var text string
	if len(data) > 0 && data[0] == '"' {
		if err := json.Unmarshal(data, &text); err != nil {
			return err
		}
	} else {
		text = string(data)
	}
	parsed, err := ParseRatio(text)
	if err != nil {
		return err
	}
	*r = parsed
	return nil
}

// ParseRatio reads a ratio exactly: a decimal, "0.029" is 2.9%, or a
// fraction of integers, "1/3", which is how a ratio with no finite decimal is
// written. Either may carry a sign. Anything else, and a ratio that does not
// fit int64 over int64, is an ErrArithmetic.
func ParseRatio(text string) (Ratio, error) {
	r, err := parseRatio(text)
	if err != nil {
		return Ratio{}, errConversion("ratio %q: %v", text, err)
	}
	return r, nil
}

// parseRatio reads what String writes: an optional sign, then a plain
// decimal or a fraction of plain integers.
func parseRatio(text string) (Ratio, error) {
	body, negative := strings.CutPrefix(text, "-")
	if !negative {
		body = strings.TrimPrefix(body, "+")
	}
	var r Ratio
	var err error
	if top, bottom, fraction := strings.Cut(body, "/"); fraction {
		r, err = parseFraction(top, bottom)
	} else {
		r, err = parsePlainDecimal(body)
	}
	if err != nil || !negative {
		return r, err
	}
	return Ratio{num: -r.num, den: r.den}, nil
}

func parseFraction(top, bottom string) (Ratio, error) {
	num, err := parseDigits(top)
	if err != nil {
		return Ratio{}, err
	}
	den, err := parseDigits(bottom)
	if err != nil {
		return Ratio{}, err
	}
	if den == 0 {
		return Ratio{}, errors.New("has a zero denominator")
	}
	return ratioOf(num, den)
}

// parsePlainDecimal reads digits with at most one point. Trailing zeros of
// the fraction say nothing, so they cost no places.
func parsePlainDecimal(text string) (Ratio, error) {
	whole, fraction, _ := strings.Cut(text, ".")
	fraction = strings.TrimRight(fraction, "0")
	if whole+fraction == "" && !strings.Contains(text, "0") {
		return Ratio{}, errors.New("is not a decimal or a fraction of integers")
	}
	if len(fraction) > 18 {
		return Ratio{}, fmt.Errorf("has %d decimal places, at most 18 fit", len(fraction))
	}
	digits := strings.TrimLeft(whole+fraction, "0")
	if digits == "" {
		return Ratio{}, nil
	}
	num, err := parseDigits(digits)
	if err != nil {
		return Ratio{}, err
	}
	return ratioOf(num, pow10(len(fraction)))
}

// parseDigits reads plain digits into an int64: no sign, no separators.
func parseDigits(text string) (int64, error) {
	if text == "" || strings.Trim(text, "0123456789") != "" {
		return 0, errors.New("is not a decimal or a fraction of integers")
	}
	n, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		return 0, errRatioOverflow
	}
	return n, nil
}

// ParseRatioIn reads a ratio written in a unit: value "2.9" in percent (scale
// 2) or "25" in basis points (scale 4), exactly.
func ParseRatioIn(value string, scale int) (Ratio, error) {
	r, err := ParseRatio(value)
	if err != nil {
		return Ratio{}, err
	}
	if scale < -18 || scale > 18 {
		return Ratio{}, errConversion("ratio %s: a unit of 10^%d does not fit", value, -scale)
	}
	unit := Ratio{num: pow10(abs(scale)), den: 1}
	if scale < 0 {
		return r.Mul(unit)
	}
	return r.Div(unit)
}

// Percent is a ratio written in percent: Percent("2.9") is 2.9%.
func Percent(value string) (Ratio, error) { return ParseRatioIn(value, 2) }

// BasisPoints is a ratio written in basis points: BasisPoints("25") is 0.25%.
func BasisPoints(value string) (Ratio, error) { return ParseRatioIn(value, 4) }

// Add is r + other, exactly.
func (r Ratio) Add(other Ratio) (Ratio, error) { return r.plus(other, 1) }

// Sub is r - other, exactly.
func (r Ratio) Sub(other Ratio) (Ratio, error) { return r.plus(other, -1) }

// plus is a/b ± c/d over the least common denominator, reduced as it goes
// so an intermediate overflows only where the operands' own sizes force it.
func (r Ratio) plus(other Ratio, sign int64) (Ratio, error) {
	a, b := r.parts()
	c, d := other.parts()
	g := int64(gcd(uint64(b), uint64(d)))
	left, leftFits := mul64(a, d/g)
	right, rightFits := mul64(sign*c, b/g)
	sum, sumFits := add64(left, right, 1)
	if !leftFits || !rightFits || !sumFits {
		return Ratio{}, errRatioOverflow
	}
	h := int64(gcd(magnitude(sum), uint64(g)))
	den, denFits := mul64(b/g, d/h)
	if !denFits {
		return Ratio{}, errRatioOverflow
	}
	return ratioOf(sum/h, den)
}

// Mul is r times other, a fee on a fee, exactly: cross-reduced first, so the
// product is already in lowest terms.
func (r Ratio) Mul(other Ratio) (Ratio, error) {
	a, b := r.parts()
	c, d := other.parts()
	if a == 0 || c == 0 {
		return Ratio{}, nil
	}
	g, h := int64(gcd(magnitude(a), uint64(d))), int64(gcd(magnitude(c), uint64(b)))
	num, numFits := mul64(a/g, c/h)
	den, denFits := mul64(b/h, d/g)
	if !numFits || !denFits {
		return Ratio{}, errRatioOverflow
	}
	return ratioOf(num, den)
}

// Div is r divided by other, exactly; by zero it is ErrArithmetic.
func (r Ratio) Div(other Ratio) (Ratio, error) {
	inverse, err := other.inverse()
	if err != nil {
		return Ratio{}, err
	}
	return r.Mul(inverse)
}

func (r Ratio) inverse() (Ratio, error) {
	if r.IsZero() {
		return Ratio{}, errDivisionByZero
	}
	return ratioOf(r.den, r.num)
}

// times is minor·r rounded once by mode, 128 bits wide in between.
func (r Ratio) times(minor int64, mode Rounding) (int64, error) {
	num, den := r.parts()
	return mulDivRound(minor, num, den, mode)
}

// gcd is the greatest common divisor, gcd(0, n) being n.
func gcd(a, b uint64) uint64 {
	for b != 0 {
		a, b = b, a%b
	}
	return a
}

// mul64 is a·b and whether it fits an int64.
func mul64(a, b int64) (int64, bool) {
	if a == 0 || b == 0 {
		return 0, true
	}
	c := a * b
	return c, c/b == a && (c < 0) == ((a < 0) != (b < 0))
}

// add64 is a + sign·b, sign being 1 or -1, and whether it fits an int64:
// a result that wrapped lands on the wrong side of a.
func add64(a, b, sign int64) (int64, bool) {
	if sign < 0 {
		c := a - b
		return c, (c < a) == (b > 0)
	}
	c := a + b
	return c, (c > a) == (b > 0)
}
