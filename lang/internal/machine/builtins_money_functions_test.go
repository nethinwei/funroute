package machine_test

import (
	"errors"
	"fmt"
	"testing"

	"funroute/lang/internal/machine"
)

func TestMoneyComesTogetherAndApart(t *testing.T) {
	t.Parallel()
	kwd, zero := machine.MoneyValue(1234, "KWD"), machine.MoneyValue(0, "")
	for name, test := range map[string]moneyCase{
		"minor units and a currency": {"money(n, cur)", "n: int; cur: currency<?>", []machine.Value{machine.Int(170), machine.CurrencyValue("JPY")}, "{JPY 170}"},
		"a negative amount":          {"money(n, KWD)", "n: int", []machine.Value{machine.Int(-5)}, "{KWD -5}"},
		"a currency read from text":  {"money(n, currency_of(s))", "n: int; s: string", []machine.Value{machine.Int(9), machine.String("EUR")}, "{EUR 9}"},
		"a code read from text":      {"currency_of(s)", "s: string", []machine.Value{machine.String("USD")}, "USD"},
		"a copied currency":          {"like(a, 30)", "a: money<?>", []machine.Value{kwd}, "{KWD 30}"},
		"a copied negative":          {"like(a, -3)", "a: money<?>", []machine.Value{kwd}, "{KWD -3}"},
		"nothing like nothing":       {"like(a, 0)", "a: money<?>", []machine.Value{zero}, "{ 0}"},
		"the minor units":            {"minor(a)", "a: money<?>", []machine.Value{kwd}, "1234"},
		"the minor units of a zero":  {"minor(a)", "a: money<?>", []machine.Value{zero}, "0"},
		"the currency":               {"currency(a)", "a: money<?>", []machine.Value{kwd}, "KWD"},
		"a negative sign":            {"sign(a)", "a: money<?>", []machine.Value{machine.MoneyValue(-1, "USD")}, "-1"},
		"a zero's sign":              {"sign(a)", "a: money<?>", []machine.Value{zero}, "0"},
		"a dollar zero's sign":       {"sign(a)", "a: money<?>", []machine.Value{machine.MoneyValue(0, "USD")}, "0"},
		"a positive sign":            {"sign(a)", "a: money<?>", []machine.Value{machine.MoneyValue(1, "USD")}, "1"},
		"a currency's code":          {"string(currency(a))", "a: money<?>", []machine.Value{kwd}, "KWD"},
		"a written currency's code":  {"string(JPY)", "", nil, "JPY"},
		"an argument's code":         {"string(c)", "c: currency<?>", []machine.Value{machine.CurrencyValue("EUR")}, "EUR"},
		"a currency compared":        {`if(currency(a) == KWD, "dinar", "other")`, "a: money<?>", []machine.Value{kwd}, "dinar"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			expectValue(t, test)
		})
	}
}

// A currency is declared or it is not one: text is read against the table,
// and a currency-less zero has none to hand out.
func TestTheMoneyFunctionsRefuseWhatHasNoCurrency(t *testing.T) {
	t.Parallel()
	zero := machine.MoneyValue(0, "")
	for name, test := range map[string]moneyCase{
		"an undeclared code":         {"currency_of(s)", "s: string", []machine.Value{machine.String("GBP")}, ""},
		"a lower-case code":          {"currency_of(s)", "s: string", []machine.Value{machine.String("usd")}, ""},
		"a code with spaces":         {"currency_of(s)", "s: string", []machine.Value{machine.String(" USD")}, ""},
		"no code at all":             {"currency_of(s)", "s: string", []machine.Value{machine.String("")}, ""},
		"money in an undeclared one": {"money(1, currency_of(s))", "s: string", []machine.Value{machine.String("GBP")}, ""},
		"like a zero, but not zero":  {"like(a, 5)", "a: money<?>", []machine.Value{zero}, ""},
		"a zero's currency":          {"currency(a)", "a: money<?>", []machine.Value{zero}, ""},
		"a zero's currency's code":   {"string(currency(a))", "a: money<c>", []machine.Value{zero}, ""},
		"an undeclared argument":     {"money(1, cur)", "cur: currency<?>", []machine.Value{machine.CurrencyValue("GBP")}, ""},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			expectError(t, test, machine.ErrCurrency)
		})
	}
}

