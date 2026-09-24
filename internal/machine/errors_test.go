package machine

import (
	"errors"
	"os"
	"regexp"
	"testing"
)

// The README's table of errors is errorClasses: every class, and whether
// fallback takes it.
func TestTheReadmeTableIsTheErrorClasses(t *testing.T) {
	t.Parallel()
	readme, err := os.ReadFile("../../README.md")
	if err != nil {
		t.Fatal(err)
	}
	sentinels := map[string]error{
		"ErrCompile": ErrCompile, "ErrContract": ErrContract, "ErrDeadline": ErrDeadline, "ErrExtension": ErrExtension,
		"ErrNoFxRate": ErrNoFxRate, "ErrFuel": ErrFuel, "ErrCurrency": ErrCurrency, "ErrArithmetic": ErrArithmetic,
		"ErrUnavailable": ErrUnavailable,
	}
	rows := regexp.MustCompile("(?m)^\\| `(Err[A-Za-z]+)` \\| [^|]+ \\| ([^|]+) \\|$").FindAllStringSubmatch(string(readme), -1)
	if len(rows) != len(errorClasses) {
		t.Fatalf("the README lists %d errors, errorClasses %d", len(rows), len(errorClasses))
	}
	for _, row := range rows {
		class, ok := classOf(sentinels[row[1]])
		if !ok || !errors.Is(class.err, sentinels[row[1]]) {
			t.Fatalf("the README's %s is not a class of its own", row[1])
		}
		if caught := row[2] == "接"; caught != class.fallback {
			t.Errorf("the README says fallback %q %s; errorClasses says %v", row[2], row[1], class.fallback)
		}
	}
}

// An error is its most specific class: an argument in the wrong currency is
// a currency error though it is a contract error too.
func TestAnErrorIsItsMostSpecificClass(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		err  error
		want string
	}{
		{argumentError(NewParameter("fee", MoneyType, ""), MoneyValue(1, "USD")), "currency"},
		{argumentError(NewParameter("n", IntType, ""), String("x")), "contract"},
		{unavailableError("f"), "unavailable"},
		{errDivisionByZero, "arithmetic"},
		{os.ErrNotExist, ""},
	} {
		if got := ClassName(test.err); got != test.want {
			t.Errorf("ClassName(%v) = %q, want %q", test.err, got, test.want)
		}
	}
}
