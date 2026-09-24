package money

import (
	"errors"
	"math"
	"math/rand/v2"
	"slices"
	"strconv"
	"testing"
)

func testCurrencies(t *testing.T) *Currencies {
	t.Helper()
	table, err := NewCurrencies(MoneySpec{Currencies: []CurrencySpec{
		{Code: "USD", Digits: 2}, {Code: "JPY", Digits: 0}, {Code: "KWD", Digits: 3},
	}})
	if err != nil {
		t.Fatal(err)
	}
	return table
}

// formatted is table.Format for money the table can write.
func formatted(t *testing.T, table *Currencies, money Money) string {
	t.Helper()
	text, err := table.Format(money)
	if err != nil {
		t.Fatalf("Format(%v) = %v", money, err)
	}
	return text
}

func TestCurrenciesReadAndWriteAmounts(t *testing.T) {
	t.Parallel()
	table := testCurrencies(t)
	for text, want := range map[string]Money{"USD 1.70": {"USD", 170}, "JPY 1500": {"JPY", 1500}, "KWD -0.005": {"KWD", -5}} {
		got, err := table.Parse(text)
		if err != nil || got != want || formatted(t, table, got) != text {
			t.Fatalf("Parse(%q) = %v, %v; Format = %q", text, got, err, formatted(t, table, got))
		}
	}
	if _, err := table.Parse("JPY 1.5"); err == nil {
		t.Fatal(`Parse("JPY 1.5") succeeded, want too many places`)
	}
	if _, err := table.Of("GBP", "1"); !errors.Is(err, ErrCurrency) {
		t.Fatalf(`Of("GBP") error = %v, want ErrCurrency`, err)
	}
	if places, err := table.Places("KWD"); err != nil || places != 3 {
		t.Fatalf("Places(KWD) = %d, %v", places, err)
	}
}

// wideCurrencies declares one currency for every number of places a
// currency may have: D0X has none, D8X has eight.
func wideCurrencies(t *testing.T) *Currencies {
	t.Helper()
	spec := MoneySpec{}
	for digits := range maxCurrencyDigits + 1 {
		spec.Currencies = append(spec.Currencies, CurrencySpec{Code: placesCode(digits), Digits: digits})
	}
	table, err := NewCurrencies(spec)
	if err != nil {
		t.Fatal(err)
	}
	return table
}

func placesCode(digits int) string { return "D" + strconv.Itoa(digits) + "X" }

func TestNewCurrenciesRefusesABadSpec(t *testing.T) {
	t.Parallel()
	for name, spec := range map[string]MoneySpec{
		"negative places":      {Currencies: []CurrencySpec{{Code: "USD", Digits: -1}}},
		"a code too short":     {Currencies: []CurrencySpec{{Code: "US", Digits: 2}}},
		"a code too long":      {Currencies: []CurrencySpec{{Code: "USDOLLARS", Digits: 2}}},
		"a leading digit":      {Currencies: []CurrencySpec{{Code: "1US", Digits: 2}}},
		"an empty code":        {Currencies: []CurrencySpec{{Digits: 2}}},
		"a twice-declared one": {Currencies: []CurrencySpec{{Code: "JPY"}, {Code: "USD", Digits: 2}, {Code: "JPY"}}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if table, err := NewCurrencies(spec); err == nil || table != nil {
				t.Fatalf("NewCurrencies(%+v) = %v, %v, want an error", spec, table, err)
			}
		})
	}
}

// The spec a table hands out is sorted and its own: neither the caller's
// slice before nor the returned one after can change the table.
func TestCurrenciesSpecIsSortedAndACopy(t *testing.T) {
	t.Parallel()
	input := []CurrencySpec{{Code: "USD", Digits: 2}, {Code: "BTC", Digits: 8}, {Code: "JPY", Digits: 0}}
	table, err := NewCurrencies(MoneySpec{Currencies: input})
	if err != nil {
		t.Fatal(err)
	}
	input[0].Digits = 5
	want := []CurrencySpec{{Code: "BTC", Digits: 8}, {Code: "JPY", Digits: 0}, {Code: "USD", Digits: 2}}
	spec := table.Spec()
	if !slices.Equal(spec.Currencies, want) {
		t.Fatalf("Spec() = %+v, want %v", spec, want)
	}
	spec.Currencies[0].Digits = 1
	if again := table.Spec(); !slices.Equal(again.Currencies, want) {
		t.Fatalf("Spec() after writing to an earlier result = %+v, want %v", again, want)
	}
	if places, err := table.Places("USD"); err != nil || places != 2 {
		t.Fatalf("Places(USD) = %d, %v, want 2 whatever the caller did to its slice", places, err)
	}
}

// Format writes only what Parse reads back: a currency the table does not
// have has no places to write it in, and an amount without a currency is
// zero or a host that forgot the currency.
func TestCurrenciesFormatOnlyWhatReadsBack(t *testing.T) {
	t.Parallel()
	table := testCurrencies(t)
	for _, money := range []Money{{"GBP", 170}, {"usd", 1}, {"", 5}, {"", -1}} {
		if text, err := table.Format(money); !errors.Is(err, ErrCurrency) {
			t.Errorf("Format(%v) = %q, %v, want ErrCurrency", money, text, err)
		}
	}
	if text, err := table.Format(Money{}); err != nil || text != "0" {
		t.Errorf("Format(Money{}) = %q, %v, want 0", text, err)
	}
}

