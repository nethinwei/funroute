package compile

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"strings"
	"testing"

	"funroute/lang/internal/machine"
)

func TestRoundScopesEveryStepInside(t *testing.T) {
	t.Parallel()
	for name, test := range map[string]struct{ source, want string }{
		"the registry's half up":     {"amount * 0.025", "{USD 3}"},
		"round down names another":   {"round(amount * 0.025, @down)", "{USD 2}"},
		"a folded step still counts": {"round(amount + money(100, USD) * 0.025, @up)", "{USD 103}"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, err := runMoney(t, test.source, "amount: money<USD>", map[string]any{"amount": "USD 1.00"})
			if err != nil || got != test.want {
				t.Fatalf("%s = %s, %v, want %s", test.source, got, err, test.want)
			}
		})
	}
}

func TestRoundWithNothingToRoundIsAnError(t *testing.T) {
	t.Parallel()
	_, err := CompileExpr("round(amount + amount, @up)", moneyRegistry(t), CompileOptions{Args: moneyContract(t, "amount: money<USD>")})
	if err == nil || !strings.Contains(err.Error(), "nothing inside round") {
		t.Fatalf("round without a rounding step: error = %v, want one naming it", err)
	}
}

func TestTheResultIsCheckedAgainstTheContract(t *testing.T) {
	t.Parallel()
	registry := moneyRegistry(t)
	result := machine.MoneyOf("USD")
	artifact, err := CompileExpr("money(minor, cur)", registry, CompileOptions{
		Args: moneyContract(t, "minor: int; cur: currency<?>"), Result: &result,
	})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := machine.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Run(t.Context(), map[string]any{"minor": 5, "cur": "EUR"}, machine.RunOptions{}); !errors.Is(err, machine.ErrCurrency) {
		t.Fatalf("euros returned as dollars: error = %v, want ErrCurrency", err)
	}
	value, err := runtime.Run(t.Context(), map[string]any{"minor": 5, "cur": "USD"}, machine.RunOptions{})
	if money, _ := value.Money(); err != nil || money.Currency() != "USD" || money.Minor() != 5 {
		t.Fatalf("dollars returned as dollars = %v, %v", value.Any(), err)
	}
}

