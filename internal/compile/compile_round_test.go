package compile

import (
	"context"
	"fmt"
	"maps"
	"strings"
	"testing"

	"github.com/nethinwei/funroute/internal/machine"
)

func TestRoundScopesEveryStepInside(t *testing.T) {
	t.Parallel()
	for name, test := range map[string]struct{ source, want string }{
		"round half up":              {"round(amount * 0.025, @half_up)", "{USD 3}"},
		"round down names another":   {"round(amount * 0.025, @down)", "{USD 2}"},
		"a folded step still counts": {"round(amount + money(100, USD) * 0.025, @up)", "{USD 103}"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, err := runMoney(t, test.source, "amount: money", map[string]any{"amount": "USD 1.00"})
			if err != nil || got != test.want {
				t.Fatalf("%s = %s, %v, want %s", test.source, got, err, test.want)
			}
		})
	}
}

func TestRoundWithNothingToRoundIsAnError(t *testing.T) {
	t.Parallel()
	_, err := CompileExpr("round(amount + amount, @up)", moneyRegistry(t), CompileOptions{Args: moneyContract(t, "amount: money")})
	if err == nil || !strings.Contains(err.Error(), "nothing inside round") {
		t.Fatalf("round without a rounding step: error = %v, want one naming it", err)
	}
}

func checkRegistry(t *testing.T) *machine.Registry {
	t.Helper()
	registry := moneyRegistry(t)
	first := func(_ context.Context, args []machine.Value) (machine.Value, error) { return args[0], nil }
	count := func(_ context.Context, args []machine.Value) (machine.Value, error) {
		return machine.Int(int64(len(args))), nil
	}
	u := machine.MoneyType
	for _, spec := range []machine.FunctionSpec{
		{Name: "fees.cap_v1", Params: []machine.Type{u, u}, Result: u, Eval: first},
		{Name: "fees.two_v1", Params: []machine.Type{u, u, u, u}, Result: machine.IntType, Eval: count},
		{Name: "fees.sum_v1", Params: []machine.Type{machine.ArrayOf(u), u}, Result: u, Eval: func(_ context.Context, args []machine.Value) (machine.Value, error) { return args[1], nil }},
		{Name: "fees.in_v1", Params: []machine.Type{u, machine.CurrencyType}, Result: machine.IntType, Eval: count},
	} {
		if err := registry.Register(spec); err != nil {
			t.Fatal(err)
		}
	}
	registerAverage(t, registry)
	return registry
}

