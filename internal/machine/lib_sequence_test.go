package machine

import (
	"math"
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/nethinwei/funroute/internal/money"
)

// unique keeps an amount or a record by a key where it can, and answers as
// keeping each item that Equal finds nowhere before it does: zeros of any
// currency are one amount, a float's zeros one field.
func TestUniqueByKeyAnswersAsEqual(t *testing.T) {
	t.Parallel()
	random := rand.New(rand.NewPCG(5, 6))
	floats := []float64{0, math.Copysign(0, -1), 1.5}
	order := RecordOf(FieldOf("ok", BoolType), FieldOf("n", IntType), FieldOf("f", FloatType), FieldOf("s", StringType), FieldOf("fee", MoneyType))
	amounts := make([]money.Money, 200)
	records := make([]Value, 200)
	for i := range records {
		amounts[i] = money.Make([]string{"USD", "", "USD", "EUR"}[random.IntN(4)], int64(random.IntN(3)))
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
	recordArray, err := Array(order, records)
	if err != nil {
		t.Fatal(err)
	}
	if _, keyed := distinctRecords(records); !keyed {
		t.Fatal("records of scalars and amounts are not keyed")
	}
	for _, array := range []Value{arrayOf(amounts), recordArray} {
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
}
