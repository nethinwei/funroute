package compile

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"funroute/lang/internal/machine"
)

// Declaring money changes nothing a program without money compiles to: the
// same bytes, the same digest, no stamp.
func TestMoneyCapabilityKeepsDigests(t *testing.T) {
	t.Parallel()
	for _, source := range []string{
		"amount * 25 / 10000 + 30",
		"if(risk < 0.5, [1, 2, 0], [0])",
		"sum_of[0] + 0.5",
		`switch(channel, case "adyen" => 0.029, else => 0.03)`,
		`{fee: 0, rate: 0.5}`,
	} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			registry := machine.CoreRegistry()
			if err := registry.EnableForm(machine.SwitchForm, machine.ForForm, machine.ReduceForm); err != nil {
				t.Fatal(err)
			}
			plain, err := CompileExpr(source, registry, CompileOptions{})
			if err != nil {
				t.Fatal(err)
			}
			declared, err := CompileExpr(source, moneyRegistry(t), CompileOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if declared.Digest() != plain.Digest() || machine.PartsOf(declared).Money != nil {
				t.Fatalf("%s digests %s with money declared and %s without (stamp %v)", source, declared.Digest(), plain.Digest(), machine.PartsOf(declared).Money)
			}
		})
	}
}

// An artifact's stamp is what it was compiled on and depends on: the default
// rounding and the places of every currency its bytecode names. A program
// that meets its currencies only at run time names none.
func TestMoneyArtifactsCarryTheStamp(t *testing.T) {
	t.Parallel()
	registry := moneyRegistry(t)
	spec, _ := registry.Money()
	for _, test := range []struct{ source, contract, want string }{
		{"amount * 2", "amount: money<USD>", "USD:2"},
		{"amount -> JPY", "amount: money<USD>", "JPY:0 USD:2"},
		{"a + a", "a: money<c>", ""},
	} {
		artifact, err := CompileExpr(test.source, registry, CompileOptions{Args: moneyContract(t, test.contract)})
		if err != nil {
			t.Fatalf("CompileExpr(%q) error = %v", test.source, err)
		}
		stamp := machine.PartsOf(artifact).Money
		if stamp == nil || stamp.Rounding != spec.Rounding {
			t.Fatalf("%s: stamp = %+v, want the registry's rounding", test.source, stamp)
		}
		var named []string
		for _, currency := range stamp.Currencies {
			named = append(named, fmt.Sprintf("%s:%d", currency.Code, currency.Digits))
		}
		if got := strings.Join(named, " "); got != test.want {
			t.Fatalf("%s: stamp names %q, want %q", test.source, got, test.want)
		}
	}
}

func TestMoneyContractsAreCheckedAgainstTheRegistry(t *testing.T) {
	t.Parallel()
	usd := machine.MoneyOf("USD")
	dollars := machine.MoneyOf("c")
	for name, test := range map[string]struct {
		registry *machine.Registry
		options  CompileOptions
		want     string
	}{
		"money needs a declaration": {machine.CoreRegistry(), CompileOptions{Args: []ArgSpec{{Name: "a", Type: usd}}}, "declares money"},
		"an undeclared code":        {moneyRegistry(t), CompileOptions{Args: []ArgSpec{{Name: "a", Type: machine.MoneyOf("GBP")}}}, "not declared"},
		"an unbound result unit":    {moneyRegistry(t), CompileOptions{Args: []ArgSpec{{Name: "a", Type: usd}}, Result: &dollars}, "no argument binds"},
		"the registry's enum name":  {moneyRegistry(t), CompileOptions{Args: []ArgSpec{{Name: "a", Type: machine.EnumOf("rounding", "x")}}}, "registry's"},
		"the other registry enum":   {moneyRegistry(t), CompileOptions{Args: []ArgSpec{{Name: "a", Type: machine.EnumOf("allocation", "x")}}}, "registry's"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := CompileExpr("a", test.registry, test.options)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("CompileExpr error = %v, want one containing %q", err, test.want)
			}
		})
	}
}

