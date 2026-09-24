package lang_test

import (
	"errors"
	"testing"

	"funroute/lang"
)

// A compile error carries where it happened; the text stays with the host, so
// turning the offset into a line and a column is a call the host makes.
func TestHostLocatesACompileError(t *testing.T) {
	t.Parallel()
	source := "amount\n  + \"x\""
	_, err := lang.CompileExpr(source, lang.CoreRegistry(), lang.CompileOptions{
		Args: []lang.ArgSpec{{Name: "amount", Type: lang.IntType}},
	})
	if err == nil {
		t.Fatalf("CompileExpr(%q) error = nil, want a type error: a string cannot be added to an int", source)
	}
	positioned, ok := errors.AsType[*lang.PositionError](err)
	if !ok {
		t.Fatalf("CompileExpr(%q) error = %v, want a *PositionError", source, err)
	}
	if start, end := positioned.Span(); positioned.Offset() != 9 || source[start:end] != "amount\n  + \"x\"" {
		t.Fatalf("the error points at %d and covers %d..%d, want 9 and the whole sum", positioned.Offset(), start, end)
	}
	line, column, ok := lang.LineColumn(err, source)
	if !ok || line != 2 || column != 3 {
		t.Fatalf("LineColumn = %d:%d (ok=%v), want 2:3", line, column, ok)
	}
	if !errors.Is(err, lang.ErrCompile) {
		t.Fatalf("error = %v, want ErrCompile: a positioned error is still a compile error", err)
	}
}

// Arithmetic with no answer is ErrArithmetic wherever it happens, and
// fallback does not take it: it is the rule's or the data's error, not an
// extension's.
func TestHostTellsArithmeticApart(t *testing.T) {
	t.Parallel()
	registry := lang.CoreRegistry()
	artifact, err := lang.CompileExpr("fallback(n / d, 0)", registry, lang.CompileOptions{Args: []lang.ArgSpec{
		{Name: "n", Type: lang.IntType}, {Name: "d", Type: lang.IntType},
	}})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := lang.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	_, err = runtime.Run(t.Context(), map[string]any{"n": 1, "d": 0}, lang.RunOptions{})
	if !errors.Is(err, lang.ErrArithmetic) || errors.Is(err, lang.ErrExtension) {
		t.Fatalf("fallback(1 / 0, 0) error = %v, want ErrArithmetic, not caught", err)
	}
	table, err := lang.NewCurrencies(lang.MoneySpec{Rounding: lang.RoundHalfUp, Currencies: []lang.CurrencySpec{{Code: "USD", Digits: 2}, {Code: "JPY"}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := table.NewRates().Add("USD", "JPY", "-1"); !errors.Is(err, lang.ErrArithmetic) {
		t.Fatalf("a negative exchange rate from Go: error = %v, want ErrArithmetic", err)
	}
}

// A conversion with no rate to make it is ErrNoRate, in Go and in a rule,
// and fallback takes it there: the rate is data not at hand, not a mistake.
func TestHostTellsAMissingRateApart(t *testing.T) {
	t.Parallel()
	registry := lang.CoreRegistry()
	if err := registry.DeclareMoney(lang.MoneySpec{Rounding: lang.RoundHalfUp, Currencies: []lang.CurrencySpec{{Code: "USD", Digits: 2}, {Code: "JPY"}}}); err != nil {
		t.Fatal(err)
	}
	table, _ := registry.Currencies()
	dollar, _ := table.Parse("USD 1.00")
	if _, err := table.NewRates().Convert(dollar, "JPY", lang.RoundHalfUp); !errors.Is(err, lang.ErrNoRate) {
		t.Fatalf("Convert(USD 1.00, JPY) with no rates error = %v, want ErrNoRate", err)
	}
	for source, caught := range map[string]bool{"amount -> JPY": false, "fallback(amount -> JPY, JPY 0)": true} {
		artifact, err := lang.CompileExpr(source, registry, lang.CompileOptions{Args: []lang.ArgSpec{{Name: "amount", Type: lang.MoneyOf("USD")}}})
		if err != nil {
			t.Fatal(err)
		}
		runtime, _ := lang.Instantiate(artifact, registry)
		_, err = runtime.Run(t.Context(), map[string]any{"amount": "USD 1.00"}, lang.RunOptions{})
		if (err == nil) != caught || (!caught && !errors.Is(err, lang.ErrNoRate)) {
			t.Errorf("Run(%q) with no rates error = %v, want caught: %v, else => ErrNoRate", source, err, caught)
		}
	}
}
