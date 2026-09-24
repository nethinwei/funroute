package money

import (
	"errors"
	"math"
	"math/rand/v2"
	"strconv"
	"strings"
	"testing"
)

func TestParseDecimalScalesExactly(t *testing.T) {
	t.Parallel()
	for name, test := range map[string]struct {
		text   string
		digits int
		want   int64
	}{
		"no places":               {"170", 0, 170},
		"a trailing point":        {"170.", 0, 170},
		"places filled":           {"1.7", 2, 170},
		"places exact":            {"1.70", 2, 170},
		"only a fraction":         {".05", 2, 5},
		"a negative fraction":     {"-.05", 2, -5},
		"a plus sign":             {"+1", 2, 100},
		"leading zeros":           {"0001.00", 2, 100},
		"negative zero":           {"-0.00", 2, 0},
		"the largest whole":       {"9223372036854775807", 0, math.MaxInt64},
		"the largest at 2 places": {"92233720368547758.07", 2, math.MaxInt64},
		"the negated largest":     {"-92233720368547758.07", 2, -math.MaxInt64},
		"eighteen places":         {"9.223372036854775807", 18, math.MaxInt64},
		"a long run of zeros":     {"000000000000000000000000000001", 0, 1},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got, err := ParseDecimal(test.text, test.digits); err != nil || got != test.want {
				t.Fatalf("ParseDecimal(%q, %d) = %d, %v, want %d", test.text, test.digits, got, err, test.want)
			}
		})
	}
}

func TestParseDecimalRefusesWhatItCannotHold(t *testing.T) {
	t.Parallel()
	for name, test := range map[string]struct {
		text   string
		digits int
		want   string
	}{
		"empty":                {"", 2, "is not a decimal"},
		"a sign alone":         {"-", 2, "is not a decimal"},
		"a point alone":        {".", 2, "is not a decimal"},
		"a signed point":       {"+.", 2, "is not a decimal"},
		"one place too many":   {"1.705", 2, "has 3 decimal places, at most 2 fit"},
		"a place for none":     {"1.0", 0, "has 1 decimal places, at most 0 fit"},
		"a space inside":       {"1 000", 0, "is not a decimal"},
		"a leading space":      {" 1", 0, "is not a decimal"},
		"an exponent":          {"1e2", 0, "is not a decimal"},
		"two points":           {"1.2.3", 3, "is not a decimal"},
		"a comma":              {"1,5", 2, "is not a decimal"},
		"a sign after a sign":  {"+-1", 0, "is not a decimal"},
		"a trailing sign":      {"1-", 0, "is not a decimal"},
		"a non-ASCII digit":    {"١", 0, "is not a decimal"},
		"past int64":           {"9223372036854775808", 0, "overflows"},
		"past int64 by places": {"92233720368547758.08", 2, "overflows"},
		"a digit past int64":   {"92233720368547758070", 0, "overflows"},
		"negative places":      {"1", -1, "at most -1 fit"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got, err := ParseDecimal(test.text, test.digits); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("ParseDecimal(%q, %d) = %d, %v, want an error containing %q", test.text, test.digits, got, err, test.want)
			}
		})
	}
	if _, err := ParseDecimal("9223372036854775808", 0); !errors.Is(err, errFixedOverflow) {
		t.Fatalf("ParseDecimal(2^63) error = %v, want errFixedOverflow", err)
	}
}

func TestFormatDecimalWritesThePlaces(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		scaled int64
		digits int
		trim   bool
		want   string
	}{
		{170, 0, false, "170"}, {170, 2, false, "1.70"}, {170, 2, true, "1.7"}, {0, 2, false, "0.00"}, {0, 2, true, "0"},
		{0, 0, false, "0"}, {-5, 2, false, "-0.05"}, {-5, 2, true, "-0.05"}, {5, 0, true, "5"}, {-100, 2, true, "-1"},
		{290_000_000, 10, true, "0.029"}, {10_000_000_000, 10, true, "1"}, {1, 8, false, "0.00000001"},
		{math.MaxInt64, 2, false, "92233720368547758.07"}, {math.MinInt64, 2, false, "-92233720368547758.08"},
		{math.MinInt64, 0, false, "-9223372036854775808"}, {math.MaxInt64, 18, false, "9.223372036854775807"},
		{math.MinInt64, 19, false, "-0.9223372036854775808"}, {1, 20, true, "0.00000000000000000001"},
	} {
		if got := FormatDecimal(test.scaled, test.digits, test.trim); got != test.want {
			t.Fatalf("FormatDecimal(%d, %d, %v) = %q, want %q", test.scaled, test.digits, test.trim, got, test.want)
		}
	}
}