// Money costs no allocation on the way in, through the kernel and out: the
// currency binding lives in the pooled frame, the boundary scan of an array
// of money reads its backing in place.
func TestMoneyRunsDoNotAllocate(t *testing.T) {
	registry := moneyRegistry(t)
	history, err := machine.ToValue(make([]machine.Money, 4096))
	if err != nil {
		t.Fatal(err)
	}
	fees := make(map[string]machine.Money, 1000)
	for i := range 1000 {
		fees[fmt.Sprint("channel", i)] = machine.Money{}
	}
	byChannel, err := machine.ToValue(fees)
	if err != nil {
		t.Fatal(err)
	}
	five, wide := fiveCurrencies(t)
	for _, test := range []struct {
		name, source string
		arg          ArgSpec
		value        machine.Value
	}{
		{"费率与固定费", `amount * 0.029 + like(amount, 30)`, ArgSpec{Name: "amount", Type: machine.MoneyOf("c")}, machine.MoneyValue(10_000, "EUR")},
		{"金额数组", `len(history)`, ArgSpec{Name: "history", Type: machine.ArrayOf(machine.MoneyOf("c"))}, history},
		{"金额字典", `len(fees)`, ArgSpec{Name: "fees", Type: machine.DictOf(machine.MoneyOf("c"))}, byChannel},
		{"五个币种变量", `wide.a`, ArgSpec{Name: "wide", Type: five}, wide},
	} {
		t.Run(test.name, func(t *testing.T) {
			assertCallDoesNotAllocate(t, registry, test.source, test.arg, test.value)
		})
	}
}

// fiveCurrencies is a record of five amounts in five currency variables, and
// one of it: more variables than a frame keeps without its spill buffer.
func fiveCurrencies(t *testing.T) (machine.Type, machine.Value) {
	t.Helper()
	var fields []machine.Field
	var values []machine.Value
	for _, name := range []string{"a", "b", "c", "d", "e"} {
		fields = append(fields, machine.FieldOf(name, machine.MoneyOf("u"+name)))
		values = append(values, machine.MoneyValue(1, "USD"))
	}
	typ := machine.RecordOf(fields...)
	value, err := machine.Record(machine.RecordOf(recordOfOpenMoney(fields)...), values)
	if err != nil {
		t.Fatal(err)
	}
	return typ, value
}

// recordOfOpenMoney is fields with their currencies left open, to build a
// value of them.
func recordOfOpenMoney(fields []machine.Field) []machine.Field {
	out := make([]machine.Field, len(fields))
	for i, field := range fields {
		out[i] = machine.FieldOf(field.Name(), machine.MoneyOf(""))
	}
	return out
}

func TestAnInexactRateLiteralIsAnError(t *testing.T) {
	t.Parallel()
	_, err := CompileExpr("amount * 0.00000000001", moneyRegistry(t), CompileOptions{Args: moneyContract(t, "amount: money<USD>")})
	if err == nil || !strings.Contains(err.Error(), "decimal places") {
		t.Fatalf("an eleven-place rate: error = %v, want one naming the places", err)
	}
}

func TestACurrencyIsAMemberOfTheRegistrysSet(t *testing.T) {
	t.Parallel()
	source := "switch(currency(amount), case USD => 1, case EUR => 2, else => 3)"
	for currency, want := range map[string]string{"USD 1.00": "1", "EUR 1.00": "2", "JPY 100": "3"} {
		got, err := runMoney(t, source, "amount: money<c>", map[string]any{"amount": currency})
		if err != nil || got != want {
			t.Fatalf("%s with %s = %s, %v, want %s", source, currency, got, err, want)
		}
	}
}

// A contract enum with a member spelled like a currency keeps its @USD: a
// currency is written bare, USD, so declaring money takes nothing away.
func TestAContractMemberNamedLikeACurrencyStaysUnambiguous(t *testing.T) {
	t.Parallel()
	region := machine.EnumOf("region", "USD", "EUR")
	options := CompileOptions{Args: []ArgSpec{{Name: "r", Type: region}}}
	if _, err := CompileExpr("r == @USD", moneyRegistry(t), options); err != nil {
		t.Fatalf("r == @USD with money declared: %v", err)
	}
	options.Args = append(options.Args, ArgSpec{Name: "m", Type: machine.MoneyOf("c")})
	if _, err := CompileExpr("currency(m) == USD", moneyRegistry(t), options); err != nil {
		t.Fatalf("the bare currency: %v", err)
	}
}

