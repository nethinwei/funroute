package money

import (
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"
)

// ratio is num/den, which the test knows fits.
func ratio(num, den int64) Ratio {
	r, err := ratioOf(num, den)
	if err != nil {
		panic(err)
	}
	return r
}

// One ratio has one form, so == is equality: reduced, the sign on top, and
// the zero value for every zero.
func TestARatioIsCanonical(t *testing.T) {
	t.Parallel()
	for name, test := range map[string][2]Ratio{
		"reduced":            {ratio(2, 4), ratio(1, 2)},
		"the sign on top":    {ratio(1, -3), ratio(-1, 3)},
		"two negatives":      {ratio(-2, -6), ratio(1, 3)},
		"zero over anything": {ratio(0, 7), Ratio{}},
		"a whole number":     {ratio(6, 3), ratio(2, 1)},
		"text":               {parsed(t, "0.029"), ratio(29, 1000)},
		"a padded fraction":  {parsed(t, "1.50000000000000000000000"), ratio(3, 2)},
	} {
		if test[0] != test[1] {
			t.Errorf("%s: %+v != %+v", name, test[0], test[1])
		}
	}
	for _, pair := range [][2]int64{{math.MinInt64, 1}, {1, math.MinInt64}} {
		if _, err := ratioOf(pair[0], pair[1]); !errors.Is(err, ErrArithmetic) {
			t.Errorf("ratioOf(%d, %d) error = %v, want ErrArithmetic: MinInt64 has no negation", pair[0], pair[1], err)
		}
	}
}

// parsed is ParseRatio's answer to a text the test knows is a ratio.
func parsed(t *testing.T, text string) Ratio {
	t.Helper()
	r, err := ParseRatio(text)
	if err != nil {
		t.Fatalf("ParseRatio(%q) error = %v", text, err)
	}
	return r
}

// A ratio's text is its decimal when it has a finite one of at most 18
// places, its fraction otherwise, and reads back to the same ratio; JSON
// carries the text.
func TestARatioWritesItsTextAndReadsItBack(t *testing.T) {
	t.Parallel()
	for want, r := range map[string]Ratio{
		"0": {}, "0.029": ratio(29, 1000), "-0.029": ratio(-29, 1000), "150.25": ratio(601, 4), "1": ratio(1, 1),
		"1/3": ratio(1, 3), "-7/6": ratio(-7, 6), "9223372036854775807": ratio(math.MaxInt64, 1),
		"1/9223372036854775807": ratio(1, math.MaxInt64), "0.000000000000000001": ratio(1, 1_000_000_000_000_000_000),
		"-9223372036854775807": ratio(-math.MaxInt64, 1),
		"1/1048576":            ratio(1, 1<<20),
	} {
		if got := r.String(); got != want {
			t.Errorf("String() = %q, want %q", got, want)
		}
		encoded, err := json.Marshal(r)
		var back Ratio
		if err == nil {
			err = json.Unmarshal(encoded, &back)
		}
		if err != nil || back != r || string(encoded) != `"`+want+`"` {
			t.Errorf("JSON of %s = %s, read back %s, %v", want, encoded, back, err)
		}
	}
}

// ParseRatio reads a decimal or a fraction of integers, either with a sign,
// that fits int64 over int64, and nothing else; a JSON number is read as the
// text it was written as.
func TestParseRatioReadsDecimalsAndFractions(t *testing.T) {
	t.Parallel()
	for text, want := range map[string]Ratio{
		"0.029": ratio(29, 1000), "-0.029": ratio(-29, 1000), "+1": ratio(1, 1), "1.": ratio(1, 1), ".5": ratio(1, 2),
		"-.5": ratio(-1, 2), "007": ratio(7, 1), "0.00000000001": ratio(1, 100_000_000_000), "-0": {}, "0.": {},
		"1/3": ratio(1, 3), "-2/6": ratio(-1, 3), "0/5": {},
	} {
		if got, err := ParseRatio(text); err != nil || got != want {
			t.Errorf("ParseRatio(%q) = %s, %v, want %s", text, got, err, want)
		}
	}
	for _, text := range []string{
		"", "-", "+", ".", "-.", "1e3", "1E-3", "1,5", " 1", "1 ", "0x10", "--1", "+-1", "-+1", "1.2.3",
		"١", "Inf", "NaN", "1_000", "%1", "1%", "1/0", "1/", "/2", "1.5/2", "1/-2",
		"9223372036854775808", "0.0000000000000000001", "1/9223372036854775808",
	} {
		if got, err := ParseRatio(text); err == nil || !strings.Contains(err.Error(), "ratio") || !errors.Is(err, ErrArithmetic) {
			t.Errorf("ParseRatio(%q) = %s, %v, want an ErrArithmetic naming the rate", text, got, err)
		}
	}
	for text, want := range map[string]Ratio{`"1/3"`: ratio(1, 3), `0.029`: ratio(29, 1000), `-0.5`: ratio(-1, 2)} {
		var r Ratio
		if err := json.Unmarshal([]byte(text), &r); err != nil || r != want {
			t.Errorf("json.Unmarshal(%s) = %s, %v, want %s", text, r, err, want)
		}
	}
	for _, text := range []string{`null`, `true`, `"abc"`, `1e-3`, `{}`, `"1`} {
		r := ratio(7, 1)
		if err := r.UnmarshalJSON([]byte(text)); err == nil || r != ratio(7, 1) {
			t.Errorf("UnmarshalJSON(%s) = %s, %v, want an error and the rate untouched", text, r, err)
		}
	}
}

