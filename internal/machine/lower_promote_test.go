package machine_test

import (
	"math"
	"testing"

	"github.com/nethinwei/funroute/internal/compile"
	"github.com/nethinwei/funroute/internal/machine"
)

type promotedOrder struct {
	Amount   int64   `funroute:"amount"`
	Currency string  `funroute:"currency"`
	Risk     float64 `funroute:"risk"`
	Flagged  bool    `funroute:"flagged"`
}

type promotedIn struct {
	Order promotedOrder `funroute:"order"`
	Limit int64         `funroute:"limit"`
}

// A promoted field answers as the record's field does, whether a Program
// loads it out of the host's struct or the run is given the whole record;
// a record the program uses any other way keeps its fields unpromoted.
func TestPromotedFieldsAnswerAsTheRecord(t *testing.T) {
	t.Parallel()
	registry := machine.CoreRegistry()
	if err := registry.EnableForm(machine.ForForm); err != nil {
		t.Fatal(err)
	}
	binding, err := compile.Bind[promotedIn, string](registry)
	if err != nil {
		t.Fatal(err)
	}
	in := &promotedIn{Order: promotedOrder{Amount: 1200, Currency: "SGD", Risk: 0.25, Flagged: true}, Limit: 1000}
	order, err := machine.ToValue(in.Order)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		source   string
		promoted int
	}{
		{`order.currency`, 1},
		{`string(order.amount + order.amount)`, 1},
		{`if(order.flagged && order.risk < 0.5, order.currency, "manual")`, 3},
		{`if(order.amount > limit, "review", order.currency)`, 2},
		{`string(len([order.amount for x in [1, 2]]))`, 1},
		{`(order with {currency: "USD"}).currency + order.currency`, 0},
		{`let(o = order, o.currency)`, 0},
	} {
		t.Run(tc.source, func(t *testing.T) {
			t.Parallel()
			program, err := binding.Compile(tc.source)
			if err != nil {
				t.Fatal(err)
			}
			if got := machine.Promotions(program.Runtime()); got != tc.promoted {
				t.Errorf("%d promoted fields, want %d", got, tc.promoted)
			}
			byStruct, err := program.Run(t.Context(), in)
			if err != nil {
				t.Fatal(err)
			}
			byRecord, err := program.Runtime().RunValues(t.Context(), []machine.Value{order, machine.Int(in.Limit)})
			if err != nil {
				t.Fatal(err)
			}
			if text, _ := byRecord.String(); text != byStruct {
				t.Errorf("from the struct %q, from the record %q; want the same", byStruct, text)
			}
		})
	}
}

// A promoted float that is not finite is read as loading the record reads
// it: a NaN in is a NaN out, either way.
func TestAPromotedFieldIsHeldAsTheRecordIs(t *testing.T) {
	t.Parallel()
	binding, err := compile.Bind[promotedIn, float64](machine.CoreRegistry())
	if err != nil {
		t.Fatal(err)
	}
	promoted, err := binding.Compile("order.risk")
	if err != nil {
		t.Fatal(err)
	}
	whole, err := binding.Compile("(order with {amount: 0}).risk")
	if err != nil {
		t.Fatal(err)
	}
	in := &promotedIn{Order: promotedOrder{Risk: math.NaN()}}
	got, gotErr := promoted.Run(t.Context(), in)
	want, wantErr := whole.Run(t.Context(), in)
	if gotErr != nil || wantErr != nil || !math.IsNaN(got) || !math.IsNaN(want) {
		t.Errorf("promoted: %v, %v; whole: %v, %v; want NaN from both", got, gotErr, want, wantErr)
	}
}