func registerAverage(t *testing.T, registry *machine.Registry) {
	t.Helper()
	amounts := machine.ArrayOf(machine.MoneyType)
	unit := func(args []machine.Value, minor int64) machine.Value {
		items, _ := args[0].Array()
		money, _ := items[0].Money()
		return machine.MoneyValue(minor, money.Currency())
	}
	for _, spec := range []machine.FunctionSpec{
		{Name: "fees.avg", Params: []machine.Type{amounts}, Result: machine.MoneyType,
			Eval: func(_ context.Context, args []machine.Value) (machine.Value, error) { return unit(args, 1), nil }},
		{Name: "fees.avg", Params: []machine.Type{amounts, machine.RoundingEnumType()}, Result: machine.MoneyType,
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

const checkContract = "usd: money; eur: money; a: money; u: money; v: money; " +
	"cur: currency; ms: array<money>; s: string; f: bool; amount: money; yen: money; quotes: array<fxrate>"

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

func runChecked(t *testing.T, source, result string, args map[string]any) (string, error) {
	t.Helper()
	artifact, err := compileChecked(t, source, result)
	if err != nil {
		t.Fatalf("CompileExpr(%q) error = %v", source, err)
	}
	all := map[string]any{
		"usd": "USD 1.00", "eur": "EUR 1.00", "a": "USD 1.00", "u": "USD 1.00", "v": "USD 1.00",
		"cur": "USD", "ms": []any{"USD 1.00"}, "s": "USD", "f": false, "amount": "USD 1.00", "yen": "JPY 150", "quotes": testQuotes(),
	}
	maps.Copy(all, args)
	registry := checkRegistry(t)
	runtime, err := machine.Instantiate(artifact, registry)
	if err != nil {
		t.Fatalf("Instantiate(%q) error = %v", source, err)
	}
	value, err := runtime.Run(t.Context(), all)
	if err != nil {
		return "", err
	}
	return fmt.Sprint(value.Any()), nil
}

// testQuotes are the exchange rates every test rule may convert at, as the
// argument quotes: array<fxrate> carries them.
func testQuotes() []any {
	quote := func(base, quote, rate string) map[string]any {
		return map[string]any{"base": base, "quote": quote, "rate": rate}
	}
	return []any{quote("USD", "JPY", "150.5"), quote("USD", "EUR", "0.9"), quote("EUR", "JPY", "167")}
}

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
		{"using(quotes, round(amount -> JPY, @down))", nil, "{JPY 150}"},
		{"round(using(quotes, amount -> JPY), @up)", nil, "{JPY 151}"},
		{"using(quotes, round(amount -> JPY, @half_up))", nil, "{JPY 151}"},
		{"using(quotes, round(yen -> USD, @up))", map[string]any{"yen": "JPY 100"}, "{USD 67}"},
		{"using(quotes, round(yen -> USD, @down))", map[string]any{"yen": "JPY 100"}, "{USD 66}"},
		{"round(round(amount * 0.025, @down) + amount * 0.025, @up)", nil, "{USD 5}"},
		{"round(let(x = USD 1.00 * 0.025, x + amount), @down)", nil, "{USD 102}"},
		{"round(let(x = USD 1.00 * 0.025, x + amount), @up)", nil, "{USD 103}"},
		{"round(if(f, amount * 2.5%, amount), @down)", map[string]any{"f": true}, "{USD 2}"},
		{"round(fees.avg(ms) * 0.5, @down)", nil, "{USD 0}"},
		{"round(fees.avg(ms) * 0.5, @up)", nil, "{USD 1}"},
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

// Inside a round the steps are the bare, exact operations and round is the
// call after them; a step's variant with the mode rounds by itself. A host
// function is no step: its answer is already whole minor units.
func TestRoundRunsAfterItsSteps(t *testing.T) {
	t.Parallel()
	for source, want := range map[string]string{
		"round(amount * u2, @down)":                         "mul round",
		"using(150 JPY / USD, round(amount -> JPY, @down))": "convert round",
		"round(fees.avg(ms) * u2, @down)":                   "fees.avg mul round",
		"mul(amount, u2, @down)":                            "mul",
		"fees.avg(ms)":                                      "fees.avg",
	} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			options := CompileOptions{Args: moneyContract(t, "amount: money; u2: ratio; ms: array<money>")}
			artifact, err := CompileExpr(source, checkRegistry(t), options)
			if err != nil {
				t.Fatal(err)
			}
			parts := machine.PartsOf(artifact).Calls
			calls := make([]string, 0, len(parts))
			for _, call := range parts {
				calls = append(calls, call.Name)
			}
			if got := strings.Join(calls, " "); got != want {
				t.Fatalf("CompileExpr(%q) calls %s, want %s", source, got, want)
			}
		})
	}
}

func TestRoundNeedsAStepAndAWrittenMode(t *testing.T) {
	t.Parallel()
	for source, want := range map[string]string{
		"round(amount * 2, @up)":        "nothing inside round",
		"round(2.5% * 2.5%, @up)":       "no overload round",
		"round(1.5, @up)":               "no overload round",
		"round(fees.cap_v1(u, u), @up)": "nothing inside round",
		"round(fees.avg(ms), @up)":      "nothing inside round",
		"let(x = round(USD 1.00 * 0.025, @half_even), round(x + amount, @up))": "nothing inside round",
		"round(round(amount + amount, @down) + amount * 0.025, @up)":           "nothing inside round",
		"round(amount * 0.025, if(f, @up, @down))":                             "mode written out",
		`round(amount * 0.025, "up")`:                                          "no overload",
		"round(amount * 0.025, USD)":                                           "no overload",
	} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			if _, err := compileChecked(t, source, ""); err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("CompileExpr(%q) error = %v, want %q", source, err, want)
			}
		})
	}
}

func TestARoundAroundOnlyARoundIsAnError(t *testing.T) {
	t.Parallel()
	options := CompileOptions{Args: moneyContract(t, "amount: money")}
	if _, err := CompileExpr("round(round(amount * 0.025, @up), @down)", moneyRegistry(t), options); err == nil || !strings.Contains(err.Error(), "nothing inside round") {
		t.Fatalf("round(round(…, @up), @down) error = %v, want nothing inside round(…) rounds", err)
	}
	if _, err := CompileExpr("round(round(amount * 0.025, @up) + amount * 0.025, @down)", moneyRegistry(t), options); err != nil {
		t.Fatalf("a round with a step of its own beside a nested one: error = %v", err)
	}
}
