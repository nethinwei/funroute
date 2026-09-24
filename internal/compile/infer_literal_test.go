package compile

import (
	"fmt"
	"strings"
	"testing"

	"github.com/nethinwei/funroute/internal/machine"
	"github.com/nethinwei/funroute/internal/money"
)

// A decimal literal is a float or a ratio and 0 an int or money, whichever
// the context asks; nothing asking leaves them what they were written as.
func TestLiteralsSettleByContext(t *testing.T) {
	t.Parallel()
	const contract = "usd: money; r: ratio; f: bool"
	for _, test := range []struct{ source, result, want string }{
		{"0.5", "", "float"},
		{"0", "", "int"},
		{"-0.5", "", "float"},
		{"[0.5, 1.5]", "", "array<float>"},
		{"[0, 1]", "", "array<int>"},
		{"[0.5, 2.5%]", "", "array<ratio>"},
		{"[0, usd]", "", "array<money>"},
		{"if(f, 0.5, 2.5%)", "", "ratio"},
		{"if(f, 0, usd)", "", "money"},
		{"round(usd * 0.5, @half_even)", "", "money"},
		{"round(usd * 2.0, @half_even)", "", "money"},
		{"r < 0.5", "", "bool"},
		{"0.5 + 2.5%", "", "ratio"},
		{"let(z = 0, z + usd)", "", "money"},
		{"let(z = 0.5, round(usd * z, @half_even))", "", "money"},
		{"0.5", "ratio", "ratio"},
		{"0", "money", "money"},
		{"0", "money", "money"},
		{"[0]", "array<money>", "array<money>"},
		{`{"a": 0}`, "dict<money>", "dict<money>"},
		{"[0.5]", "array<ratio>", "array<ratio>"},
		{"if(f, 0, 0)", "money", "money"},
	} {
		t.Run(test.source+" as "+test.result, func(t *testing.T) {
			t.Parallel()
			artifact, err := compileMoney(t, test.source, contract, test.result)
			if err != nil {
				t.Fatalf("CompileExpr(%q) error = %v", test.source, err)
			}
			if got := artifact.Result().String(); got != test.want {
				t.Fatalf("CompileExpr(%q) result = %s, want %s", test.source, got, test.want)
			}
		})
	}
}

// Negating a ratio subtracts it from a zero read as a ratio; a zero nothing
// asks to be a ratio stays an int.
func TestZeroIsARatioOnlyWhenARatioAsks(t *testing.T) {
	t.Parallel()
	for _, test := range []struct{ source, contract, want string }{
		{"-fee", "fee: ratio", "ratio"},
		{"0 - fee", "fee: ratio", "ratio"},
		{"-2.9%", "", "ratio"},
		{"x - 1", "x: int", "int"},
		{"-x", "x: int", "int"},
		{"0", "", "int"},
	} {
		t.Run(test.source, func(t *testing.T) {
			t.Parallel()
			if got := resultOrError(t, test.source, test.contract, ""); got != test.want {
				t.Fatalf("CompileExpr(%q) under %q = %s, want %s", test.source, test.contract, got, test.want)
			}
		})
	}
}

// Reading a decimal as a ratio is allowed and never preferred, so a free
// variable beside one is a float; 0 beside one is an int.
func TestFreeVariablesBesideLiteralsKeepTheirOldTypes(t *testing.T) {
	t.Parallel()
	for source, want := range map[string]string{
		"risk * 0.5":       "float",
		"risk < 0.5":       "float",
		"risk * 2.5 < 1.5": "float",
		"[risk, 0.5]":      "float",
		"risk > 0":         "int",
		"risk + 0":         "int",
		"[risk, 0]":        "int",
	} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			artifact, err := compileMoney(t, source, "", "")
			if err != nil {
				t.Fatalf("CompileExpr(%q) error = %v", source, err)
			}
			if got := artifact.Args()[0].Type().String(); got != want {
				t.Fatalf("CompileExpr(%q) gives risk %s, want %s", source, got, want)
			}
		})
	}
}

