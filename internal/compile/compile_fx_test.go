package compile

import (
	"strings"
	"testing"

	"github.com/nethinwei/funroute/internal/machine"
)

// A using pushes its quotes, opens the scope over them and closes it after
// the body.
func TestCompileUsingOpensAndClosesTheScope(t *testing.T) {
	t.Parallel()
	source := "using(170 JPY / USD, fx, implied(y, a), round(a -> JPY, @half_even))"
	artifact, err := compileMoney(t, source, "a: money; y: money; fx: fxrate", "")
	if err != nil {
		t.Fatal(err)
	}
	var ops []string
	for _, in := range machine.PartsOf(artifact).Instructions {
		ops = append(ops, in.Op.String())
		if in.Op == machine.OpFxPush && (in.A != 0 || in.B != 3 || in.C != 0 || in.Keys != nil) {
			t.Errorf("%s: fx_push = %+v, want B 3 and nothing else", source, in)
		}
	}
	want := "const load_arg load_arg load_arg call fx_push load_arg const call const call fx_pop"
	if got := strings.Join(ops, " "); got != want {
		t.Fatalf("%s compiles to %s, want %s", source, got, want)
	}
}

// A conversion outside every using has no rates to convert at, so it does
// not compile: which rates a rule converts at is written in the rule.
func TestAConversionOutsideAUsingDoesNotCompile(t *testing.T) {
	t.Parallel()
	for _, source := range []string{"round(a -> JPY, @half_even)", "round(convert(a, JPY), @half_even)", "fx(USD, JPY)", "round(a -> JPY, @up)", "fallback(round(a -> JPY, @half_even), JPY 7)", "round(using(150 JPY / USD, a) -> JPY, @half_even)"} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			if _, err := compileMoney(t, source, "a: money", ""); err == nil || !strings.Contains(err.Error(), "inside using(") {
				t.Fatalf("CompileExpr(%q) error = %v, want one that says it converts only inside using", source, err)
			}
		})
	}
}

// A using quotes exchange rates: an fxrate, or an array<fxrate> such as the
// host's quotes; anything else does not compile, and a using quotes at least
// one.
func TestAUsingQuotesRatesOrArraysOfThem(t *testing.T) {
	t.Parallel()
	contract := "a: money; fx: fxrate; quotes: array<fxrate>; n: int; ns: array<int>"
	for _, source := range []string{"using(fx, round(a -> JPY, @half_even))", "using(quotes, round(a -> JPY, @half_even))", "using(quotes, fx, 150 JPY / USD, round(a -> JPY, @half_even))", "using([fx, fx], round(a -> JPY, @half_even))"} {
		if _, err := compileMoney(t, source, contract, ""); err != nil {
			t.Errorf("CompileExpr(%q) error = %v", source, err)
		}
	}
	for source, want := range map[string]string{
		"using(n, a -> JPY)":        "using quotes exchange rates",
		"using(ns, a -> JPY)":       "using quotes exchange rates",
		"using(@half_up, a -> JPY)": "using quotes exchange rates",
		"using(a -> JPY)":           "using needs an exchange rate",
	} {
		if _, err := compileMoney(t, source, contract, ""); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("CompileExpr(%q) error = %v, want one saying %s", source, err, want)
		}
	}
}