// A host function that takes two amounts in one currency gets them checked
// before the call when the program could not prove it; the kernel's own
// operators check for themselves and get nothing emitted.
func TestAHostFunctionsCurrenciesAreCheckedBeforeTheCall(t *testing.T) {
	t.Parallel()
	registry := moneyRegistry(t)
	err := registry.Register(machine.FunctionSpec{
		Name: "fees.cap_v1", Params: []machine.Type{machine.MoneyOf("u"), machine.MoneyOf("u")}, Result: machine.MoneyOf("u"),
		Eval: func(_ context.Context, args []machine.Value) (machine.Value, error) { return args[0], nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	contract := moneyContract(t, "minor: int; cur: currency<?>; cap: money<USD>")
	artifact, err := CompileExpr("fees.cap_v1(money(minor, cur), cap)", registry, CompileOptions{Args: contract})
	if err != nil {
		t.Fatal(err)
	}
	if !emits(artifact, machine.OpCurrencyCheck) {
		t.Fatalf("instructions %v check no currency", machine.PartsOf(artifact).Instructions)
	}
	runtime, err := machine.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	_, err = runtime.Run(t.Context(), map[string]any{"minor": 5, "cur": "EUR", "cap": "USD 1.00"}, machine.RunOptions{})
	if !errors.Is(err, machine.ErrCurrency) {
		t.Fatalf("euros and dollars into one currency: error = %v, want ErrCurrency", err)
	}
	proven, err := CompileExpr("fees.cap_v1(cap, cap)", registry, CompileOptions{Args: contract})
	if err != nil {
		t.Fatal(err)
	}
	if emits(proven, machine.OpCurrencyCheck) {
		t.Fatalf("proven dollars are checked anyway: %v", machine.PartsOf(proven).Instructions)
	}
}

func emits(artifact *machine.Artifact, op machine.OpCode) bool {
	for _, instruction := range machine.PartsOf(artifact).Instructions {
		if instruction.Op == op {
			return true
		}
	}
	return false
}

// checkRegistry is the money registry with host functions whose signatures
// tie currencies together in every way a signature can.
func checkRegistry(t *testing.T) *machine.Registry {
	t.Helper()
	registry := moneyRegistry(t)
	first := func(_ context.Context, args []machine.Value) (machine.Value, error) { return args[0], nil }
	count := func(_ context.Context, args []machine.Value) (machine.Value, error) {
		return machine.Int(int64(len(args))), nil
	}
	money, u, v := machine.MoneyOf, machine.MoneyOf("u"), machine.MoneyOf("v")
	for _, spec := range []machine.FunctionSpec{
		{Name: "fees.cap_v1", Params: []machine.Type{u, u}, Result: u, Eval: first},
		{Name: "fees.usd_only_v1", Params: []machine.Type{money("USD")}, Result: machine.IntType, Eval: count},
		{Name: "fees.two_v1", Params: []machine.Type{u, u, v, v}, Result: machine.IntType, Eval: count},
		{Name: "fees.sum_v1", Params: []machine.Type{machine.ArrayOf(u), u}, Result: u, Eval: func(_ context.Context, args []machine.Value) (machine.Value, error) { return args[1], nil }},
		{Name: "fees.in_v1", Params: []machine.Type{u, machine.CurrencyOf("u")}, Result: machine.IntType, Eval: count},
	} {
		if err := registry.Register(spec); err != nil {
			t.Fatal(err)
		}
	}
	registerAverage(t, registry)
	return registry
}

// registerAverage adds fees.avg the way a pack adds avg: once rounding by the
// registry's default, once with the mode spelled out. The first answers one
// minor unit, the second as many as the mode's name has letters.
func registerAverage(t *testing.T, registry *machine.Registry) {
	t.Helper()
	amounts := machine.ArrayOf(machine.MoneyOf("u"))
	unit := func(args []machine.Value, minor int64) machine.Value {
		items, _ := args[0].Array()
		money, _ := items[0].Money()
		return machine.MoneyValue(minor, money.Currency())
	}
	for _, spec := range []machine.FunctionSpec{
		{Name: "fees.avg", Params: []machine.Type{amounts}, Result: machine.MoneyOf("u"),
			Eval: func(_ context.Context, args []machine.Value) (machine.Value, error) { return unit(args, 1), nil }},
		{Name: "fees.avg", Params: []machine.Type{amounts, machine.RoundingEnumType()}, Result: machine.MoneyOf("u"),
			Eval: func(_ context.Context, args []machine.Value) (machine.Value, error) {
				mode, _ := args[1].String()
				return unit(args, int64(len(mode))), nil
			}},
	} {
		if err := registry.Register(spec); err != nil {
			t.Fatal(err)
		}
	}
}

// checkContract names operands in every state for the host functions.
const checkContract = "usd: money<USD>; eur: money<EUR>; a: money<c>; u: money<?>; v: money<?>; " +
	"cur: currency<?>; ms: array<money<?>>; s: string; f: bool; amount: money<USD>; yen: money<JPY>"

// describeChecks spells the currency checks an artifact carries, in order:
// the mode (group n, a code, a pattern), @ how deep the operand sits, and
// the flags.
func describeChecks(artifact *machine.Artifact) string {
	var out []string
	for _, in := range machine.PartsOf(artifact).Instructions {
		if in.Op != machine.OpCurrencyCheck {
			continue
		}
		text := map[int]string{machine.CheckGroup: fmt.Sprint("group", in.D), machine.CheckCode: "code"}[in.C]
		if in.C == machine.CheckCode {
			text += in.Keys[0]
		}
		if in.C == machine.CheckPattern {
			text = "pattern " + in.Type.String()
		}
		text += fmt.Sprint("@", in.A)
		if in.B&machine.CheckNewCall != 0 {
			text += ",new"
		}
		out = append(out, text)
	}
	return strings.Join(out, " ")
}

func compileChecked(t *testing.T, source, result string) (*machine.Artifact, error) {
	t.Helper()
	options := CompileOptions{Args: moneyContract(t, checkContract)}
	if result != "" {
		typ, err := machine.ParseType(result)
		if err != nil {
			t.Fatal(err)
		}
		options.Result = &typ
	}
	return CompileExpr(source, checkRegistry(t), options)
}

// A host function gets checks exactly where its signature ties currencies
// together and inference could not prove them: a shared variable puts every
// operand of it into one group, a written code checks the operand against
// it, a conversion's result is in the currency it was converted to, and
// money inside a container is left to the function.
func TestHostSignaturesEmitTheChecksTheyNeed(t *testing.T) {
	t.Parallel()
	for source, want := range map[string]string{
		"fees.cap_v1(usd, usd)":             "",
		"fees.cap_v1(a, a)":                 "",
		"fees.cap_v1(u, v)":                 "group0@1,new group0@0",
		"fees.cap_v1(u, usd)":               "group0@1,new group0@0",
		"fees.cap_v1(0, usd)":               "",
		"fees.usd_only_v1(usd)":             "",
		"fees.usd_only_v1(u)":               "codeUSD@0,new",
		"fees.cap_v1(eur -> USD, usd)":      "",
		"fees.usd_only_v1(u -> USD)":        "",
		"fees.usd_only_v1(u -> cur)":        "codeUSD@0,new",
		"fees.two_v1(u, v, usd, u)":         "group0@3,new group0@2 group1@1 group1@0",
		"fees.two_v1(usd, usd, u, v)":       "group0@1,new group0@0",
		"fees.sum_v1(ms, u)":                "",
		"fees.in_v1(u, cur)":                "group0@1,new group0@0",
		"fees.in_v1(usd, USD)":              "",
		"fees.in_v1(a, currency(a))":        "",
		"usd + u":                           "",
		"usd + (u -> USD)":                  "",
		"fees.cap_v1(if(f, usd, eur), usd)": "group0@1,new group0@0",
	} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			artifact, err := compileChecked(t, source, "")
			if err != nil {
				t.Fatalf("CompileExpr(%q) error = %v", source, err)
			}
			if got := describeChecks(artifact); got != want {
				t.Fatalf("CompileExpr(%q) checks %q, want %q", source, got, want)
			}
		})
	}
}

