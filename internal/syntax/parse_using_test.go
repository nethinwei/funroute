package syntax

import (
	"strings"
	"testing"
)

// A using needs a rate and ends with its body. "..." is no expression, so it
// has no place in one: a using converts at what it writes.
func TestUsingNeedsARateAndABody(t *testing.T) {
	t.Parallel()
	for source, want := range map[string]string{
		"using(a / b)":            "ends with its body",
		"using(a / b, ...)":       "expected an expression",
		"using(..., x)":           "expected an expression",
		"using(a / b, ..., x)":    "expected an expression",
		"using(x)":                "ends with its body",
		"using()":                 "expected an expression",
		"using(1e3 JPY / USD, x)": "exponent",
	} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			if _, err := Parse(source); err == nil || !strings.Contains(err.Error(), want) {
				t.Errorf("Parse(%q) error = %v, want one saying %q", source, err, want)
			}
		})
	}
}

// A using's rates are its quotes in the order written; 150 JPY / USD is one
// literal exchange rate, and a figure before anything else is a number.
func TestUsingReadsItsQuotesInOrder(t *testing.T) {
	t.Parallel()
	node, ok := mustParse(t, "using(s / p, 150.25 JPY / USD, t / q, a -> JPY)").(*UsingExpr)
	if !ok || len(node.Quotes) != 3 {
		t.Fatalf("Parse(using(…)) = %#v, want three quotes", node)
	}
	if literal, ok := node.Quotes[1].(*FxRateExpr); !ok || literal.Rate != "150.25" || literal.Quote != "JPY" || literal.Base != "USD" {
		t.Errorf("150.25 JPY / USD reads as %#v, want one exchange rate literal", node.Quotes[1])
	}
	for _, source := range []string{"150 / USD", "150 JPY", "150 JPY / x", "150 x / USD"} {
		if _, isLiteral := mustParseOrNil(source).(*FxRateExpr); isLiteral {
			t.Errorf("Parse(%q) is an exchange rate literal, want it read as numbers and names", source)
		}
	}
}

// mustParseOrNil is the program source parses to, or nil.
func mustParseOrNil(source string) Expr {
	expr, _ := Parse(source)
	return expr
}

// A using's quotes round-trip through ExprJSON, and a using without one
// does not import: the quotes are required.
func TestUsingQuotesRoundTrip(t *testing.T) {
	t.Parallel()
	for _, source := range []string{"using(settlement, a -> JPY)", "using(settlement, 151 JPY / USD, a -> JPY)"} {
		expr := mustParse(t, source)
		if got := Inline(expr); got != source {
			t.Errorf("Inline(Parse(%q)) = %q", source, got)
		}
		exported := mustExport(t, expr)
		back, err := ImportExprJSON([]byte(exported))
		if err != nil || Inline(back) != source {
			t.Errorf("ExprJSON of %q = %s reads back as %v, %v", source, exported, back, err)
		}
	}
	for _, document := range []string{
		`{"version":1,"expr":{"node":"using","body":{"node":"var","name":"x"}}}`,
		`{"version":1,"expr":{"node":"using","quotes":[],"body":{"node":"var","name":"x"}}}`,
	} {
		if _, err := ImportExprJSON([]byte(document)); err == nil {
			t.Errorf("ImportExprJSON(%s) = nil error, want a using to need a quote", document)
		}
	}
}