// A closed currency_of fails where the rule is compiled, still as a currency
// error.
func TestAnUndeclaredCodeInTheSourceFailsToCompile(t *testing.T) {
	t.Parallel()
	_, err := compileMoney(t, moneyRegistry(t), `currency_of("GBP")`, "", "")
	if !errors.Is(err, machine.ErrCurrency) {
		t.Fatalf(`CompileExpr("currency_of(\"GBP\")") error = %v, want ErrCurrency`, err)
	}
}

func TestAllocateSplitsWithoutLosingAUnit(t *testing.T) {
	t.Parallel()
	usd := func(minor int64) machine.Value { return machine.MoneyValue(minor, "USD") }
	three := intArray(t, 1, 2, 3)
	for name, test := range map[string]moneyCase{
		"as many shares as items":     {"allocate(a, len(xs))", "a: money<USD>; xs: array<int>", []machine.Value{usd(100), three}, "[{USD 34} {USD 33} {USD 33}]"},
		"shares counted arithmetic":   {"allocate(a, len(xs) * 2 - 4)", "a: money<USD>; xs: array<int>", []machine.Value{usd(5), three}, "[{USD 3} {USD 2}]"},
		"one share":                   {"allocate(a, 1)", "a: money<USD>", []machine.Value{usd(7)}, "[{USD 7}]"},
		"yen by weight":               {"allocate(a, [1, 1, 1])", "a: money<JPY>", []machine.Value{machine.MoneyValue(100, "JPY")}, "[{JPY 34} {JPY 33} {JPY 33}]"},
		"a refund by weight":          {"allocate(a, [1, 2])", "a: money<USD>", []machine.Value{usd(-5)}, "[{USD -2} {USD -3}]"},
		"a zero weight gets nothing":  {"allocate(a, [0, 1])", "a: money<USD>", []machine.Value{usd(5)}, "[{USD 0} {USD 5}]"},
		"weights from the arguments":  {"allocate(a, xs)", "a: money<USD>; xs: array<int>", []machine.Value{usd(10), three}, "[{USD 2} {USD 3} {USD 5}]"},
		"a tie goes to the earlier":   {"allocate(a, [1, 3, 3])", "a: money<USD>", []machine.Value{usd(1)}, "[{USD 0} {USD 1} {USD 0}]"},
		"a product past int64":        {"allocate(a, xs)", "a: money<USD>; xs: array<int>", []machine.Value{usd(1 << 62), intArray(t, 3, 5)}, "[{USD 1729382256910270464} {USD 2882303761517117440}]"},
		"a zero splits into zeros":    {"allocate(a, 2)", "a: money<?>", []machine.Value{machine.MoneyValue(0, "")}, "[{ 0} { 0}]"},
		"a zero in a declared result": {"allocate(a, 2)", "a: money<c>", []machine.Value{machine.MoneyValue(0, "")}, "[{ 0} { 0}]"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			expectValue(t, test)
		})
	}
}

func intArray(t *testing.T, items ...int64) machine.Value {
	t.Helper()
	value, err := machine.ToValue(items)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestAllocateRefusesSharesItCannotMake(t *testing.T) {
	t.Parallel()
	usd := machine.MoneyValue(100, "USD")
	for name, test := range map[string]moneyCase{
		"no shares":          {"allocate(a, len(xs))", "a: money<USD>; xs: array<int>", []machine.Value{usd, intArray(t)}, "1 to 10000 shares, got 0"},
		"fewer than none":    {"allocate(a, len(xs) - 2)", "a: money<USD>; xs: array<int>", []machine.Value{usd, intArray(t, 1)}, "got -1"},
		"one past the limit": {"allocate(a, 10001)", "a: money<USD>", []machine.Value{usd}, "got 10001"},
		"weights all zero":   {"allocate(a, [0, 0])", "a: money<USD>", []machine.Value{usd}, "positive weight"},
		"no weights":         {"allocate(a, xs)", "a: money<USD>; xs: array<int>", []machine.Value{usd, intArray(t)}, "positive weight"},
		"a negative weight":  {"allocate(a, [1, -1])", "a: money<USD>", []machine.Value{usd}, "cannot be negative"},
		"weights past int64": {"allocate(a, xs)", "a: money<USD>; xs: array<int>", []machine.Value{usd, intArray(t, 1<<62, 1<<62)}, "overflows"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			expectError(t, test, nil)
		})
	}
}

