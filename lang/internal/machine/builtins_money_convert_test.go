package machine_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"funroute/lang/internal/machine"
)

// moneyRates is a rate table over registry's currencies holding one quote.
func moneyRates(t testing.TB, registry *machine.Registry, base, quote, rate string) *machine.Rates {
	t.Helper()
	table, ok := registry.Currencies()
	if !ok {
		t.Fatal("the registry declares no money")
	}
	rates := table.NewRates()
	if err := rates.Add(base, quote, rate); err != nil {
		t.Fatal(err)
	}
	return rates
}

// convertCase is a rule, its contract and arguments, the one quote its run
// converts through, and the result as fmt prints it.
type convertCase struct {
	source, contract string
	args             []machine.Value
	quote            [3]string
	want             string
}

// runConverting compiles test's rule and runs it with its quote as the rate
// table.
func runConverting(t *testing.T, test convertCase) (machine.Value, error) {
	t.Helper()
	registry := moneyRegistry(t)
	runtime := instantiated(t, registry, test.source, test.contract)
	options := machine.RunOptions{Rates: moneyRates(t, registry, test.quote[0], test.quote[1], test.quote[2])}
	return runtime.RunValues(t.Context(), test.args, options)
}

func instantiated(t *testing.T, registry *machine.Registry, source, contract string) *machine.Runtime {
	t.Helper()
	artifact, err := compileMoney(t, registry, source, contract, "")
	if err != nil {
		t.Fatalf("CompileExpr(%q) error = %v", source, err)
	}
	runtime, err := machine.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	return runtime
}

// A rule converts with amount -> JPY, through the rate table of its run,
// rescaling between the currencies' places and rounding once, half to even
// as the registry says, or as round(…) says.
func TestAConversionInARule(t *testing.T) {
	t.Parallel()
	usd, jpy, kwd := machine.MoneyValue(100, "USD"), machine.MoneyValue(1000, "JPY"), machine.MoneyValue(1000, "KWD")
	usdJPY := [3]string{"USD", "JPY", "150.5"}
	for name, test := range map[string]convertCase{
		"dollars into yen":        {"a -> JPY", "a: money<USD>", []machine.Value{usd}, usdJPY, "{JPY 150}"},
		"yen back into dollars":   {"a -> USD", "a: money<JPY>", []machine.Value{jpy}, usdJPY, "{USD 664}"},
		"dollars into dinars":     {"a -> KWD", "a: money<USD>", []machine.Value{usd}, [3]string{"USD", "KWD", "0.30755"}, "{KWD 308}"},
		"dinars into yen":         {"a -> JPY", "a: money<KWD>", []machine.Value{kwd}, [3]string{"KWD", "JPY", "487.5"}, "{JPY 488}"},
		"a rounding scope":        {"round(a -> JPY, @up)", "a: money<USD>", []machine.Value{usd}, [3]string{"USD", "JPY", "150.25"}, "{JPY 151}"},
		"rounded down":            {"round(a -> JPY, @down)", "a: money<USD>", []machine.Value{usd}, [3]string{"USD", "JPY", "150.75"}, "{JPY 150}"},
		"half to even by default": {"a -> JPY", "a: money<USD>", []machine.Value{usd}, [3]string{"USD", "JPY", "150.5"}, "{JPY 150}"},
		"a currency the run says": {"a -> c", "a: money<?>; c: currency<?>", []machine.Value{usd, machine.CurrencyValue("JPY")}, usdJPY, "{JPY 150}"},
		"a currency it is in":     {"a -> c", "a: money<?>; c: currency<?>", []machine.Value{usd, machine.CurrencyValue("USD")}, usdJPY, "{USD 100}"},
		"a currency-less zero":    {"a -> JPY", "a: money<?>", []machine.Value{machine.MoneyValue(0, "")}, usdJPY, "{JPY 0}"},
		"then arithmetic in yen":  {"(a -> JPY) + b", "a: money<USD>; b: money<JPY>", []machine.Value{usd, jpy}, usdJPY, "{JPY 1150}"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			value, err := runConverting(t, test)
			if got := fmt.Sprint(value.Any()); err != nil || got != test.want {
				t.Fatalf("%s on %v = %s, %v, want %s", test.source, test.args, got, err, test.want)
			}
		})
	}
}