// Literals that cannot share a type are still refused, money or not. 0 and
// 0.5 meeting are an int and a float whichever comes first — where the
// branches of an if, the cases of a switch or the items of an array meet, in
// a record field, through a let — unless the context asks for a ratio.
func TestLiteralsThatCannotMeetAreRefused(t *testing.T) {
	t.Parallel()
	const contract = "usd: money; b: bool; k: int; x: int"
	for _, source := range []string{
		"[0.5, 0]", "[0, 0.5]", "[0.5, usd]", "[0, usd, 1]", `[0.5, "a"]`, "let(z = 0, [z + usd, z + 1])", "1 + 2.5%", "2.5% - 1",
		"if(b, 0, 0.5)", "if(b, 0.5, 0)", "switch(k, case 0 => 0.5, else => 0)", "x == 0 || x == 0.5", "[x, 0, 0.5]",
		"let(z = 0, w = 0.5, if(b, z, w))", `{"a": 0, "b": 0.5}`, "{a: if(b, 0, 0.5)}",
	} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			if _, err := compileMoney(t, source, contract, ""); err == nil {
				t.Fatalf("CompileExpr(%q) compiled, want a type error", source)
			}
			if _, err := CompileExpr(source, machine.CoreRegistry(), CompileOptions{Args: moneyContract(t, "b: bool; k: int; x: int")}); err == nil && !strings.Contains(source, "usd") {
				t.Fatalf("CompileExpr(%q) without money compiled, want the type error it has with money", source)
			}
		})
	}
}

// A context asking for a ratio reads 0 and 0.5 as one: the ratio is what the
// contract says the program returns, not a reading the meeting chose.
func TestLiteralsMeetWhereARatioIsAsked(t *testing.T) {
	t.Parallel()
	for _, test := range []struct{ source, result, want string }{
		{"if(b, 0, 0.5)", "ratio", "ratio"},
		{"[0, 0.5]", "array<ratio>", "array<ratio>"},
		{"round(if(b, 0, 0.5) * usd, @half_even)", "", "money"},
	} {
		t.Run(test.source, func(t *testing.T) {
			t.Parallel()
			artifact, err := compileMoney(t, test.source, "usd: money; b: bool", test.result)
			if err != nil {
				t.Fatalf("CompileExpr(%q) error = %v", test.source, err)
			}
			if got := artifact.Result().String(); got != test.want {
				t.Fatalf("CompileExpr(%q) result = %s, want %s", test.source, got, test.want)
			}
		})
	}
}

// Two literals written as different kinds sharing one open type do not
// settle: settleLiterals refuses them rather than settling the type to one
// and leaving the other's constant disagreeing with it.
func TestSettleLiteralsRefusesDifferentlyWrittenLiterals(t *testing.T) {
	t.Parallel()
	state := newInferState()
	zero, half := state.literalTerm(machine.Int(0), true), state.literalTerm(machine.Float(0.5), true)
	if err := state.unify(zero, half); err != nil {
		t.Fatal(err)
	}
	if err := state.settleLiterals(); err == nil || !strings.Contains(err.Error(), "cannot share") {
		t.Fatalf("settleLiterals() of 0 and 0.5 sharing a type = %v, want the literals cannot share a type", err)
	}
	if forks := state.forkLiteral(zero); len(forks) != 0 {
		t.Fatalf("forkLiteral of 0 and 0.5 sharing a type = %d readings, want none", len(forks))
	}
}

// literalKinds is each candidate forkLiteral makes: the kind the literal
// became and the ratio-literal count it paid.
func literalKinds(t *testing.T, value machine.Value, money bool) string {
	t.Helper()
	state := newInferState()
	term := state.literalTerm(value, money)
	var out []string
	for _, candidate := range state.forkLiteral(term) {
		out = append(out, fmt.Sprintf("%s/%d", candidate.deref(term).kind, candidate.convertedLiterals))
	}
	return strings.Join(out, " ")
}

func TestForkLiteralSettlesOncePerKind(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name  string
		value machine.Value
		money bool
		want  string
	}{
		{"a decimal with money", machine.Float(0.5), true, "float/0 ratio/1"},
		{"zero with money", machine.Int(0), true, "int/0 ratio/1 money/1"},
		{"another int is only an int", machine.Int(7), true, "int/0"},
		{"a string is only a string", machine.String("x"), true, "string/0"},
		{"a decimal without money", machine.Float(0.5), false, "float/0"},
		{"zero without money", machine.Int(0), false, "int/0"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := literalKinds(t, test.value, test.money); got != test.want {
				t.Fatalf("forkLiteral(%v, money %v) = %s, want %s", test.value.Any(), test.money, got, test.want)
			}
		})
	}
}

// forkLiteral leaves a literal the context already settled alone.
func TestForkLiteralKeepsASettledLiteral(t *testing.T) {
	t.Parallel()
	state := newInferState()
	term := state.literalTerm(machine.Float(0.5), true)
	if err := state.unify(term, scalarTerm(machine.RatioKind)); err != nil {
		t.Fatal(err)
	}
	if forks := state.forkLiteral(term); len(forks) != 1 || forks[0] != state {
		t.Fatalf("forkLiteral of a settled literal = %d states, want the state itself", len(forks))
	}
	if err := state.unify(state.literalTerm(machine.Int(0), true), scalarTerm(machine.FloatKind)); err == nil {
		t.Fatal("0 unified with float, want a literal that cannot be float")
	}
}