// Percent and basis points are the text over their unit, exactly.
func TestARatioInItsUnit(t *testing.T) {
	t.Parallel()
	for name, test := range map[string]struct {
		read func() (Ratio, error)
		want Ratio
	}{
		"percent":             {func() (Ratio, error) { return Percent("2.9") }, ratio(29, 1000)},
		"a fine percent":      {func() (Ratio, error) { return Percent("0.000000001") }, ratio(1, 100_000_000_000)},
		"a negative percent":  {func() (Ratio, error) { return Percent("-100") }, ratio(-1, 1)},
		"basis points":        {func() (Ratio, error) { return BasisPoints("25") }, ratio(1, 400)},
		"half a basis point":  {func() (Ratio, error) { return BasisPoints("0.5") }, ratio(1, 20_000)},
		"a coarser unit":      {func() (Ratio, error) { return ParseRatioIn("0.029", -2) }, ratio(29, 10)},
		"a third of a point":  {func() (Ratio, error) { return ParseRatioIn("1/3", 2) }, ratio(1, 300)},
		"a plain ratio, too":  {func() (Ratio, error) { return ParseRatioIn("1", 0) }, ratio(1, 1)},
		"ten places of units": {func() (Ratio, error) { return ParseRatioIn("1", 10) }, ratio(1, 10_000_000_000)},
	} {
		if got, err := test.read(); err != nil || got != test.want {
			t.Errorf("%s = %s, %v, want %s", name, got, err, test.want)
		}
	}
	for _, value := range []string{"", "2.9%", "abc", "1e2", " 25", "25bps", "0.0000000000000000001"} {
		if got, err := Percent(value); err == nil || !strings.Contains(err.Error(), "ratio") {
			t.Errorf("Percent(%q) = %s, %v, want an error naming the rate", value, got, err)
		}
	}
	if got, err := ParseRatioIn("1", 19); err == nil {
		t.Errorf("ParseRatioIn(1, 19) = %s, want an error: the unit does not fit", got)
	}
}

// Ratios add, subtract, multiply and divide without rounding: a third plus a
// sixth is a half, and a third times three is one. Operands are reduced
// against each other first, so a product fits whenever its lowest terms do.
func TestRatioArithmeticIsExact(t *testing.T) {
	t.Parallel()
	for name, test := range map[string]struct {
		run  func() (Ratio, error)
		want string
	}{
		"a sum":             {func() (Ratio, error) { return ratio(1, 3).Add(ratio(1, 6)) }, "0.5"},
		"a difference":      {func() (Ratio, error) { return ratio(1, 3).Sub(ratio(1, 2)) }, "-1/6"},
		"to nothing":        {func() (Ratio, error) { return ratio(1, 3).Sub(ratio(1, 3)) }, "0"},
		"a fee on a fee":    {func() (Ratio, error) { return ratio(29, 1000).Mul(ratio(1, 2)) }, "0.0145"},
		"a third, thrice":   {func() (Ratio, error) { return ratio(1, 3).Mul(ratio(3, 1)) }, "1"},
		"a quotient":        {func() (Ratio, error) { return ratio(1, 3).Div(ratio(1, 6)) }, "2"},
		"a negative":        {func() (Ratio, error) { return ratio(1, 1).Div(ratio(-4, 1)) }, "-0.25"},
		"times zero":        {func() (Ratio, error) { return ratio(math.MaxInt64, 1).Mul(Ratio{}) }, "0"},
		"cross-reduced":     {func() (Ratio, error) { return ratio(math.MaxInt64, 3).Mul(ratio(3, math.MaxInt64)) }, "1"},
		"a common factor":   {func() (Ratio, error) { return ratio(1, 1<<40).Add(ratio(1, 1<<40)) }, "1/549755813888"},
		"the largest there": {func() (Ratio, error) { return ratio(math.MaxInt64-1, 1).Add(ratio(1, 1)) }, "9223372036854775807"},
	} {
		if got, err := test.run(); err != nil || got.String() != test.want {
			t.Errorf("%s = %s, %v, want %s", name, got, err, test.want)
		}
	}
}

