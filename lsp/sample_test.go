package lsp

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/nethinwei/funroute/internal/machine"
	"github.com/nethinwei/funroute/internal/money"
)

// A sample is JSON of the type's shape for every kind JSON can give, and
// nothing for the kinds it cannot.
func TestEveryKindHasItsSample(t *testing.T) {
	t.Parallel()
	order := machine.RecordOf(machine.FieldOf("amount", machine.MoneyType), machine.FieldOf("channel", machine.EnumOf("channel", "adyen", "stripe")))
	engine := machine.HandleOf("demo.embedding")
	cases := []struct {
		typ  machine.Type
		want string
	}{
		{machine.BoolType, "true"},
		{machine.IntType, "0"},
		{machine.FloatType, "0.5"},
		{machine.StringType, `"…"`},
		{machine.EnumOf("channel", "adyen", "stripe"), `"adyen"`},
		{machine.EnumOf("empty"), `"…"`},
		{machine.MoneyType, `"USD 1.70"`},
		{machine.RatioType, `"0.029"`},
		{machine.CurrencyType, `"USD"`},
		{machine.FxRateType, `{"base": "USD", "quote": "JPY", "rate": "150.25"}`},
		{machine.ArrayOf(machine.FxRateType), `[{"base": "USD", "quote": "JPY", "rate": "150.25"}]`},
		{machine.DictOf(machine.ArrayOf(machine.IntType)), `{"key": [0]}`},
		{order, `{"amount": "USD 1.70", "channel": "adyen"}`},
		{machine.RecordOf(), "{}"},
		{machine.ArrayOf(order), `[{"amount": "USD 1.70", "channel": "adyen"}]`},
		{engine, ""},
		{machine.TypeVar("T"), ""},
		{machine.Type{}, ""},
		{machine.ArrayOf(engine), "[]"},
		{machine.DictOf(machine.TypeVar("T")), "{}"},
		{machine.RecordOf(machine.FieldOf("tensor", engine)), ""},
	}
	for _, c := range cases {
		t.Run(c.typ.String(), func(t *testing.T) {
			t.Parallel()
			got := samplerFor(machine.CoreRegistry()).sample(c.typ)
			if got != c.want {
				t.Fatalf("sample(%s) = %q, want %q", c.typ, got, c.want)
			}
			if got != "" && !json.Valid([]byte(got)) {
				t.Fatalf("sample(%s) = %q, which is not JSON", c.typ, got)
			}
		})
	}
}

// Money is sampled in the registry's own currencies, with each one's places,
// so the sample is a value the registry reads: a registry of yen alone has
// no dollars, and its exchange rate is yen to yen, at 1.
func TestMoneyIsSampledInTheRegistrysCurrencies(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		currencies          []money.CurrencySpec
		amount, code, quote string
	}{
		{[]money.CurrencySpec{{Code: "USD", Digits: 2}, {Code: "KWD", Digits: 3}}, `"KWD 1.700"`, `"KWD"`, `{"base": "KWD", "quote": "USD", "rate": "150.25"}`},
		{[]money.CurrencySpec{{Code: "JPY"}}, `"JPY 1"`, `"JPY"`, `{"base": "JPY", "quote": "JPY", "rate": "1"}`},
		{[]money.CurrencySpec{{Code: "BHD", Digits: 1}}, `"BHD 1.7"`, `"BHD"`, `{"base": "BHD", "quote": "BHD", "rate": "1"}`},
	} {
		t.Run(test.code, func(t *testing.T) {
			t.Parallel()
			registry := machine.CoreRegistry()
			if err := registry.DeclareMoney(money.MoneySpec{Currencies: test.currencies}); err != nil {
				t.Fatal(err)
			}
			assertSamples(t, samplerFor(registry), map[string]string{"money": test.amount, "currency": test.code, "fxrate": test.quote})
			code, amount, _ := strings.Cut(strings.Trim(test.amount, `"`), " ")
			if _, err := machine.ParseMoneyAmount(registry, code, amount); err != nil {
				t.Errorf("the registry does not read the sample %s: %v", test.amount, err)
			}
		})
	}
}

// assertSamples checks the sample of each named type.
func assertSamples(t *testing.T, samples sampler, want map[string]string) {
	t.Helper()
	for name, sample := range want {
		typ, err := machine.ParseType(name)
		if err != nil {
			t.Fatal(err)
		}
		if got := samples.sample(typ); got != sample {
			t.Errorf("sample(%s) = %s, want %s", name, got, sample)
		}
	}
}
