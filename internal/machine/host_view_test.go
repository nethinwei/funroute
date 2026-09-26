package machine_test

import (
	"math"
	"testing"

	"github.com/nethinwei/funroute/internal/compile"
	"github.com/nethinwei/funroute/internal/machine"
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
	for _, source := range []string{
		`string(reduce(c in channels, t = 0, t + c.fee))`,
		`string(len([c.name for c in channels if c.ok && c.fee < limit]))`,
		`string(reduce(c in channels if c.risk > 0.5, t = 0.0, t + c.risk))`,
		`string(len([c.fee for c in channels for d in channels if d.fee > c.fee]))`,
		`string(len(channels)) + reduce(c in channels, s = "", s + c.name)`,
		`string(len([[c] for c in channels]))`,
		`string(channels[3].fee)`,
	} {
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
			if machine.IdleFrameHoldsPointers(program.Runtime()) {
				t.Error("the idle frame still points at the run; want nothing")
			}
		})
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
