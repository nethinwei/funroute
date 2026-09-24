package machine_test

import (
	"errors"
	"fmt"
	"slices"
	"testing"

	"funroute/lang/internal/compile"
	"funroute/lang/internal/machine"
)

// fxRegistry is a console for the using tests: dollars, euros, yen, dinars
// and bitcoin's eight places, with comprehensions.
func fxRegistry(t testing.TB) *machine.Registry {
	t.Helper()
	registry := machine.CoreRegistry()
	if err := registry.EnableForm(machine.ForForm); err != nil {
		t.Fatal(err)
	}
	err := registry.DeclareMoney(machine.MoneySpec{Rounding: machine.RoundHalfEven, Currencies: []machine.CurrencySpec{
		{Code: "USD", Digits: 2}, {Code: "EUR", Digits: 2}, {Code: "JPY", Digits: 0}, {Code: "KWD", Digits: 3}, {Code: "BTC", Digits: 8},
	}})
	if err != nil {
		t.Fatal(err)
	}
	return registry
}

// fxContract is the arguments every using case may read: a dollar, nine
// euros, 1600 yen and a thousand dollars.
const fxContract = "a: money<USD>; e: money<EUR>; y: money<JPY>; k: money<USD>"

func fxArgs() []machine.Value {
	return []machine.Value{
		machine.MoneyValue(100, "USD"), machine.MoneyValue(900, "EUR"),
		machine.MoneyValue(1600, "JPY"), machine.MoneyValue(100_000, "USD"),
	}
}

// outerRates is the run's rate table holding quotes, each base, quote, rate;
// nil quotes are no table at all.
func outerRates(t *testing.T, registry *machine.Registry, quotes [][3]string) *machine.Rates {
	t.Helper()
	if quotes == nil {
		return nil
	}
	table, ok := registry.Currencies()
	if !ok {
		t.Fatal("the registry declares no money")
	}
	rates := table.NewRates()
	for _, quote := range quotes {
		if err := rates.Add(quote[0], quote[1], quote[2]); err != nil {
			t.Fatalf("Add(%v) = %v", quote, err)
		}
	}
	return rates
}

// runUsing runs source on fxRegistry with the outer quotes as the run's rate
// table, and prints its result.
func runUsing(t *testing.T, source, contract string, outer [][3]string, args ...machine.Value) (string, error) {
	t.Helper()
	registry := fxRegistry(t)
	artifact, err := compileMoney(t, registry, source, contract, "")
	if err != nil {
		t.Fatalf("CompileExpr(%q) error = %v", source, err)
	}
	runtime, err := machine.Instantiate(artifact, registry)
	if err != nil {
		t.Fatalf("Instantiate(%q) error = %v", source, err)
	}
	value, err := runtime.RunValues(t.Context(), args, machine.RunOptions{Rates: outerRates(t, registry, outer)})
	if err != nil {
		return "", err
	}
	return fmt.Sprint(value.Any()), nil
}

// usingCase is a rule over fxContract, the run's quotes, and its result as
// fmt prints it, or the error it fails with.
type usingCase struct {
	source string
	outer  [][3]string
	want   string
	err    error
}

func expectUsing(t *testing.T, test usingCase) {
	t.Helper()
	got, err := runUsing(t, test.source, fxContract, test.outer, fxArgs()...)
	switch {
	case test.err != nil && !errors.Is(err, test.err):
		t.Fatalf("%s with %v = %s, %v, want %v", test.source, test.outer, got, err, test.err)
	case test.err == nil && (err != nil || got != test.want):
		t.Fatalf("%s with %v = %s, %v, want %s", test.source, test.outer, got, err, test.want)
	}
}

func runUsingCases(t *testing.T, cases map[string]usingCase) {
	t.Helper()
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			expectUsing(t, test)
		})
	}
}

