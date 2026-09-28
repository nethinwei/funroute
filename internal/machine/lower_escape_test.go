package machine_test

import (
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/nethinwei/funroute/internal/compile"
	"github.com/nethinwei/funroute/internal/machine"
)

// Which arrays stay in the run, which build the answer, and which record
// arguments are only read field by field.
func TestValueFlowFindsWhereEachArrayGoes(t *testing.T) {
	t.Parallel()
	registry := randomRegistry(t)
	pair := machine.RecordOf(machine.FieldOf("p", machine.IntType), machine.FieldOf("q", machine.IntType))
	options := compile.CompileOptions{Args: []compile.ArgSpec{
		{Name: "xs", Type: machine.ArrayOf(machine.IntType)}, {Name: "a", Type: machine.IntType},
		{Name: "r", Type: pair},
	}}
	for _, test := range []struct {
		source, arena, dest string
		answer              bool
		fieldOnly           bool
	}{
		{source: `[x + 1 for x in xs]`, dest: "loop_init=0"},
		{source: `len([x + 1 for x in xs])`, arena: "loop_init"},
		{source: `[x + 1 for x in xs][a]`, arena: "loop_init"},
		{source: `let(ys = [x * 2 for x in xs], [y + 1 for y in ys])`, arena: "loop_init", dest: "loop_init=0"},
		{source: `[y for y in [x * 2 for x in xs] if y > 1]`, arena: "loop_init", dest: "loop_init=0"},
		{source: `host.total_v1([x * 2 for x in xs])`},
		{source: `[[x] for x in xs]`},
		{source: `if(a > 0, [a], [x for x in xs])`},
		{source: `{p: len(xs), q: [x + 1 for x in xs]}`, dest: "loop_init=2", answer: true},
		{source: `r.p + r.q`, fieldOnly: true},
		{source: `r`},
		{source: `let(ys = [x for x in xs], {p: len(ys), q: ys})`, answer: true},
	} {
		artifact, err := compile.CompileExpr(test.source, registry, options)
		if err != nil {
			t.Fatalf("%s: %v", test.source, err)
		}
		runtime, err := machine.Instantiate(artifact, registry)
		if err != nil {
			t.Fatal(err)
		}
		facts := machine.Flow(runtime)
		arena, dest := ops(facts.Arena), destOps(facts.Dest)
		readsR := strings.HasPrefix(test.source, "r")
		if arena != test.arena || dest != test.dest || (facts.Answer != "") != test.answer || readsR && facts.FieldOnly[2] != test.fieldOnly {
			t.Errorf("%s: arena %q, dest %q, answer %q, field-only %v; want %q, %q, %v, %v",
				test.source, arena, dest, facts.Answer, facts.FieldOnly[2], test.arena, test.dest, test.answer, test.fieldOnly)
		}
	}
}

// ops is the opcodes of producers written op@pc, in order.
func ops(producers []string) string {
	names := make([]string, len(producers))
	for i, producer := range producers {
		names[i], _, _ = strings.Cut(producer, "@")
	}
	return strings.Join(names, " ")
}

// destOps is the answer's producers as op=dest, in pc order.
func destOps(dest map[string]int) string {
	out := make([]string, 0, len(dest))
	for _, producer := range slices.Sorted(maps.Keys(dest)) {
		op, _, _ := strings.Cut(producer, "@")
		out = append(out, op+"="+string(rune('0'+dest[producer])))
	}
	return strings.Join(out, " ")
}

// An array of plain records is handed over in the frame's own slot when the
// program only walks or measures it — through let, in nested loops, under a
// filter; an index into it, or the array itself answered or handed on, lets
// it out. Apart from that, a loop loads its items into the frame's own
// record when it only reads their fields; an item that goes anywhere else —
// collected, compared, put in a record — is made a record of its own, and
// the loop's items do not stay put.
func TestAnArrayOfRecordsIsViewedWhileItsItemsStayPut(t *testing.T) {
	t.Parallel()
	registry := randomRegistry(t)
	channel := machine.RecordOf(machine.FieldOf("name", machine.StringType), machine.FieldOf("fee", machine.IntType), machine.FieldOf("ok", machine.BoolType))
	options := compile.CompileOptions{Args: []compile.ArgSpec{{Name: "cs", Type: machine.ArrayOf(channel)}}}
	for _, test := range []struct {
		source string
		viewed bool
		items  int
	}{
		{`len([c.name for c in cs if c.ok])`, true, 1},
		{`len(cs)`, true, 0},
		{`let(d = cs, len([x.fee for x in d]))`, true, 1},
		{`len([c.fee for c in cs for d in cs if d.fee > c.fee])`, true, 2},
		{`len([let(n = c, n.fee) for c in cs])`, true, 1},
		{`len([c for c in cs])`, true, 1}, // a count: the items are never collected
		{`[c for c in cs]`, true, 0},
		{`len([[c] for c in cs])`, true, 0},
		{`cs[0].fee`, false, 0},
		{`len([c == c for c in cs])`, true, 0},
		{`cs`, false, 0},
		{`len([{name: c.name, fee: c.fee, ok: c.ok} == c for c in cs])`, true, 0},
		{`len([c.fee for c in cs if c.ok]) + len([[c] for c in cs])`, true, 1},
	} {
		artifact, err := compile.CompileExpr(test.source, registry, options)
		if err != nil {
			t.Fatalf("%s: %v", test.source, err)
		}
		runtime, err := machine.Instantiate(artifact, registry)
		if err != nil {
			t.Fatal(err)
		}
		facts := machine.Flow(runtime)
		if facts.ViewOnly[0] != test.viewed || facts.ItemsInPlace != test.items {
			t.Errorf("%s: viewed %v with %d loops' items in place, want %v and %d", test.source, facts.ViewOnly[0], facts.ItemsInPlace, test.viewed, test.items)
		}
	}
}

