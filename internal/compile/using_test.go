package compile

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/nethinwei/funroute/internal/machine"
)

// fxContract is the arguments the using tests read: dollars, yen, amounts
// in a currency only the run knows, an exchange rate and a count.
const fxContract = "a: money; b: money; y: money; p: money; q: money; fx: fxrate; n: int"

// A using is its body's value, whatever the body is.
func TestAUsingTypesAsItsBody(t *testing.T) {
	t.Parallel()
	for source, want := range map[string]string{
		"using(160 JPY / USD, round(a -> JPY, @half_even))":            "money",
		"using(160 JPY / USD, n > 1)":                                  "bool",
		"using(fx, [round(a -> JPY, @half_even), y])":                  "array<money>",
		"using(implied(y, a), fx, 150 JPY / USD, n)":                   "int",
		"using(160 JPY / USD, using(fx, round(a -> JPY, @half_even)))": "money",
	} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			artifact, err := compileMoney(t, source, fxContract, "")
			if err != nil {
				t.Fatalf("CompileExpr(%q) error = %v", source, err)
			}
			if got := artifact.Result().String(); got != want {
				t.Fatalf("CompileExpr(%q) result = %s, want %s", source, got, want)
			}
		})
	}
}

// Every quote is an exchange rate: a number, an amount, a string or a rate
// proven to be between one currency is not one.
func TestAUsingQuotesOnlyExchangeRates(t *testing.T) {
	t.Parallel()
	for source, want := range map[string]string{
		"using(1 / 2, a)":         "using quotes exchange rates",
		"using(a, a)":             "using quotes exchange rates",
		`using("150", a)`:         "using quotes exchange rates",
		"using(2.9%, a)":          "using quotes exchange rates",
		"using(n, a)":             "using quotes exchange rates",
		"using(a / b, a)":         "using quotes exchange rates",
		"using(fx, a / b, a)":     "using quotes exchange rates",
		"using(150 JPY / JPY, a)": "from a currency to itself it is 1",
		"using(150 GBP / USD, a)": `"GBP" is not declared`,
		"using(0 JPY / USD, a)":   "not positive",
	} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			_, err := compileMoney(t, source, fxContract, "")
			if !errors.Is(err, machine.ErrCompile) || !strings.Contains(err.Error(), want) {
				t.Fatalf("CompileExpr(%q) error = %v, want ErrCompile containing %q", source, err, want)
			}
		})
	}
}

// Two amounts imply an exchange rate by implied(over, under), which a using
// quotes; a / b is always their ratio, and a using does not take one.
func TestAmountsImplyTheRateAUsingQuotes(t *testing.T) {
	t.Parallel()
	for _, source := range []string{"using(implied(p, q), round(a -> JPY, @half_even))", "using(implied(y, a), round(a -> JPY, @half_even))"} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			artifact, err := compileMoney(t, source, fxContract, "")
			if err != nil {
				t.Fatalf("CompileExpr(%q) error = %v", source, err)
			}
			if want := "implied(money,money)->fxrate"; !callsSignature(artifact, want) {
				t.Fatalf("CompileExpr(%q) calls %+v, want %s", source, machine.PartsOf(artifact).Calls, want)
			}
		})
	}
	if _, err := compileMoney(t, "using(y / a, a -> JPY)", fxContract, ""); err == nil || !strings.Contains(err.Error(), "using quotes exchange rates") {
		t.Fatalf("using(y / a, …) error = %v, want a ratio refused as a quote", err)
	}
	artifact, err := compileMoney(t, "p / q", fxContract, "")
	if err != nil || artifact.Result().Kind() != machine.RatioKind {
		t.Fatalf("p / q = %v, %v, want a rate", artifact, err)
	}
}

func callsSignature(artifact *machine.Artifact, signature string) bool {
	return slices.ContainsFunc(machine.PartsOf(artifact).Calls, func(call machine.CallReference) bool { return call.Signature == signature })
}

// A using reads the run, so a closed one is not folded: its quotes are made,
// its scope opened and its conversion made every run.
func TestAUsingIsNotFolded(t *testing.T) {
	t.Parallel()
	source := "using(150 JPY / USD, round(USD 1 -> JPY, @half_even))"
	artifact, err := compileMoney(t, source, "", "")
	if err != nil {
		t.Fatalf("CompileExpr(%q) error = %v", source, err)
	}
	instructions := machine.PartsOf(artifact).Instructions
	ops := make([]machine.OpCode, 0, len(instructions))
	for _, instruction := range instructions {
		ops = append(ops, instruction.Op)
	}
	for _, op := range []machine.OpCode{machine.OpFxPush, machine.OpCall, machine.OpFxPop} {
		if !slices.Contains(ops, op) {
			t.Errorf("CompileExpr(%q) = %v, want %s in it", source, ops, op)
		}
	}
	if !strings.HasPrefix(machine.PartsOf(artifact).Calls[0].Signature, "convert(") {
		t.Errorf("CompileExpr(%q) calls %+v, want convert", source, machine.PartsOf(artifact).Calls)
	}
}

// An enum result is proven through a using's body, as through a let's.
func TestAnEnumResultPassesThroughAUsing(t *testing.T) {
	t.Parallel()
	channel, err := machine.ParseType(`enum<channel>{adyen,stripe}`)
	if err != nil {
		t.Fatal(err)
	}
	args := append(moneyContract(t, "a: money"), ArgSpec{Name: "channel", Type: channel}, ArgSpec{Name: "candidate", Type: machine.StringType})
	options := CompileOptions{Args: args, Result: &channel}
	for source, want := range map[string]string{
		"using(160 JPY / USD, if(round(a -> JPY, @half_even) > JPY 100, @adyen, @stripe))": "",
		"using(160 JPY / USD, switch(channel, case @adyen => @stripe, else => channel))":   "",
		"using(160 JPY / USD, candidate)":                                                  "returns string",
	} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			_, err := CompileExpr(source, moneyRegistry(t), options)
			if want == "" && err != nil {
				t.Fatalf("CompileExpr(%q) error = %v, want none", source, err)
			}
			if want != "" && (!errors.Is(err, machine.ErrCompile) || !strings.Contains(err.Error(), want)) {
				t.Fatalf("CompileExpr(%q) error = %v, want ErrCompile containing %q", source, err, want)
			}
		})
	}
}

// A using pushes its quotes, opens the scope over them and closes it after
// the body.
func TestCompileUsingOpensAndClosesTheScope(t *testing.T) {
	t.Parallel()
	source := "using(170 JPY / USD, fx, implied(y, a), round(a -> JPY, @half_even))"
	artifact, err := compileMoney(t, source, "a: money; y: money; fx: fxrate", "")
	if err != nil {
		t.Fatal(err)
	}
	instructions := machine.PartsOf(artifact).Instructions
	ops := make([]string, 0, len(instructions))
	for _, in := range instructions {
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