// Proven currencies a host signature cannot take are compile errors.
func TestHostSignaturesRefuseProvenMismatches(t *testing.T) {
	t.Parallel()
	for _, source := range []string{
		"fees.cap_v1(usd, eur)", "fees.cap_v1(a, usd)", "fees.usd_only_v1(eur)", "fees.usd_only_v1(a)",
		"fees.in_v1(usd, EUR)", "fees.two_v1(u, v, usd, eur)", "fees.cap_v1(usd -> EUR, usd)", "fees.usd_only_v1(u -> EUR)",
	} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			if _, err := compileChecked(t, source, ""); err == nil || !strings.Contains(err.Error(), "no overload") {
				t.Fatalf("CompileExpr(%q) error = %v, want no overload", source, err)
			}
		})
	}
}

// The emitted checks compare at run time, and each call starts its groups
// afresh.
func TestHostChecksCompareAtRunTime(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		source string
		args   map[string]any
		want   string // "" for ErrCurrency
	}{
		{"fees.usd_only_v1(u)", map[string]any{"u": "USD 1.00"}, "1"},
		{"fees.usd_only_v1(u)", map[string]any{"u": "EUR 1.00"}, ""},
		{"fees.usd_only_v1(u -> cur)", map[string]any{"cur": "USD"}, "1"},
		{"fees.usd_only_v1(u -> cur)", map[string]any{"cur": "JPY"}, ""},
		{"fees.two_v1(u, v, usd, u)", map[string]any{"u": "USD 1.00", "v": "USD 2.00"}, "4"},
		{"fees.two_v1(u, v, usd, u)", map[string]any{"u": "USD 1.00", "v": "EUR 2.00"}, ""},
		{"fees.in_v1(u, cur)", map[string]any{"u": "USD 1.00", "cur": "USD"}, "2"},
		{"fees.in_v1(u, cur)", map[string]any{"u": "USD 1.00", "cur": "JPY"}, ""},
		{"[fees.cap_v1(u, u), fees.cap_v1(v, v)]", map[string]any{"u": "USD 1.00", "v": "EUR 2.00"}, "[{USD 100} {EUR 200}]"},
	} {
		t.Run(fmt.Sprint(test.source, test.args), func(t *testing.T) {
			t.Parallel()
			got, err := runChecked(t, test.source, "", test.args)
			if test.want == "" && !errors.Is(err, machine.ErrCurrency) {
				t.Fatalf("%s = %s, %v, want ErrCurrency", test.source, got, err)
			}
			if test.want != "" && (err != nil || got != test.want) {
				t.Fatalf("%s = %s, %v, want %s", test.source, got, err, test.want)
			}
		})
	}
}

