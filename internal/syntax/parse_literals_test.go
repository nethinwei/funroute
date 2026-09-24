package syntax

import (
	"strings"
	"testing"
)

// A minus before a number makes a negative literal only when the number
// stands alone. Before the figure of an exchange rate or an indexed number
// it is the unary minus of the grammar, sub(0, …), over the whole postfix.
func TestAMinusIsALiteralOnlyBeforeANumberAlone(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		source, printed string
		negation        bool
	}{
		{"-150 JPY / USD", "-(150 JPY / USD)", true},
		{"-2[0]", "-(2)[0]", true},
		{"-9223372036854775808", "-9223372036854775808", false},
		{"-1.5", "-1.5", false},
	} {
		expr, err := Parse(test.source)
		if err != nil {
			t.Errorf("Parse(%q) error = %v", test.source, err)
			continue
		}
		call, isCall := expr.(*CallExpr)
		if negation := isCall && call.Name == "sub"; negation != test.negation {
			t.Errorf("Parse(%q) = %#v, want a negation %v", test.source, expr, test.negation)
		}
		if got := Inline(expr); got != test.printed {
			t.Errorf("Inline(Parse(%q)) = %q, want %q", test.source, got, test.printed)
		}
	}
}

// A decimal literal is the float64 it is written as, or refused: rounded on
// the way in, a rule would say one number and compute another, and a literal
// read as an exact ratio would carry the rounding with it. Exponents far out
// of range are refused without being worked out.
func TestADecimalLiteralIsExactlyAFloat(t *testing.T) {
	t.Parallel()
	for _, source := range []string{"0.1", "1.50", "1e300", "1.5e-3", "2.5", "0.0", "1e-320", "15e-1", "0.000125"} {
		if _, err := Parse(source); err != nil {
			t.Errorf("Parse(%q) error = %v, want the float", source, err)
		}
	}
	for source, nearest := range map[string]string{
		"0.30000000000000001":    "0.3",
		"3.14159265358979323846": "3.141592653589793",
		"9007199254740993.0":     "9.007199254740992e+15",
		"1e-999999999":           "0",
		"-0.30000000000000001":   "-0.3",
	} {
		_, err := Parse(source)
		if err == nil || !strings.Contains(err.Error(), "the nearest one is "+nearest) {
			t.Errorf("Parse(%q) error = %v, want one naming %s", source, err, nearest)
		}
	}
	if _, err := Parse("1e99999999999999999999"); err == nil {
		t.Error("Parse(1e99999999999999999999) = nil error, want a refusal")
	}
	for _, document := range []string{`{"version":1,"expr":{"node":"float","float":"0.30000000000000001"}}`} {
		if _, err := ImportExprJSON([]byte(document)); err == nil {
			t.Errorf("ImportExprJSON(%s) = nil error, want the float refused as it is in source", document)
		}
	}
}

// A money or an exchange rate literal is on one line with no comment in it:
// its parts are separated by spaces and tabs only, so a newline or a comment
// ends it, and what follows is read as the names and operators it is.
func TestALiteralEndsAtANewlineOrAComment(t *testing.T) {
	t.Parallel()
	literal := func(expr Expr) bool {
		switch expr.(type) {
		case *MoneyExpr, *FxRateExpr:
			return true
		}
		return false
	}
	for source, want := range map[string]bool{
		"USD 1.70": true, "USD\t-1.70": true, "150 JPY/USD": true, "150\tJPY / USD": true,
		"USD\n1.70": false, "USD // yen\n 1": false, "150 JPY\n/ USD": false, "150 JPY // c\n/ USD": false, "150 JPY /\nUSD": false,
	} {
		expr, err := Parse(source)
		if got := err == nil && literal(expr); got != want {
			t.Errorf("Parse(%q) = %T, %v: a literal %v, want %v", source, expr, err, got, want)
		}
	}
	expr, err := Parse("x + USD\n-1")
	if err != nil || Inline(expr) != "x + USD - 1" {
		t.Fatalf("Parse(x + USD⏎-1) = %v, %v, want x + USD - 1", expr, err)
	}
}