// A money literal is its amount in the currency's minor units and a rate
// literal its value, exactly, whatever places they were written with.
func TestMoneyAndRateLiteralsAreExact(t *testing.T) {
	t.Parallel()
	for source, want := range map[string]string{
		"USD 1.70":                 "{USD 170}",
		"USD 1.7":                  "{USD 170}",
		"USD 1":                    "{USD 100}",
		"USD 0.05":                 "{USD 5}",
		"USD 0":                    "{USD 0}",
		"JPY 100":                  "{JPY 100}",
		"KWD 1.234":                "{KWD 1234}",
		"KWD 0.001":                "{KWD 1}",
		"USD -1.70":                "{USD -170}",
		"-USD 1.70":                "{USD -170}",
		"- USD 1.70":               "{USD -170}",
		"USD 92233720368547758.07": "{USD 9223372036854775807}",
		"2.9%":                     "0.029",
		"-2.9%":                    "-0.029",
		"25bps":                    "0.0025",
		"0.5bps":                   "0.00005",
		"100%":                     "1",
		"0%":                       "0",
		"0.00000001%":              "0.0000000001",
		"0.000001bps":              "0.0000000001",
		"92233720368%":             "922337203.68",
	} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			got, err := runMoney(t, source, "", nil)
			if err != nil || got != want {
				t.Fatalf("%s = %s, %v, want %s", source, got, err, want)
			}
		})
	}
}

// Too many places, too large a value, a currency the registry does not
// declare, or no money at all are compile errors rather than roundings.
func TestMoneyLiteralsOutOfRangeAreCompileErrors(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		source string
		money  bool
		want   string
	}{
		{"JPY 1.5", true, "has 1 decimal places, at most 0 fit"},
		{"KWD 1.2345", true, "has 4 decimal places, at most 3 fit"},
		{"USD 0.001", true, "has 3 decimal places, at most 2 fit"},
		{"USD 92233720368547758.08", true, "overflows"},
		{"USD -92233720368547758.09", true, "overflows"},
		{"GBP 1", true, `"GBP" is not declared`},
		{"0.000000001%", true, "has 9 decimal places, at most 8 fit"},
		{"0.0000001bps", true, "has 7 decimal places, at most 6 fit"},
		{"92233720369%", true, "overflows"},
		{"USD 1.00 * 0.30000000000000004", true, "has 17 decimal places, at most 10 fit"},
		{"USD 1.00 * 1e-11", true, "decimal places"},
		{"USD 1.00 * 123456789012.5", true, "overflows"},
		{"USD 1", false, "declares no money"},
		{"2.9%", false, "declares no money"},
		{"25bps", false, "declares no money"},
	} {
		t.Run(test.source, func(t *testing.T) {
			t.Parallel()
			registry := moneyRegistry(t)
			if !test.money {
				registry = consoleRegistry(t)
			}
			_, err := CompileExpr(test.source, registry, CompileOptions{})
			if !errors.Is(err, machine.ErrCompile) || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("CompileExpr(%q) error = %v, want ErrCompile containing %q", test.source, err, test.want)
			}
		})
	}
}

// USD is a currency and @half_up a rounding mode, only on a registry that
// declares money.
func TestTheRegistrysEnumsResolve(t *testing.T) {
	t.Parallel()
	for _, test := range []struct{ source, want, value string }{
		{"USD", "currency<USD>", "USD"},
		{"KWD", "currency<KWD>", "KWD"},
		{"@half_up", machine.RoundingEnumType().String(), "half_up"},
		{"@rounding.half_even", machine.RoundingEnumType().String(), "half_even"},
		{"string(EUR)", "string", "EUR"},
		{"money(5, JPY)", "money<JPY>", "{JPY 5}"},
	} {
		t.Run(test.source, func(t *testing.T) {
			t.Parallel()
			artifact, err := compileMoney(t, test.source, "", "")
			if err != nil {
				t.Fatalf("CompileExpr(%q) error = %v", test.source, err)
			}
			got, err := runArtifact(t, artifact, nil)
			if artifact.Result().String() != test.want || err != nil || got != test.value {
				t.Fatalf("%s = %s (%s), %v, want %s (%s)", test.source, got, artifact.Result(), err, test.value, test.want)
			}
		})
	}
}