// round(amount -> JPY, @mode) selects the variant that takes the mode; the
// bare conversion is the one without.
func TestRoundSelectsTheConversionVariant(t *testing.T) {
	t.Parallel()
	registry := moneyRegistry(t)
	for source, want := range map[string]string{
		"a -> JPY":               "convert(money<a>,currency<b>)->money<b>",
		"round(a -> JPY, @down)": "convert(money<a>,currency<b>,enum<rounding>{ceiling,down,floor,half_down,half_even,half_up,up})->money<b>",
	} {
		artifact, err := compileMoney(t, registry, source, "a: money<USD>", "")
		if err != nil {
			t.Fatal(err)
		}
		if calls := machine.PartsOf(artifact).Calls; len(calls) != 1 || calls[0].Signature != want {
			t.Fatalf("%s calls %+v, want only %s", source, calls, want)
		}
	}
}

// Without a rate table, or without a chain of rates, a conversion is
// ErrNoRate: data not at hand, which fallback takes.
func TestAConversionWithoutARateIsErrNoRate(t *testing.T) {
	t.Parallel()
	registry := moneyRegistry(t)
	usdJPY := moneyRates(t, registry, "USD", "JPY", "150")
	args := []machine.Value{machine.MoneyValue(100, "EUR")}
	for name, options := range map[string]machine.RunOptions{
		"no rate table": {},
		"no chain":      {Rates: usdJPY},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			value, err := instantiated(t, registry, "a -> JPY", "a: money<EUR>").RunValues(t.Context(), args, options)
			if !errors.Is(err, machine.ErrNoRate) {
				t.Fatalf("EUR 1.00 -> JPY = %v, %v, want ErrNoRate", value.Any(), err)
			}
			value, err = instantiated(t, registry, "fallback(a -> JPY, JPY 7)", "a: money<EUR>").RunValues(t.Context(), args, options)
			if got := fmt.Sprint(value.Any()); err != nil || got != "{JPY 7}" {
				t.Fatalf("fallback(EUR 1.00 -> JPY, JPY 7) = %s, %v, want {JPY 7}", got, err)
			}
		})
	}
}

// Money already in the currency, and the currency-less zero, need no rate:
// a run without a rate table still converts them.
func TestAConversionIntoItsOwnCurrencyNeedsNoRate(t *testing.T) {
	t.Parallel()
	registry := moneyRegistry(t)
	for source, want := range map[string]string{"a -> EUR": "{EUR 100}", "0 -> EUR": "{EUR 0}"} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			value, err := instantiated(t, registry, source, "a: money<EUR>").RunValues(t.Context(), []machine.Value{machine.MoneyValue(100, "EUR")}, machine.RunOptions{})
			if got := fmt.Sprint(value.Any()); err != nil || got != want {
				t.Fatalf("%s with no rate table = %s, %v, want %s", source, got, err, want)
			}
		})
	}
}

// A rate table belongs to one currency table: one made from another is the
// host's mistake, an ErrCurrency before anything runs.
func TestARateTableOfAnotherCurrencyTableIsRefused(t *testing.T) {
	t.Parallel()
	other := machine.CoreRegistry()
	err := other.DeclareMoney(machine.MoneySpec{Rounding: machine.RoundHalfEven, Currencies: []machine.CurrencySpec{
		{Code: "USD", Digits: 2}, {Code: "JPY", Digits: 0},
	}})
	if err != nil {
		t.Fatal(err)
	}
	registry := moneyRegistry(t)
	options := machine.RunOptions{Rates: moneyRates(t, other, "USD", "JPY", "150")}
	args := []machine.Value{machine.MoneyValue(100, "USD")}
	if value, err := instantiated(t, registry, "a -> JPY", "a: money<USD>").RunValues(t.Context(), args, options); !errors.Is(err, machine.ErrCurrency) {
		t.Fatalf("a run with another table's rates = %v, %v, want ErrCurrency", value.Any(), err)
	}
	same := machine.RunOptions{Rates: moneyRates(t, moneyRegistry(t), "USD", "JPY", "150")}
	if _, err := instantiated(t, registry, "a -> JPY", "a: money<USD>").RunValues(t.Context(), args, same); err != nil {
		t.Fatalf("a run with the rates of an equal table: %v", err)
	}
}

