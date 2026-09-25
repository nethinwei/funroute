package machine_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/nethinwei/funroute/internal/machine"
	"github.com/nethinwei/funroute/internal/money"
)

// quotesOf is an array<fxrate> argument over registry's currencies, each
// quote written base, quote, rate: the host's []FxRate, as a rule gets it.
func quotesOf(t testing.TB, registry *machine.Registry, quotes ...[3]string) machine.Value {
	t.Helper()
	table, ok := registry.Currencies()
	if !ok {
		t.Fatal("the registry declares no money")
	}
	rates := make([]money.FxRate, 0, len(quotes))
	for _, quote := range quotes {
		fx, err := table.FxRate(quote[0], quote[1], quote[2])
		if err != nil {
			t.Fatal(err)
		}
		rates = append(rates, fx)
	}
	value, err := machine.ToValue(rates)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

// convertCase is a rule, its contract and arguments, the one quote its using
// converts at, and the result as fmt prints it.
type convertCase struct {
	source, contract string
	args             []machine.Value
	quote            [3]string
	want             string
}

// runConverting compiles test's rule inside using(rates, …), rates the last
// argument, and runs it with its quote as that argument.
func runConverting(t *testing.T, test convertCase) (machine.Value, error) {
	t.Helper()
	registry := moneyRegistry(t)
	runtime := instantiated(t, registry, "using(rates, "+test.source+")", test.contract+"; rates: array<fxrate>")
	args := append(append([]machine.Value(nil), test.args...), quotesOf(t, registry, test.quote))
	return runtime.RunValues(t.Context(), args)
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

// A rule converts with amount -> JPY, at the quotes its using names,
// rescaling between the currencies' places and rounding once, half to even
// as the registry says, or as round(…) says.
func TestAConversionInARule(t *testing.T) {
	t.Parallel()
	usd, jpy, kwd := machine.MoneyValue(100, "USD"), machine.MoneyValue(1000, "JPY"), machine.MoneyValue(1000, "KWD")
	usdJPY := [3]string{"USD", "JPY", "150.5"}
	for name, test := range map[string]convertCase{
		"dollars into yen":        {"round(a -> JPY, @half_even)", "a: money", []machine.Value{usd}, usdJPY, "{JPY 150}"},
		"yen back into dollars":   {"round(a -> USD, @half_even)", "a: money", []machine.Value{jpy}, usdJPY, "{USD 664}"},
		"dollars into dinars":     {"round(a -> KWD, @half_even)", "a: money", []machine.Value{usd}, [3]string{"USD", "KWD", "0.30755"}, "{KWD 308}"},
		"dinars into yen":         {"round(a -> JPY, @half_even)", "a: money", []machine.Value{kwd}, [3]string{"KWD", "JPY", "487.5"}, "{JPY 488}"},
		"a rounding scope":        {"round(a -> JPY, @up)", "a: money", []machine.Value{usd}, [3]string{"USD", "JPY", "150.25"}, "{JPY 151}"},
		"rounded down":            {"round(a -> JPY, @down)", "a: money", []machine.Value{usd}, [3]string{"USD", "JPY", "150.75"}, "{JPY 150}"},
		"half to even by default": {"round(a -> JPY, @half_even)", "a: money", []machine.Value{usd}, [3]string{"USD", "JPY", "150.5"}, "{JPY 150}"},
		"a currency the run says": {"round(a -> c, @half_even)", "a: money; c: currency", []machine.Value{usd, machine.CurrencyValue("JPY")}, usdJPY, "{JPY 150}"},
		"a currency it is in":     {"round(a -> c, @half_even)", "a: money; c: currency", []machine.Value{usd, machine.CurrencyValue("USD")}, usdJPY, "{USD 100}"},
		"a currency-less zero":    {"round(a -> JPY, @half_even)", "a: money", []machine.Value{machine.MoneyValue(0, "")}, usdJPY, "{JPY 0}"},
		"then arithmetic in yen":  {"round((a -> JPY), @half_even) + b", "a: money; b: money", []machine.Value{usd, jpy}, usdJPY, "{JPY 1150}"},
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

// Inside round(…) amount -> JPY is the exact step and round rounds after it;
// convert(amount, JPY, @mode) is the variant that rounds by its own mode.
func TestAConversionRoundsInARoundOrByItsMode(t *testing.T) {
	t.Parallel()
	registry := moneyRegistry(t)
	variant := "convert(money,currency,enum<rounding>{ceiling,down,floor,half_down,half_even,half_up,up})->money"
	for source, want := range map[string]string{
		"using(150 JPY / USD, round(a -> JPY, @down))": "convert(money,currency)->money round(money,enum<rounding>{ceiling,down,floor,half_down,half_even,half_up,up})->money",
		"using(150 JPY / USD, convert(a, JPY, @down))": variant,
	} {
		artifact, err := compileMoney(t, registry, source, "a: money", "")
		if err != nil {
			t.Fatal(err)
		}
		var calls []string
		for _, call := range machine.PartsOf(artifact).Calls {
			calls = append(calls, call.Signature)
		}
		if got := strings.Join(calls, " "); got != want {
			t.Fatalf("%s calls %s, want %s", source, got, want)
		}
	}
}

// Hops in a round are exact like every other step there: the yen between
// the dollar cent and the euro stays 1.505, and round rounds the 0.7525 euro
// once, as the chained rate would. convert with a mode, hop by hop, lands on
// whole yen first.
func TestChainedHopsRoundOnceInARound(t *testing.T) {
	t.Parallel()
	const quotes = "using(150.5 JPY / USD, 0.5 EUR / JPY, "
	for source, want := range map[string]string{
		quotes + "round(USD 0.01 -> JPY -> EUR, @half_even))":                    "{EUR 75}",
		quotes + "convert(convert(USD 0.01, JPY, @half_even), EUR, @half_even))": "{EUR 100}",
	} {
		value, err := evalMoney(t, source, "", nil)
		if got := fmt.Sprint(value.Any()); err != nil || got != want {
			t.Errorf("%s = %s, %v, want %s", source, got, err, want)
		}
	}
}

// A using with no quote of the pair, either way, cannot convert: ErrNoFxRate,
// data not at hand, which fallback takes.
func TestAConversionWithoutAQuoteIsErrNoFxRate(t *testing.T) {
	t.Parallel()
	registry := moneyRegistry(t)
	for name, rates := range map[string]machine.Value{
		"no quotes": quotesOf(t, registry),
		"no pair":   quotesOf(t, registry, [3]string{"USD", "JPY", "150"}),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			args := []machine.Value{machine.MoneyValue(100, "EUR"), rates}
			value, err := instantiated(t, registry, "using(rates, round(a -> JPY, @half_even))", "a: money; rates: array<fxrate>").RunValues(t.Context(), args)
			if !errors.Is(err, machine.ErrNoFxRate) {
				t.Fatalf("EUR 1.00 -> JPY = %v, %v, want ErrNoFxRate", value.Any(), err)
			}
			value, err = instantiated(t, registry, "fallback(using(rates, round(a -> JPY, @half_even)), JPY 7)", "a: money; rates: array<fxrate>").RunValues(t.Context(), args)
			if got := fmt.Sprint(value.Any()); err != nil || got != "{JPY 7}" {
				t.Fatalf("fallback(EUR 1.00 -> JPY, JPY 7) = %s, %v, want {JPY 7}", got, err)
			}
		})
	}
}

// Money already in the currency, and the currency-less zero, need no rate:
// a using with no quotes still converts them.
func TestAConversionIntoItsOwnCurrencyNeedsNoRate(t *testing.T) {
	t.Parallel()
	registry := moneyRegistry(t)
	for source, want := range map[string]string{"using(rates, round(a -> EUR, @half_even))": "{EUR 100}", "using(rates, round(0 -> EUR, @half_even))": "{EUR 0}"} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			args := []machine.Value{machine.MoneyValue(100, "EUR"), quotesOf(t, registry)}
			value, err := instantiated(t, registry, source, "a: money; rates: array<fxrate>").RunValues(t.Context(), args)
			if got := fmt.Sprint(value.Any()); err != nil || got != want {
				t.Fatalf("%s with no quotes = %s, %v, want %s", source, got, err, want)
			}
		})
	}
}

