package machine

import (
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/nethinwei/funroute/internal/money"
)

func TestMoneyArrivesInEitherJSONShape(t *testing.T) {
	t.Parallel()
	registry := declared(t, money.CurrencySpec{Code: "USD", Digits: 2}, money.CurrencySpec{Code: "JPY", Digits: 0})
	table := registry.currencies()
	for name, test := range map[string]struct {
		input any
		typ   Type
		want  string
	}{
		"text":            {"USD 1.70", MoneyType, "{USD 170}"},
		"minor units":     {map[string]any{"currency": "JPY", "minor": json.Number("500")}, MoneyType, "{JPY 500}"},
		"zero":            {json.Number("0"), MoneyType, "{ 0}"},
		"a ratio string":  {"0.029", RatioType, "0.029"},
		"a ratio number":  {json.Number("0.03"), RatioType, "0.03"},
		"an exact number": {json.Number("1.25"), RatioType, "1.25"},
		"18 places":       {json.Number("0.123456789012345678"), RatioType, "0.123456789012345678"},
		"18 places text":  {"-0.123456789012345678", RatioType, "-0.123456789012345678"},
		"a currency":      {"JPY", CurrencyType, "JPY"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			value, err := coerceWith(test.input, test.typ, table)
			if got := sprint(value); err != nil || got != test.want {
				t.Fatalf("coerce(%v, %s) = %s, %v, want %s", test.input, test.typ, got, err, test.want)
			}
		})
	}
}

func TestMoneyTextMustFitTheCurrency(t *testing.T) {
	t.Parallel()
	table := declared(t, money.CurrencySpec{Code: "JPY", Digits: 0}).currencies()
	for input, want := range map[string]string{
		"JPY 1.5": "decimal places",
		"XYZ 1":   "not declared",
		"JPY":     "want a currency and an amount",
	} {
		if _, err := coerceWith(input, MoneyType, table); err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("coerce(%q) error = %v, want one containing %q", input, err, want)
		}
	}
	if _, err := coerceWith(json.Number("5"), MoneyType, table); err == nil {
		t.Fatal("a bare 5 became money, want an error: only zero has no currency")
	}
}

// A []Money is an array<money>'s backing: it crosses without a copy.
func TestMoneySlicesAreWrappedNotCopied(t *testing.T) {
	t.Parallel()
	amounts := []money.Money{money.Make("USD", 1), money.Make("USD", 2)}
	value, err := ToValue(amounts)
	if err != nil {
		t.Fatal(err)
	}
	back, err := FromValue[[]money.Money](value)
	if err != nil || &back[0] != &amounts[0] {
		t.Fatalf("FromValue(ToValue(amounts)) = %p, %v, want the same backing %p", back, err, amounts)
	}
}

// sprint prints a money-kind value compactly: {USD 170}, 0.029, USD/JPY
// 150.25, JPY.
func sprint(value Value) string {
	switch shown := value.Any().(type) {
	case money.Money:
		return "{" + shown.Currency() + " " + money.FormatDecimal(shown.Minor(), 0, false) + "}"
	case money.Ratio:
		return shown.String()
	case money.FxRate:
		return shown.String()
	default:
		text, _ := value.String()
		if value.kind == CurrencyKind {
			text = value.s
		}
		return text
	}
}

// An amount with no currency is only ever zero: a host that forgot the
// currency is refused, not taken for any currency.
func TestAnAmountWithoutACurrencyMustBeZero(t *testing.T) {
	t.Parallel()
	table := declared(t, money.CurrencySpec{Code: "USD", Digits: 2}).currencies()
	if _, err := coerceWith(map[string]any{"currency": "", "minor": float64(500)}, MoneyType, table); err == nil {
		t.Fatal("500 minor units with no currency coerced, want an error")
	}
	if MoneyValue(500, "").hasType(MoneyType) || !MoneyValue(0, "").hasType(MoneyType) {
		t.Fatal("hasType lets a currency-less amount other than zero through, or refuses zero")
	}
}

