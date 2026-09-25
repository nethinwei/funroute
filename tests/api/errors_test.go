package api

import (
	"errors"
	"strings"
	"testing"

	"github.com/nethinwei/funroute"
)

// A compile error carries where it happened; the text stays with the host, so
// turning the offset into a line and a column is a call the host makes.
func TestHostLocatesACompileError(t *testing.T) {
	t.Parallel()
	source := "amount\n  + \"x\""
	_, err := funroute.CompileExpr(source, funroute.CoreRegistry(), funroute.CompileOptions{
		Args: []funroute.ArgSpec{{Name: "amount", Type: funroute.IntType}},
	})
	if err == nil {
		t.Fatalf("CompileExpr(%q) error = nil, want a type error: a string cannot be added to an int", source)
	}
	positioned, ok := errors.AsType[*funroute.PositionError](err)
	if !ok {
		t.Fatalf("CompileExpr(%q) error = %v, want a *PositionError", source, err)
	}
	if start, end := positioned.Span(); positioned.Offset() != 9 || source[start:end] != "amount\n  + \"x\"" {
		t.Fatalf("the error points at %d and covers %d..%d, want 9 and the whole sum", positioned.Offset(), start, end)
	}
	line, column, ok := funroute.LineColumn(err, source)
	if !ok || line != 2 || column != 3 {
		t.Fatalf("LineColumn = %d:%d (ok=%v), want 2:3", line, column, ok)
	}
	if !errors.Is(err, funroute.ErrCompile) {
		t.Fatalf("error = %v, want ErrCompile: a positioned error is still a compile error", err)
	}
}

// Arithmetic with no answer is ErrArithmetic wherever it happens, and
// fallback does not take it: it is the rule's or the data's error, not an
// extension's.
func TestHostTellsArithmeticApart(t *testing.T) {
	t.Parallel()
	registry := funroute.CoreRegistry()
	artifact, err := funroute.CompileExpr("fallback(n / d, 0)", registry, funroute.CompileOptions{Args: []funroute.ArgSpec{
		{Name: "n", Type: funroute.IntType}, {Name: "d", Type: funroute.IntType},
	}})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := funroute.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	_, err = runtime.Run(t.Context(), map[string]any{"n": 1, "d": 0})
	if !errors.Is(err, funroute.ErrArithmetic) || errors.Is(err, funroute.ErrExtension) {
		t.Fatalf("fallback(1 / 0, 0) error = %v, want ErrArithmetic, not caught", err)
	}
	table, err := funroute.NewCurrencies(funroute.MoneySpec{Currencies: []funroute.CurrencySpec{{Code: "USD", Digits: 2}, {Code: "JPY"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := table.FxRate("USD", "JPY", "-1"); !errors.Is(err, funroute.ErrArithmetic) {
		t.Fatalf("a negative exchange rate from Go: error = %v, want ErrArithmetic", err)
	}
}

// Data a rule has no answer for — an index past the end — is ErrDomain, and
// fallback does not take it: it is the rule's or the data's error, as
// arithmetic with no answer is.
func TestHostTellsDataWithNoAnswerApart(t *testing.T) {
	t.Parallel()
	registry := funroute.CoreRegistry()
	artifact, err := funroute.CompileExpr("fallback(fees[i], 0)", registry, funroute.CompileOptions{Args: []funroute.ArgSpec{
		{Name: "fees", Type: funroute.ArrayOf(funroute.IntType)}, {Name: "i", Type: funroute.IntType},
	}})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := funroute.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	_, err = runtime.Run(t.Context(), map[string]any{"fees": []int64{30}, "i": 5})
	if !errors.Is(err, funroute.ErrDomain) || errors.Is(err, funroute.ErrExtension) {
		t.Fatalf("fallback(fees[5], 0) error = %v, want ErrDomain, not caught", err)
	}
}

// A conversion whose using has no quote of the pair is ErrNoFxRate, and
// fallback takes it: the rate is data not at hand, not a mistake.
func TestHostTellsAMissingRateApart(t *testing.T) {
	t.Parallel()
	registry := funroute.CoreRegistry()
	if err := registry.DeclareMoney(funroute.MoneySpec{Currencies: []funroute.CurrencySpec{{Code: "USD", Digits: 2}, {Code: "JPY"}}}); err != nil {
		t.Fatal(err)
	}
	for source, caught := range map[string]bool{"using(market, round(amount -> JPY, @half_even))": false, "using(market, fallback(round(amount -> JPY, @half_even), JPY 0))": true} {
		options := funroute.CompileOptions{Args: []funroute.ArgSpec{{Name: "amount", Type: funroute.MoneyType}, {Name: "market", Type: funroute.ArrayOf(funroute.FxRateType)}}}
		artifact, err := funroute.CompileExpr(source, registry, options)
		if err != nil {
			t.Fatal(err)
		}
		runtime, _ := funroute.Instantiate(artifact, registry)
		_, err = runtime.Run(t.Context(), map[string]any{"amount": "USD 1.00", "market": []any{}})
		if (err == nil) != caught || (!caught && !errors.Is(err, funroute.ErrNoFxRate)) {
			t.Errorf("Run(%q) with no rates error = %v, want caught: %v, else => ErrNoFxRate", source, err, caught)
		}
	}
}

// quotaError is a host's own error type, with what the host needs from it.
type quotaError struct{ retryAfter int }

func (e *quotaError) Error() string { return "quota exhausted" }

// An extension's own error is classed and kept: errors.Is answers the class,
// and the host still finds its sentinel and its type behind it.
func TestHostFindsItsOwnErrorBehindTheClass(t *testing.T) {
	t.Parallel()
	errDown := errors.New("pricing service down")
	for name, test := range map[string]struct {
		returned error
		find     func(error) bool
		want     string
	}{
		"a sentinel": {errDown, func(err error) bool { return errors.Is(err, errDown) }, "extension failed: pricing service down"},
		"a type": {&quotaError{retryAfter: 30}, func(err error) bool {
			var quota *quotaError
			return errors.As(err, &quota) && quota.retryAfter == 30
		}, "extension failed: quota exhausted"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			registry := funroute.CoreRegistry()
			if err := registry.Register(funroute.FunctionSpec{
				Name: "pricing.quote_v1",
				Go:   func(int64) (int64, error) { return 0, test.returned },
			}); err != nil {
				t.Fatal(err)
			}
			artifact, err := funroute.CompileExpr(`pricing.quote_v1(1)`, registry, funroute.CompileOptions{})
			if err != nil {
				t.Fatal(err)
			}
			runtime, err := funroute.Instantiate(artifact, registry)
			if err != nil {
				t.Fatal(err)
			}
			_, err = runtime.Run(t.Context(), nil)
			if !errors.Is(err, funroute.ErrExtension) || !test.find(err) || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("pricing.quote_v1 returning %v: error = %v, want ErrExtension with the host's error behind it, reading %q", test.returned, err, test.want)
			}
		})
	}
}
