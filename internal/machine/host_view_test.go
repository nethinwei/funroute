package machine_test

import (
	"errors"
	"math"
	"slices"
	"testing"
	"time"

	"github.com/nethinwei/funroute/internal/compile"
	"github.com/nethinwei/funroute/internal/machine"
	"github.com/nethinwei/funroute/internal/money"
)

type viewedChannel struct {
	Name string  `funroute:"name"`
	Fee  int64   `funroute:"fee"`
	Risk float64 `funroute:"risk"`
	OK   bool    `funroute:"ok"`
}

type viewedIn struct {
	Channels []viewedChannel `funroute:"channels"`
	Limit    int64           `funroute:"limit"`
}

func viewedRegistry(t *testing.T) *machine.Registry {
	t.Helper()
	registry := machine.CoreRegistry()
	if err := registry.EnableForm(machine.ForForm, machine.ReduceForm); err != nil {
		t.Fatal(err)
	}
	return registry
}

func viewedInput() *viewedIn {
	in := &viewedIn{Limit: 30}
	for i := range 40 {
		in.Channels = append(in.Channels, viewedChannel{Name: string(rune('a' + i%26)), Fee: int64(i * 7 % 50), Risk: float64(i%10) / 10, OK: i%3 != 0})
	}
	return in
}

// viewedSources read an array of plain records: walked, measured, indexed,
// and as a view the library sorts, cuts, joins or compares.
var viewedSources = []string{
	`string(reduce(c in channels, t = 0, t + c.fee))`,
	`string(len([c.name for c in channels if c.ok && c.fee < limit]))`,
	`string(reduce(c in channels if c.risk > 0.5, t = 0.0, t + c.risk))`,
	`string(len([c.fee for c in channels for d in channels if d.fee > c.fee]))`,
	`string(len(channels)) + reduce(c in channels, s = "", s + c.name)`,
	`string(len([[c] for c in channels]))`,
	`string(channels[3].fee)`,
	// A view the library sorts, cuts, joins or compares.
	`sort_by(channels, [c.fee for c in channels])[0].name`,
	`take(sort_by(channels, [c.risk for c in channels]), 3)[2].name`,
	`top_k(channels, [c.fee for c in channels], 4)[3].name`,
	`bottom_k(channels, [c.name for c in channels], 9)[8].name`,
	`reverse(channels)[0].name`,
	`concat(channels, take(channels, 2))[41].name`,
	`slice(channels, 3, 9)[5].name`,
	`last(channels).name + first(sort_by_desc(channels, [c.fee for c in channels])).name`,
	`string(index_of(channels, channels[7]))`,
	`string(channels == reverse(reverse(channels)))`,
	`string(len(unique(concat(channels, channels))))`,
	`reduce(c in sort_by(channels, [c.fee for c in channels]), s = "", s + c.name)`,
	`string(len([c for c in slice(channels, 2, 30) if c.ok]))`,
}

// A loop over an array of plain records in the host's struct answers as the
// same loop over the records does, and allocates nothing for its items.
func TestAViewedArrayAnswersAsItsRecords(t *testing.T) {
	t.Parallel()
	binding, err := compile.Bind[viewedIn, string](viewedRegistry(t))
	if err != nil {
		t.Fatal(err)
	}
	in := viewedInput()
	records := viewedRecords(t, in.Channels)
	for _, source := range viewedSources {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			program, err := binding.Compile(source)
			if err != nil {
				t.Fatal(err)
			}
			byStruct, err := program.Run(t.Context(), in)
			if err != nil {
				t.Fatal(err)
			}
			byRecords, err := program.Runtime().RunValues(t.Context(), []machine.Value{records, machine.Int(in.Limit)})
			if err != nil {
				t.Fatal(err)
			}
			if text, _ := byRecords.String(); text != byStruct {
				t.Errorf("from the struct %q, from the records %q; want the same", byStruct, text)
			}
			assertBatchAnswersAsRun(t, program, in, byStruct)
			if machine.IdleFrameHoldsPointers(program.Runtime()) {
				t.Error("the idle frame still points at the run; want nothing")
			}
		})
	}
}

