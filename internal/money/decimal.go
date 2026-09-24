package money

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// ParseDecimal reads a plain decimal — digits, an optional point, an optional
// sign — into an integer scaled by 10^digits, exactly. A figure with more
// decimal places than that is refused: it cannot be represented, and
// rounding it silently is the thing money types exist to prevent.
func ParseDecimal(text string, digits int) (int64, error) {
	body, negative := strings.CutPrefix(text, "-")
	if !negative {
		body = strings.TrimPrefix(body, "+")
	}
	whole, fraction, _ := strings.Cut(body, ".")
	if whole == "" && fraction == "" {
		return 0, errors.New("is not a decimal")
	}
	if len(fraction) > digits {
		return 0, fmt.Errorf("has %d decimal places, at most %d fit", len(fraction), digits)
	}
	// A negative figure may reach one past MaxInt64: -2^63 is an int64.
	limit := uint64(math.MaxInt64)
	if negative {
		limit++
	}
	scaled, err := accumulateDigits(0, whole, limit)
	if err != nil {
		return 0, err
	}
	scaled, err = accumulateDigits(scaled, fraction+strings.Repeat("0", digits-len(fraction)), limit)
	if err != nil {
		return 0, err
	}
	if negative {
		return int64(-scaled), nil
	}
	return int64(scaled), nil
}

// accumulateDigits appends decimal digits to an unsigned total, refusing
// anything but digits and any total past limit.
func accumulateDigits(total uint64, digits string, limit uint64) (uint64, error) {
	for i := range len(digits) {
		c := digits[i]
		if c < '0' || c > '9' {
			return 0, errors.New("is not a decimal")
		}
		if total > (limit-uint64(c-'0'))/10 {
			return 0, errFixedOverflow
		}
		total = total*10 + uint64(c-'0')
	}
	return total, nil
}

// FormatDecimal writes a scaled integer back as a decimal: 170 with two
// digits is "1.70". trim drops trailing zeros — a ratio reads 0.029 — while
// money keeps its currency's places.
func FormatDecimal(scaled int64, digits int, trim bool) string {
	sign := ""
	if scaled < 0 {
		sign = "-"
	}
	text := fmt.Sprintf("%0*d", digits+1, magnitude(scaled))
	whole, fraction := text[:len(text)-digits], text[len(text)-digits:]
	if trim {
		fraction = strings.TrimRight(fraction, "0")
	}
	if fraction == "" {
		return sign + whole
	}
	return sign + whole + "." + fraction
}

// Decimal is a number written in decimal — a sign, digits, a point, an
// exponent — held as exactly what was written: its significant digits and
// the power of ten that scales them, so 1.50, 15e-1 and 0.15e1 are one.
// Nothing about it passes through a float64, and the exponent is kept, never
// worked out: 1e-999999999 costs what it takes to write.
type Decimal struct {
	negative bool
	// digits has no leading or trailing zero; it is empty for zero.
	digits   string
	exponent int
}

// maxExponent bounds a Decimal's exponent, so arithmetic on it cannot
// overflow; no value that far out fits anything a program computes with.
const maxExponent = 1 << 30

// ParseDecimalLiteral reads a decimal written the way a float literal is:
// an optional sign, digits with at most one point among them, and an
// optional exponent.
func ParseDecimalLiteral(text string) (Decimal, error) {
	body, negative := strings.CutPrefix(text, "-")
	if !negative {
		body = strings.TrimPrefix(body, "+")
	}
	mantissa, power := body, ""
	if at := strings.IndexAny(body, "eE"); at >= 0 {
		mantissa, power = body[:at], body[at+1:]
	}
	whole, fraction, _ := strings.Cut(mantissa, ".")
	if whole+fraction == "" || !isDigits(whole) || !isDigits(fraction) {
		return Decimal{}, fmt.Errorf("%q is not a decimal", text)
	}
	exponent, err := decimalExponent(power, len(body) > len(mantissa))
	if err != nil {
		return Decimal{}, fmt.Errorf("%q %w", text, err)
	}
	digits := strings.TrimLeft(whole+fraction, "0")
	trimmed := strings.TrimRight(digits, "0")
	if trimmed == "" {
		return Decimal{negative: negative}, nil
	}
	return Decimal{negative: negative, digits: trimmed, exponent: exponent - len(fraction) + len(digits) - len(trimmed)}, nil
}

// decimalExponent reads the digits after an e, with their sign.
func decimalExponent(power string, written bool) (int, error) {
	if !written {
		return 0, nil
	}
	digits, negative := strings.CutPrefix(power, "-")
	if !negative {
		digits = strings.TrimPrefix(digits, "+")
	}
	if digits == "" || !isDigits(digits) {
		return 0, errors.New("has a malformed exponent")
	}
	value, err := strconv.Atoi(digits)
	if err != nil || value > maxExponent {
		return 0, errors.New("has an exponent too large to hold")
	}
	if negative {
		return -value, nil
	}
	return value, nil
}

func isDigits(text string) bool { return strings.Trim(text, "0123456789") == "" }

// String writes the decimal the way strconv.FormatFloat(f, 'g', -1, 64)
// writes a float64 holding exactly it: the same digits, a plain decimal
// while the exponent is from -4 to 5, and e notation outside that.
func (d Decimal) String() string {
	sign := ""
	if d.negative {
		sign = "-"
	}
	if d.digits == "" {
		return sign + "0"
	}
	point := len(d.digits) + d.exponent
	if power := point - 1; power < -4 || power >= 6 {
		mantissa := d.digits[:1]
		if len(d.digits) > 1 {
			mantissa += "." + d.digits[1:]
		}
		return fmt.Sprintf("%s%se%+03d", sign, mantissa, power)
	}
	return sign + d.plain()
}

// plain is the decimal's magnitude without an exponent.
func (d Decimal) plain() string {
	point := len(d.digits) + d.exponent
	switch {
	case point <= 0:
		return "0." + strings.Repeat("0", -point) + d.digits
	case point >= len(d.digits):
		return d.digits + strings.Repeat("0", point-len(d.digits))
	}
	return d.digits[:point] + "." + d.digits[point:]
}

// Ratio is the decimal as a ratio, exactly, or the reason it does not fit
// one: a ratio holds 18 decimal places and a numerator of int64.
func (d Decimal) Ratio() (Ratio, error) {
	if d.digits == "" {
		return Ratio{}, nil
	}
	sign := ""
	if d.negative {
		sign = "-"
	}
	switch {
	case d.exponent > 18:
		return Ratio{}, errConversion("ratio %s: does not fit an int64", d)
	case d.exponent < -18:
		return Ratio{}, errConversion("ratio %s: has %d decimal places, at most 18 fit", d, -d.exponent)
	}
	return ParseRatio(sign + d.plain())
}
