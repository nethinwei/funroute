package machine_test

import (
	"errors"
	"fmt"
	"maps"
	"math"
	"testing"

	"funroute/lang/internal/machine"
)

// roundedStep is one operation that lands between two minor units, with the
// arguments that make it land on +2.5, -2.5 and +3.5 of them: enough to tell
// all seven rounding modes apart.
type roundedStep struct {
	source, contract string
	halves           [3][]machine.Value
	currency         string
}

// stepRates is the one rate the rounded steps convert through: USD 100.00
// is JPY 2.5.
func stepRates(t *testing.T) *machine.Rates {
	t.Helper()
	return moneyRates(t, moneyRegistry(t), "USD", "JPY", "0.025")
}

func roundedSteps(t *testing.T) map[string]roundedStep {
	t.Helper()
	usd := func(minor int64) machine.Value { return machine.MoneyValue(minor, "USD") }
	half, three := rateValue(t, "0.5"), rateValue(t, "2")
	return map[string]roundedStep{
		"money times a rate": {"a * r", "a: money<USD>; r: rate",
			[3][]machine.Value{{usd(5), half}, {usd(-5), half}, {usd(7), half}}, "USD"},
		"a rate times money": {"r * a", "a: money<USD>; r: rate",
			[3][]machine.Value{{usd(5), half}, {usd(-5), half}, {usd(7), half}}, "USD"},
		"money over a rate": {"a / r", "a: money<USD>; r: rate",
			[3][]machine.Value{{usd(5), three}, {usd(-5), three}, {usd(7), three}}, "USD"},
		"a conversion": {"a -> JPY", "a: money<USD>",
			[3][]machine.Value{{usd(10_000)}, {usd(-10_000)}, {usd(14_000)}}, "JPY"},
	}
}

// roundingOutcomes is what each mode makes of +2.5, -2.5 and +3.5.
var roundingOutcomes = map[string][3]int64{
	"half_up":   {3, -3, 4},
	"half_down": {2, -2, 3},
	"half_even": {2, -2, 4},
	"up":        {3, -3, 4},
	"down":      {2, -2, 3},
	"ceiling":   {3, -2, 4},
	"floor":     {2, -3, 3},
}

// round(expr, @mode) selects the variant with the mode spelled out for every
// rounding step, and without it the registry's half_even applies.
func TestEveryRoundingStepTakesEveryMode(t *testing.T) {
	t.Parallel()
	modes := map[string][3]int64{"": roundingOutcomes["half_even"]}
	maps.Copy(modes, roundingOutcomes)
	for name, step := range roundedSteps(t) {
		for mode, outcomes := range modes {
			t.Run(name+" "+mode, func(t *testing.T) {
				t.Parallel()
				expectRounded(t, step, mode, outcomes)
			})
		}
	}
}

func expectRounded(t *testing.T, step roundedStep, mode string, outcomes [3]int64) {
	t.Helper()
	source := step.source
	if mode != "" {
		source = "round(" + step.source + ", @" + mode + ")"
	}
	runtime := moneyRuntime(t, source, step.contract, "")
	options := machine.RunOptions{Rates: stepRates(t)}
	for i, args := range step.halves {
		value, err := runtime.RunValues(t.Context(), args, options)
		money, _ := value.Money()
		if want := (machine.NewMoney(step.currency, outcomes[i])); err != nil || money != want {
			t.Fatalf("%s on %v = %v, %v, want %v", source, args, money, err, want)
		}
	}
}

// Every operation that can fall between two minor units has a variant for
// round(…) to select; the exact ones have none.
func TestTheRoundingVariantsAreTheInexactSteps(t *testing.T) {
	t.Parallel()
	registry := moneyRegistry(t)
	want := map[string]bool{
		"mul(money<u>,rate)->money<u>": true, "mul(rate,money<u>)->money<u>": true,
		"div(money<u>,rate)->money<u>": true, "convert(money<a>,currency<b>)->money<b>": true,
		"mul(money<u>,int)->money<u>": false, "mul(int,money<u>)->money<u>": false,
		"div(money<u>,money<u>)->rate": false, "mul(rate,rate)->rate": false,
	}
	for key, rounds := range want {
		t.Run(key, func(t *testing.T) {
			t.Parallel()
			function, ok := registry.Resolve(key)
			if !ok {
				t.Fatalf("Resolve(%q) found nothing", key)
			}
			if _, has := machine.RoundingVariant(registry, function); has != rounds {
				t.Fatalf("RoundingVariant(%s) found = %v, want %v", key, has, rounds)
			}
		})
	}
}

// A round(…) inside another governs its own steps; the outer one governs the
// rest.
func TestTheInnermostRoundingScopeWins(t *testing.T) {
	t.Parallel()
	contract := "a: money<USD>; r: rate"
	args := []machine.Value{machine.MoneyValue(5, "USD"), rateValue(t, "0.5")}
	for name, test := range map[string]moneyCase{
		"two steps in one scope":        {"round(a * r + a * r, @up)", contract, args, "{USD 6}"},
		"an inner scope and an outer":   {"round(round(a * r, @up) + a * r, @down)", contract, args, "{USD 5}"},
		"a scope around a scoped step":  {"round(round(a * r, @up) * r, @down)", contract, args, "{USD 1}"},
		"the default outside any scope": {"round(a * r, @up) + a * r", contract, args, "{USD 5}"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			expectValue(t, test)
		})
	}
}