// settleLiterals gives an open literal its plain kind and counts the
// decimals that became ratios.
func TestSettleLiteralsCountsRatios(t *testing.T) {
	t.Parallel()
	state := newInferState()
	open, rate, zero := state.literalTerm(machine.Float(0.5), true), state.literalTerm(machine.Float(0.25), true), state.literalTerm(machine.Int(0), true)
	if err := state.unify(rate, scalarTerm(machine.RatioKind)); err != nil {
		t.Fatal(err)
	}
	if err := state.settleLiterals(); err != nil {
		t.Fatal(err)
	}
	got := fmt.Sprint(state.deref(open).kind, state.deref(rate).kind, state.deref(zero).kind, state.convertedLiterals)
	if want := "float ratio int 1"; got != want {
		t.Fatalf("settled = %s, want %s", got, want)
	}
}

func TestADecimalStaysAFloatUnlessMoneyAsks(t *testing.T) {
	t.Parallel()
	artifact, err := CompileExpr("risk < 0.5", moneyRegistry(t), CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got := artifact.Args()[0].Type(); !got.Equal(machine.FloatType) {
		t.Fatalf("risk < 0.5 gives risk %s, want float", got)
	}
}

func moneyRegistry(t testing.TB) *machine.Registry {
	t.Helper()
	registry := machine.CoreRegistry()
	if err := registry.EnableForm(machine.SwitchForm, machine.ForForm, machine.ReduceForm); err != nil {
		t.Fatal(err)
	}
	err := registry.DeclareMoney(money.MoneySpec{Currencies: []money.CurrencySpec{
		{Code: "USD", Digits: 2}, {Code: "EUR", Digits: 2}, {Code: "JPY", Digits: 0}, {Code: "KWD", Digits: 3},
	}})
	if err != nil {
		t.Fatal(err)
	}
	return registry
}

func moneyContract(t testing.TB, text string) []ArgSpec {
	t.Helper()
	var args []ArgSpec
	for part := range strings.SplitSeq(text, ";") {
		name, typeText, ok := strings.Cut(strings.TrimSpace(part), ":")
		if !ok {
			continue
		}
		typ, err := machine.ParseType(strings.TrimSpace(typeText))
		if err != nil {
			t.Fatalf("contract %q: %v", text, err)
		}
		args = append(args, ArgSpec{Name: strings.TrimSpace(name), Type: typ})
	}
	return args
}

func runMoney(t *testing.T, source, contract string, args map[string]any) (string, error) {
	t.Helper()
	registry := moneyRegistry(t)
	artifact, err := CompileExpr(source, registry, CompileOptions{Args: moneyContract(t, contract)})
	if err != nil {
		return "", err
	}
	runtime, err := machine.Instantiate(artifact, registry)
	if err != nil {
		t.Fatalf("Instantiate(%q) error = %v", source, err)
	}
	value, err := runtime.Run(t.Context(), args, machine.RunOptions{Fuel: 10_000})
	if err != nil {
		return "", err
	}
	return fmt.Sprint(value.Any()), nil
}

func compileMoney(t *testing.T, source, contract, result string) (*machine.Artifact, error) {
	t.Helper()
	options := CompileOptions{Args: moneyContract(t, contract)}
	if result != "" {
		typ, err := machine.ParseType(result)
		if err != nil {
			t.Fatalf("result %q: %v", result, err)
		}
		options.Result = &typ
	}
	return CompileExpr(source, moneyRegistry(t), options)
}

func runArtifact(t *testing.T, artifact *machine.Artifact, args map[string]any) (string, error) {
	t.Helper()
	registry := moneyRegistry(t)
	runtime, err := machine.Instantiate(artifact, registry)
	if err != nil {
		t.Fatalf("Instantiate error = %v", err)
	}
	value, err := runtime.Run(t.Context(), args, machine.RunOptions{Fuel: 10_000})
	if err != nil {
		return "", err
	}
	return fmt.Sprint(value.Any()), nil
}

func resultOrError(t *testing.T, source, contract, result string) string {
	t.Helper()
	artifact, err := compileMoney(t, source, contract, result)
	if err != nil {
		_, message, _ := strings.Cut(err.Error(), "type error: ")
		return "error: " + message
	}
	return artifact.Result().String()
}
