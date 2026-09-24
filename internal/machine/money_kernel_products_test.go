package machine_test

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/nethinwei/funroute/internal/machine"
)

// roundedStep is one operation that lands between two minor units, with the
// arguments that make it land on +2.5, -2.5 and +3.5 of them: enough to tell
// all seven rounding modes apart.
type roundedStep struct {
	source, contract string
	halves           [3][]machine.Value
	currency         string
}

func roundedSteps(t *testing.T) map[string]roundedStep {
	t.Helper()
	usd := func(minor int64) machine.Value { return machine.MoneyValue(minor, "USD") }
	half, three := ratioValue(t, "0.5"), ratioValue(t, "2")
	return map[string]roundedStep{
		"money times a ratio": {"a * r", "a: money; r: ratio",
			[3][]machine.Value{{usd(5), half}, {usd(-5), half}, {usd(7), half}}, "USD"},
		"a ratio times money": {"r * a", "a: money; r: ratio",
			[3][]machine.Value{{usd(5), half}, {usd(-5), half}, {usd(7), half}}, "USD"},
		"money over a ratio": {"a / r", "a: money; r: ratio",
			[3][]machine.Value{{usd(5), three}, {usd(-5), three}, {usd(7), three}}, "USD"},
		"a conversion": {"a -> JPY", "a: money",
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

// Every step that lands between minor units rounds by every mode, two ways:
// inside round(step, @mode), and as the step's variant with the mode as its
// last argument, which is what the operator is.
func TestEveryRoundingStepTakesEveryMode(t *testing.T) {
	t.Parallel()
	for name, step := range roundedSteps(t) {
		for mode, outcomes := range roundingOutcomes {
			t.Run(name+" "+mode, func(t *testing.T) {
				t.Parallel()
				expectRounded(t, step, mode, outcomes)
			})
		}
	}
}

func expectRounded(t *testing.T, step roundedStep, mode string, outcomes [3]int64) {
	t.Helper()
	for _, source := range []string{"round(" + step.source + ", @" + mode + ")", explicitMode(step.source, mode)} {
		expectRoundedBy(t, step, source, outcomes)
	}
}

// explicitMode is a step written as its variant with the mode: a * r is
// mul(a, r, @mode), a -> JPY convert(a, JPY, @mode).
func explicitMode(source, mode string) string {
	for operator, name := range map[string]string{" * ": "mul", " / ": "div", " -> ": "convert"} {
		if left, right, ok := strings.Cut(source, operator); ok {
			return name + "(" + left + ", " + right + ", @" + mode + ")"
		}
	}
	return source
}

func expectRoundedBy(t *testing.T, step roundedStep, source string, outcomes [3]int64) {
	t.Helper()
	if step.currency == "JPY" {
		source = "using(0.025 JPY / USD, " + source + ")"
	}
	runtime := moneyRuntime(t, source, step.contract, "")
	options := machine.RunOptions{}
	for i, args := range step.halves {
		value, err := runtime.RunValues(t.Context(), args, options)
		money, _ := value.Money()
		if want := (machine.NewMoney(step.currency, outcomes[i])); err != nil || money != want {
			t.Fatalf("%s on %v = %v, %v, want %v", source, args, money, err, want)
		}
	}
}

// Every operation that can fall between two minor units is an exact step,
// with a variant that takes its mode; the operations that stay on whole
// minor units are neither.
func TestTheRoundingVariantsAreTheInexactSteps(t *testing.T) {
	t.Parallel()
	registry := moneyRegistry(t)
	want := map[string]bool{
		"mul(money,ratio)->money": true, "mul(ratio,money)->money": true,
		"div(money,ratio)->money": true, "convert(money,currency)->money": true,
		"mul(money,int)->money": false, "mul(int,money)->money": false,
		"div(money,money)->ratio": false, "mul(ratio,ratio)->ratio": false,
	}
	for key, rounds := range want {
		t.Run(key, func(t *testing.T) {
			t.Parallel()
			function, ok := registry.Resolve(key)
			if !ok {
				t.Fatalf("Resolve(%q) found nothing", key)
			}
			variant := strings.Replace(key, ")->", ",enum<rounding>{ceiling,down,floor,half_down,half_even,half_up,up})->", 1)
			if _, has := registry.Resolve(variant); function.IsExactStep() != rounds || has != rounds {
				t.Fatalf("%s: exact step %v, variant %v, want %v", key, function.IsExactStep(), has, rounds)
			}
		})
	}
}

// Inside a round the steps are exact and the round rounds once: two halves
// of a cent are a cent. A round inside another rounds its own steps; the
// outer one rounds what is left.
func TestTheInnermostRoundingScopeWins(t *testing.T) {
	t.Parallel()
	contract := "a: money; r: ratio"
	args := []machine.Value{machine.MoneyValue(5, "USD"), ratioValue(t, "0.5")}
	for name, test := range map[string]moneyCase{
		"two steps in one scope":        {"round(a * r + a * r, @up)", contract, args, "{USD 5}"},
		"an inner scope and an outer":   {"round(round(a * r, @up) + a * r, @down)", contract, args, "{USD 5}"},
		"a scope around a scoped step":  {"round(round(a * r, @up) * r, @down)", contract, args, "{USD 1}"},
		"the default outside any scope": {"round(a * r, @up) + round(a * r, @half_even)", contract, args, "{USD 5}"},
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
		"money times a count":          {"a * n", "a: money; n: int", []machine.Value{usd(5), machine.Int(-2)}, "{USD -10}"},
		"a count times money":          {"n * a", "a: money; n: int", []machine.Value{usd(5), machine.Int(3)}, "{USD 15}"},
		"a zero times a count":         {"a * 3", "a: money", []machine.Value{machine.MoneyValue(0, "")}, "{ 0}"},
		"a fee on a fee":               {"r * s", "r: ratio; s: ratio", []machine.Value{ratioValue(t, "0.5"), ratioValue(t, "0.3")}, "0.15"},
		"a ratio product is exact":     {"r * s", "r: ratio; s: ratio", []machine.Value{ratioValue(t, "0.00001"), ratioValue(t, "0.000005")}, "0.00000000005"},
		"a finer product is exact too": {"r * s", "r: ratio; s: ratio", []machine.Value{ratioValue(t, "0.00003"), ratioValue(t, "0.000005")}, "0.00000000015"},
		"a ratio over a ratio":         {"r / s", "r: ratio; s: ratio", []machine.Value{ratioValue(t, "1"), ratioValue(t, "3")}, "1/3"},
		"the ratio a fee is":           {"a / b", "a: money; b: money", []machine.Value{usd(100), usd(300)}, "1/3"},
		"the ratio of a zero":          {"a / b", "a: money; b: money", []machine.Value{machine.MoneyValue(0, ""), usd(300)}, "0"},
		"a gross from a net":           {"round(a / (100% - r), @half_even)", "a: money; r: ratio", []machine.Value{usd(100), ratioValue(t, "0.03")}, "{USD 103}"},
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
	maxRatio := machine.RatioValue(machine.NewRatio(math.MaxInt64, 10_000_000_000))
	for name, test := range map[string]moneyCase{
		"money over a zero ratio": {"round(a / r, @half_even)", "a: money; r: ratio", []machine.Value{most, ratioValue(t, "0")}, "division by zero"},
		"money over no money":     {"a / b", "a: money; b: money", []machine.Value{most, machine.MoneyValue(0, "USD")}, "division by zero"},
		"a ratio over zero":       {"r / s", "r: ratio; s: ratio", []machine.Value{maxRatio, ratioValue(t, "0")}, "division by zero"},
		"money times two":         {"a * 2", "a: money", []machine.Value{most}, "overflows"},
		"money times a ratio":     {"round(a * r, @half_even)", "a: money; r: ratio", []machine.Value{most, ratioValue(t, "2")}, "overflows"},
		"money over half":         {"round(a / r, @half_even)", "a: money; r: ratio", []machine.Value{most, ratioValue(t, "0.5")}, "overflows"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			expectError(t, test, nil)
		})
	}
}

// Two amounts divide into a ratio only in one currency: amounts in two are
// not divided, whatever the contract left open.
func TestARatioNeedsOneCurrency(t *testing.T) {
	t.Parallel()
	test := moneyCase{"a / b", "a: money; b: money", []machine.Value{machine.MoneyValue(1, "EUR"), machine.MoneyValue(1, "USD")}, ""}
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
		"yen for dollars":       {p: machine.MoneyValue(15_000, "JPY"), q: usd(10_000), want: "150 JPY / USD"},
		"dinars for dollars":    {p: machine.MoneyValue(307, "KWD"), q: usd(100), want: "0.307 KWD / USD"},
		"a rate with no end":    {p: machine.MoneyValue(100, "EUR"), q: usd(300), want: "1/3 EUR / USD"},
		"two refunds":           {p: machine.MoneyValue(-15_025, "JPY"), q: usd(-10_000), want: "150.25 JPY / USD"},
		"one currency, equal":   {p: usd(250), q: usd(250), err: machine.ErrCurrency},
		"one currency, unequal": {p: usd(251), q: usd(250), err: machine.ErrCurrency},
		"a zero over":           {p: machine.MoneyValue(0, "JPY"), q: usd(100), err: machine.ErrArithmetic},
		"a zero under":          {p: machine.MoneyValue(150, "JPY"), q: usd(0), err: machine.ErrArithmetic},
		"opposite signs":        {p: machine.MoneyValue(-150, "JPY"), q: usd(100), err: machine.ErrArithmetic},
		"no currency":           {p: machine.MoneyValue(0, ""), q: usd(100), err: machine.ErrCurrency},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			value, err := moneyRuntime(t, "implied(p, q)", "p: money; q: money", "fxrate").RunValues(t.Context(), []machine.Value{test.p, test.q}, machine.RunOptions{})
			got := fmt.Sprint(value.Any())
			if (test.err != nil && !errors.Is(err, test.err)) || (test.err == nil && (err != nil || got != test.want)) {
				t.Fatalf("implied(p, q) on %v, %v = %s, %v, want %s, %v", test.p.Any(), test.q.Any(), got, err, test.want, test.err)
			}
		})
	}
}

