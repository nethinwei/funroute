package machine_test

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/nethinwei/funroute/internal/compile"
	"github.com/nethinwei/funroute/internal/machine"
	"github.com/nethinwei/funroute/internal/money"
)

func moneyRegistry(t testing.TB) *machine.Registry {
	t.Helper()
	registry := machine.CoreRegistry()
	err := registry.DeclareMoney(money.MoneySpec{Currencies: []money.CurrencySpec{
		{Code: "USD", Digits: 2}, {Code: "EUR", Digits: 2}, {Code: "JPY", Digits: 0}, {Code: "KWD", Digits: 3},
	}})
	if err != nil {
		t.Fatal(err)
	}
	return registry
}

// evalMoney compiles source with every argument declared by "name: type"
// and runs it on args.
func evalMoney(t *testing.T, source, contract string, args map[string]any) (machine.Value, error) {
	t.Helper()
	return moneyRuntime(t, source, contract, "").Run(t.Context(), args, machine.RunOptions{})
}

// runMoney is evalMoney with the arguments as values, in the contract's
// order: a currency-less zero and an int past JSON's precision say exactly
// what they are.
func runMoney(t *testing.T, source, contract string, args ...machine.Value) (machine.Value, error) {
	t.Helper()
	return moneyRuntime(t, source, contract, "").RunValues(t.Context(), args, machine.RunOptions{})
}

// moneyRuntime compiles source against the contract, and against a declared
// result type when result is not empty.
func moneyRuntime(t *testing.T, source, contract, result string) *machine.Runtime {
	t.Helper()
	registry := moneyRegistry(t)
	artifact, err := compileMoney(t, registry, source, contract, result)
	if err != nil {
		t.Fatalf("CompileExpr(%q) error = %v", source, err)
	}
	runtime, err := machine.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	return runtime
}

// compileMoney compiles source against the "name: type" contract.
func compileMoney(t testing.TB, registry *machine.Registry, source, contract, result string) (*machine.Artifact, error) {
	t.Helper()
	var specs []compile.ArgSpec
	for part := range strings.SplitSeq(contract, ";") {
		name, text, ok := strings.Cut(part, ":")
		if !ok {
			continue
		}
		specs = append(specs, compile.ArgSpec{Name: strings.TrimSpace(name), Type: parseType(t, text)})
	}
	options := compile.CompileOptions{Args: specs}
	if result != "" {
		typ := parseType(t, result)
		options.Result = &typ
	}
	return compile.CompileExpr(source, registry, options)
}

func parseType(t testing.TB, text string) machine.Type {
	t.Helper()
	typ, err := machine.ParseType(strings.TrimSpace(text))
	if err != nil {
		t.Fatal(err)
	}
	return typ
}

// ratioValue is a ratio written as a decimal.
func ratioValue(t *testing.T, text string) machine.Value {
	t.Helper()
	ratio, err := money.ParseRatio(text)
	if err != nil {
		t.Fatal(err)
	}
	return machine.RatioValue(ratio)
}

// moneyCase is one program run on values: want is the result as fmt prints
// its Go form, or for a failure the text its error must contain.
type moneyCase struct {
	source, contract string
	args             []machine.Value
	want             string
}

// expectValue runs test and requires its printed result.
func expectValue(t *testing.T, test moneyCase) {
	t.Helper()
	value, err := runMoney(t, test.source, test.contract, test.args...)
	if got := fmt.Sprint(value.Any()); err != nil || got != test.want {
		t.Fatalf("%s on %v = %s, %v, want %s", test.source, test.args, got, err, test.want)
	}
}

// expectError runs test and requires it to fail with want, or with an error
// whose text contains test.want when want is nil.
func expectError(t *testing.T, test moneyCase, want error) {
	t.Helper()
	value, err := runMoney(t, test.source, test.contract, test.args...)
	switch {
	case err == nil:
		t.Fatalf("%s on %v = %v, want an error", test.source, test.args, value.Any())
	case want != nil && !errors.Is(err, want):
		t.Fatalf("%s on %v error = %v, want %v", test.source, test.args, err, want)
	case want == nil && !strings.Contains(err.Error(), test.want):
		t.Fatalf("%s on %v error = %v, want one containing %q", test.source, test.args, err, test.want)
	}
}