var (
	dollarAt150    = [][3]string{{"USD", "JPY", "150"}}
	onlyTheReverse = [][3]string{{"JPY", "USD", "0.01"}}
	onlyEuros      = [][3]string{{"USD", "EUR", "0.9"}}
)

// A using converts at the rates it writes and no others: the run's table
// takes no part. A rate from it comes in by being read, fx(base, quote), and
// can be marked up on the way.
func TestUsingIsolates(t *testing.T) {
	t.Parallel()
	runUsingCases(t, map[string]usingCase{
		"alone":                    {source: "using(160 JPY / USD, a -> JPY)", outer: dollarAt150, want: "{JPY 160}"},
		"alone, without the table": {source: "using(0.9 EUR / USD, a -> JPY)", outer: dollarAt150, err: machine.ErrNoRate},
		"no table, alone":          {source: "using(160 JPY / USD, a -> JPY)", want: "{JPY 160}"},
		"the other way":            {source: "using(160 JPY / USD, y -> USD)", outer: dollarAt150, want: "{USD 1000}"},
		// Two quotes are two hops the rule writes: EUR 9.00 is USD 10.00 is
		// JPY 1600. Written as one, it is no rate.
		"two hops":                 {source: "using(160 JPY / USD, 0.9 EUR / USD, e -> USD -> JPY)", want: "{JPY 1600}"},
		"one hop through nothing":  {source: "using(160 JPY / USD, 0.9 EUR / USD, e -> JPY)", err: machine.ErrNoRate},
		"a rate read from outside": {source: "using(fx(USD, JPY), a -> JPY)", outer: dollarAt150, want: "{JPY 150}"},
		"read and marked up":       {source: "using(fx(USD, JPY) * 102%, a -> JPY)", outer: dollarAt150, want: "{JPY 153}"},
		"read the other way":       {source: "using(fx(JPY, USD), a -> JPY)", outer: dollarAt150, want: "{JPY 150}"},
		"read from the reverse":    {source: "using(fx(USD, JPY), a -> JPY)", outer: onlyTheReverse, want: "{JPY 100}"},
		"read beside a quote":      {source: "using(fx(USD, JPY), 0.9 EUR / USD, e -> USD -> JPY)", outer: dollarAt150, want: "{JPY 1500}"},
		"read, but not there":      {source: "using(fx(USD, EUR), a -> EUR)", outer: dollarAt150, err: machine.ErrNoRate},
	})
}

// Within one using, what is written later wins, whichever way round the pair
// is written; a currency's rate to itself changes nothing.
func TestUsingLaterQuotesWin(t *testing.T) {
	t.Parallel()
	runUsingCases(t, map[string]usingCase{
		"two quotes of one pair":       {source: "using(160 JPY / USD, 170 JPY / USD, a -> JPY)", outer: dollarAt150, want: "{JPY 170}"},
		"an override written reversed": {source: "using(160 JPY / USD, 0.005 USD / JPY, a -> JPY)", want: "{JPY 200}"},
		"an identity beside a quote":   {source: "using(1 USD / USD, 160 JPY / USD, a -> JPY)", outer: dollarAt150, want: "{JPY 160}"},
		"an identity alone":            {source: "using(1 USD / USD, a -> JPY)", outer: dollarAt150, err: machine.ErrNoRate},
		"an identity alone, no change": {source: "using(1 USD / USD, a -> USD)", want: "{USD 100}"},
	})
}