// assertBatchAnswersAsRun checks that RunBatch, which encodes every request
// before any runs and has no frame to hold a view, answers as Run did.
func assertBatchAnswersAsRun(t *testing.T, program *machine.Program[viewedIn, string], in *viewedIn, want string) {
	t.Helper()
	batched := make([]string, 2)
	program.RunBatch(t.Context(), len(batched), func(int) *viewedIn { return in }, func(i int) *string { return &batched[i] },
		func(i int, err error) { t.Errorf("RunBatch request %d: %v", i, err) })
	for i, text := range batched {
		if text != want {
			t.Errorf("RunBatch request %d is %q, Run %q; want the same", i, text, want)
		}
	}
}

// viewedRecords is the channels as an array of record values, as RunValues
// is given them.
func viewedRecords(t *testing.T, channels []viewedChannel) machine.Value {
	t.Helper()
	typ := machine.RecordOf(machine.FieldOf("name", machine.StringType), machine.FieldOf("fee", machine.IntType),
		machine.FieldOf("risk", machine.FloatType), machine.FieldOf("ok", machine.BoolType))
	items := make([]machine.Value, len(channels))
	for i, c := range channels {
		item, err := machine.Record(typ, []machine.Value{machine.String(c.Name), machine.Int(c.Fee), machine.Float(c.Risk), machine.Bool(c.OK)})
		if err != nil {
			t.Fatal(err)
		}
		items[i] = item
	}
	array, err := machine.Array(typ, items)
	if err != nil {
		t.Fatal(err)
	}
	return array
}

// Walking and measuring the host's slice of records costs no allocation.
func TestAViewedArrayAllocatesNothing(t *testing.T) {
	binding, err := compile.Bind[viewedIn, int64](viewedRegistry(t))
	if err != nil {
		t.Fatal(err)
	}
	program, err := binding.Compile(`reduce(c in channels if c.ok, t = len(channels), t + c.fee)`)
	if err != nil {
		t.Fatal(err)
	}
	in := viewedInput()
	ctx := t.Context()
	if allocs := testing.AllocsPerRun(100, func() {
		if _, err := program.Run(ctx, in); err != nil {
			t.Fatal(err)
		}
	}); allocs != 0 {
		t.Errorf("a run allocated %v times, want 0", allocs)
	}
}

// A float that is not finite in a viewed array is read as loading the whole
// array reads it: an infinity in the risks sums to one either way.
func TestAViewedArrayIsHeldAsLoadingIt(t *testing.T) {
	t.Parallel()
	binding, err := compile.Bind[viewedIn, float64](viewedRegistry(t))
	if err != nil {
		t.Fatal(err)
	}
	viewed, err := binding.Compile(`reduce(c in channels, t = 0.0, t + c.risk)`)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := binding.Compile(`reduce(c in [d for d in channels], t = 0.0, t + c.risk)`)
	if err != nil {
		t.Fatal(err)
	}
	in := viewedInput()
	in.Channels[5].Risk = math.Inf(1)
	got, gotErr := viewed.Run(t.Context(), in)
	want, wantErr := loaded.Run(t.Context(), in)
	if gotErr != nil || wantErr != nil || !math.IsInf(got, 1) || !math.IsInf(want, 1) {
		t.Errorf("viewed: %v, %v; loaded: %v, %v; want +Inf from both", got, gotErr, want, wantErr)
	}
}

type channelOut struct {
	Name string  `funroute:"name"`
	Fee  int64   `funroute:"fee"`
	Risk float64 `funroute:"risk"`
	OK   bool    `funroute:"ok"`
}

// A view answered into a slice of records is copied field by field: into a
// slice of the host's own struct, or of another with the same fields, the
// same as the records it holds are written.
func TestAViewIsAnsweredAsItsRecords(t *testing.T) {
	t.Parallel()
	source := `sort_by(channels, [c.fee for c in channels])`
	in := viewedInput()
	same := answerOf[[]viewedChannel](t, source, in)
	other := answerOf[[]channelOut](t, source, in)
	if len(same) != len(in.Channels) || len(other) != len(same) {
		t.Fatalf("answered %d and %d items, want %d", len(same), len(other), len(in.Channels))
	}
	for i := range same {
		if same[i] != viewedChannel(other[i]) || i > 0 && same[i-1].Fee > same[i].Fee {
			t.Fatalf("item %d: %+v and %+v, want the same, in order of fee", i, same[i], other[i])
		}
	}
}

