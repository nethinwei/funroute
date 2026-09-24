package money

import (
	"fmt"
	"math"
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
		return 0, fmt.Errorf("is not a decimal")
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
	for i := 0; i < len(digits); i++ {
		c := digits[i]
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("is not a decimal")
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