// A conversion reads the run, so folding never calls it: USD 1 -> JPY
// compiles without a rate table and converts only when it runs.
func TestAConversionIsNotFolded(t *testing.T) {
	t.Parallel()
	registry := moneyRegistry(t)
	artifact, err := compileMoney(t, registry, "USD 1 -> JPY", "", "")
	if err != nil {
		t.Fatalf("CompileExpr(USD 1 -> JPY) error = %v, want it compiled", err)
	}
	if calls := machine.PartsOf(artifact).Calls; len(calls) != 1 || calls[0].Name != "convert" {
		t.Fatalf("USD 1 -> JPY calls %+v, want convert, not a folded constant", calls)
	}
	runtime, err := machine.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	options := machine.RunOptions{Rates: moneyRates(t, registry, "USD", "JPY", "150")}
	if value, err := runtime.RunValues(t.Context(), nil, options); err != nil || fmt.Sprint(value.Any()) != "{JPY 150}" {
		t.Fatalf("USD 1 -> JPY = %v, %v, want JPY 150", value.Any(), err)
	}
	if value, err := runtime.RunValues(t.Context(), nil, machine.RunOptions{}); !errors.Is(err, machine.ErrNoRate) {
		t.Fatalf("USD 1 -> JPY without rates = %v, %v, want ErrNoRate", value.Any(), err)
	}
}

// A run converts through the version of the table it started with: a rate
// added while it runs is the next run's.
func TestARunConvertsThroughTheRatesItStartedWith(t *testing.T) {
	t.Parallel()
	registry := moneyRegistry(t)
	rates := moneyRates(t, registry, "USD", "JPY", "150")
	err := registry.Register(machine.FunctionSpec{
		Name: "host.reprice_v1", Params: []machine.Type{machine.MoneyOf("u")}, Result: machine.MoneyOf("u"),
		Eval: func(_ context.Context, args []machine.Value) (machine.Value, error) {
			return args[0], rates.Add("USD", "JPY", "200")
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	runtime := instantiated(t, registry, "host.reprice_v1(a) -> JPY", "a: money<USD>")
	args, options := []machine.Value{machine.MoneyValue(100, "USD")}, machine.RunOptions{Rates: rates}
	for run, want := range []string{"{JPY 150}", "{JPY 200}"} {
		value, err := runtime.RunValues(t.Context(), args, options)
		if got := fmt.Sprint(value.Any()); err != nil || got != want {
			t.Fatalf("run %d: USD 1.00 -> JPY = %s, %v, want %s", run, got, err, want)
		}
	}
}

// fx(base, quote) is the rate -> converts at, as a value: from the run's
// table, through a chain where there is no quote, the identity for one
// currency, the scope's rates inside a using, and ErrNoRate — which
// fallback takes — where there is none. It reads the run, so it never folds.
func TestFxReadsTheRateTheRunConvertsAt(t *testing.T) {
	t.Parallel()
	registry := moneyRegistry(t)
	rates := moneyRates(t, registry, "USD", "JPY", "150")
	if err := rates.Add("EUR", "USD", "1.08"); err != nil {
		t.Fatal(err)
	}
	for source, want := range map[string]string{
		"fx(USD, JPY)":                         "USD/JPY 150",
		"fx(JPY, USD)":                         "JPY/USD 1/150",
		"fx(USD, USD)":                         "USD/USD 1",
		"using(155 JPY / USD, fx(USD, JPY))":   "USD/JPY 155",
		"fallback(fx(USD, KWD), fx(USD, JPY))": "USD/JPY 150",
	} {
		runtime := instantiated(t, registry, source, "")
		value, err := runtime.RunValues(t.Context(), nil, machine.RunOptions{Rates: rates})
		if fx, _ := value.FxRate(); err != nil || fx.String() != want {
			t.Errorf("%s = %v, %v, want %s", source, fx, err, want)
		}
	}
	runtime := instantiated(t, registry, "fx(USD, JPY)", "")
	if _, err := runtime.RunValues(t.Context(), nil, machine.RunOptions{}); !errors.Is(err, machine.ErrNoRate) {
		t.Fatalf("fx(USD, JPY) with no table: error = %v, want ErrNoRate", err)
	}
	if ops := fmt.Sprint(machine.PartsOf(mustCompileMoney(t, registry, "fx(USD, JPY)")).Instructions); !containsCall(ops) {
		t.Fatalf("fx(USD, JPY) compiles to %s, want a call the run makes", ops)
	}
}

func mustCompileMoney(t *testing.T, registry *machine.Registry, source string) *machine.Artifact {
	t.Helper()
	artifact, err := compileMoney(t, registry, source, "", "")
	if err != nil {
		t.Fatal(err)
	}
	return artifact
}

func containsCall(ops string) bool { return strings.Contains(ops, "call") }