func TestTheRegistrysEnumsRefuseWhatTheyLack(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		source string
		money  bool
		want   string
	}{
		{"GBP", true, `"GBP" is not declared`},
		{"@USD", true, "not a member of any enum"},
		{"@currency.USD", true, `no enum named "currency"`},
		{"@rounding.nearest", true, `"nearest" is not a member of enum<rounding>`},
		{"USD", false, "declares no money"},
		{"@currency.USD", false, `no enum named "currency"`},
		{"@half_up", false, "the contract declares no enum"},
	} {
		t.Run(test.source, func(t *testing.T) {
			t.Parallel()
			registry := moneyRegistry(t)
			if !test.money {
				registry = consoleRegistry(t)
			}
			_, err := CompileExpr(test.source, registry, CompileOptions{})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("CompileExpr(%q) error = %v, want %q", test.source, err, test.want)
			}
		})
	}
}

// A contract enum whose members are spelled like the registry's keeps the
// bare spelling; the rounding mode is then written qualified, and a currency
// is never an enum member.
func TestContractMembersShadowTheRegistrysBareSpelling(t *testing.T) {
	t.Parallel()
	contract := []ArgSpec{
		{Name: "amount", Type: machine.MoneyOf("USD")},
		{Name: "dir", Type: machine.EnumOf("dir", "up", "down")},
		{Name: "region", Type: machine.EnumOf("region", "USD", "EUR")},
	}
	for source, want := range map[string]string{
		"round(amount * 0.025, @rounding.up)": "",
		"dir == @up":                          "",
		"region == @USD":                      "",
		"currency(amount) == USD":             "",
		"round(amount * 0.025, @up)":          "no overload",
		"currency(amount) == @USD":            "no overload",
	} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			_, err := CompileExpr(source, moneyRegistry(t), CompileOptions{Args: contract})
			if want == "" && err != nil {
				t.Fatalf("CompileExpr(%q) error = %v, want it to compile", source, err)
			}
			if want != "" && (err == nil || !strings.Contains(err.Error(), want)) {
				t.Fatalf("CompileExpr(%q) error = %v, want %q", source, err, want)
			}
		})
	}
}

func TestEnumNamespace(t *testing.T) {
	t.Parallel()
	options := CompileOptions{Args: []ArgSpec{{Name: "o", Type: machine.RecordOf(machine.FieldOf("ch", machine.EnumOf("channel", "adyen")))}}}
	declared := EnumNamespace(options, moneyRegistry(t))
	names := slices.Sorted(func(yield func(string) bool) {
		for name := range declared {
			yield(name)
		}
	})
	if got := strings.Join(names, " "); got != "allocation channel rounding" {
		t.Fatalf("EnumNamespace with money = %s, want channel rounding", got)
	}
	if !declared["rounding"].Equal(machine.RoundingEnumType()) {
		t.Fatalf("rounding enum = %s, want %s", declared["rounding"], machine.RoundingEnumType())
	}
	if plain := EnumNamespace(options, consoleRegistry(t)); len(plain) != 1 || plain["channel"].Name() != "channel" {
		t.Fatalf("EnumNamespace without money = %v, want only channel", plain)
	}
}

