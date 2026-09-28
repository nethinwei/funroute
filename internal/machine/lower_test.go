package machine_test

import (
	"fmt"
	"math"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"

	"github.com/nethinwei/funroute/internal/compile"
	"github.com/nethinwei/funroute/internal/machine"
)

// The shapes most rules are made of lower to the few operations they need,
// each in its file: a value that is only read is read where it is, a result
// is made where it goes, a comparison is one with its branch, a
// comprehension's collect is one with its next — a filtered one's too, the
// next staying for the items the filter skips — and a nested one's inner
// clause fills the outer's array.
func TestLoweringFusesTheCommonShapes(t *testing.T) {
	t.Parallel()
	registry := randomRegistry(t)
	for _, test := range []struct{ source, ops string }{
		{`a * b / 10000 + a`, "mul_i div_i add_i halt"},
		{`if(a < b, a, b)`, "branch_lt_i move_i jump move_i halt"},
		{`let(c = a * b, c + a)`, "mul_i add_i halt"},
		{`reduce(x in xs, t = 0, t + x)`, "loop_init add_i loop_next halt"},
		{`[x + 1 for x in xs]`, "loop_init add_i collect_next_i halt"},
		{`[x for x in xs if x > 0]`, "loop_init branch_lt_i collect_next_i loop_next halt"},
		{`[x * y for x in xs for y in xs]`, "loop_init loop_init mul_i collect_next_i loop_next halt"},
		{`[x for x in xs if x > 0 && x < 9]`, "loop_init branch_lt_i branch_lt_i collect_next_i loop_next halt"},
		{`[x for x in xs if !(x > 0) || x == 5]`, "loop_init branch_lt_i branch_eq_i collect_next_i loop_next halt"},
		{`len([c for c in cs if c.ok && c.fee < 50])`, "loop_init branch_field field_i branch_lt_i add_i loop_next halt"},
		{`"k" in d`, "has_key halt"},
	} {
		artifact, err := compile.CompileExpr(test.source, registry, shapesContract)
		if err != nil {
			t.Fatalf("%s: %v", test.source, err)
		}
		runtime, err := machine.Instantiate(artifact, registry)
		if err != nil {
			t.Fatal(err)
		}
		listing := machine.RegisterForm(runtime)
		var ops []string
		for line := range strings.Lines(listing) {
			ops = append(ops, strings.Fields(line)[1])
		}
		if got := strings.Join(ops, " "); got != test.ops {
			t.Errorf("%s lowers to %s, want %s:\n%s", test.source, got, test.ops, listing)
		}
	}
}

// shapesContract is randomContract with an array of records and a
// dictionary.
var shapesContract = compile.CompileOptions{Args: append(slices.Clone(randomContract.Args),
	compile.ArgSpec{Name: "cs", Type: machine.ArrayOf(machine.RecordOf(machine.FieldOf("ok", machine.BoolType), machine.FieldOf("fee", machine.IntType)))},
	compile.ArgSpec{Name: "d", Type: machine.DictOf(machine.IntType)})}

// generator writes random programs of the language: every form a lowering
// has a rule for, arithmetic that overflows and divides by zero, indexes
// outside their arrays, and a host function fallback takes the failures of.
type generator struct {
	rand *rand.Rand
	// ints and arrays are the names in scope of type int and array<int>.
	ints, arrays []string
	fresh        int
}

func (g *generator) pick(options ...string) string { return options[g.rand.IntN(len(options))] }

func (g *generator) name() string {
	g.fresh++
	return fmt.Sprintf("v%d", g.fresh)
}

// of is an expression of typ no deeper than depth.
func (g *generator) of(typ string, depth int) string {
	if depth <= 0 || g.rand.IntN(5) == 0 {
		return g.leaf(typ)
	}
	switch typ {
	case "int":
		return g.integer(depth - 1)
	case "float":
		return g.float(depth - 1)
	case "bool":
		return g.boolean(depth - 1)
	case "string":
		return g.text(depth - 1)
	}
	return g.array(depth - 1)
}