// The limit is ten thousand shares, and all of them add back up.
func TestAllocateMakesUpToTenThousandShares(t *testing.T) {
	t.Parallel()
	value, err := runMoney(t, "allocate(a, 10000)", "a: money<USD>", machine.MoneyValue(10_001, "USD"))
	shares, ok := value.Any().([]machine.Money)
	if err != nil || !ok || len(shares) != 10_000 {
		t.Fatalf("allocate(USD 100.01, 10000) = %d shares, %v, want 10000", len(shares), err)
	}
	total := int64(0)
	for _, share := range shares {
		total += share.Minor()
	}
	if shares[0].Minor() != 2 || shares[1].Minor() != 1 || total != 10_001 {
		t.Fatalf("allocate(USD 100.01, 10000) = %v first, %v second, total %d, want 2, 1 and 10001", shares[0], shares[1], total)
	}
}

// allocate(m, n) makes an array of n shares out of nothing, so n must be
// bounded by the inputs where the rule is compiled; the weights form is
// bounded by its own array.
func TestAllocateNeedsBoundedShares(t *testing.T) {
	t.Parallel()
	registry := moneyRegistry(t)
	for key, bounded := range map[string]bool{
		"allocate(money<u>,int)->array<money<u>>":        true,
		"allocate(money<u>,array<int>)->array<money<u>>": false,
	} {
		function, ok := registry.Resolve(key)
		if !ok || function.NeedsBoundedArgs() != bounded {
			t.Fatalf("Resolve(%q) = %v, bounded %v, want bounded %v", key, ok, ok && function.NeedsBoundedArgs(), bounded)
		}
	}
	for _, source := range []string{"allocate(a, n)", "allocate(a, n + 1)", "allocate(a, minor(a))"} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			_, err := compileMoney(t, registry, source, "a: money<USD>; n: int", "")
			if !errors.Is(err, machine.ErrCompile) {
				t.Fatalf("CompileExpr(%q) error = %v, want ErrCompile", source, err)
			}
		})
	}
}

// A rate is read from text only: floats and ints do not become rates.
func TestRatesAreReadFromText(t *testing.T) {
	t.Parallel()
	text := func(s string) []machine.Value { return []machine.Value{machine.String(s)} }
	for name, test := range map[string]moneyCase{
		"text":               {"rate(s)", "s: string", text("0.029"), "0.029"},
		"text with spaces":   {"rate(s)", "s: string", text(" 0.5 "), "0.5"},
		"negative text":      {"rate(s)", "s: string", text("-2.5"), "-2.5"},
		"signed text":        {"rate(s)", "s: string", text("+1"), "1"},
		"ten places of text": {"rate(s)", "s: string", text("0.0000000001"), "0.0000000001"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			expectValue(t, test)
		})
	}
}

func TestRatesRefuseWhatTheyCannotHold(t *testing.T) {
	t.Parallel()
	for name, test := range map[string]moneyCase{
		"eleven places of text": {"rate(s)", "s: string", []machine.Value{machine.String("0.00000000001")}, "11 decimal places"},
		"text that is a word":   {"rate(s)", "s: string", []machine.Value{machine.String("abc")}, "is not a decimal"},
		"no text":               {"rate(s)", "s: string", []machine.Value{machine.String("")}, "is not a decimal"},
		"a point alone":         {"rate(s)", "s: string", []machine.Value{machine.String(".")}, "is not a decimal"},
		"a percent sign":        {"rate(s)", "s: string", []machine.Value{machine.String("2.9%")}, "is not a decimal"},
		"an exponent":           {"rate(s)", "s: string", []machine.Value{machine.String("1e5")}, "is not a decimal"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			expectError(t, test, nil)
		})
	}
}

// A currency indexes a dictionary by its code, the way a string would.
func TestACurrencyIsADictionaryKey(t *testing.T) {
	t.Parallel()
	limits, err := machine.ToValue(map[string]int64{"USD": 5, "JPY": 700})
	if err != nil {
		t.Fatal(err)
	}
	contract := "d: dict<int>; a: money<?>"
	for name, test := range map[string]moneyCase{
		"a lookup":            {"d[currency(a)]", contract, []machine.Value{limits, machine.MoneyValue(1, "JPY")}, "700"},
		"a present key":       {"currency(a) in d", contract, []machine.Value{limits, machine.MoneyValue(1, "USD")}, "true"},
		"an absent key":       {"currency(a) in d", contract, []machine.Value{limits, machine.MoneyValue(1, "EUR")}, "false"},
		"a written currency":  {"d[USD]", "d: dict<int>", []machine.Value{limits}, "5"},
		"a currency argument": {"c in d", "d: dict<int>; c: currency<?>", []machine.Value{limits, machine.CurrencyValue("KWD")}, "false"},
		"a threshold":         {"a > like(a, d[currency(a)])", contract, []machine.Value{limits, machine.MoneyValue(701, "JPY")}, "true"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			expectValue(t, test)
		})
	}
	expectError(t, moneyCase{"d[currency(a)]", contract, []machine.Value{limits, machine.MoneyValue(1, "EUR")}, "EUR"}, nil)
}

