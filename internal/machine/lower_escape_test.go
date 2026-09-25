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

// An array of plain records is handed over in place when the program only
// walks or measures it and reads each item's fields: through let, in nested
// loops, under a filter — or never reads an item, as a count does. An item that goes anywhere else — collected,
// compared, answered, handed to a function — or an index into the array,
// which would make a record of an item, lets the array out.
func TestAnArrayOfRecordsIsViewedWhileItsItemsStayPut(t *testing.T) {
	t.Parallel()
	registry := randomRegistry(t)
	channel := machine.RecordOf(machine.FieldOf("name", machine.StringType), machine.FieldOf("fee", machine.IntType), machine.FieldOf("ok", machine.BoolType))
	options := compile.CompileOptions{Args: []compile.ArgSpec{{Name: "cs", Type: machine.ArrayOf(channel)}}}
	for _, test := range []struct {
		source string
		viewed bool
	}{
		{`len([c.name for c in cs if c.ok])`, true},
		{`len(cs)`, true},
		{`let(d = cs, len([x.fee for x in d]))`, true},
		{`len([c.fee for c in cs for d in cs if d.fee > c.fee])`, true},
		{`len([let(n = c, n.fee) for c in cs])`, true},
		{`len([c for c in cs])`, true},
		{`len([[c] for c in cs])`, false},
		{`cs[0].fee`, false},
		{`len([c == c for c in cs])`, false},
		{`cs`, false},
		{`len([{name: c.name, fee: c.fee, ok: c.ok} == c for c in cs])`, false},
	} {
		artifact, err := compile.CompileExpr(test.source, registry, options)
		if err != nil {
			t.Fatalf("%s: %v", test.source, err)
		}
		runtime, err := machine.Instantiate(artifact, registry)
		if err != nil {
			t.Fatal(err)
		}
		if got := machine.Flow(runtime).ViewOnly[0]; got != test.viewed {
			t.Errorf("%s: viewed %v, want %v", test.source, got, test.viewed)
		}
	}
}