// answerOf is source's answer on in, as an Out.
func answerOf[Out any](t *testing.T, source string, in *viewedIn) Out {
	t.Helper()
	binding, err := compile.Bind[viewedIn, Out](viewedRegistry(t))
	if err != nil {
		t.Fatal(err)
	}
	program, err := binding.Compile(source)
	if err != nil {
		t.Fatal(err)
	}
	out, err := program.Run(t.Context(), in)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// Sorting a view costs the same allocations for ten records as for a
// thousand: the keys, the order, the answer — no record per item.
func TestSortingAViewAllocatesByTheArray(t *testing.T) {
	binding, err := compile.Bind[viewedIn, []viewedChannel](viewedRegistry(t))
	if err != nil {
		t.Fatal(err)
	}
	program, err := binding.Compile(`sort_by(channels, [c.fee for c in channels])`)
	if err != nil {
		t.Fatal(err)
	}
	allocs := func(n int) float64 {
		in := &viewedIn{Channels: make([]viewedChannel, n)}
		ctx := t.Context()
		return testing.AllocsPerRun(50, func() {
			if _, err := program.Run(ctx, in); err != nil {
				t.Fatal(err)
			}
		})
	}
	if few, many := allocs(10), allocs(1000); few != many {
		t.Errorf("a run allocated %v times for 10 records and %v for 1000, want the same", few, many)
	}
}

type pricedChannel struct {
	Name string         `funroute:"name"`
	Fee  money.Money    `funroute:"fee"`
	Cur  money.Currency `funroute:"cur"`
	Risk float64        `funroute:"risk"`
	OK   bool           `funroute:"ok"`
}

type pricedIn struct {
	Channels []pricedChannel `funroute:"channels"`
}

func pricedRegistry(t *testing.T) *machine.Registry {
	t.Helper()
	registry := moneyRegistry(t)
	if err := registry.EnableForm(machine.ForForm, machine.ReduceForm); err != nil {
		t.Fatal(err)
	}
	return registry
}

func pricedInput() *pricedIn {
	in := &pricedIn{}
	for i := range 40 {
		code := []string{"USD", "EUR"}[i%2]
		in.Channels = append(in.Channels, pricedChannel{
			Name: string(rune('a' + i%26)), Fee: machine.NewMoney("USD", int64(i*7%50)), Cur: machine.NewCurrency(code),
			Risk: float64(i%10) / 10, OK: i%3 != 0,
		})
	}
	return in
}

// pricedSources read an array of records with an amount and a currency
// among their fields: in a loop, indexed, and as a view the library sorts,
// cuts, joins or compares.
var pricedSources = []string{
	`string(minor(reduce(c in channels, t = USD 0, t + c.fee)))`,
	`string(len([c.name for c in channels if c.ok && c.fee > USD 0.10]))`,
	`string(len([c for c in channels if c.cur == USD]))`,
	`string(reduce(c in channels if c.risk > 0.5, t = 0.0, t + c.risk))`,
	`string(minor([c.fee for c in channels][3])) + string(channels[4].cur == EUR)`,
	`string(minor(channels[3].fee))`,
	`sort_by(channels, [c.fee for c in channels])[0].name`,
	`top_k(channels, [c.fee for c in channels], 4)[3].name`,
	`take(sort_by(channels, [c.risk for c in channels]), 3)[2].name`,
	`reverse(channels)[0].name + string(minor(slice(channels, 3, 9)[5].fee))`,
	`string(index_of(channels, channels[7]))`,
	`string(channels == reverse(reverse(channels)))`,
	`string(len(unique(concat(channels, channels))))`,
	`string(len([c for c in slice(channels, 2, 30) if c.ok]))`,
}

// A loop over an array of records with an amount and a currency in the
// host's struct answers as the same loop over the records does: the view
// loads those fields where a record is asked for, and the loop the ones it
// reads.
func TestAViewOfPricedRecordsAnswersAsItsRecords(t *testing.T) {
	t.Parallel()
	binding, err := compile.Bind[pricedIn, string](pricedRegistry(t))
	if err != nil {
		t.Fatal(err)
	}
	in := pricedInput()
	records := pricedRecords(t, in.Channels)
	for _, source := range pricedSources {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			program, err := binding.Compile(source)
			if err != nil {
				t.Fatal(err)
			}
			byStruct, err := program.Run(t.Context(), in)
			if err != nil {
				t.Fatal(err)
			}
			byRecords, err := program.Runtime().RunValues(t.Context(), []machine.Value{records})
			if err != nil {
				t.Fatal(err)
			}
			if text, _ := byRecords.String(); text != byStruct {
				t.Errorf("from the struct %q, from the records %q; want the same", byStruct, text)
			}
			if machine.IdleFrameHoldsPointers(program.Runtime()) {
				t.Error("the idle frame still points at the run; want nothing")
			}
		})
	}
}

