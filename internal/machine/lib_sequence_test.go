package machine

import (
	"errors"
	"math"
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/nethinwei/funroute/internal/money"
)

// unique keeps an amount or a record by a key where it can, and answers as
// keeping each item that Equal finds nowhere before it does: a currency's
// zero and the currency-less zero are one amount, a float's zeros one field.
// Amounts in two currencies have no answer, in whichever order.
func TestUniqueByKeyAnswersAsEqual(t *testing.T) {
	t.Parallel()
	random := rand.New(rand.NewPCG(5, 6))
	amounts, records, recordArray := scalarRecords(t, random)
	if _, keyed := distinctKeyed(records); !keyed {
		t.Fatal("records of scalars and amounts are not keyed")
	}
	nested, held := nestedArrays(t, random)
	if _, keyed := distinctKeyed(valuesOf(nested)); !keyed {
		t.Fatal("arrays of ints are not keyed")
	}
	rates, dicts := ratesAndDicts(t, random)
	if _, keyed := distinctKeyed(valuesOf(dicts)); !keyed {
		t.Fatal("records holding a dictionary are not keyed")
	}
	for _, array := range []Value{arrayOf(amounts), recordArray, nested, held, rates, dicts} {
		got, err := uniqueItems(t.Context(), []Value{array})
		if err != nil {
			t.Fatal(err)
		}
		items, _ := array.Array()
		var want []Value
		for _, item := range items {
			if !slices.ContainsFunc(want, item.Equal) {
				want = append(want, item)
			}
		}
		if kept, _ := got.Array(); !slices.EqualFunc(kept, want, Value.Equal) {
			t.Errorf("unique of %s kept %d items, Equal keeps %d", array.Type(), len(kept), len(want))
		}
	}
	for _, mixed := range [][]money.Money{{money.Make("USD", 1), money.Make("EUR", 2)}, {money.Make("EUR", 0), money.Make("USD", 0)}} {
		if got, err := uniqueItems(t.Context(), []Value{arrayOf(mixed)}); !errors.Is(err, ErrCurrency) {
			t.Errorf("unique of %v = %v, %v; want ErrCurrency", mixed, got.Any(), err)
		}
	}
}

// scalarRecords is 200 amounts in dollars or none, and 200 records of a
// field of each scalar kind and one of those amounts, as values and as an
// array.
func scalarRecords(t *testing.T, random *rand.Rand) ([]money.Money, []Value, Value) {
	t.Helper()
	floats := []float64{0, math.Copysign(0, -1), 1.5}
	order := RecordOf(FieldOf("ok", BoolType), FieldOf("n", IntType), FieldOf("f", FloatType), FieldOf("s", StringType), FieldOf("fee", MoneyType))
	amounts := make([]money.Money, 200)
	records := make([]Value, 200)
	for i := range records {
		amounts[i] = money.Make([]string{"USD", "", "USD"}[random.IntN(3)], int64(random.IntN(3)))
		if amounts[i].Currency() == "" {
			amounts[i] = money.Make("", 0)
		}
		fields := []Value{Bool(random.IntN(2) == 0), Int(int64(random.IntN(2))), Float(floats[random.IntN(3)]),
			String([]string{"a", "ab", "b|"}[random.IntN(3)]), MoneyValue(amounts[i].Minor(), amounts[i].Currency())}
		record, err := Record(order, fields)
		if err != nil {
			t.Fatal(err)
		}
		records[i] = record
	}
	array, err := Array(order, records)
	if err != nil {
		t.Fatal(err)
	}
	return amounts, records, array
}

// nestedArrays is 200 arrays of up to two small ints, the same items more
// than once and the empty array among them, and 200 records that hold one.
func nestedArrays(t *testing.T, random *rand.Rand) (Value, Value) {
	t.Helper()
	held := RecordOf(FieldOf("n", IntType), FieldOf("xs", ArrayOf(IntType)))
	arrays, records := make([]Value, 200), make([]Value, 200)
	for i := range arrays {
		items := make([]int64, random.IntN(3))
		for j := range items {
			items[j] = int64(random.IntN(2))
		}
		arrays[i] = arrayOf(items)
		record, err := Record(held, []Value{Int(int64(random.IntN(2))), arrays[i]})
		if err != nil {
			t.Fatal(err)
		}
		records[i] = record
	}
	nested, err := Array(ArrayOf(IntType), arrays)
	if err != nil {
		t.Fatal(err)
	}
	holding, err := Array(held, records)
	if err != nil {
		t.Fatal(err)
	}
	return nested, holding
}

// ratesAndDicts is 200 rates of one pair, a few ratios among them, and 200
// records that hold a dictionary of up to two small entries.
func ratesAndDicts(t *testing.T, random *rand.Rand) (Value, Value) {
	t.Helper()
	pair := &money.Pair{Base: "USD", Quote: "JPY"}
	rates := make([]money.FxRate, 200)
	for i := range rates {
		ratio, err := money.RatioOf(int64(150+random.IntN(3)), int64(1+random.IntN(2)))
		if err != nil {
			t.Fatal(err)
		}
		rates[i] = money.FxRateFrom(pair, ratio)
	}
	holds := RecordOf(FieldOf("n", IntType), FieldOf("d", DictOf(IntType)))
	records := make([]Value, 200)
	for i := range records {
		entries := map[string]int64{}
		for range random.IntN(3) {
			entries[[]string{"a", "b"}[random.IntN(2)]] = int64(random.IntN(2))
		}
		record, err := Record(holds, []Value{Int(int64(random.IntN(2))), {kind: DictKind, box: entries}})
		if err != nil {
			t.Fatal(err)
		}
		records[i] = record
	}
	array, err := Array(holds, records)
	if err != nil {
		t.Fatal(err)
	}
	return arrayOf(rates), array
}
