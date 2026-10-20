package syntax

import (
	"strings"
	"testing"
)

func TestScopeAtFollowsTheBindsTags(t *testing.T) {
	t.Parallel()
	cases := []struct {
		source, at string
		want       []string
	}{
		{`[x + 1 for x in xs if x > 0]`, "x > 0", []string{"x"}},
		{`[x + 1 for x in xs if x > 0]`, "xs", []string{}},
		{`{k: v for k, v in d}`, "k: v", []string{"k", "v"}},
		{`reduce(p in ps if p > 0, t = 0, t + p)`, "t + p", []string{"p", "t"}},
		{`reduce(p in ps if p > 0, t = 0, t + p)`, "p > 0", []string{"p"}},
		{`reduce(p in ps if p > 0, t = start, t + p)`, "start", []string{}},
		{`let(a = 1, b = a * 2, c = b, a + c)`, "a * 2", []string{"a"}},
		{`let(a = 1, b = a * 2, c = b, a + c)`, "a + c", []string{"a", "b", "c"}},
		{`let(a = 1, [a + y for y in ys])`, "a + y", []string{"a", "y"}},
		// A clause's name is bound from its own clause on: before it, in the
		// clause outside, it is not yet.
		{`[x + y for x in xs for y in ys]`, "x in", []string{}},
		{`[x + y for x in xs for y in ys]`, "xs", []string{}},
		{`[x + y for x in xs for y in ys]`, "ys", []string{"x"}},
		{`[x + y for x in xs for y in ys]`, "x + y", []string{"x", "y"}},
		{`[x + y + z for x in xs for y in ys for z in zs if z > y]`, "ys", []string{"x"}},
		{`[x + y + z for x in xs for y in ys for z in zs if z > y]`, "z > y", []string{"x", "y", "z"}},
	}
	for _, c := range cases {
		t.Run(c.source+" at "+c.at, func(t *testing.T) {
			t.Parallel()
			offset := strings.Index(c.source, c.at)
			if got := ScopeAt(mustParse(t, c.source), offset); strings.Join(got, ",") != strings.Join(c.want, ",") {
				t.Errorf("%s at %q: %v, want %v", c.source, c.at, got, c.want)
			}
		})
	}
}

func TestLocalReferencesTellLocalsFromArguments(t *testing.T) {
	t.Parallel()
	expr := mustParse(t, `let(rate = fee, [rate * x for x in xs])`)
	references := LocalReferences(expr)
	locals := map[string]bool{}
	var visit func(Expr)
	visit = func(e Expr) {
		if variable, ok := e.(*VariableExpr); ok {
			locals[variable.Name] = references[variable.ID]
		}
		for _, child := range Children(e) {
			visit(child)
		}
	}
	visit(expr)
	want := map[string]bool{"fee": false, "rate": true, "x": true, "xs": false}
	for name, local := range want {
		if locals[name] != local {
			t.Errorf("%s: local = %v, want %v", name, locals[name], local)
		}
	}
}
