package machine

import (
	"encoding/json"
	"errors"
	"math"
	"math/big"
	"math/rand/v2"
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
			if got, err := parseDecimal(test.text, test.digits); err != nil || got != test.want {
				t.Fatalf("parseDecimal(%q, %d) = %d, %v, want %d", test.text, test.digits, got, err, test.want)
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
			if got, err := parseDecimal(test.text, test.digits); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("parseDecimal(%q, %d) = %d, %v, want an error containing %q", test.text, test.digits, got, err, test.want)
			}
		})
	}
	if _, err := parseDecimal("9223372036854775808", 0); !errors.Is(err, errFixedOverflow) {
		t.Fatalf("parseDecimal(2^63) error = %v, want errFixedOverflow", err)
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
		{290_000_000, 10, true, "0.029"}, {RateScale, 10, true, "1"}, {1, 8, false, "0.00000001"},
		{math.MaxInt64, 2, false, "92233720368547758.07"}, {math.MinInt64, 2, false, "-92233720368547758.08"},
		{math.MinInt64, 0, false, "-9223372036854775808"}, {math.MaxInt64, 18, false, "9.223372036854775807"},
		{math.MinInt64, 19, false, "-0.9223372036854775808"}, {1, 20, true, "0.00000000000000000001"},
	} {
		if got := formatDecimal(test.scaled, test.digits, test.trim); got != test.want {
			t.Fatalf("formatDecimal(%d, %d, %v) = %q, want %q", test.scaled, test.digits, test.trim, got, test.want)
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
		text := formatDecimal(scaled, digits, trim)
		if back, err := parseDecimal(text, digits); err != nil || back != scaled {
			t.Fatalf("parseDecimal(formatDecimal(%d, %d, %v) = %q) = %d, %v", scaled, digits, trim, text, back, err)
		}
	}
}

// A unit the type leaves open admits any currency, a code admits only
// itself, and the currency-less value fits a money type only as zero.
func TestValuesFitTheirUnits(t *testing.T) {
	t.Parallel()
	for name, test := range map[string]struct {
		value Value
		typ   Type
		want  bool
	}{
		"dollars in unknown money":        {MoneyValue(1, "USD"), MoneyOf(""), true},
		"dollars in a variable":           {MoneyValue(1, "USD"), MoneyOf("c"), true},
		"dollars in dollars":              {MoneyValue(1, "USD"), MoneyOf("USD"), true},
		"dollars in euros":                {MoneyValue(1, "USD"), MoneyOf("EUR"), false},
		"a dollar zero in euros":          {MoneyValue(0, "USD"), MoneyOf("EUR"), false},
		"a currency-less zero in euros":   {MoneyValue(0, ""), MoneyOf("EUR"), true},
		"a currency-less one anywhere":    {MoneyValue(1, ""), MoneyOf("c"), false},
		"a currency-less minus one":       {MoneyValue(-1, ""), MoneyOf(""), false},
		"a currency in its type":          {CurrencyValue("USD"), CurrencyOf("USD"), true},
		"a currency in another":           {CurrencyValue("USD"), CurrencyOf("EUR"), false},
		"a currency in a variable":        {CurrencyValue("USD"), CurrencyOf("u"), true},
		"an empty currency in a code":     {CurrencyValue(""), CurrencyOf("USD"), false},
		"money is not a rate":             {MoneyValue(0, ""), RateType, false},
		"a rate is a rate":                {RateValue(newRate(1)), RateType, true},
		"a currency is not a string":      {CurrencyValue("USD"), StringType, false},
		"a currency code string is not a": {String("USD"), CurrencyOf(""), false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := test.value.hasType(test.typ); got != test.want {
				t.Fatalf("%s hasType(%s) = %v, want %v", test.value.Type(), test.typ, got, test.want)
			}
		})
	}
}