// Allocation never loses a cent: the remainder goes to the largest weights
// first, the earlier share on a tie (Dinero's examples).
func TestAllocateAddsBackUp(t *testing.T) {
	t.Parallel()
	for source, want := range map[string]string{
		"allocate(m, 3)":          "[{USD 334} {USD 333} {USD 333}]",
		"allocate(m, [70, 30])":   "[{USD 700} {USD 300}]",
		"allocate(n, [1, 3])":     "[{USD 1} {USD 4}]",
		"allocate(-m, [1, 1, 1])": "[{USD -334} {USD -333} {USD -333}]",
	} {
		value, err := evalMoney(t, source, "m: money; n: money", map[string]any{"m": "USD 10.00", "n": "USD 0.05"})
		if got := fmt.Sprint(value.Any()); err != nil || got != want {
			t.Fatalf("%s = %s, %v, want %s", source, got, err, want)
		}
	}
}

func TestRatiosConvertExactlyOrSayWhy(t *testing.T) {
	t.Parallel()
	for source, want := range map[string]string{
		`ratio("0.03")`:               "0.03",
		`ratio("1") / ratio("3")`:     "1/3",
		`ratio("2.5") * ratio("2")`:   "5",
		`ratio("-0.125")`:             "-0.125",
		`ratio("2") > ratio("1.999")`: "true",
	} {
		value, err := evalMoney(t, source, "", nil)
		if got := fmt.Sprint(value.Any()); err != nil || got != want {
			t.Fatalf("%s = %s, %v, want %s", source, got, err, want)
		}
	}
}

// The kernel checks the currencies it is handed, whatever inference proved:
// money whose currency the run decides meets its match or fails.
func TestTheKernelChecksCurrenciesItself(t *testing.T) {
	t.Parallel()
	_, err := evalMoney(t, "money(1, cur) + m", "cur: currency; m: money", map[string]any{"cur": "EUR", "m": "USD 1.00"})
	if !errors.Is(err, machine.ErrCurrency) {
		t.Fatalf("euros plus dollars: error = %v, want ErrCurrency", err)
	}
	_, err = evalMoney(t, "m * 2", "m: money", map[string]any{"m": map[string]any{"currency": "USD", "minor": int64(1 << 62)}})
	if err == nil || !strings.Contains(err.Error(), "overflows") {
		t.Fatalf("an amount past int64: error = %v, want an overflow", err)
	}
}

// A ratio serves money: it meets another ratio or an amount, never an int or a
// float, and a whole number is written as a ratio (100%, not 1).
func TestRatiosDoNotMixWithOtherNumbers(t *testing.T) {
	t.Parallel()
	registry := moneyRegistry(t)
	for _, source := range []string{"1 - r", "r + 1", "r * 2", "2 * r", "r / 2", "2 / r", "r < 1", "n == r", "ratio(x)", "ratio(n)", "float(r)"} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			_, err := compileMoney(t, registry, source, "r: ratio; n: int; x: float", "")
			if !errors.Is(err, machine.ErrCompile) {
				t.Fatalf("CompileExpr(%q) error = %v, want ErrCompile", source, err)
			}
		})
	}
}