// Later quotes win, whichever way round the pair is written, a quote in an
// array like one on its own.
func TestALaterQuoteWins(t *testing.T) {
	t.Parallel()
	registry := moneyRegistry(t)
	for name, test := range map[string]struct {
		source string
		quotes [][3]string
		want   string
	}{
		"the last of the array":      {"using(rates, round(a -> JPY, @half_even))", [][3]string{{"USD", "JPY", "150"}, {"USD", "JPY", "160"}}, "{JPY 160}"},
		"the other way round, later": {"using(rates, round(a -> JPY, @half_even))", [][3]string{{"USD", "JPY", "150"}, {"JPY", "USD", "0.005"}}, "{JPY 200}"},
		"a quote after the array":    {"using(rates, 170 JPY / USD, round(a -> JPY, @half_even))", [][3]string{{"USD", "JPY", "150"}}, "{JPY 170}"},
		"the array after a quote":    {"using(170 JPY / USD, rates, round(a -> JPY, @half_even))", [][3]string{{"USD", "JPY", "150"}}, "{JPY 150}"},
		"two hops the rule writes":   {"using(rates, round(e -> USD -> JPY, @half_even))", [][3]string{{"USD", "JPY", "150"}, {"USD", "EUR", "0.9"}}, "{JPY 1500}"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			args := []machine.Value{machine.MoneyValue(100, "USD"), machine.MoneyValue(900, "EUR"), quotesOf(t, registry, test.quotes...)}
			runtime := instantiated(t, registry, test.source, "a: money; e: money; rates: array<fxrate>")
			value, err := runtime.RunValues(t.Context(), args)
			if got := fmt.Sprint(value.Any()); err != nil || got != test.want {
				t.Fatalf("%s with %v = %s, %v, want %s", test.source, test.quotes, got, err, test.want)
			}
		})
	}
}