// runChecked runs source on checkRegistry with rateTable; every argument
// checkContract declares and the case leaves out gets a dollar value.
func runChecked(t *testing.T, source, result string, args map[string]any) (string, error) {
	t.Helper()
	artifact, err := compileChecked(t, source, result)
	if err != nil {
		t.Fatalf("CompileExpr(%q) error = %v", source, err)
	}
	all := map[string]any{
		"usd": "USD 1.00", "eur": "EUR 1.00", "a": "USD 1.00", "u": "USD 1.00", "v": "USD 1.00",
		"cur": "USD", "ms": []any{"USD 1.00"}, "s": "USD", "f": false, "amount": "USD 1.00", "yen": "JPY 150",
	}
	maps.Copy(all, args)
	registry := checkRegistry(t)
	runtime, err := machine.Instantiate(artifact, registry)
	if err != nil {
		t.Fatalf("Instantiate(%q) error = %v", source, err)
	}
	value, err := runtime.Run(t.Context(), all, machine.RunOptions{Fuel: 10_000, Rates: rateTable(t, registry)})
	if err != nil {
		return "", err
	}
	return fmt.Sprint(value.Any()), nil
}

// rateTable quotes a dollar at 150.5 yen and 0.9 euro over the registry's
// currencies.
func rateTable(t *testing.T, registry *machine.Registry) *machine.Rates {
	t.Helper()
	currencies, ok := registry.Currencies()
	if !ok {
		t.Fatal("the registry declares no money")
	}
	rates := currencies.NewRates()
	for _, quote := range [][3]string{{"USD", "JPY", "150.5"}, {"USD", "EUR", "0.9"}, {"EUR", "JPY", "167"}} {
		if err := rates.Add(quote[0], quote[1], quote[2]); err != nil {
			t.Fatal(err)
		}
	}
	return rates
}

// The result is checked against the contract's currencies at the end where
// the expression did not prove them — through arrays, dictionaries and
// nesting, for codes and contract variables alike — and not where it did.
func TestResultChecksFollowTheDeclaredPattern(t *testing.T) {
	t.Parallel()
	for _, test := range []struct{ source, result, want string }{
		{"usd", "money<USD>", ""},
		{"usd", "money<?>", ""},
		{"u", "money<?>", ""},
		{"a", "money<c>", ""},
		{"{fee: a}", "record{fee: money<c>}", ""},
		{"[usd]", "array<money<USD>>", ""},
		{"u", "money<USD>", "pattern money<USD>@0"},
		{"u", "money<c>", "pattern money<c>@0"},
		{"usd + u", "money<USD>", "pattern money<USD>@0"},
		{"if(f, a, u)", "money<c>", "pattern money<c>@0"},
		{"[u]", "array<money<USD>>", "pattern array<money<USD>>@0"},
		{"[usd, u]", "array<money<USD>>", "pattern array<money<USD>>@0"},
		{`{"k": u}`, "dict<money<USD>>", "pattern dict<money<USD>>@0"},
		{"[[u]]", "array<array<money<USD>>>", "pattern array<array<money<USD>>>@0"},
		{"currency_of(s)", "currency<USD>", "pattern currency<USD>@0"},
		{"usd -> EUR", "money<EUR>", ""},
		{"u -> cur", "money<USD>", "pattern money<USD>@0"},
	} {
		t.Run(test.source+" as "+test.result, func(t *testing.T) {
			t.Parallel()
			artifact, err := compileChecked(t, test.source, test.result)
			if err != nil {
				t.Fatalf("CompileExpr(%q) as %s error = %v", test.source, test.result, err)
			}
			if got := describeChecks(artifact); got != test.want {
				t.Fatalf("CompileExpr(%q) as %s checks %q, want %q", test.source, test.result, got, test.want)
			}
		})
	}
}

