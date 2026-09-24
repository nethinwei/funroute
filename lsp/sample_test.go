package lsp

import (
	"encoding/json"
	"testing"

	"github.com/nethinwei/funroute/internal/machine"
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
			got := sample(c.typ)
			if got != c.want {
				t.Fatalf("sample(%s) = %q, want %q", c.typ, got, c.want)
			}
			if got != "" && !json.Valid([]byte(got)) {
				t.Fatalf("sample(%s) = %q, which is not JSON", c.typ, got)
			}
		})
	}
}