// A quote the host hands in is checked at the boundary like an amount: a
// currency the registry does not declare is ErrCurrency before anything
// runs, and the zero FxRate is no rate.
func TestAQuoteOfAnUndeclaredCurrencyIsRefused(t *testing.T) {
	t.Parallel()
	other := machine.CoreRegistry()
	err := other.DeclareMoney(money.MoneySpec{Currencies: []money.CurrencySpec{
		{Code: "USD", Digits: 2}, {Code: "GBP", Digits: 2},
	}})
	if err != nil {
		t.Fatal(err)
	}
	registry := moneyRegistry(t)
	runtime := instantiated(t, registry, "using(rates, round(a -> JPY, @half_even))", "a: money; rates: array<fxrate>")
	args := []machine.Value{machine.MoneyValue(100, "USD"), quotesOf(t, other, [3]string{"USD", "GBP", "0.8"})}
	if value, err := runtime.RunValues(t.Context(), args); !errors.Is(err, machine.ErrCurrency) {
		t.Fatalf("a quote in GBP, which is not declared = %v, %v, want ErrCurrency", value.Any(), err)
	}
	if _, err := machine.ToValue([]money.FxRate{{}}); !errors.Is(err, machine.ErrCurrency) {
		t.Fatalf("ToValue of the zero FxRate: error = %v, want ErrCurrency", err)
	}
}