// A result whose lowest terms do not fit int64 over int64 is ErrArithmetic,
// never a rounding; so is dividing by zero.
func TestARatioPastInt64IsArithmetic(t *testing.T) {
	t.Parallel()
	for name, run := range map[string]func() (Ratio, error){
		"a sum past int64":     func() (Ratio, error) { return ratio(math.MaxInt64, 1).Add(ratio(1, 1)) },
		"a difference below":   func() (Ratio, error) { return ratio(-math.MaxInt64, 1).Sub(ratio(2, 1)) },
		"a product past int64": func() (Ratio, error) { return ratio(math.MaxInt64, 1).Mul(ratio(2, 1)) },
		"a fine quotient":      func() (Ratio, error) { return ratio(1, math.MaxInt64).Div(ratio(math.MaxInt64, 1)) },
		"coprime denominators": func() (Ratio, error) {
			return ratio(1, 1<<40).Add(ratio(1, 3*3*3*3*3*3*3*3*3*3*3*3*3*3*3*3*3*3*3*3*3*3*3*3*3*3*3))
		},
		"over zero": func() (Ratio, error) { return ratio(1, 3).Div(Ratio{}) },
	} {
		if got, err := run(); !errors.Is(err, ErrArithmetic) {
			t.Errorf("%s = %s, %v, want ErrArithmetic", name, got, err)
		}
	}
}

// Cmp and Sign order ratios exactly, in 128 bits where the cross products do
// not fit int64.
func TestRatiosOrderExactly(t *testing.T) {
	t.Parallel()
	for name, test := range map[string]struct {
		left, right Ratio
		want        int
	}{
		"a third and 0.34":          {ratio(1, 3), ratio(34, 100), -1},
		"equal":                     {ratio(2, 6), ratio(1, 3), 0},
		"negatives":                 {ratio(-1, 2), ratio(-1, 3), -1},
		"products past int64":       {ratio(math.MaxInt64, 2), ratio(math.MaxInt64-1, 2), 1},
		"large and fine":            {ratio(math.MaxInt64, math.MaxInt64-1), ratio(math.MaxInt64-1, math.MaxInt64-2), -1},
		"negative and past int64":   {ratio(-math.MaxInt64, 3), ratio(-math.MaxInt64+1, 3), -1},
		"the zero value and a zero": {Ratio{}, ratio(0, 5), 0},
		"a sign decides":            {ratio(-1, math.MaxInt64), ratio(1, math.MaxInt64), -1},
	} {
		if got := test.left.Cmp(test.right); got != test.want {
			t.Errorf("%s: Cmp = %d, want %d", name, got, test.want)
		}
		if got := test.right.Cmp(test.left); got != -test.want {
			t.Errorf("%s reversed: Cmp = %d, want %d", name, got, -test.want)
		}
	}
	if ratio(-1, 3).Sign() != -1 || !(Ratio{}).IsZero() || ratio(1, 3).Sign() != 1 {
		t.Fatal("Sign or IsZero is wrong")
	}
}

// Money times a ratio rounds once, 128 bits wide in between.
func TestTimesRoundsOnce(t *testing.T) {
	t.Parallel()
	for mode, want := range map[Rounding]int64{RoundHalfEven: 33, RoundUp: 34, RoundDown: 33} {
		if got, err := ratio(1, 3).times(100, mode); err != nil || got != want {
			t.Errorf("100 × 1/3 by %s = %d, %v, want %d", mode, got, err, want)
		}
	}
	if got, err := ratio(math.MaxInt64, math.MaxInt64-1).times(math.MaxInt64-1, RoundDown); err != nil || got != math.MaxInt64 {
		t.Fatalf("a product past int64 in between = %d, %v, want MaxInt64", got, err)
	}
}

// Arithmetic on ratios allocates nothing. The count is global, so this test
// does not run in parallel.
func TestRatiosDoNotAllocate(t *testing.T) {
	a, b := ratio(29, 1000), ratio(1, 3)
	allocs := testing.AllocsPerRun(100, func() {
		sum, _ := a.Add(b)
		product, _ := sum.Mul(b)
		quotient, _ := product.Div(a)
		if quotient.Sign() != 1 {
			panic("a positive quotient lost its sign")
		}
	})
	if allocs != 0 {
		t.Fatalf("rate arithmetic allocated %v times, want 0", allocs)
	}
}
