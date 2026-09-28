package golden

import (
	"fmt"
	"math/rand/v2"
	"strings"
)

// generator writes random programs of a type out of rules: a rule is a
// template whose {type} holes are filled by expressions of that type, so
// every program it writes is typed, and most compile.
type generator struct {
	rand *rand.Rand
	// scope is, by type, the names an expression may read: the contract's,
	// and those a let, a loop or a reduce binds around it.
	scope map[string][]string
	fresh int
	// loops is how many of the loops it wrote the expression is inside:
	// past two, a program's run grows as the cube of an input.
	loops int
}

// newGenerator is the generator of one program: attempt is how many were
// written for it before and refused.
func newGenerator(seed, attempt uint64) *generator {
	return &generator{rand: rand.New(rand.NewPCG(seed, 11+attempt)), scope: map[string][]string{
		"int": {"a", "b"}, "float": {"f", "g"}, "bool": {"flag"}, "string": {"s", "t"},
		"ints": {"xs", "ys"}, "floats": {"fs"}, "strings": {"ss"}, "dict": {"d"},
		"order": {"order"}, "chans": {"chans"},
	}}
}

func (g *generator) pick(options ...string) string { return options[g.rand.IntN(len(options))] }

// of is an expression of typ, no deeper than depth.
func (g *generator) of(typ string, depth int) string {
	if depth <= 0 || g.rand.IntN(5) == 0 {
		return g.leaf(typ)
	}
	if g.rand.IntN(4) == 0 {
		if source, ok := g.bound(typ, depth-1); ok {
			return source
		}
	}
	options := rules[typ]
	return g.fill(options[g.rand.IntN(len(options))], depth-1)
}

// leaf is a literal or a name in scope.
func (g *generator) leaf(typ string) string {
	return g.pick(append(append([]string(nil), literals[typ]...), g.scope[typ]...)...)
}

// fill writes template with each {type} hole an expression of that type; a
// brace that opens anything else — a record or a dictionary — is written as
// it is.
func (g *generator) fill(template string, depth int) string {
	var out strings.Builder
	for i := 0; i < len(template); i++ {
		end := strings.IndexByte(template[i:], '}')
		if template[i] != '{' || end < 0 || !isHole(template[i+1:i+end]) {
			out.WriteByte(template[i])
			continue
		}
		out.WriteString(g.of(template[i+1:i+end], depth))
		i += end
	}
	return out.String()
}

// isHole reports a {name} that names a type rather than a record literal.
func isHole(name string) bool {
	_, ok := rules[name]
	return ok
}

// within writes body with name bound as a value of typ.
func (g *generator) within(typ string, body func(name string) string) string {
	g.fresh++
	name := fmt.Sprintf("v%d", g.fresh)
	g.scope[typ] = append(g.scope[typ], name)
	defer func() { g.scope[typ] = g.scope[typ][:len(g.scope[typ])-1] }()
	return body(name)
}

// element is the type of an array type's items.
var element = map[string]string{"ints": "int", "floats": "float", "strings": "string"}

var literals = map[string][]string{
	"int":    {"0", "1", "2", "3", "-1", "7", "250", "9223372036854775807"},
	"float":  {"1.5", "0.0", "-2.25", "0.1"},
	"bool":   {"true", "false"},
	"string": {`"a"`, `"zz"`, `""`, `"sg"`},
}