// A conversion reads the run, so folding never calls it: a using of constant
// quotes still converts when it runs.
func TestAConversionIsNotFolded(t *testing.T) {
	t.Parallel()
	registry := moneyRegistry(t)
	artifact, err := compileMoney(t, registry, "using(150 JPY / USD, round(USD 1 -> JPY, @half_even))", "", "")
	if err != nil {
		t.Fatalf("CompileExpr(USD 1 -> JPY) error = %v, want it compiled", err)
	}
	if calls := machine.PartsOf(artifact).Calls; len(calls) != 2 || calls[0].Name != "convert" {
		t.Fatalf("USD 1 -> JPY calls %+v, want convert and round, not a folded constant", calls)
	}
	runtime, err := machine.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	if value, err := runtime.RunValues(t.Context(), nil); err != nil || fmt.Sprint(value.Any()) != "{JPY 150}" {
		t.Fatalf("USD 1 -> JPY = %v, %v, want JPY 150", value.Any(), err)
	}
}

// fx(base, quote) is the rate -> converts at, as a value: from the using's
// quotes, the inverse where only the other way is quoted, the identity for
// one currency, and ErrNoFxRate — which fallback takes — where there is none.
// It reads the run, so it never folds.
func TestFxReadsTheRateTheUsingConvertsAt(t *testing.T) {
	t.Parallel()
	registry := moneyRegistry(t)
	rates := quotesOf(t, registry, [3]string{"USD", "JPY", "150"}, [3]string{"EUR", "USD", "1.08"})
	for source, want := range map[string]string{
		"using(rates, fx(USD, JPY))":                         "150 JPY / USD",
		"using(rates, fx(JPY, USD))":                         "1/150 USD / JPY",
		"using(rates, fx(USD, USD))":                         "1 USD / USD",
		"using(155 JPY / USD, fx(USD, JPY))":                 "155 JPY / USD",
		"using(rates, fallback(fx(USD, KWD), fx(USD, JPY)))": "150 JPY / USD",
	} {
		runtime := instantiated(t, registry, source, "rates: array<fxrate>")
		value, err := runtime.RunValues(t.Context(), []machine.Value{rates})
		if fx, _ := value.FxRate(); err != nil || fx.String() != want {
			t.Errorf("%s = %v, %v, want %s", source, fx, err, want)
		}
	}
	runtime := instantiated(t, registry, "using(rates, fx(USD, KWD))", "rates: array<fxrate>")
	if _, err := runtime.RunValues(t.Context(), []machine.Value{rates}); !errors.Is(err, machine.ErrNoFxRate) {
		t.Fatalf("fx(USD, KWD) with no such quote: error = %v, want ErrNoFxRate", err)
	}
	if ops := fmt.Sprint(machine.PartsOf(mustCompileMoney(t, registry, "using(150 JPY / USD, fx(USD, JPY))")).Instructions); !containsCall(ops) {
		t.Fatalf("fx(USD, JPY) compiles to %s, want a call the run makes", ops)
	}
}

// A conversion at the host's quotes allocates nothing: the []FxRate is the
// using's scope as it came, and the rate a value. The count is global, so
// this test does not run in parallel.
func TestAConversionAllocatesNothing(t *testing.T) {
	registry := moneyRegistry(t)
	runtime := instantiated(t, registry, "using(rates, round(using(fx(USD, JPY), a -> JPY) -> USD, @half_even))", "a: money; rates: array<fxrate>")
	args := []machine.Value{machine.MoneyValue(100, "USD"), quotesOf(t, registry, [3]string{"EUR", "USD", "1.08"}, [3]string{"USD", "JPY", "150"})}
	if value, err := runtime.RunValues(t.Context(), args); err != nil || fmt.Sprint(value.Any()) != "{USD 100}" {
		t.Fatalf("USD 1.00 there and back = %v, %v, want USD 1.00", value.Any(), err)
	}
	allocs := testing.AllocsPerRun(100, func() {
		if _, err := runtime.RunValues(t.Context(), args); err != nil {
			panic(err)
		}
	})
	if allocs != 0 {
		t.Fatalf("a run that converts allocated %v times, want 0", allocs)
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