// round(expr, @mode) is compiled away: no call to it is emitted, and its
// evaluator, which nothing calls, hands its expression back unchanged.
func TestRoundIsAScopeNotACall(t *testing.T) {
	t.Parallel()
	registry := moneyRegistry(t)
	overloads := registry.Overloads("round")
	if len(overloads) != 1 || !overloads[0].IsRoundingScope() {
		t.Fatalf("Overloads(round) = %v, want one rounding scope", overloads)
	}
	money := machine.MoneyValue(5, "USD")
	value, err := overloads[0].Eval(t.Context(), []machine.Value{money, machine.String("half_up")})
	if got, _ := value.Money(); err != nil || got != (machine.NewMoney("USD", 5)) {
		t.Fatalf("round's Eval(USD 0.05, @half_up) = %v, %v, want USD 0.05", got, err)
	}
	artifact, err := compileMoney(t, registry, "round(a * r, @up)", "a: money<USD>; r: rate", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, call := range machine.PartsOf(artifact).Calls {
		if call.Name == "round" {
			t.Fatalf("round(a * r, @up) calls %s, want no call to round", call.Signature)
		}
	}
}

// rate(text) that reads no rate is ErrArithmetic, the data's error: text that
// is no decimal, and a figure with more places than a rate keeps.
func TestRateOfTextThatIsNoRateIsArithmetic(t *testing.T) {
	t.Parallel()
	for _, text := range []string{"seven", "0.00000000001", ""} {
		if _, err := evalMoney(t, "rate(s)", "s: string", map[string]any{"s": text}); !errors.Is(err, machine.ErrArithmetic) {
			t.Errorf("rate(%q) error = %v, want ErrArithmetic", text, err)
		}
	}
}

// allocate takes a strategy as its last argument, by weight and in equal
// shares alike, written as a member of the registry's allocation enum.
func TestAllocateTakesAStrategy(t *testing.T) {
	t.Parallel()
	for source, want := range map[string]string{
		"allocate(a, [6, 3, 1], @largest_remainder)": "[{USD 5} {USD 2} {USD 1}]",
		"allocate(a, [6, 3, 1], @largest_weight)":    "[{USD 5} {USD 3} {USD 0}]",
		"allocate(a, [6, 3, 1], @all_last)":          "[{USD 4} {USD 2} {USD 2}]",
		"allocate(a, 3, @reverse_order)":             "[{USD 2} {USD 3} {USD 3}]",
		"allocate(a, 3, @allocation.all_first)":      "[{USD 4} {USD 2} {USD 2}]",
		"allocate(a, 3)":                             "[{USD 3} {USD 3} {USD 2}]",
	} {
		value, err := evalMoney(t, source, "a: money<USD>", map[string]any{"a": "USD 0.08"})
		if got := fmt.Sprint(value.Any()); err != nil || got != want {
			t.Errorf("%s = %s, %v, want %s", source, got, err, want)
		}
	}
}

// prorate and round_to round by the registry's default, or by the mode a
// round around them names.
func TestProrateAndRoundToInARule(t *testing.T) {
	t.Parallel()
	const contract = "fee: money<USD>; refund: money<EUR>; paid: money<EUR>; cash: money<USD>"
	args := map[string]any{"fee": "USD 1.00", "refund": "EUR 1.00", "paid": "EUR 3.00", "cash": "USD 1.03"}
	for source, want := range map[string]string{
		"prorate(fee, refund, paid)":                  "{USD 33}",
		"round(prorate(fee, 2, 3), @down)":            "{USD 66}",
		"prorate(fee, 2, 3)":                          "{USD 67}",
		"round_to(cash, USD 0.05)":                    "{USD 105}",
		"round(round_to(cash, like(cash, 5)), @down)": "{USD 100}",
	} {
		value, err := evalMoney(t, source, contract, args)
		if got := fmt.Sprint(value.Any()); err != nil || got != want {
			t.Errorf("%s = %s, %v, want %s", source, got, err, want)
		}
	}
	if _, err := compileMoney(t, moneyRegistry(t), "prorate(fee, refund, cash)", contract, ""); err == nil {
		t.Fatal("prorate by euros over dollars compiled, want a currency mismatch")
	}
}