func (g *generator) leaf(typ string) string {
	switch typ {
	case "int":
		return g.pick(append([]string{"0", "1", "2", "3", "-1", "7", "9223372036854775807"}, g.ints...)...)
	case "float":
		return g.pick("f", "1.5", "0.0", "-2.25")
	case "bool":
		return g.pick("flag", "true", "false")
	case "string":
		return g.pick("s", `"a"`, `"zz"`)
	}
	return g.pick(g.arrays...)
}

func (g *generator) integer(d int) string {
	switch g.rand.IntN(13) {
	case 0:
		return fmt.Sprintf("(%s %s %s)", g.of("int", d), g.pick("+", "-", "*", "/", "%"), g.of("int", d))
	case 1:
		return fmt.Sprintf("if(%s, %s, %s)", g.of("bool", d), g.of("int", d), g.of("int", d))
	case 2:
		return g.let(d)
	case 3:
		return fmt.Sprintf("len(%s)", g.of("array", d))
	case 4:
		return fmt.Sprintf("%s[%s]", g.of("array", d), g.of("int", d))
	case 5:
		return g.reduce(d)
	case 6:
		return fmt.Sprintf("fallback(host.flaky_v1(%s), %s)", g.of("int", d), g.of("int", d))
	case 7:
		return fmt.Sprintf("switch(case %s => %s, case %s => %s, else => %s)",
			g.of("bool", d), g.of("int", d), g.of("bool", d), g.of("int", d), g.of("int", d))
	case 8:
		return fmt.Sprintf("host.flaky_v1(%s)", g.of("int", d))
	case 9:
		return g.dictionary(d)
	case 10:
		return fmt.Sprintf("host.total_v1(%s)", g.of("array", d))
	case 11:
		return fmt.Sprintf("len(%s) + %s[0]", g.of("array", d), g.of("array", d))
	}
	return fmt.Sprintf("int(%s)", g.of("float", d))
}

// dictionary builds a dictionary — its keys may repeat — and reads it: its
// length, an entry, or a walk over it.
func (g *generator) dictionary(d int) string {
	source, item := g.of("array", d), g.name()
	g.ints = append(g.ints, item)
	key, value := g.of("int", d), g.of("int", d)
	g.ints = g.ints[:len(g.ints)-1]
	built := fmt.Sprintf("{string(%s): %s for %s in %s}", key, value, item, source)
	switch g.rand.IntN(3) {
	case 0:
		return fmt.Sprintf("len(%s)", built)
	case 1:
		return fmt.Sprintf("%s[string(%s)]", built, g.of("int", d))
	}
	k, v := g.name(), g.name()
	g.ints = append(g.ints, v)
	body := g.of("int", d)
	g.ints = g.ints[:len(g.ints)-1]
	return fmt.Sprintf("len([%s for %s, %s in %s])", body, k, v, built)
}

// let binds a new int name for its body.
func (g *generator) let(d int) string {
	value, name := g.of("int", d), g.name()
	g.ints = append(g.ints, name)
	body := g.of("int", d)
	g.ints = g.ints[:len(g.ints)-1]
	return fmt.Sprintf("let(%s = %s, %s)", name, value, body)
}

// reduce folds an array, filtered or not, into an int.
func (g *generator) reduce(d int) string {
	source, seed := g.of("array", d), g.of("int", d)
	item, acc := g.name(), g.name()
	g.ints = append(g.ints, item, acc)
	filter := ""
	if g.rand.IntN(2) == 0 {
		filter = " if " + g.of("bool", d)
	}
	body := g.of("int", d)
	g.ints = g.ints[:len(g.ints)-2]
	return fmt.Sprintf("reduce(%s in %s%s, %s = %s, %s)", item, source, filter, acc, seed, body)
}

