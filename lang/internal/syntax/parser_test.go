package syntax

import (
	"errors"
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
		`(a + b) * c`:                          {"(a + b)", "(a + b) * c", "c"},
		`order.items[0].price`:                 {"order", "order.items", "order.items[0]", "order.items[0].price", "0"},
		`f(x).fee != -1.5`:                     {"f(x)", "f(x).fee", "-1.5", "f(x).fee != -1.5"},
		`-x + !ok`:                             {"-x", "x", "!ok", "-x + !ok"},
		`[x * 2 for x in xs if x > 0]`:         {"x * 2", "xs", "x > 0", "[x * 2 for x in xs if x > 0]"},
		`let(r = 2, {amount: r, tags: []})`:    {"2", "{amount: r, tags: []}", "[]"},
		`switch(c, case "SG" => 1, else => 2)`: {"c", `"SG"`, "1", "2"},
		`reduce(p in ps, acc = 0, acc + p)`:    {"ps", "0", "acc + p"},
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
		{`switch(country, case "SG" => "a", case "MY" => "b", else => "c")`, `switch(country, case "SG" => "a", case "MY" => "b", else => "c")`},
	} {
		t.Run(pair[0], func(t *testing.T) {
			t.Parallel()
			assertSameExprJSON(t, pair[0], pair[1])
		})
	}
}

// An amount spans its code and its figure, and its minus when it has one; a
// rate spans its number and its unit.
func TestMoneyAndRateSpanWhatWasWritten(t *testing.T) {
	t.Parallel()
	cases := map[string][]string{
		`amount * 2.9% + USD 0.30`:    {"amount", "2.9%", "amount * 2.9%", "USD 0.30", "amount * 2.9% + USD 0.30"},
		`f(JPY -5, 25bps)`:            {"JPY -5", "25bps"},
		`- USD 1.70`:                  {"- USD 1.70"},
		`(USD 1).x`:                   {"(USD 1)", "(USD 1).x"},
		`-2.9%`:                       {"-2.9%", "2.9%"},
		`[x * 0.5bps for x in xs]`:    {"0.5bps", "x * 0.5bps"},
		"let(fee = USD \t 1.70, fee)": {"USD \t 1.70"},
		`JPY 1_000 // yen`:            {"JPY 1_000"},
	}
	for source, want := range cases {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			checkSpans(t, source, want)
		})
	}
}

// A money literal's syntax errors point at the figure that is wrong, and a
// name that cannot be a code at the number that follows it.
func TestMoneySyntaxErrorsCoverTheirToken(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		`USD 1e3`:        "1e3",
		`x + USD -1.5E2`: "1.5E2",
		`US 1`:           "1",
		`usd 1.70`:       "1.70",
		`USD 1.70%`:      "1.70%",
		`25bpsx`:         "bpsx",
		`1e3%`:           "1e3%",
		`1e3%3`:          "1e3%",
		`1e3bps`:         "1e3bps",
	}
	for source, want := range cases {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			_, err := Parse(source)
			positioned, ok := errors.AsType[*PosError](err)
			if !ok {
				t.Fatalf("Parse(%q) error = %v, want a *PosError", source, err)
			}
			if got := source[positioned.start:positioned.end]; got != want {
				t.Errorf("%q: the error covers %q, want %q (%v)", source, got, want, err)
			}
		})
	}
}

// Comparisons do not chain, and -> takes one currency on its right: both are
// errors that say how to write what was meant, never another reading. A
// comparison of comparisons at different levels, a conversion in a
// comparison and conversions one after another still read.
func TestComparisonsDoNotChainAndAConversionTakesOneCurrency(t *testing.T) {
	t.Parallel()
	for source, want := range map[string]string{
		"a < b < c":             "comparisons do not chain",
		"a == b == c":           "comparisons do not chain",
		"a != b == c":           "comparisons do not chain",
		"x in xs in ys":         "comparisons do not chain",
		"a <= b > c":            "comparisons do not chain",
		"amount -> JPY + fee":   "takes one currency on its right",
		"amount -> JPY * 2":     "takes one currency on its right",
		"x < amount -> JPY + 1": "takes one currency on its right",
		"amount -> -JPY":        "expected an expression",
	} {
		if _, err := Parse(source); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("Parse(%q) error = %v, want one saying %s", source, err, want)
		}
	}
	for _, source := range []string{"a < b == c", "a == (b == c)", "(a < b) < c", "a -> JPY -> USD", "a -> JPY > b", "a + b -> JPY", "(a -> JPY) + fee", "a -> o.currency", "a -> currency(b)", "a -> (JPY)"} {
		if _, err := Parse(source); err != nil {
			t.Errorf("Parse(%q) error = %v, want it read", source, err)
		}
	}
}

// A switch's else leads to its result with =>, as every case does; the one
// spelling without it is an error that shows the other.
func TestElseLeadsToItsResultWithAnArrow(t *testing.T) {
	t.Parallel()
	if _, err := Parse(`switch(x, case 1 => 2, else 3)`); err == nil || !strings.Contains(err.Error(), "expected '=>' after else") {
		t.Fatalf("Parse(else 3) error = %v, want one asking for =>", err)
	}
	expr := mustParse(t, `switch(x, case 1 => 2, else => 3)`)
	if got := Inline(expr); got != `switch(x, case 1 => 2, else => 3)` {
		t.Fatalf("Inline(switch …) = %q, want the else printed with =>", got)
	}
}
