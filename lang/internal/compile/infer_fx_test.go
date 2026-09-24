package compile

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"funroute/lang/internal/machine"
)

// fxContract is the arguments the using tests read: dollars, yen, amounts
// in a currency only the run knows, a rate and a count.
const fxContract = "a: money<USD>; b: money<USD>; y: money<JPY>; p: money<?>; q: money<?>; fx: fxrate<USD,JPY>; n: int"

// A using is its body's value, whatever the body is.
func TestAUsingTypesAsItsBody(t *testing.T) {
	t.Parallel()
	for source, want := range map[string]string{
		"using(160 JPY / USD, a -> JPY)":            "money<JPY>",
		"using(160 JPY / USD, n > 1)":               "bool",
		"using(fx, [a -> JPY, y])":                  "array<money<JPY>>",
		"using(y / a, fx, 150 JPY / USD, n)":        "int",
		"using(160 JPY / USD, using(fx, a -> JPY))": "money<JPY>",
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

// money / money is a rate where it may be one; a using asks for an exchange
// rate, so there it divides into one — and in known different currencies it
// can only be one.
func TestAmountsDivideIntoTheRateAUsingQuotes(t *testing.T) {
	t.Parallel()
	for source, want := range map[string]string{
		"using(p / q, a -> JPY)": "div(money<b>,money<a>)->fxrate<a,b>",
		"using(y / a, a -> JPY)": "div(money<b>,money<a>)->fxrate<a,b>",
		"using(y / b, a -> JPY)": "div(money<b>,money<a>)->fxrate<a,b>",
	} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			artifact, err := compileMoney(t, source, fxContract, "")
			if err != nil {
				t.Fatalf("CompileExpr(%q) error = %v", source, err)
			}
			if !callsSignature(artifact, want) {
				t.Fatalf("CompileExpr(%q) calls %+v, want %s", source, machine.PartsOf(artifact).Calls, want)
			}
		})
	}
	artifact, err := compileMoney(t, "p / q", fxContract, "")
	if err != nil || artifact.Result().Kind() != machine.RateKind {
		t.Fatalf("p / q outside a using = %v, %v, want a rate", artifact, err)
	}
}

func callsSignature(artifact *machine.Artifact, signature string) bool {
	return slices.ContainsFunc(machine.PartsOf(artifact).Calls, func(call machine.CallReference) bool { return call.Signature == signature })
}

// A using reads the run, so a closed one is not folded: its quotes are made,
// its scope opened and its conversion made every run.
func TestAUsingIsNotFolded(t *testing.T) {
	t.Parallel()
	source := "using(150 JPY / USD, USD 1 -> JPY)"
	artifact, err := compileMoney(t, source, "", "")
	if err != nil {
		t.Fatalf("CompileExpr(%q) error = %v", source, err)
	}
	var ops []machine.OpCode
	for _, instruction := range machine.PartsOf(artifact).Instructions {
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
	args := append(moneyContract(t, "a: money<USD>"), ArgSpec{Name: "channel", Type: channel}, ArgSpec{Name: "candidate", Type: machine.StringType})
	options := CompileOptions{Args: args, Result: &channel}
	for source, want := range map[string]string{
		"using(160 JPY / USD, if(a -> JPY > JPY 100, @adyen, @stripe))":                  "",
		"using(160 JPY / USD, switch(channel, case @adyen => @stripe, else => channel))": "",
		"using(160 JPY / USD, candidate)":                                                "returns string",
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