// Comparing amounts in two currencies is an error, not false; a
// currency-less zero meets anything, and currencies themselves compare.
func TestSameUnitsRefusesToCompareCurrencies(t *testing.T) {
	t.Parallel()
	for name, test := range map[string]struct {
		left, right Value
		fails       bool
	}{
		"dollars and dollars":           {MoneyValue(1, "USD"), MoneyValue(2, "USD"), false},
		"dollars and euros":             {MoneyValue(1, "USD"), MoneyValue(1, "EUR"), true},
		"dollars and a currency-less 0": {MoneyValue(1, "USD"), MoneyValue(0, ""), false},
		"a currency-less 0 and euros":   {MoneyValue(0, ""), MoneyValue(1, "EUR"), false},
		"two currencies":                {CurrencyValue("USD"), CurrencyValue("EUR"), false},
		"two rates":                     {RateValue(newRate(1)), RateValue(newRate(2)), false},
		"two kinds":                     {MoneyValue(1, "USD"), CurrencyValue("EUR"), false},
		"two ints":                      {Int(1), Int(2), false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			err := sameUnits(test.left, test.right)
			if test.fails != errors.Is(err, ErrCurrency) || (!test.fails && err != nil) {
				t.Fatalf("sameUnits(%s, %s) = %v, want ErrCurrency %v", test.left.Type(), test.right.Type(), err, test.fails)
			}
		})
	}
}

func TestEqualUnits(t *testing.T) {
	t.Parallel()
	for name, test := range map[string]struct {
		left, right Value
		want        bool
	}{
		"the same dollars":             {MoneyValue(5, "USD"), MoneyValue(5, "USD"), true},
		"other dollars":                {MoneyValue(5, "USD"), MoneyValue(6, "USD"), false},
		"a dollar zero and a bare one": {MoneyValue(0, "USD"), MoneyValue(0, ""), true},
		"a bare zero and a euro zero":  {MoneyValue(0, ""), MoneyValue(0, "EUR"), true},
		"two currencies' zeros":        {MoneyValue(0, "USD"), MoneyValue(0, "EUR"), true},
		"the same amount elsewhere":    {MoneyValue(5, "USD"), MoneyValue(5, "EUR"), false},
		"one currency":                 {CurrencyValue("USD"), CurrencyValue("USD"), true},
		"two currencies":               {CurrencyValue("USD"), CurrencyValue("EUR"), false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := test.left.equalUnits(test.right); got != test.want {
				t.Fatalf("%s equalUnits %s = %v, want %v", test.left.Type(), test.right.Type(), got, test.want)
			}
		})
	}
}

func TestSameCurrency(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		a, b string
		want bool
	}{{"USD", "USD", true}, {"USD", "", true}, {"", "EUR", true}, {"", "", true}, {"USD", "EUR", false}, {"USD", "usd", false}} {
		if got := sameCurrency(test.a, test.b); got != test.want {
			t.Fatalf("sameCurrency(%q, %q) = %v, want %v", test.a, test.b, got, test.want)
		}
	}
}

// Every money kind interns into a constant and reads back as itself, through
// JSON too, and each keeps only the fields it has.
func TestMoneyConstantsRoundTrip(t *testing.T) {
	t.Parallel()
	for name, value := range map[string]Value{
		"dollars":              MoneyValue(-170, "USD"),
		"a currency-less zero": MoneyValue(0, ""),
		"the smallest amount":  MoneyValue(math.MinInt64, "JPY"),
		"a rate":               RateValue(newRate(math.MaxInt64)),
		"a zero rate":          RateValue(newRate(0)),
		"a currency":           CurrencyValue("KWD"),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			constant := moneyConstant(value)
			encoded, err := json.Marshal(constant)
			if err != nil {
				t.Fatal(err)
			}
			var decoded Constant
			if err := json.Unmarshal(encoded, &decoded); err != nil {
				t.Fatal(err)
			}
			back, err := decoded.moneyValue()
			if err != nil || back.kind != value.kind || back.i != value.i || back.s != value.s {
				t.Fatalf("moneyValue(%s) = %+v, %v, want %+v", encoded, back, err, value)
			}
			assertConstantShape(t, value.kind, decoded)
		})
	}
}

func assertConstantShape(t *testing.T, kind Kind, constant Constant) {
	t.Helper()
	if (constant.Int == nil) != (kind == CurrencyKind) || (constant.String == nil) != (kind == RateKind) || constant.Keys != nil {
		t.Fatalf("moneyConstant of a %s = %+v, want only the fields that kind has", kind, constant)
	}
	if constant.Float != nil || constant.Bool != nil || constant.Elem != nil || constant.Items != nil {
		t.Fatalf("moneyConstant of a %s = %+v, want no container or scalar fields", kind, constant)
	}
}