// Every JSON shape each money kind accepts, and the Go forms a host hands
// over directly.
func TestEveryMoneyShapeCoerces(t *testing.T) {
	t.Parallel()
	table := declared(t, money.CurrencySpec{Code: "USD", Digits: 2}).currencies()
	for name, test := range map[string]struct {
		input any
		typ   Type
		want  string
	}{
		"minor as a json.Number":    {map[string]any{"currency": "USD", "minor": json.Number("170")}, MoneyType, "{USD 170}"},
		"minor as an int":           {map[string]any{"currency": "USD", "minor": 170}, MoneyType, "{USD 170}"},
		"minor as a negative int64": {map[string]any{"currency": "USD", "minor": int64(-5)}, MoneyType, "{USD -5}"},
		"extra keys are ignored":    {map[string]any{"currency": "USD", "minor": 1, "note": "x"}, MoneyType, "{USD 1}"},
		"an empty code with zero":   {map[string]any{"currency": "", "minor": 0}, MoneyType, "{ 0}"},
		"an int zero":               {0, MoneyType, "{ 0}"},
		"an int64 zero":             {int64(0), MoneyType, "{ 0}"},
		"a json.Number zero":        {json.Number("0"), MoneyType, "{ 0}"},
		"a Go money.Money":          {money.Make("USD", 3), MoneyType, "{USD 3}"},
		"a money Value":             {MoneyValue(3, "USD"), MoneyType, "{USD 3}"},
		"a ratio as a whole int":    {3, RatioType, "3"},
		"a negative ratio int64":    {int64(-2), RatioType, "-2"},
		"a ratio as a uint8":        {uint8(1), RatioType, "1"},
		"a negative ratio string":   {"-0.5", RatioType, "-0.5"},
		"a ratio of ten places":     {json.Number("0.0000000001"), RatioType, "0.0000000001"},
		"a Go money.Ratio":          {ratio(29, 1000), RatioType, "0.029"},
		"a Go money.Currency":       {money.CurrencyOf("USD"), CurrencyType, "USD"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			value, err := coerceWith(test.input, test.typ, table)
			if got := sprint(value); err != nil || got != test.want {
				t.Fatalf("coerce(%#v, %s) = %s, %v, want %s", test.input, test.typ, got, err, test.want)
			}
		})
	}
}

func TestMoneyShapesThatDoNotCoerce(t *testing.T) {
	t.Parallel()
	table := declared(t, money.CurrencySpec{Code: "USD", Digits: 2}).currencies()
	for name, test := range map[string]struct {
		input any
		typ   Type
		want  string
	}{
		"no currency key":             {map[string]any{"minor": 1}, MoneyType, `"currency"`},
		"a currency that is a number": {map[string]any{"currency": 840, "minor": 1}, MoneyType, `"currency"`},
		"no minor key":                {map[string]any{"currency": "USD"}, MoneyType, `"minor"`},
		"a fractional minor":          {map[string]any{"currency": "USD", "minor": 1.5}, MoneyType, `"minor"`},
		"a minor that is text":        {map[string]any{"currency": "USD", "minor": "170"}, MoneyType, `"minor"`},
		"a minor past int64":          {map[string]any{"currency": "USD", "minor": uint64(math.MaxUint64)}, MoneyType, `"minor"`},
		"no currency but an amount":   {map[string]any{"currency": "", "minor": -1}, MoneyType, "no currency"},
		"a bare non-zero":             {json.Number("5"), MoneyType, "want money"},
		"a bool":                      {false, MoneyType, "want money"},
		"nothing":                     {nil, MoneyType, "want money"},
		"a list":                      {[]any{"USD", 1}, MoneyType, "want money"},
		"a ratio with a percent":      {"2.9%", RatioType, "ratio"},
		"a ratio in exponent form":    {json.Number("1e-3"), RatioType, "ratio"},
		"a ratio that is a bool":      {true, RatioType, "want a ratio"},
		"a ratio that is nothing":     {nil, RatioType, "want a ratio"},
		"a currency code as a number": {840, CurrencyType, "want a currency code"},
		"a currency that is nothing":  {nil, CurrencyType, "want a currency code"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if value, err := coerceWith(test.input, test.typ, table); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("coerce(%#v, %s) = %s, %v, want an error containing %q", test.input, test.typ, sprint(value), err, test.want)
			}
		})
	}
	if _, err := coerceWith(map[string]any{"currency": "", "minor": 7}, MoneyType, table); !errors.Is(err, ErrCurrency) {
		t.Fatalf("7 minor units with no currency: error = %v, want ErrCurrency", err)
	}
}

// A float64 never becomes money, a ratio or a rate: decoded into any, JSON
// has already rounded it to the binary fraction nearest what was written.
func TestAFloat64NeverBecomesMoney(t *testing.T) {
	t.Parallel()
	table := declared(t, money.CurrencySpec{Code: "USD", Digits: 2}).currencies()
	for name, test := range map[string]struct {
		input any
		typ   Type
	}{
		"money zero":         {0.0, MoneyType},
		"a negative zero":    {math.Copysign(0, -1), MoneyType},
		"minor units":        {map[string]any{"currency": "USD", "minor": 170.0}, MoneyType},
		"a ratio":            {0.03, RatioType},
		"a whole ratio":      {3.0, RatioType},
		"a float32 ratio":    {float32(0.5), RatioType},
		"an exchange rate":   {map[string]any{"base": "USD", "quote": "JPY", "rate": 150.25}, FxRateType},
		"an array of ratios": {[]any{0.1}, ArrayOf(RatioType)},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if value, err := coerceWith(test.input, test.typ, table); err == nil || !strings.Contains(err.Error(), "may already be rounded") {
				t.Fatalf("coerce(%#v, %s) = %s, %v, want the float64 refused", test.input, test.typ, sprint(value), err)
			}
		})
	}
}