// An inner using sees nothing of the one around it but what it reads, and
// each using's rates end with it: what follows converts at the rates outside
// again.
func TestUsingNestsAndEnds(t *testing.T) {
	t.Parallel()
	runUsingCases(t, map[string]usingCase{
		"an inner using alone":           {source: "using(160 JPY / USD, using(0.9 EUR / USD, e -> USD -> JPY))", err: machine.ErrNoRate},
		"an inner using reads the outer": {source: "using(160 JPY / USD, using(fx(USD, JPY), 0.9 EUR / USD, e -> USD -> JPY))", outer: onlyEuros, want: "{JPY 1600}"},
		"after the body":                 {source: "using(160 JPY / USD, a -> JPY) + (a -> JPY)", outer: dollarAt150, want: "{JPY 310}"},
		"before the body":                {source: "(a -> JPY) + using(160 JPY / USD, a -> JPY)", outer: dollarAt150, want: "{JPY 310}"},
		"after an inner body":            {source: "using(160 JPY / USD, using(170 JPY / USD, a -> JPY) + (a -> JPY))", want: "{JPY 330}"},
		"one after another":              {source: "using(160 JPY / USD, a -> JPY) + using(170 JPY / USD, a -> JPY)", want: "{JPY 330}"},
	})
}

// A fallback that catches a failure inside a using puts the run back where
// the fallback began, rates included.
func TestAFallbackEndsTheUsingsItCatches(t *testing.T) {
	t.Parallel()
	runUsingCases(t, map[string]usingCase{
		"then the table":           {source: "fallback(using(0.9 EUR / USD, a -> JPY), JPY 0) + (a -> JPY)", outer: dollarAt150, want: "{JPY 150}"},
		"then the using around it": {source: "using(160 JPY / USD, fallback(using(0.9 EUR / USD, a -> JPY), JPY 0) + (a -> JPY))", want: "{JPY 160}"},
		"two deep":                 {source: "fallback(using(160 JPY / USD, using(0.9 EUR / USD, a -> JPY)), JPY 0) + (a -> JPY)", outer: dollarAt150, want: "{JPY 150}"},
		"a candidate with its own": {source: "fallback(using(0.9 EUR / USD, a -> JPY), using(160 JPY / USD, a -> JPY))", outer: dollarAt150, want: "{JPY 160}"},
		"a quote of two amounts":   {source: "fallback(using(y / a, a -> JPY), JPY 0)", outer: dollarAt150, want: "{JPY 1600}"},
		"no table after the catch": {source: "fallback(using(0.9 EUR / USD, a -> JPY), JPY 0) + (a -> JPY)", err: machine.ErrNoRate},
	})
}

// A using in a comprehension opens and closes once an item, each with its
// own rate.
func TestAUsingInAComprehension(t *testing.T) {
	t.Parallel()
	contract := "a: money<USD>; xs: array<money<JPY>>"
	xs := moneyValues(t, []machine.Money{machine.NewMoney("JPY", 16_000), machine.NewMoney("JPY", 17_000)})
	source := "[using(x / (a * 100), a -> JPY) + (a -> JPY) for x in xs]"
	got, err := runUsing(t, source, contract, dollarAt150, machine.MoneyValue(100, "USD"), xs)
	if want := "[{JPY 310} {JPY 320}]"; err != nil || got != want {
		t.Fatalf("%s = %s, %v, want %s", source, got, err, want)
	}
}

// A rate written in figures is exact, however many places its currencies
// have.
func TestUsingQuotesInFiguresPastTheCurrencysPlaces(t *testing.T) {
	t.Parallel()
	runUsingCases(t, map[string]usingCase{
		"a yen rate to a thousandth":   {source: "using(150.255 JPY / USD, k -> JPY)", want: "{JPY 150255}"},
		"a satoshi rate past satoshis": {source: "using(0.000000001 BTC / USD, k -> BTC)", want: "{BTC 100}"},
		"a dollar rate past cents":     {source: "using(0.006667 USD / JPY, y -> USD)", want: "{USD 1067}"},
		"a dinar rate past fils":       {source: "using(0.30755 KWD / USD, k -> KWD)", want: "{KWD 307550}"},
		"rounded once, at the end":     {source: "using(150.255 JPY / USD, a -> JPY)", want: "{JPY 150}"},
		"two amounts in figures":       {source: "using(JPY 15000 / USD 100, a -> JPY)", want: "{JPY 150}"},
	})
}