// pricedRecords is the channels as an array of record values.
func pricedRecords(t *testing.T, channels []pricedChannel) machine.Value {
	t.Helper()
	typ := machine.RecordOf(machine.FieldOf("name", machine.StringType), machine.FieldOf("fee", machine.MoneyType),
		machine.FieldOf("cur", machine.CurrencyType), machine.FieldOf("risk", machine.FloatType), machine.FieldOf("ok", machine.BoolType))
	items := make([]machine.Value, len(channels))
	for i, c := range channels {
		item, err := machine.Record(typ, []machine.Value{machine.String(c.Name), machine.MoneyValue(c.Fee.Minor(), c.Fee.Currency()),
			machine.CurrencyValue(c.Cur.Code()), machine.Float(c.Risk), machine.Bool(c.OK)})
		if err != nil {
			t.Fatal(err)
		}
		items[i] = item
	}
	array, err := machine.Array(typ, items)
	if err != nil {
		t.Fatal(err)
	}
	return array
}

// A view of priced records answered into a slice of the host's struct is
// written through the records, the amounts and currencies as they were.
func TestAViewOfPricedRecordsIsAnsweredAsItsRecords(t *testing.T) {
	t.Parallel()
	binding, err := compile.Bind[pricedIn, []pricedChannel](pricedRegistry(t))
	if err != nil {
		t.Fatal(err)
	}
	program, err := binding.Compile(`sort_by(channels, [c.fee for c in channels])`)
	if err != nil {
		t.Fatal(err)
	}
	in := pricedInput()
	sorted, err := program.Run(t.Context(), in)
	if err != nil || len(sorted) != len(in.Channels) {
		t.Fatalf("answered %d items, %v; want %d", len(sorted), err, len(in.Channels))
	}
	for i := range sorted {
		if i > 0 && sorted[i-1].Fee.Minor() > sorted[i].Fee.Minor() || !slices.Contains(in.Channels, sorted[i]) {
			t.Fatalf("item %d: %+v, want the channels in order of fee, each one as it was", i, sorted[i])
		}
	}
}

// A view of priced records is written into the host's slice field by field,
// the amounts and currencies too: no record made of an item, so what it
// allocates does not grow with the array.
func TestAnsweringAViewOfPricedRecordsAllocatesByTheArray(t *testing.T) {
	binding, err := compile.Bind[pricedIn, []pricedChannel](pricedRegistry(t))
	if err != nil {
		t.Fatal(err)
	}
	program, err := binding.Compile(`sort_by(channels, [c.risk for c in channels])`)
	if err != nil {
		t.Fatal(err)
	}
	allocs := func(n int) float64 {
		in := &pricedIn{}
		for i := range n {
			in.Channels = append(in.Channels, pricedChannel{Name: "c", Fee: machine.NewMoney("USD", int64(i)), Cur: machine.NewCurrency("EUR"), Risk: float64(i%7) / 7})
		}
		return testing.AllocsPerRun(100, func() {
			if _, err := program.Run(t.Context(), in); err != nil {
				t.Fatal(err)
			}
		})
	}
	if short, long := allocs(10), allocs(1000); short != long {
		t.Fatalf("answering a sorted view allocates %v times over 10 channels and %v over 1000; want the same", short, long)
	}
}

// timedChannel is a priced channel with a time, which a view does not
// carry: an array of them is loaded as records and checked item by item.
type timedChannel struct {
	Name string         `funroute:"name"`
	Fee  money.Money    `funroute:"fee"`
	Cur  money.Currency `funroute:"cur"`
	Risk float64        `funroute:"risk"`
	OK   bool           `funroute:"ok"`
	At   time.Time      `funroute:"at"`
}