// Without a table only the text form needs one: minor units, ratios and
// currencies read the same.
func TestCoercingMoneyWithoutATable(t *testing.T) {
	t.Parallel()
	if _, err := coerceWith("USD 1.70", MoneyType, nil); err == nil || !strings.Contains(err.Error(), "currency table") {
		t.Fatalf(`coerce("USD 1.70") without a table: error = %v, want one naming the currency table`, err)
	}
	for name, test := range map[string]struct {
		input any
		typ   Type
		want  string
	}{
		"minor units": {map[string]any{"currency": "USD", "minor": json.Number("170")}, MoneyType, "{USD 170}"},
		"zero":        {json.Number("0"), MoneyType, "{ 0}"},
		"a ratio":     {"0.029", RatioType, "0.029"},
		"a currency":  {"USD", CurrencyType, "USD"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			value, err := coerceWith(test.input, test.typ, nil)
			if got := sprint(value); err != nil || got != test.want {
				t.Fatalf("coerce(%#v, %s) without a table = %s, %v, want %s", test.input, test.typ, got, err, test.want)
			}
		})
	}
}

// A dictionary of money crosses without a copy too, and JSON containers of
// money are packed into the native backing.
func TestMoneyContainersKeepTheNativeBacking(t *testing.T) {
	t.Parallel()
	fees := map[string]money.Money{"card": money.Make("USD", 30)}
	value, err := coerceWith(fees, DictOf(MoneyType), nil)
	if err != nil {
		t.Fatal(err)
	}
	back, err := FromValue[map[string]money.Money](value)
	if err != nil || reflect.ValueOf(back).UnsafePointer() != reflect.ValueOf(fees).UnsafePointer() {
		t.Fatalf("FromValue(coerce(fees)) = %v, %v, want the same map", back, err)
	}
	amounts := []money.Money{money.Make("USD", 1)}
	if value, err := coerceWith(amounts, ArrayOf(MoneyType), nil); err != nil {
		t.Fatalf("coerce([]money.Money) = %v, %v, want the same backing", value.box, err)
	} else if packed, ok := value.box.([]money.Money); !ok || &packed[0] != &amounts[0] {
		t.Fatalf("coerce([]money.Money) = %v, %v, want the same backing", value.box, err)
	}
	table := declared(t, money.CurrencySpec{Code: "USD", Digits: 2}).currencies()
	list, err := coerceWith([]any{"USD 1.00", map[string]any{"currency": "USD", "minor": 2}, 0}, ArrayOf(MoneyType), table)
	if packed, ok := list.box.([]money.Money); err != nil || !ok || !slices.Equal(packed, []money.Money{money.Make("USD", 100), money.Make("USD", 2), {}}) {
		t.Fatalf("coerce(a JSON list of money) = %#v, %v, want a []money.Money backing", list.box, err)
	}
	dict, err := coerceWith(map[string]any{"a": "USD 0.01"}, DictOf(MoneyType), table)
	if packed, ok := dict.box.(map[string]money.Money); err != nil || !ok || packed["a"] != (money.Make("USD", 1)) {
		t.Fatalf("coerce(a JSON object of money) = %#v, %v, want a map[string]money.Money backing", dict.box, err)
	}
}

func TestFromValueErrorNamesBothSides(t *testing.T) {
	t.Parallel()
	if code, err := FromValue[money.Currency](CurrencyValue("USD")); err != nil || code.Code() != "USD" {
		t.Fatalf("FromValue[money.Currency](USD) = %v, %v, want USD", code, err)
	}
	if _, err := FromValue[money.Currency](Int(1)); err == nil || err.Error() != "argument is int, want money.Currency" {
		t.Fatalf("FromValue[money.Currency](int) error = %v, want %q", err, "argument is int, want money.Currency")
	}
	if _, err := FromValue[money.Ratio](MoneyValue(1, "USD")); err == nil || !strings.Contains(err.Error(), "want money.Ratio") {
		t.Fatalf("FromValue[money.Ratio](money) error = %v, want one naming money.Ratio", err)
	}
}