// A constant missing what its kind needs is refused, not read as a zero.
func TestMoneyConstantsMissingAFieldAreRefused(t *testing.T) {
	t.Parallel()
	amount, code := int64(1), "USD"
	for name, test := range map[string]struct {
		constant Constant
		want     string
	}{
		"money with no amount":    {Constant{Type: MoneyKind, String: &code}, "missing its amount"},
		"money with no currency":  {Constant{Type: MoneyKind, Int: &amount}, "missing its currency"},
		"a rate with no amount":   {Constant{Type: RateKind}, "missing its amount"},
		"a currency with no code": {Constant{Type: CurrencyKind, Int: &amount}, "missing its currency"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if value, err := test.constant.moneyValue(); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("moneyValue(%+v) = %+v, %v, want an error containing %q", test.constant, value, err, test.want)
			}
		})
	}
}

// Each constructor makes its own kind, and each accessor answers only for
// its own kind while still handing over the fields.
func TestMoneyConstructorsAndAccessors(t *testing.T) {
	t.Parallel()
	money, rate, currency := MoneyValue(170, "USD"), RateValue(newRate(290_000_000)), CurrencyValue("JPY")
	if got, ok := money.Money(); !ok || got != (Money{"USD", 170}) || money.Type().String() != MoneyOf("USD").String() {
		t.Fatalf("MoneyValue(170, USD).Money() = %v, %v; type %s", got, ok, money.Type())
	}
	if got, ok := rate.Rate(); !ok || got != newRate(290_000_000) || !rate.Type().Equal(RateType) {
		t.Fatalf("RateValue.Rate() = %v, %v; type %s", got, ok, rate.Type())
	}
	if got, ok := currency.Currency(); !ok || got.Code() != "JPY" || currency.Type().String() != CurrencyOf("JPY").String() {
		t.Fatalf("CurrencyValue.Currency() = %q, %v; type %s", got, ok, currency.Type())
	}
	for _, value := range []Value{rate, currency, Int(170), String("USD")} {
		if _, ok := value.Money(); ok {
			t.Fatalf("%s.Money() ok, want only money to answer", value.Type())
		}
	}
	for _, value := range []Value{money, currency, Int(1)} {
		_, rateOK := value.Rate()
		_, currencyOK := value.Currency()
		if rateOK || (currencyOK && value.kind != CurrencyKind) {
			t.Fatalf("%s answered for another kind", value.Type())
		}
	}
}

// Equality with money in it has no answer across currencies, at any depth:
// two exchange rates of different pairs, amounts in a record's fields, in a
// dictionary's entries. Same currencies compare as values; a currency-less
// zero meets any.
func TestEqualityAcrossUnitsHasNoAnswer(t *testing.T) {
	t.Parallel()
	usdJPY := FxRateValue(FxRate{base: "USD", quote: "JPY", rate: big.NewRat(150, 1)})
	eurJPY := FxRateValue(FxRate{base: "EUR", quote: "JPY", rate: big.NewRat(160, 1)})
	fee := RecordOf(FieldOf("fee", MoneyOf("")))
	record := func(currency string) Value {
		value, err := Record(fee, []Value{MoneyValue(100, currency)})
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	dict := func(currency string) Value {
		return Value{kind: DictKind, box: map[string]Money{"k": {currency: currency, minor: 1}}}
	}
	for name, test := range map[string]struct {
		left, right Value
		equal       bool
		fails       bool
	}{
		"rates of one pair":       {usdJPY, usdJPY, true, false},
		"rates of two pairs":      {usdJPY, eurJPY, false, true},
		"records in one currency": {record("USD"), record("USD"), true, false},
		"records in two":          {record("USD"), record("EUR"), false, true},
		"dictionaries in two":     {dict("USD"), dict("EUR"), false, true},
		"a zero meets any":        {MoneyValue(0, ""), MoneyValue(0, "USD"), true, false},
	} {
		equal, err := equalInUnits(test.left, test.right)
		if equal != test.equal || (err != nil) != test.fails || (err != nil && !errors.Is(err, ErrCurrency)) {
			t.Errorf("%s: equalInUnits = %v, %v, want %v and failing %v", name, equal, err, test.equal, test.fails)
		}
	}
}