func TestCurrenciesPlaces(t *testing.T) {
	t.Parallel()
	table := wideCurrencies(t)
	for digits := range maxCurrencyDigits + 1 {
		if places, err := table.Places(placesCode(digits)); err != nil || places != digits {
			t.Fatalf("Places(%s) = %d, %v, want %d", placesCode(digits), places, err, digits)
		}
	}
	for _, code := range []string{"", "d0x", "D9X", "USD", " D0X"} {
		if _, err := table.Places(code); !errors.Is(err, ErrCurrency) {
			t.Fatalf("Places(%q) error = %v, want ErrCurrency", code, err)
		}
	}
}

func TestCurrenciesParseTheWaysAmountsAreWritten(t *testing.T) {
	t.Parallel()
	table := testCurrencies(t)
	for text, want := range map[string]Money{
		" USD 1.70 ": {"USD", 170}, "USD  1.70": {"USD", 170}, "USD +1.70": {"USD", 170}, "USD -1.70": {"USD", -170},
		"USD 001.70": {"USD", 170}, "USD 1": {"USD", 100}, "USD 1.7": {"USD", 170}, "USD .5": {"USD", 50},
		"USD 1.": {"USD", 100}, "JPY 0": {"JPY", 0}, "USD -0": {"USD", 0}, "KWD 1.234": {"KWD", 1234},
		"USD 92233720368547758.07": {"USD", math.MaxInt64}, "\tUSD 1.70\n": {"USD", 170},
	} {
		if got, err := table.Parse(text); err != nil || got != want {
			t.Fatalf("Parse(%q) = %v, %v, want %v", text, got, err, want)
		}
	}
}

func TestCurrenciesRefuseWhatIsNotAnAmount(t *testing.T) {
	t.Parallel()
	table := testCurrencies(t)
	for text, currencyError := range map[string]bool{
		"usd 1.70": true, "GBP 1": true, "1.70 USD": true, "-USD 1.70": true, "-USD 1.7": true, "USD 1.705": false, "JPY 1.0": false,
		"USD1.70": false, "USD\t1.70": false, "": false, "USD": false, "USD ": false, "USD 1 000": false,
		"USD 1,70": false, "USD --1": false, "USD 1e2": false, "USD 92233720368547758.08": false, "USD $1": false,
	} {
		got, err := table.Parse(text)
		if err == nil || errors.Is(err, ErrCurrency) != currencyError || got != (Money{}) {
			t.Fatalf("Parse(%q) = %v, %v, want an error (ErrCurrency %v) and no amount", text, got, err, currencyError)
		}
	}
	for _, test := range [][2]string{{"USD", " 1.70"}, {" USD", "1.70"}, {"USD", ""}, {"USD", "1.701"}, {"", "0"}} {
		if got, err := table.Of(test[0], test[1]); err == nil {
			t.Fatalf("Of(%q, %q) = %v, want an error", test[0], test[1], got)
		}
	}
	if got, err := table.Of("KWD", "-0.001"); err != nil || got != (Money{"KWD", -1}) {
		t.Fatalf(`Of("KWD", "-0.001") = %v, %v, want KWD -1 minor`, got, err)
	}
}

func TestCurrenciesFormatEveryAmount(t *testing.T) {
	t.Parallel()
	table := wideCurrencies(t)
	for money, want := range map[Money]string{
		{}: "0", {"D0X", -1500}: "D0X -1500", {"D2X", -5}: "D2X -0.05", {"D8X", 1}: "D8X 0.00000001",
		{"D2X", 0}: "D2X 0.00", {"D3X", 1000}: "D3X 1.000", {"D2X", math.MaxInt64}: "D2X 92233720368547758.07",
		{"D2X", math.MinInt64}: "D2X -92233720368547758.08", {"D8X", -123456789}: "D8X -1.23456789",
	} {
		if got := formatted(t, table, money); got != want {
			t.Fatalf("Format(%v) = %q, want %q", money, got, want)
		}
	}
}

// Whatever the places, an amount written out reads back as itself.
func TestCurrenciesRoundTripAtEveryNumberOfPlaces(t *testing.T) {
	t.Parallel()
	table := wideCurrencies(t)
	random := rand.New(rand.NewPCG(11, 12))
	for digits := range maxCurrencyDigits + 1 {
		code := placesCode(digits)
		minors := make([]int64, 0, 8+200)
		minors = append(minors, 0, 1, -1, pow10(digits), -pow10(digits), 123456789, math.MaxInt64, math.MinInt64+1)
		for range 200 {
			minors = append(minors, random.Int64()>>random.IntN(63)-math.MaxInt64/2)
		}
		for _, minor := range minors {
			money := Money{currency: code, minor: minor}
			text := formatted(t, table, money)
			if back, err := table.Parse(text); err != nil || back != money {
				t.Fatalf("Parse(Format(%v)) = %v, %v, via %q", money, back, err, text)
			}
		}
	}
}

func TestCurrenciesRoundTripTheSmallestAmount(t *testing.T) {
	t.Parallel()
	table := testCurrencies(t)
	money := Money{currency: "USD", minor: math.MinInt64}
	if back, err := table.Parse(formatted(t, table, money)); err != nil || back != money {
		t.Fatalf("Parse(Format(%v)) = %v, %v", money, back, err)
	}
}