// A whole number read as a ratio is that number; MinInt64, whose negation
// does not fit, is ErrArithmetic.
func TestWholeRatiosAreExact(t *testing.T) {
	t.Parallel()
	for _, whole := range []int64{0, 1, -1, 922_337_204, math.MaxInt64, -math.MaxInt64} {
		got, err := coerceRatio(whole)
		if want := strconv.FormatInt(whole, 10); err != nil || got.String() != want {
			t.Fatalf("coerceRatio(%d) = %s, %v, want %s", whole, got, err, want)
		}
	}
	if got, err := coerceRatio(int64(math.MinInt64)); !errors.Is(err, ErrArithmetic) {
		t.Fatalf("coerceRatio(MinInt64) = %s, %v, want ErrArithmetic", got, err)
	}
}

// An exchange rate arrives as {"base", "quote", "ratio"}, its rate an exact
// decimal or fraction, or as a Go FxRate.
func TestExchangeRatesCoerce(t *testing.T) {
	t.Parallel()
	table := declared(t, money.CurrencySpec{Code: "USD", Digits: 2}, money.CurrencySpec{Code: "JPY", Digits: 0}).currencies()
	usdJPY := func(rate any) map[string]any { return map[string]any{"base": "USD", "quote": "JPY", "rate": rate} }
	for name, test := range map[string]struct {
		input any
		typ   Type
		want  string
	}{
		"a decimal string":     {usdJPY("150.25"), FxRateType, "150.25 JPY / USD"},
		"a json.Number":        {usdJPY(json.Number("150.25")), FxRateType, "150.25 JPY / USD"},
		"a fraction":           {usdJPY("1/3"), FxRateType, "1/3 JPY / USD"},
		"a currency to itself": {map[string]any{"base": "USD", "quote": "USD", "rate": "1"}, FxRateType, "1 USD / USD"},
		"extra keys":           {map[string]any{"base": "USD", "quote": "JPY", "rate": "150", "at": "noon"}, FxRateType, "150 JPY / USD"},
		"a Go money.FxRate":    {money.FxRateFrom(&money.Pair{Base: "USD", Quote: "JPY"}, ratio(601, 4)), FxRateType, "150.25 JPY / USD"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			value, err := coerceWith(test.input, test.typ, table)
			if got := sprint(value); err != nil || got != test.want {
				t.Fatalf("coerce(%#v, %s) = %s, %v, want %s", test.input, test.typ, got, err, test.want)
			}
		})
	}
}

func TestExchangeRateShapesThatDoNotCoerce(t *testing.T) {
	t.Parallel()
	for name, test := range map[string]struct {
		input any
		want  error
		text  string
	}{
		"text":                     {input: "USD/JPY 150", text: "want an exchange rate"},
		"no base":                  {input: map[string]any{"quote": "JPY", "rate": "150"}, text: `"base" and "quote"`},
		"a quote that is a number": {input: map[string]any{"base": "USD", "quote": 392, "rate": "150"}, text: `"base" and "quote"`},
		"no rate":                  {input: map[string]any{"base": "USD", "quote": "JPY"}, text: `needs a "rate"`},
		"a rate that is a bool":    {input: map[string]any{"base": "USD", "quote": "JPY", "rate": true}, text: `needs a "rate"`},
		"a lowercase code":         {input: map[string]any{"base": "usd", "quote": "JPY", "rate": "150"}, want: ErrCurrency},
		"a zero rate":              {input: map[string]any{"base": "USD", "quote": "JPY", "rate": "0"}, want: ErrArithmetic},
		"a negative rate":          {input: map[string]any{"base": "USD", "quote": "JPY", "rate": json.Number("-150")}, want: ErrArithmetic},
		"an exponent":              {input: map[string]any{"base": "USD", "quote": "JPY", "rate": "1.5e2"}, want: ErrArithmetic},
		"itself, not 1":            {input: map[string]any{"base": "USD", "quote": "USD", "rate": "2"}, want: ErrArithmetic},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := coerceWith(test.input, FxRateType, nil)
			if err == nil || (test.want != nil && !errors.Is(err, test.want)) || !strings.Contains(err.Error(), test.text) {
				t.Fatalf("coerce(%#v, fxrate) error = %v, want %v containing %q", test.input, err, test.want, test.text)
			}
		})
	}
}

// An amount's minor units as a JSON number past 2^53 were rounded by the
// decoding: refused, where the same amount as text or json.Number is exact.
func TestMoneyMinorPastExactFloatsIsRefused(t *testing.T) {
	t.Parallel()
	table := declared(t, money.CurrencySpec{Code: "USD", Digits: 2}).currencies()
	if _, err := coerceMoney(map[string]any{"currency": "USD", "minor": float64(1<<53 + 2)}, table); err == nil {
		t.Fatal("minor 2^53+2 as a float64 coerced, want it refused")
	}
	got, err := coerceMoney(map[string]any{"currency": "USD", "minor": json.Number("9007199254740993")}, table)
	if err != nil || got.i != 9007199254740993 {
		t.Fatalf("minor as json.Number = %d, %v, want 9007199254740993", got.i, err)
	}
}