// The contract's money types are the registry's to allow: every kind money
// brings, at any depth, needs money declared and every code it names
// declared, and the registry's rounding enum is not the contract's.
func TestMoneyContractsAreCheckedAtAnyDepth(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name     string
		money    bool
		contract string
		result   string
		want     string
	}{
		{"a rate needs money", false, "a: rate", "", "declares money"},
		{"a currency needs money", false, "a: currency<?>", "", "declares money"},
		{"money in an array", false, "a: array<money<USD>>", "", "declares money"},
		{"money in a record", false, "a: record{fee: money<?>}", "", "declares money"},
		{"a money result", false, "a: int", "money<?>", "declares money"},
		{"a currency's code", true, "a: currency<GBP>", "", `"GBP" is not declared`},
		{"a code in an array", true, "a: array<money<GBP>>", "", `"GBP" is not declared`},
		{"a code in a nested record", true, "a: record{b: record{c: money<GBP>}}", "", `"GBP" is not declared`},
		{"a code in the result", true, "a: int", "money<GBP>", `"GBP" is not declared`},
		{"the rounding enum", true, "a: enum<rounding>{up}", "", "registry's"},
		{"the rounding enum in a record", true, "a: record{e: enum<rounding>{x}}", "", "registry's"},
		{"the rounding enum as the result", true, "a: int", "enum<rounding>{up}", "registry's"},
		{"a result unit nothing binds", true, "a: money<c>", "money<d>", "currency d, which no argument binds"},
		{"a record result unit nothing binds", true, "a: money<c>", "record{fee: money<e>}", "currency e"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := compileContract(t, test.money, test.contract, test.result)
			if !errors.Is(err, machine.ErrContract) || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("contract %q -> %q: error = %v, want ErrContract containing %q", test.contract, test.result, err, test.want)
			}
		})
	}
}

// compileContract compiles "a" under the contract and result on a registry
// with or without money.
func compileContract(t *testing.T, money bool, contract, result string) (*machine.Artifact, error) {
	t.Helper()
	if money {
		return compileMoney(t, "a", contract, result)
	}
	options := CompileOptions{Args: moneyContract(t, contract)}
	if result != "" {
		typ, err := machine.ParseType(result)
		if err != nil {
			t.Fatal(err)
		}
		options.Result = &typ
	}
	return CompileExpr("a", consoleRegistry(t), options)
}

// A result's currency variable may be bound anywhere an argument holds one.
func TestResultUnitsBoundByAnyArgument(t *testing.T) {
	t.Parallel()
	for _, test := range []struct{ source, contract, result string }{
		{"a -> cur", "a: money<c>; cur: currency<d>", "money<d>"},
		{"[money(1, cur)]", "cur: currency<c>", "array<money<c>>"},
		{"r.fee", "r: record{fee: money<c>}", "money<c>"},
		{"{fee: m}", "m: money<c>; unused: currency<e>", "record{fee: money<c>}"},
		// Currencies are no enum, so a contract's enum may be named currency.
		{"a", "a: enum<currency>{x}", ""},
		{"a.e", "a: record{e: enum<currency>{x}}", ""},
	} {
		t.Run(test.source+" as "+test.result, func(t *testing.T) {
			t.Parallel()
			if _, err := compileMoney(t, test.source, test.contract, test.result); err != nil {
				t.Fatalf("CompileExpr(%q) under %q -> %q: %v", test.source, test.contract, test.result, err)
			}
		})
	}
}

// The stamp is written exactly when the artifact states a money kind — an
// argument, the result, a call's type or a constant — and one whose money
// was all folded away loads where no money is declared.
func TestTheStampIsWrittenOnlyWhereMoneyIs(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		source, contract string
		stamped          bool
	}{
		{"r * r", "r: rate", true},
		{"2.9% * 2.9%", "", true},
		{"k + 1", "k: int; unused: money<?>", true},
		{"string(currency_of(s))", "s: string", true},
		{"currency(m) == USD", "m: money<?>", true},
		{"minor(USD 1.70) * 2", "", false},
		{"USD 1.00 > USD 0.50", "", false},
		{"string(USD)", "", false},
		{"@half_up", "", false},
		{"k * 2", "k: int", false},
	} {
		t.Run(test.source, func(t *testing.T) {
			t.Parallel()
			artifact, err := compileMoney(t, test.source, test.contract, "")
			if err != nil {
				t.Fatalf("CompileExpr(%q) error = %v", test.source, err)
			}
			if stamped := machine.PartsOf(artifact).Money != nil; stamped != test.stamped {
				t.Fatalf("CompileExpr(%q) stamped %v, want %v", test.source, stamped, test.stamped)
			}
			_, err = machine.Instantiate(artifact, consoleRegistry(t))
			if loads := err == nil; loads == test.stamped {
				t.Fatalf("CompileExpr(%q) loads without money: %v, want %v", test.source, loads, !test.stamped)
			}
		})
	}
}