// rules are the templates of each type's expressions.
var rules = map[string][]string{
	"int": {
		"({int} + {int})", "({int} - {int})", "({int} * {int})", "({int} / {int})", "({int} % {int})",
		"if({bool}, {int}, {int})", "len({ints})", "len({string})", "len({dict})", "len({strings})", "len({chans})",
		"{ints}[{int}]", "int({float})", "ceil({float})", "floor({float})", "round({float})",
		"sum({ints})", "min({ints})", "max({ints})", "max({int}, {int})", "min({int}, {int})", "abs({int})",
		"first({ints})", "last({ints})", "index_of({ints}, {int})", "arg_min({ints})", "arg_max({floats})",
		"get({dict}, {string}, {int})", "{dict}[{string}]", "order.amount", "order.fee", "{chans}[{int}].fee",
		"fallback(host.flaky_v1({int}), {int})", "host.flaky_v1({int})", "host.total_v1({ints})",
		"switch(case {bool} => {int}, case {bool} => {int}, else => {int})",
		"switch({int}, case 1 => {int}, case 2, 3 => {int}, else => {int})",
		"sum([c.fee for c in {chans} if c.ok])", "arg_min([c.fee for c in {chans}])",
	},
	"float": {
		"({float} + {float})", "({float} - {float})", "({float} * {float})", "({float} / {float})",
		"float({int})", "{floats}[{int}]", "if({bool}, {float}, {float})", "avg({ints})", "avg({floats})",
		"median({floats})", "sum({floats})", "min({floats})", "max({floats})", "abs({float})",
		"pow({float}, float({int}))", "stddev({floats})", "{chans}[{int}].risk", "sum([c.risk for c in {chans}])",
	},
	"bool": {
		"({int} < {int})", "({int} <= {int})", "({int} == {int})", "({int} != {int})",
		"({float} < {float})", "({float} >= {float})", "({float} == {float})", "({float} != {float})",
		"({string} < {string})", "({string} == {string})", "({bool} && {bool})", "({bool} || {bool})", "!{bool}",
		"({int} in {ints})", "({string} in {strings})", "({string} in {dict})", "contains({string}, {string})",
		"starts_with({string}, {string})", "ends_with({string}, {string})", "{chans}[{int}].ok",
		"any([c.ok for c in {chans}])", "all([c.fee > {int} for c in {chans}])",
	},
	"string": {
		"({string} + {string})", "string({int})", "string({float})", "if({bool}, {string}, {string})",
		"upper({string})", "lower({string})", "trim({string})", "replace({string}, {string}, {string})",
		`join({strings}, ",")`, "{strings}[{int}]", "order.tag", "{chans}[{int}].name", "slice({string}, {int}, {int})",
	},
	"ints": {
		"[{int}, {int}]", "sort({ints})", "sort_desc({ints})", "reverse({ints})", "take({ints}, {int})",
		"slice({ints}, {int}, {int})", "concat({ints}, {ints})", "range(len({ints}))", "unique({ints})",
		"cumsum({ints})", "deltas({ints})", "top_k(xs, xs, {int})", "sort_by(ys, [y * {int} for y in ys])",
		"except({ints}, {ints})", "intersect({ints}, {ints})", "flatten([{ints}, {ints}])", "[c.fee for c in {chans}]",
	},
	"floats": {
		"[{float}, {float}]", "sort({floats})", "sort_desc({floats})", "reverse({floats})", "cumsum({floats})",
		"[float(x) for x in {ints}]", "[c.risk for c in {chans} if c.ok]", "take({floats}, {int})",
	},
	"strings": {
		"[{string}, {string}]", "sort({strings})", "unique({strings})", `split({string}, ",")`,
		"[c.name for c in {chans}]", "take({strings}, {int})", "reverse({strings})",
	},
	"dict": {
		"{string(x): x for x in {ints}}", "merge({dict}, {dict})", `{"k": {int}, "j": {int}}`,
	},
	"order": {
		"order with {fee: {int}}", "order with {tag: {string}}", "if({bool}, order, order with {amount: {int}})",
	},
	"chans": {
		"[c for c in {chans} if c.ok]", "take({chans}, {int})", "sort_by(chans, [c.fee for c in chans])",
		"sort_by(chans, [c.risk for c in chans])", "top_k(chans, [c.fee for c in chans], {int})",
		"reverse({chans})", "slice({chans}, {int}, {int})", "concat({chans}, {chans})",
	},
}

// bound is an expression of typ that binds a name — let, a comprehension,
// reduce — or false for a type none binds for.
func (g *generator) bound(typ string, d int) (string, bool) {
	if g.loops >= 2 {
		return "", false
	}
	switch typ {
	case "int":
		return g.boundInt(d), true
	case "bool":
		return "any(" + g.comprehension("ints", "bool", d) + ")", true
	case "float":
		return "sum(" + g.comprehension("floats", "float", d) + ")", true
	case "ints":
		// Two clauses multiply their sources' lengths: only over names, so
		// that no program grows past the square of an input.
		if d == 0 {
			return g.nested(d), true
		}
		return g.comprehension("ints", "int", d), true
	case "floats":
		return g.comprehension("floats", "float", d), true
	}
	return "", false
}

func (g *generator) boundInt(d int) string {
	switch g.rand.IntN(3) {
	case 0:
		return g.let(d)
	case 1:
		return g.reduce(d)
	}
	return "len(" + g.comprehension("ints", "int", d) + ")"
}

func (g *generator) let(d int) string {
	value := g.of("int", d)
	return g.within("int", func(v string) string { return fmt.Sprintf("let(%s = %s, %s)", v, value, g.of("int", d)) })
}

func (g *generator) reduce(d int) string {
	source, seed := g.of("ints", d), g.of("int", d)
	g.loops++
	defer func() { g.loops-- }()
	return g.within("int", func(item string) string {
		// The filter sees the item, not the answer so far.
		filter := g.of("bool", d)
		return g.within("int", func(acc string) string {
			return fmt.Sprintf("reduce(%s in %s if %s, %s = %s, %s)", item, source, filter, acc, seed, g.of("int", d))
		})
	})
}

// nested is a comprehension of two clauses.
func (g *generator) nested(d int) string {
	outer, inner := g.of("ints", d), g.of("ints", d)
	g.loops++
	defer func() { g.loops-- }()
	return g.within("int", func(x string) string {
		return g.within("int", func(y string) string {
			return fmt.Sprintf("[%s for %s in %s for %s in %s]", g.of("int", d), x, outer, y, inner)
		})
	})
}

// comprehension maps an array of source's type to body's type, filtered or
// not.
func (g *generator) comprehension(source, body string, d int) string {
	from := g.of(source, d)
	g.loops++
	defer func() { g.loops-- }()
	return g.within(element[source], func(item string) string {
		filter := ""
		if g.rand.IntN(2) == 0 {
			filter = " if " + g.of("bool", d)
		}
		return fmt.Sprintf("[%s for %s in %s%s]", g.of(body, d), item, from, filter)
	})
}
