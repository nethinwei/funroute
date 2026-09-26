package machine

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/nethinwei/funroute/internal/money"
)

func TestEncodeJSONWritesMoneyAsPeopleDo(t *testing.T) {
	t.Parallel()
	registry := declared(t, money.CurrencySpec{Code: "USD", Digits: 2}, money.CurrencySpec{Code: "JPY", Digits: 0})
	fee := RecordOf(Field{name: "net", typ: MoneyType}, Field{name: "rate", typ: RatioType}, Field{name: "zero", typ: MoneyType})
	record, err := Record(fee, []Value{MoneyValue(-5, "USD"), RatioValue(ratio(29, 1000)), MoneyValue(0, "")})
	if err != nil {
		t.Fatal(err)
	}
	for value, want := range map[*Value]string{
		new(arrayOf([]money.Money{money.Make("JPY", 150), money.Make("USD", 1)})): `["JPY 150","USD 0.01"]`,
		&record: `{"net":"USD -0.05","rate":"0.029","zero":0}`,
	} {
		encoded, err := registry.EncodeJSON(*value)
		if err != nil || string(encoded) != want {
			t.Fatalf("EncodeJSON = %s, %v, want %s", encoded, err, want)
		}
	}
	plain, err := CoreRegistry().EncodeJSON(MoneyValue(170, "USD"))
	if err != nil || string(plain) != `{"currency":"USD","minor":170}` {
		t.Fatalf("EncodeJSON without a table = %s, %v, want the machine form", plain, err)
	}
}

func TestEncodeJSONWritesEveryMoneyShape(t *testing.T) {
	t.Parallel()
	registry := declared(t, money.CurrencySpec{Code: "USD", Digits: 2}, money.CurrencySpec{Code: "JPY", Digits: 0}, money.CurrencySpec{Code: "KWD", Digits: 3})
	for name, test := range map[string]struct {
		value Value
		want  string
	}{
		"dollars":                 {MoneyValue(170, "USD"), `"USD 1.70"`},
		"a dollar zero":           {MoneyValue(0, "USD"), `"USD 0.00"`},
		"a currency-less zero":    {MoneyValue(0, ""), `0`},
		"negative dinars":         {MoneyValue(-5, "KWD"), `"KWD -0.005"`},
		"yen":                     {MoneyValue(-1500, "JPY"), `"JPY -1500"`},
		"a ratio":                 {RatioValue(ratio(-29, 1000)), `"-0.029"`},
		"a currency":              {CurrencyValue("KWD"), `"KWD"`},
		"an int":                  {Int(5), `5`},
		"a string":                {String("USD 1.70"), `"USD 1.70"`},
		"an empty array of money": {mustValue(t, []money.Money{}), `[]`},
		"a dictionary of money":   {mustValue(t, map[string]money.Money{"b": money.Make("JPY", 1), "a": {}}), `{"a":0,"b":"JPY 1"}`},
		"arrays of money":         {nestedMoney(t), `[["USD 0.01"],[0,"KWD 1.000"]]`},
		"records in an array":     {recordsOfMoney(t), `[{"zeta":"USD 0.01","alpha":{"inner":"JPY 2","rate":"1"}}]`},
		"a record without a type": {Value{kind: RecordKind}, `null`},
		// What has no places to be written in keeps the exact machine form.
		"an undeclared currency":         {MoneyValue(170, "GBP"), `{"currency":"GBP","minor":170}`},
		"no currency and not zero":       {MoneyValue(5, ""), `{"currency":"","minor":5}`},
		"an undeclared item in an array": {mustValue(t, []money.Money{money.Make("USD", 1), money.Make("GBP", 2)}), `["USD 0.01",{"currency":"GBP","minor":2}]`},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if encoded, err := registry.EncodeJSON(test.value); err != nil || string(encoded) != test.want {
				t.Fatalf("EncodeJSON(%s) = %s, %v, want %s", name, encoded, err, test.want)
			}
		})
	}
}