type updateIn struct {
	Order promotedOrder `funroute:"order"`
	Fee   int64         `funroute:"fee"`
}

// A record argument updated by with is only read — its fields are copied
// into the update — so a Program reads it where it is, and an update the
// program answers is built in the frame's own record: none is made.
func TestAnUpdatedRecordIsReadInPlace(t *testing.T) {
	binding, err := compile.Bind[updateIn, promotedOrder](randomRegistry(t))
	if err != nil {
		t.Fatal(err)
	}
	program, err := binding.Compile(`order with {amount: order.amount + fee, flagged: true}`)
	if err != nil {
		t.Fatal(err)
	}
	facts := machine.Flow(program.Runtime())
	if !facts.FieldOnly[0] || !strings.HasPrefix(facts.Answer, "record_with@") {
		t.Fatalf("the record is read in place %v, the answer %q; want true and the update", facts.FieldOnly[0], facts.Answer)
	}
	in, ctx := &updateIn{Order: promotedOrder{Amount: 100, Currency: "sg", Risk: 0.5}, Fee: 7}, t.Context()
	var out promotedOrder
	if allocs := testing.AllocsPerRun(100, func() {
		if err := program.RunInto(ctx, in, &out); err != nil {
			t.Fatal(err)
		}
	}); allocs != 0 || out != (promotedOrder{Amount: 107, Currency: "sg", Risk: 0.5, Flagged: true}) {
		t.Errorf("a run answered %+v and allocated %v times, want the update and 0", out, allocs)
	}
	// An update that is not the answer is a record of its own.
	nested, err := binding.Compile(`(order with {risk: 0.0}) with {amount: 1}`)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := nested.Run(ctx, in); err != nil || got.Amount != 1 || got.Currency != "sg" {
		t.Errorf("an update of an update = %+v, %v; want amount 1 of the order", got, err)
	}
}

type fieldsChannel struct {
	Name string `funroute:"name"`
	Fee  int64  `funroute:"fee"`
	OK   bool   `funroute:"ok"`
}

type fieldsIn struct {
	Channels []fieldsChannel `funroute:"cs"`
}

// A loop whose items are read in place loads only the fields its body reads,
// through a let or an inner loop too, and answers as the same program over
// the records themselves.
func TestAnItemReadInPlaceLoadsTheFieldsItsBodyReads(t *testing.T) {
	t.Parallel()
	registry := randomRegistry(t)
	binding, err := compile.Bind[fieldsIn, int64](registry)
	if err != nil {
		t.Fatal(err)
	}
	in := &fieldsIn{Channels: []fieldsChannel{{"a", 30, true}, {"bb", 10, false}, {"ccc", 20, true}}}
	records := make([]any, len(in.Channels))
	for i, c := range in.Channels {
		records[i] = map[string]any{"name": c.Name, "fee": c.Fee, "ok": c.OK}
	}
	for _, source := range []string{
		`sum([c.fee for c in cs if c.ok])`,
		`sum([let(n = c, n.fee + len(n.name)) for c in cs])`,
		`len([c for c in cs if c.ok && c.fee < 25])`,
		`len([c.name for c in cs for d in cs if d.fee > c.fee && d.ok])`,
		`sum([len(c.name) for c in cs if !c.ok || c.fee > 25])`,
	} {
		program, err := binding.Compile(source)
		if err != nil {
			t.Fatalf("%s: %v", source, err)
		}
		if machine.Flow(program.Runtime()).ItemsInPlace == 0 {
			t.Errorf("%s reads no loop's items in place", source)
		}
		got, err := program.Run(t.Context(), in)
		want, wantErr := program.Runtime().Run(t.Context(), map[string]any{"cs": records})
		if err != nil || wantErr != nil || want.Any() != got {
			t.Errorf("%s = %v, %v; over the records %v, %v", source, got, err, want.Any(), wantErr)
		}
	}
}