// An artifact stamped by one money declaration does not load on another.
func TestAStampedArtifactNeedsTheSameMoney(t *testing.T) {
	t.Parallel()
	artifact, err := compileMoney(t, "amount * 2.9%", "amount: money<USD>", "")
	if err != nil {
		t.Fatal(err)
	}
	other := consoleRegistry(t)
	if err := other.DeclareMoney(machine.MoneySpec{Rounding: machine.RoundHalfEven, Currencies: []machine.CurrencySpec{
		{Code: "USD", Digits: 2}, {Code: "EUR", Digits: 2}, {Code: "JPY", Digits: 0}, {Code: "KWD", Digits: 3},
	}}); err != nil {
		t.Fatal(err)
	}
	if _, err := machine.Instantiate(artifact, other); err == nil {
		t.Fatal("an artifact stamped half up loaded on a half even registry")
	}
}

// A closed money expression is computed while compiling: the artifact holds
// one constant, and it is the value the run would have produced.
func TestClosedMoneyExpressionsFold(t *testing.T) {
	t.Parallel()
	for source, want := range map[string]string{
		"USD 1.00 + USD 2.00":             "{USD 300}",
		"USD -1.70 + USD 0.70":            "{USD -100}",
		"USD 1.00 * 2.9%":                 "{USD 3}",
		"USD 1.00 * -2.9%":                "{USD -3}",
		"round(USD 1.00 * 2.5%, @down)":   "{USD 2}",
		"money(170, USD)":                 "{USD 170}",
		"like(USD 1.00, 5)":               "{USD 5}",
		"minor(USD 1.70) * 2":             "340",
		"2.9% + 100%":                     "1.029",
		`currency_of("EUR")`:              "EUR",
		"USD 10.00 / USD 4.00":            "2.5",
		"allocate(USD 1.00, 3)":           "[{USD 34} {USD 33} {USD 33}]",
		"let(fee = USD 0.30, fee * 3)":    "{USD 90}",
		"[m * 2 for m in [USD 1, EUR 2]]": "[{USD 200} {EUR 400}]",
	} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			artifact, err := compileMoney(t, source, "", "")
			if err != nil {
				t.Fatalf("CompileExpr(%q) error = %v", source, err)
			}
			if len(machine.PartsOf(artifact).Instructions) != 1 || machine.PartsOf(artifact).Instructions[0].Op != machine.OpConstant {
				t.Fatalf("CompileExpr(%q) = %v, want one constant", source, machine.PartsOf(artifact).Instructions)
			}
			if got, err := runArtifact(t, artifact, nil); err != nil || got != want {
				t.Fatalf("%s = %s, %v, want %s", source, got, err, want)
			}
		})
	}
}

// A closed money expression that fails fails every time, so it is a compile
// error even in a branch that might not run; a currency failure stays
// ErrCurrency underneath, and fallback does not catch it.
func TestClosedMoneyFailuresAreCompileErrors(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		source   string
		currency bool
		want     string
	}{
		{"USD 92233720368547758.07 + USD 0.01", false, "overflows"},
		{"money(9223372036854775807, USD) * 2", false, "overflows"},
		{"USD 1 / USD 0", false, "division by zero"},
		{`currency_of("XYZ")`, true, `"XYZ" is not declared`},
		{`currency_of("usd")`, true, `"usd" is not declared`},
		{`if(f, currency_of("XYZ"), USD)`, true, `"XYZ" is not declared`},
		{`fallback(currency_of("XYZ"), USD)`, true, `"XYZ" is not declared`},
		{"like(0, 5)", true, "currency-less zero"},
		{"currency(0)", true, "currency-less zero"},
		{"USD 1 == EUR 1", false, "no overload eq(money<USD>, money<EUR>)"},
	} {
		t.Run(test.source, func(t *testing.T) {
			t.Parallel()
			_, err := compileMoney(t, test.source, "f: bool", "")
			if !errors.Is(err, machine.ErrCompile) || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("CompileExpr(%q) error = %v, want ErrCompile containing %q", test.source, err, test.want)
			}
			if errors.Is(err, machine.ErrCurrency) != test.currency {
				t.Fatalf("CompileExpr(%q) error = %v, ErrCurrency %v, want %v", test.source, err, !test.currency, test.currency)
			}
		})
	}
}