// Without a table money has no places to be written in, so it is the machine
// form; the other money kinds need no table and read the same.
func TestEncodeJSONWithoutATableWritesTheMachineForm(t *testing.T) {
	t.Parallel()
	var none *Registry
	for name, test := range map[string]struct {
		value Value
		want  string
	}{
		"dollars":              {MoneyValue(170, "USD"), `{"currency":"USD","minor":170}`},
		"a currency-less zero": {MoneyValue(0, ""), `{"currency":"","minor":0}`},
		"an array of money":    {mustValue(t, []money.Money{money.Make("USD", 1)}), `[{"currency":"USD","minor":1}]`},
		"a ratio":              {RatioValue(ratio(29, 1000)), `"0.029"`},
		"a currency":           {CurrencyValue("USD"), `"USD"`},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assertEncodes(t, CoreRegistry(), test.value, test.want)
			assertEncodes(t, none, test.value, test.want)
		})
	}
}

func assertEncodes(t *testing.T, registry *Registry, value Value, want string) {
	t.Helper()
	if encoded, err := registry.EncodeJSON(value); err != nil || string(encoded) != want {
		t.Fatalf("EncodeJSON(%s) = %s, %v, want %s", value.Type(), encoded, err, want)
	}
}

func mustValue(t *testing.T, input any) Value {
	t.Helper()
	value, err := ToValue(input)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func nestedMoney(t *testing.T) Value {
	t.Helper()
	rows, err := Array(ArrayOf(MoneyType), []Value{mustValue(t, []money.Money{money.Make("USD", 1)}), mustValue(t, []money.Money{{}, money.Make("KWD", 1000)})})
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

// recordsOfMoney is an array holding a record whose fields are not in name
// order and one of which is a record itself.
func recordsOfMoney(t *testing.T) Value {
	t.Helper()
	innerType := RecordOf(Field{name: "inner", typ: MoneyType}, Field{name: "rate", typ: RatioType})
	outerType := RecordOf(Field{name: "zeta", typ: MoneyType}, Field{name: "alpha", typ: innerType})
	inner, err := Record(innerType, []Value{MoneyValue(2, "JPY"), RatioValue(ratio(1, 1))})
	if err != nil {
		t.Fatal(err)
	}
	outer, err := Record(outerType, []Value{MoneyValue(1, "USD"), inner})
	if err != nil {
		t.Fatal(err)
	}
	items, err := Array(outerType, []Value{outer})
	if err != nil {
		t.Fatal(err)
	}
	return items
}

// A record that holds something JSON cannot write fails as a whole.
func TestOrderedFieldsPassOnAFailure(t *testing.T) {
	t.Parallel()
	if encoded, err := json.Marshal(orderedFields{}); err != nil || string(encoded) != `{}` {
		t.Fatalf("json.Marshal(no fields) = %s, %v, want {}", encoded, err)
	}
	if encoded, err := json.Marshal(orderedFields{names: []string{"a"}, values: []any{make(chan int)}}); err == nil {
		t.Fatalf("json.Marshal(a channel field) = %s, want an error", encoded)
	}
}

func TestSpecCodesAreSorted(t *testing.T) {
	t.Parallel()
	table, err := money.NewCurrencies(money.MoneySpec{Currencies: []money.CurrencySpec{{Code: "USD"}, {Code: "BTC"}, {Code: "JPY"}}})
	if err != nil {
		t.Fatal(err)
	}
	codes := table.Spec().Codes()
	if !slices.Equal(codes, []string{"BTC", "JPY", "USD"}) || cap(codes) != len(codes) {
		t.Fatalf("Codes() = %v (cap %d), want [BTC JPY USD] with no spare capacity", codes, cap(codes))
	}
	if codes := (money.MoneySpec{}).Codes(); codes == nil || len(codes) != 0 {
		t.Fatalf("Codes() of an empty spec = %#v, want an empty list", codes)
	}
}