// A quote of two amounts is checked when it is made: one currency is only
// its rate of 1, and zero or opposite signs are no rate. These are the
// rule's errors, which fallback does not take.
func TestAUsingQuoteOfTwoAmountsIsCheckedAtRunTime(t *testing.T) {
	t.Parallel()
	usd := func(minor int64) machine.Value { return machine.MoneyValue(minor, "USD") }
	jpy := func(minor int64) machine.Value { return machine.MoneyValue(minor, "JPY") }
	for name, test := range map[string]struct {
		p, q machine.Value
		want string
		err  error
	}{
		"yen for dollars":       {p: jpy(16_000), q: usd(10_000), want: "{JPY 160}"},
		"two refunds":           {p: jpy(-16_000), q: usd(-10_000), want: "{JPY 160}"},
		"one currency, equal":   {p: usd(100), q: usd(100), want: "{JPY 150}"},
		"one currency, unequal": {p: usd(200), q: usd(100), err: machine.ErrArithmetic},
		"a zero":                {p: jpy(0), q: usd(100), err: machine.ErrArithmetic},
		"opposite signs":        {p: jpy(-150), q: usd(100), err: machine.ErrArithmetic},
		"no currency":           {p: machine.MoneyValue(0, ""), q: usd(100), err: machine.ErrCurrency},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			source := "fallback(using(fx(USD, JPY), p / q, a -> JPY), JPY 0)"
			got, err := runUsing(t, source, "a: money<USD>; p: money<?>; q: money<?>", dollarAt150, usd(100), test.p, test.q)
			if (test.err != nil && !errors.Is(err, test.err)) || (test.err == nil && (err != nil || got != test.want)) {
				t.Fatalf("%s on p = %v, q = %v: %s, %v, want %s, %v", source, test.p.Any(), test.q.Any(), got, err, test.want, test.err)
			}
		})
	}
}

// A rate a host hands in is a quote like any other.
func TestAUsingQuotesAnArgument(t *testing.T) {
	t.Parallel()
	table, ok := fxRegistry(t).Currencies()
	if !ok {
		t.Fatal("the registry declares no money")
	}
	fx, err := table.FxRate("USD", "JPY", "160.5")
	if err != nil {
		t.Fatal(err)
	}
	source := "using(fx, k -> JPY)"
	got, err := runUsing(t, source, "fx: fxrate<USD,JPY>; k: money<USD>", dollarAt150, machine.FxRateValue(fx), machine.MoneyValue(100_000, "USD"))
	if want := "{JPY 160500}"; err != nil || got != want {
		t.Fatalf("%s with fx = %s: %s, %v, want %s", source, fx, got, err, want)
	}
}

// fx_push pops its quotes and has no other operand.
func TestFxPushIsValidatedOnLoad(t *testing.T) {
	t.Parallel()
	artifact := machine.ArtifactWith(machine.ArtifactParts{Instructions: make([]machine.Instruction, 3)})
	for name, test := range map[string]struct {
		instruction machine.Instruction
		wantError   bool
	}{
		"one quote":                {machine.Instruction{Op: machine.OpFxPush, B: 1}, false},
		"no quotes":                {machine.Instruction{Op: machine.OpFxPush}, true},
		"negative quotes":          {machine.Instruction{Op: machine.OpFxPush, B: -1}, true},
		"an A operand":             {machine.Instruction{Op: machine.OpFxPush, A: 1, B: 1}, true},
		"a C operand":              {machine.Instruction{Op: machine.OpFxPush, B: 1, C: 1}, true},
		"fx_pop takes no operands": {machine.Instruction{Op: machine.OpFxPop}, false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			err := machine.ValidateInstruction(0, test.instruction, artifact)
			if (err != nil) != test.wantError {
				t.Fatalf("validateInstruction(0, %+v) error = %v, want error = %v", test.instruction, err, test.wantError)
			}
		})
	}
}