func TestResultChecksCompareAtRunTime(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		source, result string
		args           map[string]any
		want           string // "" for ErrCurrency
	}{
		{"u", "money<USD>", map[string]any{"u": "USD 2.00"}, "{USD 200}"},
		{"u", "money<USD>", map[string]any{"u": "EUR 2.00"}, ""},
		{"u", "money<c>", map[string]any{"a": "JPY 1", "u": "JPY 2"}, "{JPY 2}"},
		{"u", "money<c>", map[string]any{"a": "JPY 1", "u": "EUR 2.00"}, ""},
		{"[usd, u]", "array<money<USD>>", map[string]any{"u": "EUR 2.00"}, ""},
		{`{"k": u}`, "dict<money<USD>>", map[string]any{"u": "EUR 2.00"}, ""},
		{"[[u]]", "array<array<money<USD>>>", map[string]any{"u": "EUR 2.00"}, ""},
		{"u -> cur", "money<USD>", map[string]any{"cur": "EUR"}, ""},
		{"u -> cur", "money<USD>", map[string]any{"cur": "USD"}, "{USD 100}"},
		{"currency_of(s)", "currency<USD>", map[string]any{"s": "EUR"}, ""},
		{"currency_of(s)", "currency<USD>", map[string]any{"s": "USD"}, "USD"},
	} {
		t.Run(fmt.Sprint(test.source, test.args), func(t *testing.T) {
			t.Parallel()
			got, err := runChecked(t, test.source, test.result, test.args)
			if test.want == "" && !errors.Is(err, machine.ErrCurrency) {
				t.Fatalf("%s as %s = %s, %v, want ErrCurrency", test.source, test.result, got, err)
			}
			if test.want != "" && (err != nil || got != test.want) {
				t.Fatalf("%s as %s = %s, %v, want %s", test.source, test.result, got, err, test.want)
			}
		})
	}
}

// A result proven in another currency than the contract's is refused.
func TestAProvenResultInAnotherCurrencyIsRefused(t *testing.T) {
	t.Parallel()
	for _, test := range []struct{ source, result, want string }{
		{"eur", "money<USD>", "is in EUR"},
		{"usd", "money<c>", "is in USD"},
		{"a", "money<USD>", "is in c"},
		{"[eur]", "array<money<USD>>", "is in EUR"},
		{"usd -> EUR", "money<USD>", "is in EUR"},
		{"currency(eur)", "currency<USD>", "is in EUR"},
	} {
		t.Run(test.source+" as "+test.result, func(t *testing.T) {
			t.Parallel()
			if _, err := compileChecked(t, test.source, test.result); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("CompileExpr(%q) as %s error = %v, want %q", test.source, test.result, err, test.want)
			}
		})
	}
}

// Every step inside round(…) takes its mode — money times a rate, money over
// a rate, a conversion, a host function's rounding variant — the innermost
// round deciding, and a step folded while compiling the scope still counts.
func TestRoundModesReachEveryStep(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		source string
		args   map[string]any
		want   string
	}{
		{"round(amount * 0.025, @half_even)", nil, "{USD 2}"},
		{"round(amount * 0.025, @half_down)", nil, "{USD 2}"},
		{"round(amount * 0.025, @ceiling)", nil, "{USD 3}"},
		{"round(amount * 0.025, @floor)", map[string]any{"amount": "USD -1.00"}, "{USD -3}"},
		{"round(amount * 0.025, @ceiling)", map[string]any{"amount": "USD -1.00"}, "{USD -2}"},
		{"round(amount * 0.025, @down)", map[string]any{"amount": "USD -1.00"}, "{USD -2}"},
		{"round(amount * 0.025, @up)", map[string]any{"amount": "USD -1.00"}, "{USD -3}"},
		{"round(amount / 0.9, @down)", nil, "{USD 111}"},
		{"round(amount / 0.9, @up)", nil, "{USD 112}"},
		{"round(amount -> JPY, @down)", nil, "{JPY 150}"},
		{"round(amount -> JPY, @up)", nil, "{JPY 151}"},
		{"amount -> JPY", nil, "{JPY 151}"},
		{"round(yen -> USD, @up)", map[string]any{"yen": "JPY 100"}, "{USD 67}"},
		{"round(yen -> USD, @down)", map[string]any{"yen": "JPY 100"}, "{USD 66}"},
		{"round(round(amount * 0.025, @down) + amount * 0.025, @up)", nil, "{USD 5}"},
		{"round(let(x = USD 1.00 * 0.025, x + amount), @down)", nil, "{USD 102}"},
		{"round(let(x = USD 1.00 * 0.025, x + amount), @up)", nil, "{USD 103}"},
		{"round(if(f, amount * 2.5%, amount), @down)", map[string]any{"f": true}, "{USD 2}"},
		{"round(fees.avg(ms), @half_even)", nil, "{USD 9}"},
		{"fees.avg(ms)", nil, "{USD 1}"},
		{"round(fees.avg(ms) * 0.5, @down)", nil, "{USD 2}"},
	} {
		t.Run(test.source+fmt.Sprint(test.args), func(t *testing.T) {
			t.Parallel()
			got, err := runChecked(t, test.source, "", test.args)
			if err != nil || got != test.want {
				t.Fatalf("%s with %v = %s, %v, want %s", test.source, test.args, got, err, test.want)
			}
		})
	}
}