func TestMoneyAddsAndSubtractsInOneCurrency(t *testing.T) {
	t.Parallel()
	usd, zero := machine.MoneyValue(100, "USD"), machine.MoneyValue(0, "")
	two := "a: money; b: money"
	for name, test := range map[string]moneyCase{
		"dollars plus dollars":         {"a + b", "a: money; b: money", []machine.Value{usd, machine.MoneyValue(250, "USD")}, "{USD 350}"},
		"dollars minus dollars":        {"a - b", "a: money; b: money", []machine.Value{usd, machine.MoneyValue(250, "USD")}, "{USD -150}"},
		"a zero on the right":          {"a + b", two, []machine.Value{usd, zero}, "{USD 100}"},
		"a zero on the left":           {"a + b", two, []machine.Value{zero, usd}, "{USD 100}"},
		"money minus a zero":           {"a - b", two, []machine.Value{usd, zero}, "{USD 100}"},
		"a zero minus money":           {"a - b", two, []machine.Value{zero, usd}, "{USD -100}"},
		"two zeros stay currency-less": {"a + b", two, []machine.Value{zero, zero}, "{ 0}"},
		"the literal zero":             {"a + 0", "a: money", []machine.Value{machine.MoneyValue(7, "JPY")}, "{JPY 7}"},
		"negation":                     {"-a", "a: money", []machine.Value{machine.MoneyValue(1234, "KWD")}, "{KWD -1234}"},
		"ratio plus ratio":             {"r + s", "r: ratio; s: ratio", []machine.Value{ratioValue(t, "0.5"), ratioValue(t, "0.25")}, "0.75"},
		"ratio minus ratio":            {"r - s", "r: ratio; s: ratio", []machine.Value{ratioValue(t, "0.5"), ratioValue(t, "0.75")}, "-0.25"},
		"a literal ratio with a ratio": {"100% - r", "r: ratio", []machine.Value{ratioValue(t, "0.2")}, "0.8"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			expectValue(t, test)
		})
	}
}

// Whatever inference proved, the kernel's addition looks at the currencies
// it is handed: two currencies never add.
func TestTheAdditiveKernelRefusesTwoCurrencies(t *testing.T) {
	t.Parallel()
	usd, eur := machine.MoneyValue(100, "USD"), machine.MoneyValue(100, "EUR")
	for name, test := range map[string]moneyCase{
		"dollars plus euros":           {"a + b", "a: money; b: money", []machine.Value{usd, eur}, ""},
		"dollars minus euros":          {"a - b", "a: money; b: money", []machine.Value{usd, eur}, ""},
		"a currency the run decides":   {"money(1, cur) + a", "cur: currency; a: money", []machine.Value{machine.CurrencyValue("EUR"), usd}, ""},
		"a zero in another's currency": {"money(0, currency(a)) + b", "a: money; b: money", []machine.Value{usd, eur}, ""},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			expectError(t, test, machine.ErrCurrency)
		})
	}
}

func TestAdditionRefusesToWrap(t *testing.T) {
	t.Parallel()
	most, least := machine.MoneyValue(math.MaxInt64, "USD"), machine.MoneyValue(math.MinInt64, "USD")
	for name, test := range map[string]moneyCase{
		"the largest amount plus a cent":    {"a + b", "a: money; b: money", []machine.Value{most, machine.MoneyValue(1, "USD")}, "overflows"},
		"the smallest amount minus a cent":  {"a - b", "a: money; b: money", []machine.Value{least, machine.MoneyValue(1, "USD")}, "overflows"},
		"nothing minus the smallest amount": {"-a", "a: money", []machine.Value{least}, "overflows"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			expectError(t, test, nil)
		})
	}
}

// orderedPair is two values of one comparable kind and how the first orders
// against the second.
type orderedPair struct {
	contract    string
	left, right machine.Value
	order       int
}

// Every comparison is registered for money, ratios and exchange rates; each
// is run below on a smaller, an equal and a larger left operand.
func TestEveryComparisonOrdersEveryPairOfKinds(t *testing.T) {
	t.Parallel()
	accept := map[string]func(int) bool{
		"<": func(order int) bool { return order < 0 }, "<=": func(order int) bool { return order <= 0 },
		">": func(order int) bool { return order > 0 }, ">=": func(order int) bool { return order >= 0 },
	}
	for name, pair := range orderedPairs(t) {
		for operator, holds := range accept {
			t.Run(name+" "+operator, func(t *testing.T) {
				t.Parallel()
				want := fmt.Sprint(holds(pair.order))
				expectValue(t, moneyCase{"a " + operator + " b", pair.contract, []machine.Value{pair.left, pair.right}, want})
			})
		}
	}
}

