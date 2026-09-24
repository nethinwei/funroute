package machine_test

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"funroute/lang/internal/compile"
	"funroute/lang/internal/machine"
)

// A host function's money comes back in a declared currency or not at all.
func TestAHostResultInAnUndeclaredCurrencyIsRefused(t *testing.T) {
	t.Parallel()
	registry := moneyRegistry(t)
	err := registry.Register(machine.FunctionSpec{
		Name: "fees.quote_v1", Params: []machine.Type{machine.MoneyOf("u")}, Result: machine.MoneyOf("u"),
		Eval: func(context.Context, []machine.Value) (machine.Value, error) {
			return machine.MoneyValue(5, "usd"), nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := compile.CompileExpr("fees.quote_v1(a)", registry, compile.CompileOptions{Args: []compile.ArgSpec{{Name: "a", Type: machine.MoneyOf("")}}})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := machine.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Run(t.Context(), map[string]any{"a": "USD 1.00"}, machine.RunOptions{}); !errors.Is(err, machine.ErrCurrency) {
		t.Fatalf("a result in \"usd\": error = %v, want ErrCurrency", err)
	}
}

// checkedRuntime is pick_v1(a, b) with checks run between loading the two
// arguments and the call: a sits one below the stack top, b on it.
func checkedRuntime(t *testing.T, contract string, checks ...machine.Instruction) (*machine.Runtime, error) {
	t.Helper()
	registry := moneyRegistry(t)
	tv, uv := machine.TypeVar("T"), machine.TypeVar("U")
	err := registry.Register(machine.FunctionSpec{
		Name: "checks.pick_v1", Params: []machine.Type{tv, uv}, Result: tv,
		Eval: func(_ context.Context, args []machine.Value) (machine.Value, error) { return args[0], nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := compileMoney(t, registry, "checks.pick_v1(a, b)", contract, "")
	if err != nil || len(machine.PartsOf(artifact).Instructions) != 3 {
		t.Fatalf("CompileExpr(checks.pick_v1(a, b)) = %v, %v, want three instructions", artifact, err)
	}
	parts := machine.PartsOf(artifact)
	instructions := append([]machine.Instruction(nil), parts.Instructions[:2]...)
	parts.Instructions = append(append(instructions, checks...), parts.Instructions[2])
	if artifact, err = machine.SealArtifact(parts, nil); err != nil {
		t.Fatal(err)
	}
	return machine.Instantiate(artifact, registry)
}

// checkCase runs checks on a and b.
type checkCase struct {
	contract string
	checks   []machine.Instruction
	a, b     machine.Value
	fails    bool
}

func codeCheck(depth int, code string) machine.Instruction {
	return machine.Instruction{Op: machine.OpCurrencyCheck, A: depth, C: machine.CheckCode, Keys: []string{code}}
}

func groupCheck(depth, flags, group int) machine.Instruction {
	return machine.Instruction{Op: machine.OpCurrencyCheck, A: depth, B: flags, C: machine.CheckGroup, D: group}
}

func patternCheck(depth int, typ machine.Type) machine.Instruction {
	return machine.Instruction{Op: machine.OpCurrencyCheck, A: depth, C: machine.CheckPattern, Type: &typ}
}

func TestACurrencyCheckRunsInEveryMode(t *testing.T) {
	t.Parallel()
	for name, test := range checkCases(t) {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			runtime, err := checkedRuntime(t, test.contract, test.checks...)
			if err != nil {
				t.Fatal(err)
			}
			_, err = runtime.RunValues(t.Context(), []machine.Value{test.a, test.b}, machine.RunOptions{})
			if test.fails != (err != nil) || (err != nil && !errors.Is(err, machine.ErrCurrency)) {
				t.Fatalf("checks %+v on %v, %v: error = %v, want failure %v as ErrCurrency", test.checks, test.a.Any(), test.b.Any(), err, test.fails)
			}
		})
	}
}

func checkCases(t *testing.T) map[string]checkCase {
	t.Helper()
	money := "a: money<?>; b: money<?>"
	usd, eur, zero := machine.MoneyValue(1, "USD"), machine.MoneyValue(1, "EUR"), machine.MoneyValue(0, "")
	newCall := machine.CheckNewCall
	return map[string]checkCase{
		"the code":                  {money, []machine.Instruction{codeCheck(0, "USD")}, eur, usd, false},
		"another code":              {money, []machine.Instruction{codeCheck(0, "USD")}, usd, eur, true},
		"a code one below":          {money, []machine.Instruction{codeCheck(1, "USD")}, eur, usd, true},
		"a code and a zero":         {money, []machine.Instruction{codeCheck(0, "USD")}, eur, zero, false},
		"a code of a currency":      {"a: money<?>; b: currency<?>", []machine.Instruction{codeCheck(0, "EUR")}, usd, machine.CurrencyValue("EUR"), false},
		"a code of another":         {"a: money<?>; b: currency<?>", []machine.Instruction{codeCheck(0, "USD")}, usd, machine.CurrencyValue("EUR"), true},
		"one group agreed":          {money, []machine.Instruction{groupCheck(1, newCall, 0), groupCheck(0, 0, 0)}, usd, usd, false},
		"one group in conflict":     {money, []machine.Instruction{groupCheck(1, newCall, 0), groupCheck(0, 0, 0)}, usd, eur, true},
		"a zero binds no group":     {money, []machine.Instruction{groupCheck(1, newCall, 0), groupCheck(0, 0, 0)}, zero, eur, false},
		"a group meets a zero":      {money, []machine.Instruction{groupCheck(1, newCall, 0), groupCheck(0, 0, 0)}, usd, zero, false},
		"two groups apart":          {money, []machine.Instruction{groupCheck(1, newCall, 0), groupCheck(0, 0, 1)}, usd, eur, false},
		"the last group":            {money, []machine.Instruction{groupCheck(1, newCall, 7), groupCheck(0, 0, 7)}, usd, eur, true},
		"a new call clears groups":  {money, []machine.Instruction{groupCheck(1, newCall, 0), groupCheck(0, newCall, 0)}, usd, eur, false},
		"a pattern's code":          {money, []machine.Instruction{patternCheck(0, machine.MoneyOf("USD"))}, eur, usd, false},
		"a pattern of another":      {money, []machine.Instruction{patternCheck(0, machine.MoneyOf("USD"))}, usd, eur, true},
		"a pattern and a zero":      {money, []machine.Instruction{patternCheck(0, machine.MoneyOf("USD"))}, usd, zero, false},
		"a pattern left open":       {money, []machine.Instruction{patternCheck(0, machine.MoneyOf(""))}, usd, eur, false},
		"a pattern's variable":      {"a: money<c>; b: money<?>", []machine.Instruction{patternCheck(0, machine.MoneyOf("c"))}, usd, usd, false},
		"a variable bound apart":    {"a: money<c>; b: money<?>", []machine.Instruction{patternCheck(0, machine.MoneyOf("c"))}, usd, eur, true},
		"a variable not contracted": {money, []machine.Instruction{patternCheck(1, machine.MoneyOf("c")), patternCheck(0, machine.MoneyOf("c"))}, usd, eur, false},
		"a pattern's currency":      {"a: money<?>; b: currency<?>", []machine.Instruction{patternCheck(0, machine.CurrencyOf("USD"))}, usd, machine.CurrencyValue("EUR"), true},
	}
}

// A pattern is walked through containers: every item, entry and field.
func TestAPatternCheckWalksContainers(t *testing.T) {
	t.Parallel()
	fee := machine.RecordOf(machine.FieldOf("fee", machine.MoneyOf("USD")))
	for name, test := range map[string]struct {
		contract string
		pattern  machine.Type
		value    machine.Value
		fails    bool
	}{
		"an array in its code": {"a: money<?>; b: array<money<?>>", machine.ArrayOf(machine.MoneyOf("USD")), moneyValues(t, []machine.Money{machine.NewMoney("USD", 1), {}}), false},
		"an item in another":   {"a: money<?>; b: array<money<?>>", machine.ArrayOf(machine.MoneyOf("USD")), moneyValues(t, []machine.Money{machine.NewMoney("USD", 1), machine.NewMoney("EUR", 1)}), true},
		"an entry in another":  {"a: money<?>; b: dict<money<?>>", machine.DictOf(machine.MoneyOf("USD")), moneyValues(t, map[string]machine.Money{"x": machine.NewMoney("EUR", 1)}), true},
		"a field in another": {"a: money<?>; b: record{fee: money<?>}", fee, moneyValues(t, struct {
			Fee machine.Money `funroute:"fee"`
		}{machine.NewMoney("EUR", 1)}), true},
		"a field in its code": {"a: money<?>; b: record{fee: money<?>}", fee, moneyValues(t, struct {
			Fee machine.Money `funroute:"fee"`
		}{machine.NewMoney("USD", 1)}), false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			runtime, err := checkedRuntime(t, test.contract, patternCheck(0, test.pattern))
			if err != nil {
				t.Fatal(err)
			}
			_, err = runtime.RunValues(t.Context(), []machine.Value{machine.MoneyValue(1, "USD"), test.value}, machine.RunOptions{})
			if test.fails != (err != nil) || (err != nil && !errors.Is(err, machine.ErrCurrency)) {
				t.Fatalf("pattern %s on %v: error = %v, want failure %v as ErrCurrency", test.pattern, test.value.Any(), err, test.fails)
			}
		})
	}
}

func moneyValues[T any](t *testing.T, input T) machine.Value {
	t.Helper()
	value, err := machine.ToValue(input)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

// A check that reaches below the stack is a broken artifact, not a currency
// error: load cannot see how deep the stack is where the check runs.
func TestACurrencyCheckBelowTheStackFails(t *testing.T) {
	t.Parallel()
	runtime, err := checkedRuntime(t, "a: money<?>; b: money<?>", codeCheck(2, "USD"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = runtime.RunValues(t.Context(), []machine.Value{machine.MoneyValue(1, "USD"), machine.MoneyValue(1, "USD")}, machine.RunOptions{})
	if err == nil || errors.Is(err, machine.ErrCurrency) || !strings.Contains(err.Error(), "below the stack") {
		t.Fatalf("a check two below the top of two: error = %v, want below the stack", err)
	}
}

func TestACurrencyCheckIsValidatedOnLoad(t *testing.T) {
	t.Parallel()
	artifact := machine.ArtifactWith(machine.ArtifactParts{Instructions: make([]machine.Instruction, 1)})
	open, concrete := machine.TypeVar("T"), machine.ArrayOf(machine.MoneyOf("c"))
	check := func(in machine.Instruction) machine.Instruction { in.Op = machine.OpCurrencyCheck; return in }
	for name, test := range map[string]struct {
		instruction machine.Instruction
		valid       bool
	}{
		"a group":                {check(machine.Instruction{C: machine.CheckGroup, D: 7, B: machine.CheckNewCall}), true},
		"a code":                 {check(machine.Instruction{A: 3, C: machine.CheckCode, Keys: []string{"USD"}}), true},
		"a pattern":              {check(machine.Instruction{C: machine.CheckPattern, Type: &concrete}), true},
		"a negative depth":       {check(machine.Instruction{A: -1}), false},
		"negative flags":         {check(machine.Instruction{B: -1}), false},
		"an unknown flag":        {check(machine.Instruction{B: 4}), false},
		"a negative group":       {check(machine.Instruction{C: machine.CheckGroup, D: -1}), false},
		"a group past the slots": {check(machine.Instruction{C: machine.CheckGroup, D: 8}), false},
		"a code without one":     {check(machine.Instruction{C: machine.CheckCode}), false},
		"a code with two":        {check(machine.Instruction{C: machine.CheckCode, Keys: []string{"USD", "EUR"}}), false},
		"a code that is not one": {check(machine.Instruction{C: machine.CheckCode, Keys: []string{"usd"}}), false},
		"a pattern without one":  {check(machine.Instruction{C: machine.CheckPattern}), false},
		"a pattern left open":    {check(machine.Instruction{C: machine.CheckPattern, Type: &open}), false},
		"an unknown mode":        {check(machine.Instruction{C: 3}), false},
		"a negative mode":        {check(machine.Instruction{C: -1}), false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			err := machine.ValidateInstruction(0, test.instruction, artifact)
			if test.valid != (err == nil) {
				t.Fatalf("ValidateInstruction(%+v) error = %v, want valid %v", test.instruction, err, test.valid)
			}
		})
	}
	if _, err := checkedRuntime(t, "a: money<?>; b: money<?>", groupCheck(0, 0, 8)); err == nil || !strings.Contains(err.Error(), "group 8") {
		t.Fatalf("Instantiate with a check of group 8: error = %v, want a refusal", err)
	}
}

// hostRuntime runs host.quote_v1(a) for a: money, the function returning out
// whatever it is handed.
func hostRuntime(t *testing.T, source string, result machine.Type, out machine.Value) *machine.Runtime {
	t.Helper()
	registry := moneyRegistry(t)
	err := registry.Register(machine.FunctionSpec{
		Name: "host.quote_v1", Params: []machine.Type{machine.MoneyOf("u")}, Result: result,
		Eval: func(context.Context, []machine.Value) (machine.Value, error) { return out, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := compileMoney(t, registry, source, "a: money<?>", "")
	if err != nil {
		t.Fatalf("CompileExpr(%q) error = %v", source, err)
	}
	runtime, err := machine.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	return runtime
}

// Whatever a host function hands back holds only declared currencies, and a
// currency error is not the kind fallback moves past.
func TestAHostResultHoldsDeclaredCurrencies(t *testing.T) {
	t.Parallel()
	for name, test := range map[string]struct {
		source string
		result machine.Type
		out    machine.Value
	}{
		"an undeclared currency":    {"host.quote_v1(a)", machine.CurrencyOf(""), machine.CurrencyValue("GBP")},
		"an undeclared amount":      {"host.quote_v1(a)", machine.MoneyOf("u"), machine.MoneyValue(1, "GBP")},
		"one fallback cannot catch": {"fallback(host.quote_v1(a), a)", machine.MoneyOf("u"), machine.MoneyValue(1, "GBP")},
		// A host that breaks its own signature's currency is a currency
		// error too, not a failing extension that fallback would move past.
		"not its signature's code":     {"host.quote_v1(a)", machine.MoneyOf("USD"), machine.MoneyValue(1, "EUR")},
		"not its signature's currency": {"host.quote_v1(a)", machine.CurrencyOf("USD"), machine.CurrencyValue("EUR")},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := hostRuntime(t, test.source, test.result, test.out).RunValues(t.Context(), []machine.Value{machine.MoneyValue(1, "USD")}, machine.RunOptions{})
			if !errors.Is(err, machine.ErrCurrency) || errors.Is(err, machine.ErrExtension) {
				t.Fatalf("%s returning %v: error = %v, want ErrCurrency and not ErrExtension", test.source, test.out.Any(), err)
			}
		})
	}
}

// An amount with no currency must be zero: a host that forgot the currency
// is a currency error, like the boundary's, and fallback does not take it.
func TestAHostResultWithoutACurrencyMustBeZero(t *testing.T) {
	t.Parallel()
	value, err := hostRuntime(t, "host.quote_v1(a)", machine.MoneyOf("u"), machine.MoneyValue(0, "")).
		RunValues(t.Context(), []machine.Value{machine.MoneyValue(1, "USD")}, machine.RunOptions{})
	if got, _ := value.Money(); err != nil || got != (machine.NewMoney("", 0)) {
		t.Fatalf("a host's currency-less zero = %v, %v, want it as it is", got, err)
	}
	_, err = hostRuntime(t, "host.quote_v1(a)", machine.MoneyOf("u"), machine.MoneyValue(5, "")).
		RunValues(t.Context(), []machine.Value{machine.MoneyValue(1, "USD")}, machine.RunOptions{})
	if err == nil {
		t.Fatal("a host's 5 minor units with no currency were accepted")
	}
	if !errors.Is(err, machine.ErrCurrency) {
		t.Fatalf("a host's 5 minor units with no currency: error = %v, want ErrCurrency", err)
	}
}

// The currencies in a host's array are its result's currencies too.
func TestAHostArrayHoldsDeclaredCurrencies(t *testing.T) {
	t.Parallel()
	for name, items := range map[string][]machine.Money{
		"an undeclared item":          {machine.NewMoney("USD", 1), machine.NewMoney("GBP", 1)},
		"an item with no currency":    {machine.NewMoney("", 5)},
		"a lower-case currency label": {machine.NewMoney("usd", 5)},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			out := moneyValues(t, items)
			value, err := hostRuntime(t, "host.quote_v1(a)", machine.ArrayOf(machine.MoneyOf("u")), out).
				RunValues(t.Context(), []machine.Value{machine.MoneyValue(1, "USD")}, machine.RunOptions{})
			if !errors.Is(err, machine.ErrCurrency) {
				t.Fatalf("a host array %v = %v, %v, want ErrCurrency", items, value.Any(), err)
			}
		})
	}
}

// fxValue is the money registry's exchange rate from base to quote.
func fxValue(t *testing.T, base, quote, rate string) machine.Value {
	t.Helper()
	table, ok := moneyRegistry(t).Currencies()
	if !ok {
		t.Fatal("the registry declares no money")
	}
	fx, err := table.FxRate(base, quote, rate)
	if err != nil {
		t.Fatal(err)
	}
	return machine.FxRateValue(fx)
}

// With CheckQuote a check reads an exchange rate's quote currency; without
// it, its base.
func TestACurrencyCheckReadsARatesQuote(t *testing.T) {
	t.Parallel()
	contract := "a: money<?>; b: fxrate<?,?>"
	usd, usdJPY, eurUSD := machine.MoneyValue(1, "USD"), fxValue(t, "USD", "JPY", "150"), fxValue(t, "EUR", "USD", "1.1")
	quoteCode := func(code string) machine.Instruction {
		check := codeCheck(0, code)
		check.B = machine.CheckQuote
		return check
	}
	newCall, quote := machine.CheckNewCall, machine.CheckQuote
	for name, test := range map[string]checkCase{
		"the quote's code":          {contract, []machine.Instruction{quoteCode("JPY")}, usd, usdJPY, false},
		"the base is not the quote": {contract, []machine.Instruction{quoteCode("USD")}, usd, usdJPY, true},
		"the base's code":           {contract, []machine.Instruction{codeCheck(0, "USD")}, usd, usdJPY, false},
		"the quote is not the base": {contract, []machine.Instruction{codeCheck(0, "JPY")}, usd, usdJPY, true},
		"a quote in the group":      {contract, []machine.Instruction{groupCheck(1, newCall, 0), groupCheck(0, quote, 0)}, usd, eurUSD, false},
		"a quote apart":             {contract, []machine.Instruction{groupCheck(1, newCall, 0), groupCheck(0, quote, 0)}, usd, usdJPY, true},
		"a pattern of both":         {contract, []machine.Instruction{patternCheck(0, machine.FxRateOf("USD", "JPY"))}, usd, usdJPY, false},
		"a pattern's other quote":   {contract, []machine.Instruction{patternCheck(0, machine.FxRateOf("USD", "EUR"))}, usd, usdJPY, true},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			runtime, err := checkedRuntime(t, test.contract, test.checks...)
			if err != nil {
				t.Fatal(err)
			}
			_, err = runtime.RunValues(t.Context(), []machine.Value{test.a, test.b}, machine.RunOptions{})
			if test.fails != (err != nil) || (err != nil && !errors.Is(err, machine.ErrCurrency)) {
				t.Fatalf("checks %+v on %v, %v: error = %v, want failure %v as ErrCurrency", test.checks, test.a.Any(), test.b.Any(), err, test.fails)
			}
		})
	}
	artifact := machine.ArtifactWith(machine.ArtifactParts{Instructions: make([]machine.Instruction, 1)})
	both := machine.Instruction{Op: machine.OpCurrencyCheck, B: newCall | quote, C: machine.CheckGroup}
	if err := machine.ValidateInstruction(0, both, artifact); err != nil {
		t.Fatalf("ValidateInstruction(%+v) = %v, want both flags valid", both, err)
	}
}

// A host function that takes a rate and an amount has the compiler check
// both of the rate's currencies against the amount's, where nothing proved
// them.
func TestAHostFunctionsRateIsCheckedOnBothSides(t *testing.T) {
	t.Parallel()
	registry := moneyRegistry(t)
	err := registry.Register(machine.FunctionSpec{
		Name: "fx.apply_v1", Params: []machine.Type{machine.FxRateOf("a", "b"), machine.MoneyOf("b")}, Result: machine.MoneyOf("b"),
		Eval: func(_ context.Context, args []machine.Value) (machine.Value, error) { return args[1], nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := compileMoney(t, registry, "fx.apply_v1(fx, m)", "fx: fxrate<?,?>; m: money<?>", "")
	if err != nil {
		t.Fatal(err)
	}
	readsQuote := func(in machine.Instruction) bool {
		return in.Op == machine.OpCurrencyCheck && in.B&machine.CheckQuote != 0
	}
	if !slices.ContainsFunc(machine.PartsOf(artifact).Instructions, readsQuote) {
		t.Fatalf("fx.apply_v1(fx, m) compiles to %+v, want a check of the rate's quote", machine.PartsOf(artifact).Instructions)
	}
	runtime, err := machine.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	for name, test := range map[string]struct {
		m     machine.Value
		fails bool
	}{
		"the quote's currency": {machine.MoneyValue(100, "JPY"), false},
		"the base's currency":  {machine.MoneyValue(100, "USD"), true},
		"a currency-less zero": {machine.MoneyValue(0, ""), false},
	} {
		_, err := runtime.RunValues(t.Context(), []machine.Value{fxValue(t, "USD", "JPY", "150"), test.m}, machine.RunOptions{})
		if test.fails != (err != nil) || (err != nil && !errors.Is(err, machine.ErrCurrency)) {
			t.Errorf("fx.apply_v1(USD/JPY 150, %v), %s: error = %v, want failure %v as ErrCurrency", test.m.Any(), name, err, test.fails)
		}
	}
}

// A rate a host function hands back is in declared currencies and keeps the
// rules; breaking either is a currency error, not a failing extension.
func TestAHostResultRateIsDeclared(t *testing.T) {
	t.Parallel()
	var undeclared machine.FxRate
	if err := json.Unmarshal([]byte(`{"base":"USD","quote":"GBP","rate":"0.8"}`), &undeclared); err != nil {
		t.Fatal(err)
	}
	for name, test := range map[string]struct {
		result machine.Type
		out    machine.Value
		fails  bool
	}{
		"a declared rate":        {machine.FxRateOf("", ""), fxValue(t, "USD", "JPY", "150"), false},
		"an undeclared currency": {machine.FxRateOf("", ""), machine.FxRateValue(undeclared), true},
		"the zero FxRate":        {machine.FxRateOf("", ""), machine.FxRateValue(machine.FxRate{}), true},
		"not its signature's":    {machine.FxRateOf("USD", "JPY"), fxValue(t, "EUR", "JPY", "160"), true},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := hostRuntime(t, "host.quote_v1(a)", test.result, test.out).RunValues(t.Context(), []machine.Value{machine.MoneyValue(1, "USD")}, machine.RunOptions{})
			if test.fails != (err != nil) || (err != nil && (!errors.Is(err, machine.ErrCurrency) || errors.Is(err, machine.ErrExtension))) {
				t.Fatalf("host.quote_v1 returning %v: error = %v, want failure %v as ErrCurrency and not ErrExtension", test.out.Any(), err, test.fails)
			}
		})
	}
}
