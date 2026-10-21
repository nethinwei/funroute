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

type promotedOrders struct {
	Orders []promotedOrder `funroute:"orders"`
}

// The fields a loop reads of an item that is only read field by field are
// loaded into registers of their own, whether a Program walks the host's
// slice or the run is given an array of records, and answer as the records'
// fields do; an item the loop uses any other way keeps its fields
// unpromoted.
func TestPromotedItemFieldsAnswerAsTheRecords(t *testing.T) {
	t.Parallel()
	registry := machine.CoreRegistry()
	if err := registry.EnableForm(machine.ForForm); err != nil {
		t.Fatal(err)
	}
	binding, err := compile.Bind[promotedOrders, string](registry)
	if err != nil {
		t.Fatal(err)
	}
	in := &promotedOrders{Orders: []promotedOrder{
		{Amount: 1200, Currency: "SGD", Risk: 0.25, Flagged: true}, {Amount: 7, Currency: "HKD", Risk: 0.75},
		{Amount: -3, Currency: "SGD", Risk: math.NaN(), Flagged: true},
	}}
	for _, tc := range []struct {
		source, want string
		promoted     int
	}{
		{`string(sum([o.amount for o in orders if o.flagged]))`, "1197", 2},
		{`string(len([1 for o in orders if o.currency == "SGD" && o.risk < 0.5]))`, "1", 2},
		{`first([o.currency for o in orders if !(o.risk < 0.5)])`, "HKD", 2},
		{`string(len([1 for o in orders]))`, "3", 0},
		{`string(sum([o.amount * p.amount for o in orders for p in orders if p.flagged]))`, "1441188", 3},
		{`first([o for o in orders if o.amount < 10]).currency`, "HKD", 0},
		{`first([o with {currency: "USD"} for o in orders if o.flagged]).currency`, "USD", 4},
		{`string(sum([(o with {amount: o.amount * 2}).amount for o in orders]))`, "2408", 4},
	} {
		t.Run(tc.source, func(t *testing.T) {
			t.Parallel()
			program, err := binding.Compile(tc.source)
			if err != nil {
				t.Fatal(err)
			}
			if got := machine.ItemPromotions(program.Runtime()); got != tc.promoted {
				t.Errorf("%d promoted item fields, want %d", got, tc.promoted)
			}
			answersBothWays(t, program, in, tc.want)
		})
	}
}

// A loop that collects its item as it is, filtered on the item's fields,
// makes a view of the host's slice — no record of an item — and the array
// answers as the records do: handed back to the host, measured, walked
// again, sorted, picked from.
func TestCollectedItemsAnswerAsTheRecords(t *testing.T) {
	t.Parallel()
	registry := machine.CoreRegistry()
	if err := registry.EnableForm(machine.ForForm); err != nil {
		t.Fatal(err)
	}
	in := &promotedOrders{Orders: []promotedOrder{
		{Amount: 1200, Currency: "SGD", Risk: 0.25, Flagged: true}, {Amount: 7, Currency: "HKD", Risk: 0.75},
		{Amount: -3, Currency: "MYR", Risk: math.NaN(), Flagged: true}, {Amount: 50, Currency: "THB", Flagged: true},
	}}
	binding, err := compile.Bind[promotedOrders, []promotedOrder](registry)
	if err != nil {
		t.Fatal(err)
	}
	program, err := binding.Compile(`[o for o in orders if o.flagged && o.amount < 1000]`)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := program.Run(t.Context(), in); err != nil || len(got) != 2 || got[0].Currency != "MYR" || got[1] != in.Orders[3] {
		t.Errorf("the flagged orders under 1000 = %+v, %v; want MYR and THB", got, err)
	}
	updated, err := binding.Compile(`[o with {amount: 0} for o in orders if o.flagged]`)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := updated.Run(t.Context(), in); err != nil || len(got) != 3 || got[2] != (promotedOrder{Currency: "THB", Flagged: true}) {
		t.Errorf("the flagged orders at no amount = %+v, %v; want three, THB last", got, err)
	}
	strings, err := compile.Bind[promotedOrders, string](registry)
	if err != nil {
		t.Fatal(err)
	}
	for source, want := range map[string]string{
		`string(len([o for o in orders if o.flagged]))`:                                                      "3",
		`first([o.currency for o in [p for p in orders if p.flagged] if o.amount > 0])`:                      "SGD",
		`first([o for o in orders if !o.flagged]).currency`:                                                  "HKD",
		`string(sum([o.amount for o in [p for p in orders if p.amount > 0]]))`:                               "1257",
		`first(sort_by([o for o in orders if o.flagged], [o.amount for o in orders if o.flagged])).currency`: "MYR",
	} {
		program, err := strings.Compile(source)
		if err != nil {
			t.Fatalf("%s: %v", source, err)
		}
		answersBothWays(t, program, in, want)
	}
}

// recordsOf is the orders as an array of records, which RunValues walks as
// it is given.
func recordsOf(t *testing.T, orders []promotedOrder) machine.Value {
	t.Helper()
	records := make([]machine.Value, len(orders))
	for i := range orders {
		records[i] = must(machine.ToValue(orders[i]))
	}
	return must(machine.Array(records[0].Type(), records))
}

// answersBothWays runs program on in as a Program, which walks the host's
// slice, and on the same orders as an array of records, and checks both
// answer want.
func answersBothWays(t *testing.T, program *machine.Program[promotedOrders, string], in *promotedOrders, want string) {
	t.Helper()
	byStruct, err := program.Run(t.Context(), in)
	if err != nil || byStruct != want {
		t.Errorf("from the host's slice %q, %v; want %q", byStruct, err, want)
	}
	byRecords, err := program.Runtime().RunValues(t.Context(), []machine.Value{recordsOf(t, in.Orders)})
	if text, _ := byRecords.String(); err != nil || text != want {
		t.Errorf("from an array of records %q, %v; want %q", text, err, want)
	}
}

// Collecting the items of a host's slice makes no record of any: what a run
// allocates does not grow with the slice. The count is global, so this test
// does not run in parallel.
func TestCollectedItemsAllocateNoRecords(t *testing.T) {
	registry := machine.CoreRegistry()
	if err := registry.EnableForm(machine.ForForm); err != nil {
		t.Fatal(err)
	}
	binding, err := compile.Bind[promotedOrders, []promotedOrder](registry)
	if err != nil {
		t.Fatal(err)
	}
	program, err := binding.Compile(`[o for o in orders if o.flagged]`)
	if err != nil {
		t.Fatal(err)
	}
	allocs := func(n int) float64 {
		in := &promotedOrders{Orders: make([]promotedOrder, n)}
		for i := range in.Orders {
			in.Orders[i].Flagged = i%2 == 0
		}
		return testing.AllocsPerRun(50, func() {
			if _, err := program.Run(t.Context(), in); err != nil {
				t.Fatal(err)
			}
		})
	}
	if few, many := allocs(16), allocs(512); few != many {
		t.Fatalf("collecting 16 items allocates %v times, 512 items %v; want the same", few, many)
	}
}
