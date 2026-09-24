package hosttest

import (
	"errors"
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
	_, err = runtime.Run(t.Context(), map[string]any{"n": 1, "d": 0}, funroute.RunOptions{})
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
		_, err = runtime.Run(t.Context(), map[string]any{"amount": "USD 1.00", "market": []any{}}, funroute.RunOptions{})
		if (err == nil) != caught || (!caught && !errors.Is(err, funroute.ErrNoFxRate)) {
			t.Errorf("Run(%q) with no rates error = %v, want caught: %v, else => ErrNoFxRate", source, err, caught)
		}
	}
}
