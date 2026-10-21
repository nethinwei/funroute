package machine

import (
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"testing"
	"time"

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
// Money between minor units has no places to be written in: alone or in a
// record it is written as MarshalJSON writes it, never as the minor unit its
// numerator would be.
func TestEncodeJSONWritesExactMoneyAsMarshalJSON(t *testing.T) {
	t.Parallel()
	registry := declared(t, money.CurrencySpec{Code: "USD", Digits: 2})
	half := exactValue(money.ExactFrom("USD", ratio(1, 2)))
	alone, err := json.Marshal(half)
	if err != nil {
		t.Fatal(err)
	}
	record, err := Record(RecordOf(Field{name: "fee", typ: MoneyType}), []Value{half})
	if err != nil {
		t.Fatal(err)
	}
	for value, want := range map[*Value]string{&half: string(alone), &record: `{"fee":` + string(alone) + `}`} {
		encoded, err := registry.EncodeJSON(*value)
		if err != nil || string(encoded) != want || string(alone) == `"USD 0.01"` {
			t.Errorf("EncodeJSON of half a cent = %s, %v, want %s as MarshalJSON writes it", encoded, err, want)
		}
	}
}

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

// openingAll is readable as it was: every container walked to its leaves.
func openingAll(t *money.Currencies, value Value) ([]byte, error) {
	return json.Marshal(jsonTree(value, func(v Value) bool {
		return v.kind == ArrayKind || v.kind == DictKind || v.kind == RecordKind
	}, readableLeaf(t)))
}

// unordered is an array and a dictionary of one record whose fields are not
// in name order.
func unordered(t *testing.T) (Value, Value) {
	t.Helper()
	typ := RecordOf(Field{name: "b", typ: IntType}, Field{name: "a", typ: StringType})
	record, err := Record(typ, []Value{Int(1), String("x")})
	if err != nil {
		t.Fatal(err)
	}
	byKey, err := Dict(typ, map[string]Value{"k": record})
	if err != nil {
		t.Fatal(err)
	}
	return nestedOf(typ, []Value{record}), byKey
}

// Only what holds money is walked; everything else is written whole, and
// either way the bytes, and the failures, are the ones walking it all wrote.
func TestEncodeJSONWritesWhatHoldsNoMoneyWhole(t *testing.T) {
	t.Parallel()
	registry := declared(t, money.CurrencySpec{Code: "USD", Digits: 2}, money.CurrencySpec{Code: "KWD", Digits: 3})
	records, recordsByKey := unordered(t)
	for name, value := range map[string]Value{
		"an empty array":            mustValue(t, []int64{}),
		"a nil array":               mustValue(t, []float64(nil)),
		"an empty dictionary":       mustValue(t, map[string]float64{}),
		"floats":                    mustValue(t, []float64{1, 2.5, math.Copysign(0, -1), 1e21}),
		"a float that JSON refuses": mustValue(t, []float64{1, math.NaN()}),
		"strings":                   mustValue(t, []string{"<a&b>", ""}),
		"arrays of floats":          mustValue(t, [][]float64{{1}, {}}),
		"durations":                 nestedOf(DurationType, []Value{Duration(time.Second), Duration(0)}),
		"records without money":     records,
		"a dictionary of records":   recordsByKey,
		"amounts":                   mustValue(t, []money.Money{money.Make("USD", 1), {}}),
		"arrays of money":           nestedMoney(t),
		"records of money":          recordsOfMoney(t),
		"a dictionary of money":     mustValue(t, map[string]money.Money{"b": money.Make("KWD", 1), "a": {}}),
		"a bare zero":               MoneyValue(0, ""),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			want, wantErr := openingAll(registry.currencies(), value)
			got, err := registry.EncodeJSON(value)
			if string(got) != string(want) || fmt.Sprint(err) != fmt.Sprint(wantErr) {
				t.Fatalf("EncodeJSON(%s) = %s, %v, want %s, %v", value.Type(), got, err, want, wantErr)
			}
		})
	}
}

// An array with no money in it is written whole: its allocations do not
// grow with its length.
func TestEncodeJSONDoesNotBoxEachFloat(t *testing.T) {
	registry := declared(t, money.CurrencySpec{Code: "USD", Digits: 2})
	allocs := func(n int) float64 {
		value := arrayOf(make([]float64, n))
		return testing.AllocsPerRun(10, func() { _, _ = registry.EncodeJSON(value) })
	}
	if short, long := allocs(10), allocs(10_000); long > short+4 {
		t.Fatalf("EncodeJSON allocates %v times for 10 floats and %v for 10000, want about the same", short, long)
	}
}

func BenchmarkEncodeJSONFloats(b *testing.B) {
	registry := CoreRegistry()
	if err := registry.DeclareMoney(money.MoneySpec{Currencies: []money.CurrencySpec{{Code: "USD", Digits: 2}}}); err != nil {
		b.Fatal(err)
	}
	value := arrayOf(make([]float64, 10_000))
	for b.Loop() {
		_, _ = registry.EncodeJSON(value)
	}
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