// assertMoneyChangesNothing compiles source with and without money declared
// and wants the same arguments, result and digest, and no stamp.
func assertMoneyChangesNothing(t *testing.T, source string) {
	t.Helper()
	plain, err := CompileExpr(source, consoleRegistry(t), CompileOptions{})
	if err != nil {
		t.Fatalf("CompileExpr(%q) without money: %v", source, err)
	}
	declared, err := CompileExpr(source, moneyRegistry(t), CompileOptions{})
	if err != nil {
		t.Fatalf("CompileExpr(%q) with money: %v", source, err)
	}
	if got, want := fmt.Sprint(declared.Args(), declared.Result()), fmt.Sprint(plain.Args(), plain.Result()); got != want {
		t.Fatalf("CompileExpr(%q) types with money %s, want %s as without", source, got, want)
	}
	if declared.Digest() != plain.Digest() || machine.PartsOf(declared).Money != nil {
		t.Fatalf("CompileExpr(%q) digest %s (stamp %v) with money, want %s", source, declared.Digest(), machine.PartsOf(declared).Money, plain.Digest())
	}
}

// A program that writes no money infers the same types and seals the same
// digest whether the registry declares money or not.
func TestDeclaringMoneyKeepsTypesOfProgramsWithoutMoney(t *testing.T) {
	t.Parallel()
	for _, source := range []string{
		"risk * 0.5", "risk < 0.5", "[risk, 0.5]", "risk > 0.5 && risk < 1", "if(f, 0, n)",
		"reduce(x in xs, t = 0, t + x)", "[x * 0.5 for x in xs]", "{fee: 0, r: 0.5}", "let(z = 0.5, z * w)",
		"switch(k, case 0 => 0.5, else => 1.5)", "0.1 + 0.2", "7 % -2", "10 % 3", "x % 3 == 0", "-0.5 * w",
		`{"a": 0, "b": n}`, "[0.5, w][0] > 0.25", "score * 0.5 > 0.1",
	} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			assertMoneyChangesNothing(t, source)
		})
	}
}

// A decimal beside a free variable and a 0 elsewhere: without money the
// variable is a float paying the mixed penalty for float against int; with
// money, money times a rate compared with the zero amount is cheaper, so the
// argument silently becomes money and the digest changes.
func TestDeclaringMoneyKeepsFloatsBesideZero(t *testing.T) {
	t.Parallel()
	for _, source := range []string{"score * 0.5 > 0", "score * 0.5 + 0", "score / 2.0 >= 0", "if(score * 0.5 > 0, 1, 2)"} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			assertMoneyChangesNothing(t, source)
		})
	}
}

// A literal the context turned into a rate or money runs as that value.
func TestSettledLiteralsRunAsTheirKind(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		source string
		args   map[string]any
		want   string
	}{
		{"usd * 0.1", map[string]any{"usd": "USD 10.00", "r": "0.4"}, "{USD 100}"},
		{"usd * 1e-2", map[string]any{"usd": "USD 10.00", "r": "0.4"}, "{USD 10}"},
		{"r < 0.5", map[string]any{"usd": "USD 1.00", "r": "0.4"}, "true"},
		{"r + 1.0", map[string]any{"usd": "USD 1.00", "r": "0.4"}, "1.4"},
		{"1.0 / r", map[string]any{"usd": "USD 1.00", "r": "0.4"}, "2.5"},
		{"if(usd > 0, usd, 0)", map[string]any{"usd": "USD -1.00", "r": "0"}, "{USD 0}"},
		{"{r: 0.5}.r * usd", map[string]any{"usd": "USD 3.00", "r": "0"}, "{USD 150}"},
		{"0.1 + 0.2", map[string]any{"usd": "USD 1.00", "r": "0"}, "0.30000000000000004"},
		{"0.1 + 0.2 + r", map[string]any{"usd": "USD 1.00", "r": "0"}, "0.3"},
	} {
		t.Run(test.source, func(t *testing.T) {
			t.Parallel()
			got, err := runMoney(t, test.source, "usd: money<USD>; r: rate", test.args)
			if err != nil || got != test.want {
				t.Fatalf("%s with %v = %s, %v, want %s", test.source, test.args, got, err, test.want)
			}
		})
	}
}