// tamperedUsing is using(160 JPY / USD, a -> JPY) with its instructions
// changed by edit, resealed and loaded.
func tamperedUsing(t *testing.T, edit func([]machine.Instruction) []machine.Instruction) *machine.Runtime {
	t.Helper()
	registry := fxRegistry(t)
	artifact, err := compileMoney(t, registry, "using(160 JPY / USD, a -> JPY)", "a: money<USD>", "")
	if err != nil {
		t.Fatal(err)
	}
	parts := machine.PartsOf(artifact)
	parts.Instructions = edit(slices.Clone(parts.Instructions))
	tampered, err := machine.SealArtifact(parts, nil)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := machine.Instantiate(tampered, registry)
	if err != nil {
		t.Fatalf("Instantiate(the tampered using) = %v", err)
	}
	return runtime
}

// Load cannot see what a quote is or how deep the stack is where a using
// opens: a broken artifact fails its run rather than the process.
func TestABrokenUsingFailsItsRun(t *testing.T) {
	t.Parallel()
	for name, edit := range map[string]func([]machine.Instruction) []machine.Instruction{
		"a push past the stack": func(in []machine.Instruction) []machine.Instruction {
			for i := range in {
				if in[i].Op == machine.OpFxPush {
					in[i].B = 3
				}
			}
			return in
		},
		"a pop without a push": func(in []machine.Instruction) []machine.Instruction {
			return slices.DeleteFunc(in, func(i machine.Instruction) bool {
				return i.Op == machine.OpFxPush
			})
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			runtime := tamperedUsing(t, edit)
			if value, err := runtime.RunValues(t.Context(), []machine.Value{machine.MoneyValue(100, "USD")}, machine.RunOptions{}); err == nil {
				t.Fatalf("the run of a broken using = %v, nil, want an error", value.Any())
			}
		})
	}
}

// tableRuntime is source over a: money<USD> on fxRegistry, with the contract
// declaring the named rate tables settlement and market.
func tableRuntime(t *testing.T, registry *machine.Registry, source string) *machine.Runtime {
	t.Helper()
	options := compile.CompileOptions{Args: []compile.ArgSpec{{Name: "a", Type: machine.MoneyOf("USD")}}, RateTables: []string{"settlement", "market"}}
	artifact, err := compile.CompileExpr(source, registry, options)
	if err != nil {
		t.Fatalf("CompileExpr(%q) error = %v", source, err)
	}
	runtime, err := machine.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	return runtime
}

// using(@name, …) converts through the named table the host handed in, in
// place of the run's own, with the quotes after it laid over it. A declared
// table the host left out is empty, so a conversion through it is ErrNoRate,
// which fallback takes.
func TestUsingANamedTableConvertsThroughIt(t *testing.T) {
	t.Parallel()
	registry := fxRegistry(t)
	options := machine.RunOptions{
		Rates:      outerRates(t, registry, [][3]string{{"USD", "JPY", "150"}}),
		RateTables: map[string]*machine.Rates{"settlement": outerRates(t, registry, [][3]string{{"USD", "JPY", "149"}})},
	}
	for source, want := range map[string]string{
		"a -> JPY":                     "{JPY 150}",
		"using(@settlement, a -> JPY)": "{JPY 149}",
		"using(@settlement, 151 JPY / USD, a -> JPY)":       "{JPY 151}",
		"using(@settlement, using(fx(USD, JPY), a -> JPY))": "{JPY 149}",
		"fallback(using(@market, a -> JPY), a -> JPY)":      "{JPY 150}",
		"using(@market, 152 JPY / USD, a -> JPY)":           "{JPY 152}",
	} {
		value, err := tableRuntime(t, registry, source).RunValues(t.Context(), []machine.Value{machine.MoneyValue(100, "USD")}, options)
		if got := fmt.Sprint(value.Any()); err != nil || got != want {
			t.Errorf("%s = %s, %v, want %s", source, got, err, want)
		}
	}
	runtime := tableRuntime(t, registry, "using(@market, a -> JPY)")
	if _, err := runtime.RunValues(t.Context(), []machine.Value{machine.MoneyValue(100, "USD")}, options); !errors.Is(err, machine.ErrNoRate) {
		t.Fatalf("a declared table left out: error = %v, want ErrNoRate", err)
	}
}