func (g *generator) float(d int) string {
	switch g.rand.IntN(4) {
	case 0:
		return fmt.Sprintf("(%s %s %s)", g.of("float", d), g.pick("+", "-", "*", "/"), g.of("float", d))
	case 1:
		return fmt.Sprintf("float(%s)", g.of("int", d))
	case 2:
		return fmt.Sprintf("fs[%s]", g.of("int", d))
	}
	return fmt.Sprintf("if(%s, %s, %s)", g.of("bool", d), g.of("float", d), g.of("float", d))
}

func (g *generator) boolean(d int) string {
	comparison := g.pick("<", "<=", ">", ">=", "==", "!=")
	switch g.rand.IntN(6) {
	case 0:
		return fmt.Sprintf("(%s %s %s)", g.of("int", d), comparison, g.of("int", d))
	case 1:
		return fmt.Sprintf("(%s %s %s)", g.of("float", d), comparison, g.of("float", d))
	case 2:
		return fmt.Sprintf("(%s %s %s)", g.of("string", d), comparison, g.of("string", d))
	case 3:
		return fmt.Sprintf("(%s %s %s)", g.of("bool", d), g.pick("&&", "||", "==", "!="), g.of("bool", d))
	case 4:
		return fmt.Sprintf("(%s in %s)", g.of("int", d), g.of("array", d))
	}
	return "!" + g.of("bool", d)
}

func (g *generator) text(d int) string {
	switch g.rand.IntN(3) {
	case 0:
		return fmt.Sprintf("(%s + %s)", g.of("string", d), g.of("string", d))
	case 1:
		return fmt.Sprintf("string(%s)", g.of("int", d))
	}
	return fmt.Sprintf("if(%s, %s, %s)", g.of("bool", d), g.of("string", d), g.of("string", d))
}

func (g *generator) array(d int) string {
	switch g.rand.IntN(5) {
	case 4:
		// An array bound, then walked twice: it stays in the run.
		name := g.name()
		value := g.of("array", d)
		g.arrays = append(g.arrays, name)
		body := g.comprehension(d, "")
		g.arrays = g.arrays[:len(g.arrays)-1]
		return fmt.Sprintf("let(%s = %s, %s)", name, value, body)
	case 0:
		return fmt.Sprintf("[%s, %s]", g.of("int", d), g.of("int", d))
	case 1:
		return g.comprehension(d, "")
	case 2:
		return g.comprehension(d, " if "+"%s")
	}
	return g.nested(d)
}

// comprehension maps an array; filter, when set, is where its condition
// goes.
func (g *generator) comprehension(d int, filter string) string {
	source, item := g.of("array", d), g.name()
	g.ints = append(g.ints, item)
	body := g.of("int", d)
	if filter != "" {
		filter = fmt.Sprintf(filter, g.of("bool", d))
	}
	g.ints = g.ints[:len(g.ints)-1]
	return fmt.Sprintf("[%s for %s in %s%s]", body, item, source, filter)
}

// nested is a comprehension of two clauses.
func (g *generator) nested(d int) string {
	outer, inner := g.of("array", d), g.of("array", d)
	x, y := g.name(), g.name()
	g.ints = append(g.ints, x, y)
	body := g.of("int", d)
	g.ints = g.ints[:len(g.ints)-2]
	return fmt.Sprintf("[%s for %s in %s for %s in %s]", body, x, outer, y, inner)
}

// args is one request, with the edges of every type likely.
func (g *generator) args() map[string]any {
	ints := []int64{0, 1, -1, 2, 3, 5, 7, math.MaxInt64, math.MinInt64}
	integer := func() any { return ints[g.rand.IntN(len(ints))] }
	floats := []float64{0, 1.5, -2.25, 3}
	float := func() any { return floats[g.rand.IntN(len(floats))] }
	list := func(item func() any) []any {
		out := make([]any, g.rand.IntN(5))
		for i := range out {
			out[i] = item()
		}
		return out
	}
	return map[string]any{
		"a": integer(), "b": integer(), "f": float(), "s": strings.Repeat("m", g.rand.IntN(3)),
		"flag": g.rand.IntN(2) == 0, "xs": list(integer), "fs": list(float),
	}
}