// 150 JPY / USD is an exchange rate written in figures: exact, whatever
// places its currencies have, and from a currency to itself only 1.
func TestExchangeRateLiteralsAreExact(t *testing.T) {
	t.Parallel()
	for source, want := range map[string]string{
		"150 JPY / USD":              "USD/JPY 150",
		"150.25 JPY / USD":           "USD/JPY 150.25",
		"150.123456789012 JPY / USD": "USD/JPY 150.123456789012",
		"0.0067 USD / JPY":           "JPY/USD 0.0067",
		"0.307550 KWD / EUR":         "EUR/KWD 0.30755",
		"1 USD / USD":                "USD/USD 1",
		"1.000 JPY / JPY":            "JPY/JPY 1",
		"[150 JPY / USD, fx]":        "[USD/JPY 150 USD/JPY 160]",
		"150 JPY / USD == fx":        "false",
		"160.0 JPY / USD == fx":      "true",
	} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			got, err := runMoney(t, source, "fx: fxrate<USD,JPY>", map[string]any{"fx": map[string]any{"base": "USD", "quote": "JPY", "rate": "160"}})
			if err != nil || got != want {
				t.Fatalf("%s = %s, %v, want %s", source, got, err, want)
			}
		})
	}
}

func TestExchangeRateLiteralsOutOfRangeAreCompileErrors(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		source string
		money  bool
		want   string
	}{
		{"150 USD / USD", true, "from a currency to itself it is 1"},
		{"150 GBP / USD", true, `"GBP" is not declared`},
		{"150 JPY / GBP", true, `"GBP" is not declared`},
		{"0 JPY / USD", true, "not positive"},
		{"0.0 JPY / USD", true, "not positive"},
		{"1.00000000000000000000000000000000000000001 JPY / USD", true, "at most 40 digits"},
		{"150 JPY / USD == 150 USD / JPY", true, "no overload"},
		{"150 JPY / USD", false, "declares no money"},
	} {
		t.Run(test.source, func(t *testing.T) {
			t.Parallel()
			registry := moneyRegistry(t)
			if !test.money {
				registry = consoleRegistry(t)
			}
			_, err := CompileExpr(test.source, registry, CompileOptions{})
			if !errors.Is(err, machine.ErrCompile) || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("CompileExpr(%q) error = %v, want ErrCompile containing %q", test.source, err, test.want)
			}
		})
	}
}

// A literal rate is one constant, and it survives the artifact's JSON.
func TestAnExchangeRateLiteralIsAConstant(t *testing.T) {
	t.Parallel()
	artifact, err := compileMoney(t, "150.255 JPY / USD", "", "")
	if err != nil {
		t.Fatal(err)
	}
	parts := machine.PartsOf(artifact)
	if len(parts.Instructions) != 1 || parts.Instructions[0].Op != machine.OpConstant || artifact.Result().String() != "fxrate<USD,JPY>" {
		t.Fatalf("150.255 JPY / USD compiles to %+v -> %s, want one constant of fxrate<USD,JPY>", parts.Instructions, artifact.Result())
	}
	encoded, err := json.Marshal(artifact)
	if err != nil {
		t.Fatal(err)
	}
	var back machine.Artifact
	if err := json.Unmarshal(encoded, &back); err != nil {
		t.Fatalf("Unmarshal(%s) = %v", encoded, err)
	}
	if got, err := runArtifact(t, &back, nil); err != nil || got != "USD/JPY 150.255" {
		t.Fatalf("the artifact read back = %s, %v, want USD/JPY 150.255", got, err)
	}
}

// minor(...) gives an int like any other, and what a rule makes of it is the
// rule's to answer for: money(minor(a), JPY) compiles and runs.
func TestMinorUnitsAreAnIntLikeAnyOther(t *testing.T) {
	t.Parallel()
	got, err := runMoney(t, "money(minor(a), JPY)", "a: money<USD>", map[string]any{"a": "USD 1.70"})
	if err != nil || got != "{JPY 170}" {
		t.Fatalf("money(minor(USD 1.70), JPY) = %s, %v, want JPY 170", got, err)
	}
}