func orderedPairs(t *testing.T) map[string]orderedPair {
	t.Helper()
	money, rates := "a: money; b: money", "a: ratio; b: ratio"
	usd := func(minor int64) machine.Value { return machine.MoneyValue(minor, "USD") }
	pairs := map[string]orderedPair{
		"money less":           {money, usd(1), usd(2), -1},
		"money equal":          {money, usd(2), usd(2), 0},
		"money more":           {money, usd(3), usd(2), 1},
		"a zero below money":   {"a: money; b: money", machine.MoneyValue(0, ""), usd(1), -1},
		"money below a zero":   {"a: money; b: money", usd(-1), machine.MoneyValue(0, ""), -1},
		"a zero equal to zero": {"a: money; b: money", machine.MoneyValue(0, ""), usd(0), 0},
		"ratio less":           {rates, ratioValue(t, "0.25"), ratioValue(t, "0.5"), -1},
		"ratio equal":          {rates, ratioValue(t, "0.5"), ratioValue(t, "0.5"), 0},
		"ratio more":           {rates, ratioValue(t, "0.75"), ratioValue(t, "0.5"), 1},
	}
	return pairs
}

func TestComparisonsRefuseTwoCurrencies(t *testing.T) {
	t.Parallel()
	for _, operator := range []string{"<", "<=", ">", ">="} {
		for name, test := range map[string]moneyCase{
			"money": {"a " + operator + " b", "a: money; b: money", []machine.Value{machine.MoneyValue(1, "USD"), machine.MoneyValue(1, "EUR")}, ""},
		} {
			t.Run(name+" "+operator, func(t *testing.T) {
				t.Parallel()
				expectError(t, test, machine.ErrCurrency)
			})
		}
	}
}

// Ratio arithmetic works on the stack: no operation between two ratios
// allocates.
func TestRatioArithmeticDoesNotAllocate(t *testing.T) {
	registry := moneyRegistry(t)
	for _, source := range []string{"r * s", "r + s", "r - s", "r / s", "r < s", "100% - r"} {
		artifact, err := compileMoney(t, registry, source, "r: ratio; s: ratio", "")
		if err != nil {
			t.Fatal(err)
		}
		runtime, err := machine.Instantiate(artifact, registry)
		if err != nil {
			t.Fatal(err)
		}
		args := []machine.Value{machine.RatioValue(machine.NewRatio(1, 2)), machine.RatioValue(machine.NewRatio(1, 5))}[:len(artifact.Args())]
		ctx := t.Context()
		allocs := testing.AllocsPerRun(100, func() {
			if _, err := runtime.RunValues(ctx, args, machine.RunOptions{}); err != nil {
				t.Fatal(err)
			}
		})
		if allocs != 0 {
			t.Fatalf("%s allocated %v times, want 0", source, allocs)
		}
	}
}

// BenchmarkMoneyFee is a fee rule in one currency variable, through the
// boundary's currency binding and the fixed-point kernel.
func BenchmarkMoneyFee(b *testing.B) {
	registry := moneyRegistry(b)
	artifact, err := compile.CompileExpr("let(fee = round(amount * 0.029, @half_even) + money(30, currency(amount)), if(fee < cap, fee, cap))", registry, compile.CompileOptions{
		Args: []compile.ArgSpec{{Name: "amount", Type: machine.MoneyType}, {Name: "cap", Type: machine.MoneyType}},
	})
	if err != nil {
		b.Fatal(err)
	}
	runtime, err := machine.Instantiate(artifact, registry)
	if err != nil {
		b.Fatal(err)
	}
	args := []machine.Value{machine.MoneyValue(10_000, "EUR"), machine.MoneyValue(500, "EUR")}
	ctx := b.Context()
	for b.Loop() {
		if _, err := runtime.RunValues(ctx, args, machine.RunOptions{}); err != nil {
			b.Fatal(err)
		}
	}
}
