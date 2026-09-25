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
