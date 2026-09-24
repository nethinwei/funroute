package syntax

import (
	"strings"
	"testing"

	"github.com/nethinwei/funroute/internal/machine"
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

// A decimal literal is the decimal it is written as. Read as a float it is
// the float64 it is written as, or refused, naming the one it would have
// been: rounded on the way in, a rule would say one number and compute
// another. Exponents far out of range are refused without being worked out.
func TestADecimalLiteralIsExactlyAFloat(t *testing.T) {
	t.Parallel()
	for _, source := range []string{"0.1", "1.50", "1e300", "1.5e-3", "2.5", "0.0", "1e-320", "15e-1", "0.000125"} {
		if _, err := floatOf(t, source); err != nil {
			t.Errorf("Parse(%q).Float() error = %v, want the float", source, err)
		}
	}
	for source, nearest := range map[string]string{
		"0.30000000000000001":    "0.3",
		"3.14159265358979323846": "3.141592653589793",
		"9007199254740993.0":     "9.007199254740992e+15",
		"1e-999999999":           "0",
		"-0.30000000000000001":   "-0.3",
	} {
		_, err := floatOf(t, source)
		if err == nil || !strings.Contains(err.Error(), "the nearest one is "+nearest) {
			t.Errorf("Parse(%q).Float() error = %v, want one naming %s", source, err, nearest)
		}
	}
	for _, source := range []string{"1e99999999999999999999", "1e400"} {
		if _, err := Parse(source); err == nil {
			t.Errorf("Parse(%s) = nil error, want a refusal", source)
		}
	}
	expr, err := ImportExprJSON([]byte(`{"version":1,"expr":{"node":"float","float":"0.30000000000000001"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if literal, ok := expr.(*LiteralExpr); !ok {
		t.Errorf("ImportExprJSON(0.30000000000000001) = %T, want a literal", expr)
	} else if _, err := literal.Float(); err == nil {
		t.Error("an imported 0.30000000000000001 read as a float = nil error, want it refused as it is in source")
	}
}

// floatOf parses source, a decimal literal, and reads it as a float.
func floatOf(t *testing.T, source string) (machine.Value, error) {
	t.Helper()
	expr, err := Parse(source)
	if err != nil {
		t.Fatalf("Parse(%q) error = %v", source, err)
	}
	literal, ok := expr.(*LiteralExpr)
	if !ok {
		t.Fatalf("Parse(%q) = %T, want a literal", source, expr)
	}
	return literal.Float()
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