// Two amounts divide into a ratio, a / b, and imply an exchange rate,
// implied(a, b): two operations, so a reader never has to ask which one a
// division is.
func TestWhatMoneyOverMoneyReadsAs(t *testing.T) {
	t.Parallel()
	for source, want := range map[string]string{
		"a / b":                 "ratio",
		"implied(y, a)":         "fxrate",
		"[implied(y, a), fx]":   "array<fxrate>",
		"implied(y, a) == fx":   "bool",
		"implied(JPY 1, USD 1)": "fxrate",
	} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			artifact, err := compileMoney(t, moneyRegistry(t), source, "a: money; b: money; y: money; p: money; q: money; fx: fxrate", "")
			if err != nil || artifact.Result().String() != want {
				t.Fatalf("%s = %v, %v, want %s", source, artifact.Result(), err, want)
			}
		})
	}
}

// A markup on an exchange rate is the same pair, exactly, whichever side the
// rate is written on; converting at it rounds once, where converting and
// then marking the amount up rounds twice — unless both are one round's
// steps, which is exact until the end. A markup that takes a rate to
// zero or below is ErrArithmetic.
func TestAMarkupOnAnExchangeRateRoundsOnce(t *testing.T) {
	t.Parallel()
	fx := map[string]any{"base": "USD", "quote": "JPY", "rate": "150.265025"}
	const contract = "fx: fxrate; a: money"
	args := map[string]any{"fx": fx, "a": "USD 100.00"}
	for source, want := range map[string]string{
		"fx * 101%":  "151.76767525 JPY / USD",
		"99.5% * fx": "149.513699875 JPY / USD",
		"using(fx * 99.5%, round(a -> JPY, @half_even))":                    "{JPY 14951}",
		"round(using(fx, a -> JPY) * 99.5%, @half_even)":                    "{JPY 14951}",
		"round(round(using(fx, a -> JPY), @half_even) * 99.5%, @half_even)": "{JPY 14952}",
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
	const contract = "bid: fxrate; ask: fxrate; eur: fxrate"
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
	if _, err := evalMoney(t, "bid < eur", contract, args); !errors.Is(err, machine.ErrCurrency) {
		t.Fatalf("bid < eur error = %v, want ErrCurrency: two pairs do not compare", err)
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