// Inside round the step calls the variant with the mode, pushed as a
// constant; outside it calls the default.
func TestRoundSelectsTheRoundingVariant(t *testing.T) {
	t.Parallel()
	for source, want := range map[string]bool{
		"round(amount * u2, @down)":   true,
		"round(fees.avg(ms), @down)":  true,
		"round(amount -> JPY, @down)": true,
		"amount * u2":                 false,
		"fees.avg(ms)":                false,
		"amount -> JPY":               false,
	} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			options := CompileOptions{Args: moneyContract(t, "amount: money<USD>; u2: rate; ms: array<money<?>>")}
			artifact, err := CompileExpr(source, checkRegistry(t), options)
			if err != nil {
				t.Fatal(err)
			}
			variant := len(machine.PartsOf(artifact).Calls) == 1 && strings.Contains(machine.PartsOf(artifact).Calls[0].Signature, "enum<rounding>")
			if variant != want {
				t.Fatalf("CompileExpr(%q) calls %v, want the rounding variant %v", source, machine.PartsOf(artifact).Calls, want)
			}
		})
	}
}

func TestRoundNeedsAStepAndAWrittenMode(t *testing.T) {
	t.Parallel()
	for source, want := range map[string]string{
		"round(amount * 2, @up)":                                     "nothing inside round",
		"round(2.5% * 2.5%, @up)":                                    "nothing inside round",
		"round(1.5, @up)":                                            "nothing inside round",
		"round(fees.cap_v1(u, u), @up)":                              "nothing inside round",
		"let(x = USD 1.00 * 0.025, round(x + amount, @up))":          "nothing inside round",
		"round(round(amount + amount, @down) + amount * 0.025, @up)": "nothing inside round",
		"round(amount * 0.025, if(f, @up, @down))":                   "mode written out",
		`round(amount * 0.025, "up")`:                                "no overload",
		"round(amount * 0.025, USD)":                                 "no overload",
	} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			if _, err := compileChecked(t, source, ""); err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("CompileExpr(%q) error = %v, want %q", source, err, want)
			}
		})
	}
}

// A round around nothing but another round rounds nothing: each scope counts
// its own steps, and the inner one took them all.
func TestARoundAroundOnlyARoundIsAnError(t *testing.T) {
	t.Parallel()
	options := CompileOptions{Args: moneyContract(t, "amount: money<USD>")}
	if _, err := CompileExpr("round(round(amount * 0.025, @up), @down)", moneyRegistry(t), options); err == nil || !strings.Contains(err.Error(), "nothing inside round") {
		t.Fatalf("round(round(…, @up), @down) error = %v, want nothing inside round(…) rounds", err)
	}
	if _, err := CompileExpr("round(round(amount * 0.025, @up) + amount * 0.025, @down)", moneyRegistry(t), options); err != nil {
		t.Fatalf("a round with a step of its own beside a nested one: error = %v", err)
	}
}

// Two contract variables at a host function that wants one currency are
// checked where they meet, before the call: the compiler cannot say they
// differ, and the run can.
func TestTwoContractVariablesAtAHostFunctionAreChecked(t *testing.T) {
	t.Parallel()
	registry := checkRegistry(t)
	artifact, err := CompileExpr("fees.cap_v1(a, b)", registry, CompileOptions{Args: moneyContract(t, "a: money<c>; b: money<d>")})
	if err != nil {
		t.Fatalf("CompileExpr(fees.cap_v1(a, b)) error = %v, want the check left to the run", err)
	}
	if !emits(artifact, machine.OpCurrencyCheck) {
		t.Fatalf("fees.cap_v1(a, b) emits no currency check: %s", describeChecks(artifact))
	}
	runtime, err := machine.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Run(t.Context(), map[string]any{"a": "USD 1.00", "b": "USD 2.00"}, machine.RunOptions{}); err != nil {
		t.Fatalf("fees.cap_v1 of two dollars: %v", err)
	}
	if _, err := runtime.Run(t.Context(), map[string]any{"a": "USD 1.00", "b": "EUR 2.00"}, machine.RunOptions{}); !errors.Is(err, machine.ErrCurrency) {
		t.Fatalf("fees.cap_v1 of dollars and euros: error = %v, want ErrCurrency", err)
	}
}
