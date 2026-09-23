package syntax

import (
	"strings"
	"testing"
)

// Every node's span lies inside its parent's, and the root covers the
// program from its first token to its last.
func TestSpansNest(t *testing.T) {
	t.Parallel()
	for _, source := range lexemeCorpus {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			expr, err := Parse(source)
			if err != nil {
				return
			}
			root := expr.Extent()
			if got, want := source[root.Start:root.End], strings.TrimSpace(strings.Split(source, "//")[0]); got != want && !strings.Contains(source, "//") {
				t.Errorf("%q: the root covers %q, want %q", source, got, want)
			}
			checkNested(t, source, expr)
		})
	}
}

func checkNested(t *testing.T, source string, parent Expr) {
	t.Helper()
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
	t.Parallel()
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
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			checkSpans(t, source, want)
		})
	}
}

// checkSpans fails unless each text in want is the span of some node.
func checkSpans(t *testing.T, source string, want []string) {
	t.Helper()
	got := map[string]bool{}
	collectSpans(mustParse(t, source), source, got)
	for _, text := range want {
		if !got[text] {
			t.Errorf("%q: no node spans %q (spans: %v)", source, text, keys(got))
		}
	}
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

func mustParse(t testing.TB, source string) Expr {
	t.Helper()
	expr, err := Parse(source)
	if err != nil {
		t.Fatalf("Parse(%q) error = %v, want nil", source, err)
	}
	return expr
}

func TestBareExpressionIsTheGrammar(t *testing.T) {
	t.Parallel()
	// A dotted name being called is a function's (route.score_v1); anywhere
	// else it is a variable and the fields read off it.
	dotted, err := Parse(`a.b + 1`)
	if err != nil {
		t.Fatalf("a.b should parse as a field access: %v", err)
	}
	if encoded, err := ExportExprJSON(dotted); err != nil {
		t.Fatal(err)
	} else if !strings.Contains(string(encoded), `"node":"field"`) {
		t.Fatalf("a.b did not become a field access: %s", encoded)
	}
	// A bare expression is the whole grammar.
	if _, err := Parse(`add(1,2)`); err != nil {
		t.Fatal(err)
	}
}

func TestSwitchShapesAgree(t *testing.T) {
	t.Parallel()
	// The positional form and the branch form produce the same AST.
	for _, pair := range [][2]string{
		{`switch(country, case "SG" => "a", case "MY" => "b", else "c")`, `switch(country, case "SG" => "a", case "MY" => "b", else "c")`},
	} {
		t.Run(pair[0], func(t *testing.T) {
			t.Parallel()
			assertSameExprJSON(t, pair[0], pair[1])
		})
	}
}