// What formatDecimal writes, parseDecimal reads back, trimmed or not.
func TestDecimalsRoundTrip(t *testing.T) {
	t.Parallel()
	random := rand.New(rand.NewPCG(13, 14))
	for range 5000 {
		scaled := random.Int64() >> random.IntN(64)
		if random.IntN(2) == 0 {
			scaled = -scaled
		}
		digits, trim := random.IntN(19), random.IntN(2) == 0
		text := FormatDecimal(scaled, digits, trim)
		if back, err := ParseDecimal(text, digits); err != nil || back != scaled {
			t.Fatalf("ParseDecimal(FormatDecimal(%d, %d, %v) = %q) = %d, %v", scaled, digits, trim, text, back, err)
		}
	}
}

// A decimal literal writes back as FormatFloat writes the float64 holding
// it exactly, so a program without ratios keeps its bytes; one no float64
// holds writes its own digits the same way.
func TestADecimalWritesAsItsFloatWould(t *testing.T) {
	t.Parallel()
	for _, value := range []float64{0.1, 2, 1e6, 1234567, 100000, 123456.7, 0.0001, 0.00001, 1e21, 5e-324, -2.5, 1.5e300} {
		text := strconv.FormatFloat(value, 'g', -1, 64)
		decimal, err := ParseDecimalLiteral(text)
		if err != nil || decimal.String() != text {
			t.Errorf("ParseDecimalLiteral(%q) = %v, %v, want it written back as %s", text, decimal, err, text)
		}
	}
	for text, want := range map[string]string{
		"1.50": "1.5", "15e-1": "1.5", "0.15e1": "1.5", "0.000": "0", "-0.0": "-0", "+2.0": "2",
		"0.123456789012345678": "0.123456789012345678", "1e-999999999": "1e-999999999", "12345678.0": "1.2345678e+07",
	} {
		if decimal, err := ParseDecimalLiteral(text); err != nil || decimal.String() != want {
			t.Errorf("ParseDecimalLiteral(%q) = %v, %v, want %s", text, decimal, err, want)
		}
	}
	for _, text := range []string{"", ".", "nan", "inf", "0x1p-2", "1_000.5", "1e", "1e+", "1.2.3", "1e99999999999999999999", "--1"} {
		if _, err := ParseDecimalLiteral(text); err == nil {
			t.Errorf("ParseDecimalLiteral(%q) = nil error, want it refused", text)
		}
	}
}

// A decimal is a ratio exactly, as far as 18 places and an int64 go.
func TestADecimalIsARatioExactly(t *testing.T) {
	t.Parallel()
	for text, want := range map[string]string{
		"0.123456789012345678": "0.123456789012345678", "2.5e-3": "0.0025", "-0.029": "-0.029", "9e18": "9000000000000000000", "0": "0",
	} {
		decimal, err := ParseDecimalLiteral(text)
		if err != nil {
			t.Fatal(err)
		}
		if ratio, err := decimal.Ratio(); err != nil || ratio.String() != want {
			t.Errorf("Decimal(%s).Ratio() = %v, %v, want %s", text, ratio, err, want)
		}
	}
	for _, text := range []string{"1e-19", "0.1234567890123456789", "1e19", "1e999999999"} {
		decimal, err := ParseDecimalLiteral(text)
		if err != nil {
			t.Fatal(err)
		}
		if ratio, err := decimal.Ratio(); err == nil {
			t.Errorf("Decimal(%s).Ratio() = %v, want it refused", text, ratio)
		}
	}
}
