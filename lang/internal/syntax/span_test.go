package syntax

import (
	"errors"
	"strings"
	"testing"
)

// Every node's span lies inside its parent's, and the root covers the
// program from its first token to its last.
func TestSpansNest(t *testing.T) {
	for _, source := range lexemeCorpus {
		expr, err := Parse(source)
		if err != nil {
			continue
		}
		root := expr.Extent()
		if got, want := source[root.Start:root.End], strings.TrimSpace(strings.Split(source, "//")[0]); got != want && !strings.Contains(source, "//") {
			t.Errorf("%q: the root covers %q", source, got)
		}
		checkNested(t, source, expr)
	}
}

func checkNested(t *testing.T, source string, parent Expr) {
	outer := parent.Extent()
	if outer.Start < 0 || outer.End > len(source) || outer.End <= outer.Start {
		t.Fatalf("%q: %T has span %+v", source, parent, outer)
	}
	for _, child := range Children(parent) {
		inner := child.Extent()
		if inner.Start < outer.Start || inner.End > outer.End {
			t.Errorf("%q: %q is not inside %q", source, source[inner.Start:inner.End], source[outer.Start:outer.End])
		}
		checkNested(t, source, child)
	}
}

// A node spans exactly the source it was read from.
func TestSpansCoverWhatWasWritten(t *testing.T) {
	cases := map[string][]string{
		`(a + b) * c`:                       {"(a + b)", "(a + b) * c", "c"},
		`order.items[0].price`:              {"order", "order.items", "order.items[0]", "order.items[0].price", "0"},
		`f(x).fee != -1.5`:                  {"f(x)", "f(x).fee", "-1.5", "f(x).fee != -1.5"},
		`-x + !ok`:                          {"-x", "x", "!ok", "-x + !ok"},
		`[x * 2 for x in xs if x > 0]`:      {"x * 2", "xs", "x > 0", "[x * 2 for x in xs if x > 0]"},
		`let(r = 2, {amount: r, tags: []})`: {"2", "{amount: r, tags: []}", "[]"},
		`switch(c, case "SG" => 1, else 2)`: {"c", `"SG"`, "1", "2"},
		`reduce(p in ps, acc = 0, acc + p)`: {"ps", "0", "acc + p"},
	}
	for source, want := range cases {
		got := map[string]bool{}
		collectSpans(Must(t, source), source, got)
		for _, text := range want {
			if !got[text] {
				t.Errorf("%q: no node spans %q (spans: %v)", source, text, keys(got))
			}
		}
	}
}

func Must(t *testing.T, source string) Expr {
	t.Helper()
	expr, err := Parse(source)
	if err != nil {
		t.Fatalf("%q: %v", source, err)
	}
	return expr
}

func collectSpans(expr Expr, source string, out map[string]bool) {
	extent := expr.Extent()
	out[source[extent.Start:extent.End]] = true
	for _, child := range Children(expr) {
		collectSpans(child, source, out)
	}
}

func keys(set map[string]bool) []string {
	var out []string
	for key := range set {
		out = append(out, key)
	}
	return out
}

// A syntax error covers the token it is about; a lexical one covers what the
// lexer could not read.
func TestSyntaxErrorsCoverTheirToken(t *testing.T) {
	cases := map[string]string{
		`f(1,, 2)`:     ",",
		`a ¥ b`:        "¥",
		`x + "open`:    `"open`,
		`let(x = 1)`:   ")",
		`[1, 2`:        "",
		`switch(x, 1)`: "switch",
	}
	for source, want := range cases {
		_, err := Parse(source)
		var positioned *PosError
		if !errors.As(err, &positioned) {
			t.Fatalf("%q: %v has no position", source, err)
		}
		if got := source[positioned.Start:positioned.End]; got != want {
			t.Errorf("%q: the error covers %q, want %q (%v)", source, got, want, err)
		}
	}
}

func TestScopeAtFollowsTheBindsTags(t *testing.T) {
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
	}
	for _, c := range cases {
		offset := strings.Index(c.source, c.at)
		if got := ScopeAt(Must(t, c.source), offset); strings.Join(got, ",") != strings.Join(c.want, ",") {
			t.Errorf("%s at %q: %v, want %v", c.source, c.at, got, c.want)
		}
	}
}

func TestLocalReferencesTellLocalsFromArguments(t *testing.T) {
	expr := Must(t, `let(rate = fee, [rate * x for x in xs])`)
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