// The host hands in only the tables the contract declares, over the
// registry's currencies.
func TestNamedTablesAreTheContracts(t *testing.T) {
	t.Parallel()
	registry := fxRegistry(t)
	runtime := tableRuntime(t, registry, "using(@settlement, a -> JPY)")
	args := []machine.Value{machine.MoneyValue(100, "USD")}
	unknown := machine.RunOptions{RateTables: map[string]*machine.Rates{"ledger": outerRates(t, registry, [][3]string{{"USD", "JPY", "1"}})}}
	if _, err := runtime.RunValues(t.Context(), args, unknown); !errors.Is(err, machine.ErrContract) {
		t.Fatalf("a table the contract does not declare: error = %v, want ErrContract", err)
	}
	other := moneyRates(t, moneyRegistryWith(t, machine.CurrencySpec{Code: "USD", Digits: 2}, machine.CurrencySpec{Code: "JPY", Digits: 0}), "USD", "JPY", "150")
	if _, err := runtime.RunValues(t.Context(), args, machine.RunOptions{RateTables: map[string]*machine.Rates{"settlement": other}}); !errors.Is(err, machine.ErrCurrency) {
		t.Fatalf("a table over other currencies: error = %v, want ErrCurrency", err)
	}
}

// A run takes the version each named table has when it starts: a quote the
// host adds while the rule runs is for the next run.
func TestANamedTableIsTheVersionTheRunStartedWith(t *testing.T) {
	t.Parallel()
	registry := fxRegistry(t)
	settlement := outerRates(t, registry, [][3]string{{"USD", "JPY", "150"}})
	err := machine.Logic(registry, "h.requote_v1", machine.Doc{Cost: 1}, func(a machine.Money) (machine.Money, error) {
		return a, settlement.Add("USD", "JPY", "999")
	})
	if err != nil {
		t.Fatal(err)
	}
	runtime := tableRuntime(t, registry, "let(b = h.requote_v1(a), using(@settlement, b -> JPY))")
	options := machine.RunOptions{RateTables: map[string]*machine.Rates{"settlement": settlement}}
	args := []machine.Value{machine.MoneyValue(100, "USD")}
	if value, err := runtime.RunValues(t.Context(), args, options); err != nil || fmt.Sprint(value.Any()) != "{JPY 150}" {
		t.Fatalf("first run = %v, %v, want JPY 150", value.Any(), err)
	}
	if value, err := runtime.RunValues(t.Context(), args, options); err != nil || fmt.Sprint(value.Any()) != "{JPY 999}" {
		t.Fatalf("second run = %v, %v, want JPY 999", value.Any(), err)
	}
}

// moneyRegistryWith is a registry declaring only currencies.
func moneyRegistryWith(t *testing.T, currencies ...machine.CurrencySpec) *machine.Registry {
	t.Helper()
	registry := machine.CoreRegistry()
	if err := registry.DeclareMoney(machine.MoneySpec{Rounding: machine.RoundHalfEven, Currencies: currencies}); err != nil {
		t.Fatal(err)
	}
	return registry
}

