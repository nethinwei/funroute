package machine

import (
	"encoding/json"
	"errors"
	"math"
	"math/big"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestMoneyArrivesInEitherJSONShape(t *testing.T) {
	t.Parallel()
	registry := declared(t, RoundHalfUp, CurrencySpec{Code: "USD", Digits: 2}, CurrencySpec{Code: "JPY", Digits: 0})
	table := registry.currencies()
	for name, test := range map[string]struct {
		input any
		typ   Type
		want  string
	}{
		"text":            {"USD 1.70", MoneyOf(""), "{USD 170}"},
		"minor units":     {map[string]any{"currency": "JPY", "minor": float64(500)}, MoneyOf("c"), "{JPY 500}"},
		"zero":            {float64(0), MoneyOf("USD"), "{ 0}"},
		"a rate string":   {"0.029", RateType, "0.029"},
		"a rate number":   {0.03, RateType, "0.03"},
		"an exact number": {json.Number("1.25"), RateType, "1.25"},
		"a currency":      {"JPY", CurrencyOf(""), "JPY"},
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
	table := declared(t, RoundHalfUp, CurrencySpec{Code: "JPY", Digits: 0}).currencies()
	for input, want := range map[string]string{
		"JPY 1.5": "decimal places",
		"XYZ 1":   "not declared",
		"JPY":     "want a currency and an amount",
	} {
		if _, err := coerceWith(input, MoneyOf(""), table); err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("coerce(%q) error = %v, want one containing %q", input, err, want)
		}
	}
	if _, err := coerceWith(float64(5), MoneyOf(""), table); err == nil {
		t.Fatal("a bare 5 became money, want an error: only zero has no currency")
	}
}

// A []Money is an array<money>'s backing: it crosses without a copy.
func TestMoneySlicesAreWrappedNotCopied(t *testing.T) {
	t.Parallel()
	amounts := []Money{{currency: "USD", minor: 1}, {currency: "USD", minor: 2}}
	value, err := ToValue(amounts)
	if err != nil {
		t.Fatal(err)
	}
	back, err := FromValue[[]Money](value)
	if err != nil || &back[0] != &amounts[0] {
		t.Fatalf("FromValue(ToValue(amounts)) = %p, %v, want the same backing %p", back, err, amounts)
	}
}

// sprint prints a money-kind value compactly: {USD 170}, 0.029, USD/JPY
// 150.25, JPY.
func sprint(value Value) string {
	switch shown := value.Any().(type) {
	case Money:
		return "{" + shown.currency + " " + formatDecimal(shown.minor, 0, false) + "}"
	case Rate:
		return shown.String()
	case FxRate:
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
	table := declared(t, RoundHalfUp, CurrencySpec{Code: "USD", Digits: 2}).currencies()
	if _, err := coerceWith(map[string]any{"currency": "", "minor": float64(500)}, MoneyOf(""), table); err == nil {
		t.Fatal("500 minor units with no currency coerced, want an error")
	}
	if MoneyValue(500, "").hasType(MoneyOf("")) || !MoneyValue(0, "").hasType(MoneyOf("USD")) {
		t.Fatal("hasType lets a currency-less amount other than zero through, or refuses zero")
	}
}

// Every JSON shape each money kind accepts, and the Go forms a host hands
// over directly.
func TestEveryMoneyShapeCoerces(t *testing.T) {
	t.Parallel()
	table := declared(t, RoundHalfUp, CurrencySpec{Code: "USD", Digits: 2}).currencies()
	for name, test := range map[string]struct {
		input any
		typ   Type
		want  string
	}{
		"minor as a json.Number":     {map[string]any{"currency": "USD", "minor": json.Number("170")}, MoneyOf(""), "{USD 170}"},
		"minor as an int":            {map[string]any{"currency": "USD", "minor": 170}, MoneyOf(""), "{USD 170}"},
		"minor as a negative int64":  {map[string]any{"currency": "USD", "minor": int64(-5)}, MoneyOf(""), "{USD -5}"},
		"extra keys are ignored":     {map[string]any{"currency": "USD", "minor": 1, "note": "x"}, MoneyOf(""), "{USD 1}"},
		"an empty code with zero":    {map[string]any{"currency": "", "minor": 0}, MoneyOf("USD"), "{ 0}"},
		"an int zero":                {0, MoneyOf(""), "{ 0}"},
		"an int64 zero":              {int64(0), MoneyOf(""), "{ 0}"},
		"a json.Number zero":         {json.Number("0"), MoneyOf(""), "{ 0}"},
		"a negative float zero":      {math.Copysign(0, -1), MoneyOf(""), "{ 0}"},
		"a Go Money":                 {Money{currency: "USD", minor: 3}, MoneyOf(""), "{USD 3}"},
		"a money Value":              {MoneyValue(3, "USD"), MoneyOf("USD"), "{USD 3}"},
		"a rate as a whole int":      {3, RateType, "3"},
		"a negative rate int64":      {int64(-2), RateType, "-2"},
		"a rate as a uint8":          {uint8(1), RateType, "1"},
		"a negative rate string":     {"-0.5", RateType, "-0.5"},
		"a rate float of ten places": {0.0000000001, RateType, "0.0000000001"},
		"a Go Rate":                  {newRate(290_000_000), RateType, "0.029"},
		"a Go Currency":              {Currency{code: "USD"}, CurrencyOf(""), "USD"},
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
	table := declared(t, RoundHalfUp, CurrencySpec{Code: "USD", Digits: 2}).currencies()
	for name, test := range map[string]struct {
		input any
		typ   Type
		want  string
	}{
		"no currency key":             {map[string]any{"minor": 1}, MoneyOf(""), `"currency"`},
		"a currency that is a number": {map[string]any{"currency": 840, "minor": 1}, MoneyOf(""), `"currency"`},
		"no minor key":                {map[string]any{"currency": "USD"}, MoneyOf(""), `"minor"`},
		"a fractional minor":          {map[string]any{"currency": "USD", "minor": 1.5}, MoneyOf(""), `"minor"`},
		"a minor that is text":        {map[string]any{"currency": "USD", "minor": "170"}, MoneyOf(""), `"minor"`},
		"a minor past int64":          {map[string]any{"currency": "USD", "minor": uint64(math.MaxUint64)}, MoneyOf(""), `"minor"`},
		"no currency but an amount":   {map[string]any{"currency": "", "minor": -1}, MoneyOf(""), "no currency"},
		"a bare non-zero":             {json.Number("5"), MoneyOf(""), "want money"},
		"a bool":                      {false, MoneyOf(""), "want money"},
		"nothing":                     {nil, MoneyOf(""), "want money"},
		"a list":                      {[]any{"USD", 1}, MoneyOf(""), "want money"},
		"money of another currency":   {MoneyValue(1, "USD"), MoneyOf("EUR"), "want money<EUR>"},
		"a Go Money of another":       {Money{currency: "USD", minor: 1}, MoneyOf("EUR"), "want money"},
		"a rate with a percent":       {"2.9%", RateType, "rate"},
		"a rate in exponent form":     {json.Number("1e-3"), RateType, "rate"},
		"a float past ten places":     {0.30000000000000004, RateType, "decimal places"},
		"a float past int64":          {1e20, RateType, "overflows"},
		"a whole rate past int64":     {int64(1_000_000_000), RateType, "overflows"},
		"a rate that is a bool":       {true, RateType, "want a rate"},
		"a rate that is nothing":      {nil, RateType, "want a rate"},
		"a currency code as a number": {840, CurrencyOf(""), "want a currency code"},
		"a currency that is nothing":  {nil, CurrencyOf(""), "want a currency code"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if value, err := coerceWith(test.input, test.typ, table); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("coerce(%#v, %s) = %s, %v, want an error containing %q", test.input, test.typ, sprint(value), err, test.want)
			}
		})
	}
	if _, err := coerceWith(map[string]any{"currency": "", "minor": 7}, MoneyOf(""), table); !errors.Is(err, ErrCurrency) {
		t.Fatalf("7 minor units with no currency: error = %v, want ErrCurrency", err)
	}
}

// Without a table only the text form needs one: minor units, rates and
// currencies read the same.
func TestCoercingMoneyWithoutATable(t *testing.T) {
	t.Parallel()
	if _, err := coerceWith("USD 1.70", MoneyOf(""), nil); err == nil || !strings.Contains(err.Error(), "currency table") {
		t.Fatalf(`coerce("USD 1.70") without a table: error = %v, want one naming the currency table`, err)
	}
	for name, test := range map[string]struct {
		input any
		typ   Type
		want  string
	}{
		"minor units": {map[string]any{"currency": "USD", "minor": 170.0}, MoneyOf(""), "{USD 170}"},
		"zero":        {0.0, MoneyOf(""), "{ 0}"},
		"a rate":      {"0.029", RateType, "0.029"},
		"a currency":  {"USD", CurrencyOf(""), "USD"},
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
	fees := map[string]Money{"card": {currency: "USD", minor: 30}}
	value, err := coerceWith(fees, DictOf(MoneyOf("")), nil)
	if err != nil {
		t.Fatal(err)
	}
	back, err := FromValue[map[string]Money](value)
	if err != nil || reflect.ValueOf(back).UnsafePointer() != reflect.ValueOf(fees).UnsafePointer() {
		t.Fatalf("FromValue(coerce(fees)) = %v, %v, want the same map", back, err)
	}
	amounts := []Money{{currency: "USD", minor: 1}}
	if value, err := coerceWith(amounts, ArrayOf(MoneyOf("")), nil); err != nil || &value.box.([]Money)[0] != &amounts[0] {
		t.Fatalf("coerce([]Money) = %v, %v, want the same backing", value.box, err)
	}
	table := declared(t, RoundHalfUp, CurrencySpec{Code: "USD", Digits: 2}).currencies()
	list, err := coerceWith([]any{"USD 1.00", map[string]any{"currency": "USD", "minor": 2}, 0}, ArrayOf(MoneyOf("")), table)
	if packed, ok := list.box.([]Money); err != nil || !ok || !slices.Equal(packed, []Money{{"USD", 100}, {"USD", 2}, {}}) {
		t.Fatalf("coerce(a JSON list of money) = %#v, %v, want a []Money backing", list.box, err)
	}
	dict, err := coerceWith(map[string]any{"a": "USD 0.01"}, DictOf(MoneyOf("")), table)
	if packed, ok := dict.box.(map[string]Money); err != nil || !ok || packed["a"] != (Money{"USD", 1}) {
		t.Fatalf("coerce(a JSON object of money) = %#v, %v, want a map[string]Money backing", dict.box, err)
	}
}

func TestKindErrorNamesBothSides(t *testing.T) {
	t.Parallel()
	if err := kindError(true, Int(1), Money{}); err != nil {
		t.Fatalf("kindError(ok) = %v, want nil", err)
	}
	if err := kindError(false, Int(1), Money{}); err == nil || err.Error() != "argument is int, want machine.Money" {
		t.Fatalf("kindError(int, Money) = %v, want %q", err, "argument is int, want machine.Money")
	}
	if _, err := FromValue[Rate](MoneyValue(1, "USD")); err == nil || !strings.Contains(err.Error(), "want machine.Rate") {
		t.Fatalf("FromValue[Rate](money<?>) error = %v, want one naming machine.Rate", err)
	}
}

func TestWholeRatesScaleExactly(t *testing.T) {
	t.Parallel()
	for whole, want := range map[int64]Rate{0: newRate(0), 1: newRate(RateScale), -1: newRate(-RateScale), 922_337_203: newRate(922_337_203 * RateScale), -922_337_203: newRate(-922_337_203 * RateScale)} {
		if got, err := mulRateInt(whole); err != nil || got != want {
			t.Fatalf("mulRateInt(%d) = %d, %v, want %d", whole, got.scaled, err, want.scaled)
		}
	}
	for _, whole := range []int64{922_337_204, -922_337_204, math.MaxInt64, math.MinInt64} {
		if got, err := mulRateInt(whole); !errors.Is(err, errFixedOverflow) {
			t.Fatalf("mulRateInt(%d) = %d, %v, want an overflow", whole, got.scaled, err)
		}
	}
}

// An exchange rate arrives as {"base", "quote", "rate"}, its rate an exact
// decimal or fraction, or as a Go FxRate.
func TestExchangeRatesCoerce(t *testing.T) {
	t.Parallel()
	table := declared(t, RoundHalfUp, CurrencySpec{Code: "USD", Digits: 2}, CurrencySpec{Code: "JPY", Digits: 0}).currencies()
	usdJPY := func(rate any) map[string]any { return map[string]any{"base": "USD", "quote": "JPY", "rate": rate} }
	for name, test := range map[string]struct {
		input any
		typ   Type
		want  string
	}{
		"a decimal string":     {usdJPY("150.25"), FxRateOf("", ""), "USD/JPY 150.25"},
		"a json.Number":        {usdJPY(json.Number("150.25")), FxRateOf("USD", "JPY"), "USD/JPY 150.25"},
		"a float":              {usdJPY(150.25), FxRateOf("USD", ""), "USD/JPY 150.25"},
		"a fraction":           {usdJPY("1/3"), FxRateOf("", ""), "USD/JPY 1/3"},
		"a currency to itself": {map[string]any{"base": "USD", "quote": "USD", "rate": "1"}, FxRateOf("", ""), "USD/USD 1"},
		"extra keys":           {map[string]any{"base": "USD", "quote": "JPY", "rate": "150", "at": "noon"}, FxRateOf("", ""), "USD/JPY 150"},
		"a Go FxRate":          {FxRate{base: "USD", quote: "JPY", rate: big.NewRat(601, 4)}, FxRateOf("", ""), "USD/JPY 150.25"},
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
		"a negative rate":          {input: map[string]any{"base": "USD", "quote": "JPY", "rate": -150.0}, want: ErrArithmetic},
		"an exponent":              {input: map[string]any{"base": "USD", "quote": "JPY", "rate": "1.5e2"}, want: ErrArithmetic},
		"itself, not 1":            {input: map[string]any{"base": "USD", "quote": "USD", "rate": "2"}, want: ErrArithmetic},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := coerceWith(test.input, FxRateOf("", ""), nil)
			if err == nil || (test.want != nil && !errors.Is(err, test.want)) || !strings.Contains(err.Error(), test.text) {
				t.Fatalf("coerce(%#v, fxrate<?,?>) error = %v, want %v containing %q", test.input, err, test.want, test.text)
			}
		})
	}
}

// An amount's minor units as a JSON number past 2^53 were rounded by the
// decoding: refused, where the same amount as text or json.Number is exact.
func TestMoneyMinorPastExactFloatsIsRefused(t *testing.T) {
	t.Parallel()
	table := declared(t, RoundHalfUp, CurrencySpec{Code: "USD", Digits: 2}).currencies()
	if _, err := coerceMoney(map[string]any{"currency": "USD", "minor": float64(1<<53 + 2)}, table); err == nil {
		t.Fatal("minor 2^53+2 as a float64 coerced, want it refused")
	}
	got, err := coerceMoney(map[string]any{"currency": "USD", "minor": json.Number("9007199254740993")}, table)
	if err != nil || got.i != 9007199254740993 {
		t.Fatalf("minor as json.Number = %d, %v, want 9007199254740993", got.i, err)
	}
}