type timedIn struct {
	Channels []timedChannel `funroute:"channels"`
}

// The boundary checks the currencies of a viewed array where they are, and
// refuses one as it refuses the records it loads of an array it does not
// view: the same item, the same field, the same words.
func TestAViewOfPricedRecordsIsCheckedAsItsRecords(t *testing.T) {
	t.Parallel()
	viewed, err := compile.Bind[pricedIn, int64](pricedRegistry(t))
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := compile.Bind[timedIn, int64](pricedRegistry(t))
	if err != nil {
		t.Fatal(err)
	}
	byView, err := viewed.Compile(`len(channels)`)
	if err != nil {
		t.Fatal(err)
	}
	byRecords, err := loaded.Compile(`len(channels)`)
	if err != nil {
		t.Fatal(err)
	}
	for name, spoil := range map[string]func(*pricedIn){
		"an undeclared amount":   func(in *pricedIn) { in.Channels[5].Fee = machine.NewMoney("GBP", 1) },
		"an amount of none":      func(in *pricedIn) { in.Channels[6].Fee = machine.NewMoney("", 3) },
		"an undeclared currency": func(in *pricedIn) { in.Channels[7].Cur = machine.NewCurrency("GBP") },
	} {
		in := pricedInput()
		spoil(in)
		timed := &timedIn{}
		for _, c := range in.Channels {
			timed.Channels = append(timed.Channels, timedChannel{Name: c.Name, Fee: c.Fee, Cur: c.Cur, Risk: c.Risk, OK: c.OK, At: time.Unix(0, 0)})
		}
		_, viewErr := byView.Run(t.Context(), in)
		_, recordsErr := byRecords.Run(t.Context(), timed)
		if !errors.Is(viewErr, machine.ErrContract) || recordsErr == nil || viewErr.Error() != recordsErr.Error() {
			t.Errorf("%s: the view is refused with %v, the records with %v; want the same contract error", name, viewErr, recordsErr)
		}
		var batchErr error
		byView.RunBatch(t.Context(), 1, func(int) *pricedIn { return in }, func(int) *int64 { return new(int64) }, func(_ int, err error) { batchErr = err })
		if batchErr == nil || batchErr.Error() != viewErr.Error() {
			t.Errorf("%s: RunBatch refuses the view with %v, Run with %v; want the same", name, batchErr, viewErr)
		}
	}
}

// Walking a viewed array of priced records, and checking its currencies,
// costs no allocation.
func TestAViewOfPricedRecordsAllocatesNothing(t *testing.T) {
	binding, err := compile.Bind[pricedIn, float64](pricedRegistry(t))
	if err != nil {
		t.Fatal(err)
	}
	program, err := binding.Compile(`reduce(c in channels if c.ok && c.fee > USD 0.10, t = 0.0, t + c.risk)`)
	if err != nil {
		t.Fatal(err)
	}
	in := pricedInput()
	ctx := t.Context()
	if allocs := testing.AllocsPerRun(100, func() {
		if _, err := program.Run(ctx, in); err != nil {
			t.Fatal(err)
		}
	}); allocs != 0 {
		t.Errorf("a run allocated %v times, want 0", allocs)
	}
}

// Two views compared, and the amounts of a view met before they are, are
// read where they are: what == costs does not grow with the arrays.
func TestComparingViewsAllocatesByNothing(t *testing.T) {
	binding, err := compile.Bind[pricedIn, bool](pricedRegistry(t))
	if err != nil {
		t.Fatal(err)
	}
	program, err := binding.Compile(`channels == channels && [channels[0]] != channels`)
	if err != nil {
		t.Fatal(err)
	}
	allocs := func(n int) float64 {
		in := &pricedIn{}
		for i := range n {
			in.Channels = append(in.Channels, pricedChannel{Name: "c", Fee: machine.NewMoney("USD", int64(i)), Cur: machine.NewCurrency("EUR")})
		}
		return testing.AllocsPerRun(100, func() {
			if same, err := program.Run(t.Context(), in); err != nil || !same {
				t.Fatalf("channels == channels = %v, %v, want true", same, err)
			}
		})
	}
	if short, long := allocs(10), allocs(1000); short != long {
		t.Fatalf("comparing views allocates %v times over 10 channels and %v over 1000; want the same", short, long)
	}
}