func TestProductsOfMoneyAndPlainNumbers(t *testing.T) {
	t.Parallel()
	usd := func(minor int64) machine.Value { return machine.MoneyValue(minor, "USD") }
	for name, test := range map[string]moneyCase{
		"money times a count":              {"a * n", "a: money<USD>; n: int", []machine.Value{usd(5), machine.Int(-2)}, "{USD -10}"},
		"a count times money":              {"n * a", "a: money<USD>; n: int", []machine.Value{usd(5), machine.Int(3)}, "{USD 15}"},
		"a zero times a count":             {"a * 3", "a: money<?>", []machine.Value{machine.MoneyValue(0, "")}, "{ 0}"},
		"a fee on a fee":                   {"r * s", "r: rate; s: rate", []machine.Value{rateValue(t, "0.5"), rateValue(t, "0.3")}, "0.15"},
		"a rate product rounds evenly":     {"r * s", "r: rate; s: rate", []machine.Value{rateValue(t, "0.00001"), rateValue(t, "0.000005")}, "0"},
		"a rate product rounds up to even": {"r * s", "r: rate; s: rate", []machine.Value{rateValue(t, "0.00003"), rateValue(t, "0.000005")}, "0.0000000002"},
		"a rate over a rate":               {"r / s", "r: rate; s: rate", []machine.Value{rateValue(t, "1"), rateValue(t, "3")}, "0.3333333333"},
		"the rate a fee is":                {"a / b", "a: money<USD>; b: money<USD>", []machine.Value{usd(100), usd(300)}, "0.3333333333"},
		"the rate of a zero":               {"a / b", "a: money<?>; b: money<?>", []machine.Value{machine.MoneyValue(0, ""), usd(300)}, "0"},
		"a gross from a net":               {"a / (100% - r)", "a: money<USD>; r: rate", []machine.Value{usd(100), rateValue(t, "0.03")}, "{USD 103}"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			expectValue(t, test)
		})
	}
}

func TestProductsRefuseZeroDivisorsAndOverflow(t *testing.T) {
	t.Parallel()
	most := machine.MoneyValue(math.MaxInt64, "USD")
	maxRate := machine.RateValue(machine.NewRate(math.MaxInt64))
	for name, test := range map[string]moneyCase{
		"money over a zero rate":    {"a / r", "a: money<USD>; r: rate", []machine.Value{most, rateValue(t, "0")}, "division by zero"},
		"money over no money":       {"a / b", "a: money<USD>; b: money<USD>", []machine.Value{most, machine.MoneyValue(0, "USD")}, "division by zero"},
		"a rate over zero":          {"r / s", "r: rate; s: rate", []machine.Value{maxRate, rateValue(t, "0")}, "division by zero"},
		"money times two":           {"a * 2", "a: money<USD>", []machine.Value{most}, "overflows"},
		"money times a rate":        {"a * r", "a: money<USD>; r: rate", []machine.Value{most, rateValue(t, "2")}, "overflows"},
		"money over half":           {"a / r", "a: money<USD>; r: rate", []machine.Value{most, rateValue(t, "0.5")}, "overflows"},
		"a fee on the largest rate": {"r * s", "r: rate; s: rate", []machine.Value{maxRate, rateValue(t, "2")}, "overflows"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			expectError(t, test, nil)
		})
	}
}

// A ratio of two amounts is a rate only in one currency: amounts in two are
// not divided, whatever the contract left open.
func TestARatioNeedsOneCurrency(t *testing.T) {
	t.Parallel()
	test := moneyCase{"a / b", "a: money<?>; b: money<?>", []machine.Value{machine.MoneyValue(1, "EUR"), machine.MoneyValue(1, "USD")}, ""}
	expectError(t, test, machine.ErrCurrency)
}

