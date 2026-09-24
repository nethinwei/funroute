package machine

import (
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/nethinwei/funroute/internal/money"
)

// A unit the type leaves open admits any currency, a code admits only
// itself, and the currency-less value fits a money type only as zero.
func TestValuesFitTheirUnits(t *testing.T) {
	t.Parallel()
	for name, test := range map[string]struct {
		value Value
		typ   Type
		want  bool
	}{
		"dollars in unknown money":        {MoneyValue(1, "USD"), MoneyType, true},
		"dollars":                         {MoneyValue(1, "USD"), MoneyType, true},
		"a currency-less zero":            {MoneyValue(0, ""), MoneyType, true},
		"a currency-less one anywhere":    {MoneyValue(1, ""), MoneyType, false},
		"a currency-less minus one":       {MoneyValue(-1, ""), MoneyType, false},
		"a currency":                      {CurrencyValue("USD"), CurrencyType, true},
		"money is not a rate":             {MoneyValue(0, ""), RatioType, false},
		"a rate is a rate":                {RatioValue(ratio(1, 10_000_000_000)), RatioType, true},
		"a currency is not a string":      {CurrencyValue("USD"), StringType, false},
		"a currency code string is not a": {String("USD"), CurrencyType, false},
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
		"two rates":                     {RatioValue(ratio(1, 10_000_000_000)), RatioValue(ratio(2, 10_000_000_000)), false},
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
		"a ratio":              RatioValue(ratio(math.MaxInt64, 10_000_000_000)),
		"a zero rate":          RatioValue(ratio(0, 1)),
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
	if (constant.Int == nil) != (kind == CurrencyKind || kind == RatioKind) || constant.String == nil || constant.Keys != nil {
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
		"a rate with no text":     {Constant{Type: RatioKind}, "missing its text"},
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
	amount, rate, currency := MoneyValue(170, "USD"), RatioValue(ratio(29, 1000)), CurrencyValue("JPY")
	if got, ok := amount.Money(); !ok || got != (money.Make("USD", 170)) || amount.Type().String() != MoneyType.String() {
		t.Fatalf("MoneyValue(170, USD).Money() = %v, %v; type %s", got, ok, amount.Type())
	}
	if got, ok := rate.Ratio(); !ok || got != ratio(29, 1000) || !rate.Type().Equal(RatioType) {
		t.Fatalf("RatioValue.Ratio() = %v, %v; type %s", got, ok, rate.Type())
	}
	if got, ok := currency.Currency(); !ok || got.Code() != "JPY" || currency.Type().String() != CurrencyType.String() {
		t.Fatalf("CurrencyValue.Currency() = %q, %v; type %s", got, ok, currency.Type())
	}
	for _, value := range []Value{rate, currency, Int(170), String("USD")} {
		if _, ok := value.Money(); ok {
			t.Fatalf("%s.Money() ok, want only money to answer", value.Type())
		}
	}
	for _, value := range []Value{amount, currency, Int(1)} {
		_, ratioOK := value.Ratio()
		_, currencyOK := value.Currency()
		if ratioOK || (currencyOK && value.kind != CurrencyKind) {
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
	usdJPY := FxRateValue(money.FxRateFrom(&money.Pair{Base: "USD", Quote: "JPY"}, ratio(150, 1)))
	eurJPY := FxRateValue(money.FxRateFrom(&money.Pair{Base: "EUR", Quote: "JPY"}, ratio(160, 1)))
	fee := RecordOf(FieldOf("fee", MoneyType))
	record := func(currency string) Value {
		value, err := Record(fee, []Value{MoneyValue(100, currency)})
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	dict := func(currency string) Value {
		return Value{kind: DictKind, box: map[string]money.Money{"k": money.Make(currency, 1)}}
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

// ratio is num/den, which the test knows fits.
func ratio(num, den int64) money.Ratio {
	r, err := money.RatioOf(num, den)
	if err != nil {
		panic(err)
	}
	return r
}