// Loading refuses an fx_push naming a table the contract does not declare,
// or one over a named table that also fills from the table outside.
func TestAForgedPushOverATableIsRefused(t *testing.T) {
	t.Parallel()
	registry := fxRegistry(t)
	options := compile.CompileOptions{Args: []compile.ArgSpec{{Name: "a", Type: machine.MoneyOf("USD")}}, RateTables: []string{"settlement"}}
	artifact, err := compile.CompileExpr("using(@settlement, 151 JPY / USD, a -> JPY)", registry, options)
	if err != nil {
		t.Fatal(err)
	}
	for name, forge := range map[string]func(*machine.Instruction){
		"an undeclared table":     func(in *machine.Instruction) { in.Keys = []string{"ledger"} },
		"a table and the outside": func(in *machine.Instruction) { in.C = 1 },
		"two tables":              func(in *machine.Instruction) { in.Keys = []string{"settlement", "settlement"} },
	} {
		parts := machine.PartsOf(artifact)
		parts.Instructions = slices.Clone(parts.Instructions)
		for i := range parts.Instructions {
			if parts.Instructions[i].Op == machine.OpFxPush {
				forge(&parts.Instructions[i])
			}
		}
		forged, err := machine.SealArtifact(parts, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := machine.Instantiate(forged, registry); err == nil {
			t.Errorf("Instantiate with %s = nil error, want a refusal", name)
		}
	}
}

// BenchmarkUsing is a using of constant quotes around a conversion, the
// shape a rule with its own agreed rate has.
func BenchmarkUsing(b *testing.B) {
	registry := fxRegistry(b)
	artifact, err := compileMoney(b, registry, "using(150 JPY / USD, 0.92 EUR / USD, a -> JPY)", "a: money<USD>", "")
	if err != nil {
		b.Fatal(err)
	}
	runtime, err := machine.Instantiate(artifact, registry)
	if err != nil {
		b.Fatal(err)
	}
	args := []machine.Value{machine.MoneyValue(100, "USD")}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := runtime.RunValues(b.Context(), args, machine.RunOptions{}); err != nil {
			b.Fatal(err)
		}
	}
}

// A using of constant quotes alone takes the table built for it at load: a
// run allocates the context it hands the table in, not the table. The count
// is global, so this test does not run in parallel.
func TestAUsingOfConstantQuotesBuildsNoTable(t *testing.T) {
	registry := fxRegistry(t)
	artifact, err := compileMoney(t, registry, "using(150 JPY / USD, 0.92 EUR / USD, a -> JPY)", "a: money<USD>", "")
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := machine.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	args := []machine.Value{machine.MoneyValue(100, "USD")}
	if value, err := runtime.RunValues(t.Context(), args, machine.RunOptions{}); err != nil || fmt.Sprint(value.Any()) != "{JPY 150}" {
		t.Fatalf("using(150 JPY / USD, …) = %v, %v, want JPY 150", value.Any(), err)
	}
	allocs := testing.AllocsPerRun(100, func() {
		if _, err := runtime.RunValues(t.Context(), args, machine.RunOptions{}); err != nil {
			t.Fatal(err)
		}
	})
	if allocs > 2 {
		t.Fatalf("a run of a constant using allocates %.0f times, want at most 2", allocs)
	}
}

// The table built at load is taken only for the very constants it was built
// from; other quotes on the stack — a jump into the middle of the code before
// the push — build their own.
func TestAConstantScopeHoldsOnlyItsQuotes(t *testing.T) {
	t.Parallel()
	registry := fxRegistry(t)
	table, _ := registry.Currencies()
	rate := func(text string) machine.Value {
		fx, err := table.FxRate("USD", "JPY", text)
		if err != nil {
			t.Fatal(err)
		}
		return machine.FxRateValue(fx)
	}
	built := []machine.Value{rate("150")}
	if !machine.ScopeHolds(built, built) {
		t.Fatal("a scope does not hold its own quotes")
	}
	if machine.ScopeHolds(built, []machine.Value{rate("150")}) || machine.ScopeHolds(built, []machine.Value{rate("151")}) || machine.ScopeHolds(built, nil) {
		t.Fatal("a scope holds quotes it was not built from")
	}
}