// Amounts in two currencies divide into the exchange rate they imply,
// exactly, in major units; a declared fxrate result reads money / money as
// one even where the currencies are only known at run time.
func TestAmountsDivideIntoAnExchangeRate(t *testing.T) {
	t.Parallel()
	usd := func(minor int64) machine.Value { return machine.MoneyValue(minor, "USD") }
	for name, test := range map[string]struct {
		p, q machine.Value
		want string
		err  error
	}{
		"yen for dollars":       {p: machine.MoneyValue(15_000, "JPY"), q: usd(10_000), want: "USD/JPY 150"},
		"dinars for dollars":    {p: machine.MoneyValue(307, "KWD"), q: usd(100), want: "USD/KWD 0.307"},
		"a rate with no end":    {p: machine.MoneyValue(100, "EUR"), q: usd(300), want: "USD/EUR 1/3"},
		"two refunds":           {p: machine.MoneyValue(-15_025, "JPY"), q: usd(-10_000), want: "USD/JPY 150.25"},
		"one currency, equal":   {p: usd(250), q: usd(250), want: "USD/USD 1"},
		"one currency, unequal": {p: usd(251), q: usd(250), err: machine.ErrArithmetic},
		"a zero over":           {p: machine.MoneyValue(0, "JPY"), q: usd(100), err: machine.ErrArithmetic},
		"a zero under":          {p: machine.MoneyValue(150, "JPY"), q: usd(0), err: machine.ErrArithmetic},
		"opposite signs":        {p: machine.MoneyValue(-150, "JPY"), q: usd(100), err: machine.ErrArithmetic},
		"no currency":           {p: machine.MoneyValue(0, ""), q: usd(100), err: machine.ErrCurrency},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			value, err := moneyRuntime(t, "p / q", "p: money<?>; q: money<?>", "fxrate<?,?>").RunValues(t.Context(), []machine.Value{test.p, test.q}, machine.RunOptions{})
			got := fmt.Sprint(value.Any())
			if (test.err != nil && !errors.Is(err, test.err)) || (test.err == nil && (err != nil || got != test.want)) {
				t.Fatalf("p / q on %v, %v = %s, %v, want %s, %v", test.p.Any(), test.q.Any(), got, err, test.want, test.err)
			}
		})
	}
}

// Known different currencies can only divide into an exchange rate; known
// one currency, or unknown ones with no rate asked for, is a ratio.
func TestWhatMoneyOverMoneyReadsAs(t *testing.T) {
	t.Parallel()
	for source, want := range map[string]string{
		"y / a":         "fxrate<USD,JPY>",
		"a / y":         "fxrate<JPY,USD>",
		"a / b":         "rate",
		"p / q":         "rate",
		"[y / a, fx]":   "array<fxrate<USD,JPY>>",
		"y / a == fx":   "bool",
		"JPY 1 / USD 1": "fxrate<USD,JPY>",
	} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			artifact, err := compileMoney(t, moneyRegistry(t), source, "a: money<USD>; b: money<USD>; y: money<JPY>; p: money<?>; q: money<?>; fx: fxrate<USD,JPY>", "")
			if err != nil || artifact.Result().String() != want {
				t.Fatalf("%s = %v, %v, want %s", source, artifact.Result(), err, want)
			}
		})
	}
}

// A markup on an exchange rate is the same pair, exactly, whichever side the
// rate is written on; converting at it rounds once, where converting and
// then marking the amount up rounds twice. A markup that takes a rate to
// zero or below is ErrArithmetic.
func TestAMarkupOnAnExchangeRateRoundsOnce(t *testing.T) {
	t.Parallel()
	fx := map[string]any{"base": "USD", "quote": "JPY", "rate": "150.265025"}
	const contract = "fx: fxrate<USD,JPY>; a: money<USD>"
	args := map[string]any{"fx": fx, "a": "USD 100.00"}
	for source, want := range map[string]string{
		"fx * 101%":                   "USD/JPY 151.76767525",
		"99.5% * fx":                  "USD/JPY 149.513699875",
		"using(fx * 99.5%, a -> JPY)": "{JPY 14951}",
		"using(fx, a -> JPY) * 99.5%": "{JPY 14952}",
	} {
		value, err := evalMoney(t, source, contract, args)
		if got := describeValue(value); err != nil || got != want {
			t.Errorf("%s = %s, %v, want %s", source, got, err, want)
		}
	}
	if _, err := evalMoney(t, "fx * (0% - 1%)", contract, args); !errors.Is(err, machine.ErrArithmetic) {
		t.Fatalf("a markup to below zero: error = %v, want ErrArithmetic", err)
	}
}

// Two exchange rates of one pair compare by what they buy; two of different
// pairs have no order, a compile error where both pairs are known.
func TestExchangeRatesOfOnePairCompare(t *testing.T) {
	t.Parallel()
	const contract = "bid: fxrate<USD,JPY>; ask: fxrate<USD,JPY>; eur: fxrate<EUR,USD>"
	args := map[string]any{
		"bid": map[string]any{"base": "USD", "quote": "JPY", "rate": "149.5"},
		"ask": map[string]any{"base": "USD", "quote": "JPY", "rate": "150.5"},
		"eur": map[string]any{"base": "EUR", "quote": "USD", "rate": "1.08"},
	}
	for source, want := range map[string]string{"bid < ask": "true", "ask <= bid": "false", "if(bid > ask, bid, ask) == ask": "true"} {
		value, err := evalMoney(t, source, contract, args)
		if got := describeValue(value); err != nil || got != want {
			t.Errorf("%s = %s, %v, want %s", source, got, err, want)
		}
	}
	if _, err := compileMoney(t, moneyRegistry(t), "bid < eur", contract, ""); err == nil {
		t.Fatal("bid < eur compiled, want two pairs refused")
	}
}

// describeValue is a value as the tests above compare it: an exchange rate
// in its market notation, anything else as Any prints it.
func describeValue(value machine.Value) string {
	if fx, ok := value.FxRate(); ok {
		return fx.String()
	}
	return fmt.Sprint(value.Any())
}
