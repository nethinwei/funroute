package compile

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"

	"funroute/lang/internal/machine"
)

// moneyRegistry is a console with money declared: two-place dollars and
// euros, zero-place yen and three-place dinars, rounding half up.
func moneyRegistry(t testing.TB) *machine.Registry {
	t.Helper()
	registry := machine.CoreRegistry()
	if err := registry.EnableForm(machine.SwitchForm, machine.ForForm, machine.ReduceForm); err != nil {
		t.Fatal(err)
	}
	err := registry.DeclareMoney(machine.MoneySpec{Rounding: machine.RoundHalfUp, Currencies: []machine.CurrencySpec{
		{Code: "USD", Digits: 2}, {Code: "EUR", Digits: 2}, {Code: "JPY", Digits: 0}, {Code: "KWD", Digits: 3},
	}})
	if err != nil {
		t.Fatal(err)
	}
	return registry
}

// moneyContract reads "name: type; name: type" into arguments.
func moneyContract(t testing.TB, text string) []ArgSpec {
	t.Helper()
	var args []ArgSpec
	for part := range strings.SplitSeq(text, ";") {
		name, typeText, ok := strings.Cut(strings.TrimSpace(part), ":")
		if !ok {
			continue
		}
		typ, err := machine.ParseType(strings.TrimSpace(typeText))
		if err != nil {
			t.Fatalf("contract %q: %v", text, err)
		}
		args = append(args, ArgSpec{Name: strings.TrimSpace(name), Type: typ})
	}
	return args
}

// runMoney compiles source under the contract and runs it, converting
// through rateTable; the result prints as fmt does, so money reads {USD 170}.
func runMoney(t *testing.T, source, contract string, args map[string]any) (string, error) {
	t.Helper()
	registry := moneyRegistry(t)
	artifact, err := CompileExpr(source, registry, CompileOptions{Args: moneyContract(t, contract)})
	if err != nil {
		return "", err
	}
	runtime, err := machine.Instantiate(artifact, registry)
	if err != nil {
		t.Fatalf("Instantiate(%q) error = %v", source, err)
	}
	value, err := runtime.Run(t.Context(), args, machine.RunOptions{Fuel: 10_000, Rates: rateTable(t, registry)})
	if err != nil {
		return "", err
	}
	return fmt.Sprint(value.Any()), nil
}

func TestMoneyArithmeticRuns(t *testing.T) {
	t.Parallel()
	for name, test := range map[string]struct {
		source, contract string
		args             map[string]any
		want             string
	}{
		"a decimal is a rate beside money": {"amount * 0.029", "amount: money<USD>", map[string]any{"amount": "USD 100.00"}, "{USD 290}"},
		"a count keeps the amount exact":   {"amount * 2 + money(30, USD)", "amount: money<USD>", map[string]any{"amount": "USD 100.00"}, "{USD 20030}"},
		"one currency variable":            {"amount + fee", "amount: money<c>; fee: money<c>", map[string]any{"amount": "EUR 1.00", "fee": "EUR 0.30"}, "{EUR 130}"},
		"zero widens to money":             {"if(flag, fee, 0)", "flag: bool; fee: money<USD>", map[string]any{"flag": false, "fee": "USD 1.00"}, "{USD 0}"},
		"money compares with zero":         {"amount > 0", "amount: money<c>", map[string]any{"amount": "JPY 5"}, "true"},
		"a fee over an amount is a rate":   {"fee / amount", "amount: money<c>; fee: money<c>", map[string]any{"amount": "USD 100.00", "fee": "USD 2.90"}, "0.029"},
		"converting rescales the places":   {"amount -> JPY", "amount: money<USD>", map[string]any{"amount": "USD 100.00"}, "{JPY 15050}"},
		"a markup is a sum of rates":       {"amount * (100% + 2.5%)", "amount: money<USD>", map[string]any{"amount": "USD 10.00"}, "{USD 1025}"},
	} {
		if strings.Contains(test.source, "%") {
			test.source = strings.ReplaceAll(test.source, "2.5%", "0.025")
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, err := runMoney(t, test.source, test.contract, test.args)
			if err != nil || got != test.want {
				t.Fatalf("%s = %s, %v, want %s", test.source, got, err, test.want)
			}
		})
	}
}

func TestProvenCurrenciesThatDifferDoNotCompile(t *testing.T) {
	t.Parallel()
	for name, test := range map[string]struct{ source, contract string }{
		"a variable meets a code": {"amount + money(1, USD)", "amount: money<c>"},
		"two codes":               {"usd + eur", "usd: money<USD>; eur: money<EUR>"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := CompileExpr(test.source, moneyRegistry(t), CompileOptions{Args: moneyContract(t, test.contract)})
			if err == nil || !strings.Contains(err.Error(), "no overload") {
				t.Fatalf("CompileExpr(%q) error = %v, want a currency mismatch", test.source, err)
			}
		})
	}
}

func TestBranchesOfDifferentCurrenciesMeetAtRunTime(t *testing.T) {
	t.Parallel()
	registry := moneyRegistry(t)
	artifact, err := CompileExpr("if(flag, usd, eur)", registry, CompileOptions{Args: moneyContract(t, "flag: bool; usd: money<USD>; eur: money<EUR>")})
	if err != nil {
		t.Fatal(err)
	}
	if want := machine.MoneyOf(""); !artifact.Result().Equal(want) {
		t.Fatalf("result type = %s, want %s", artifact.Result(), want)
	}
	_, err = runMoney(t, "if(flag, usd, eur) + usd", "flag: bool; usd: money<USD>; eur: money<EUR>",
		map[string]any{"flag": false, "usd": "USD 1.00", "eur": "EUR 1.00"})
	if !errors.Is(err, machine.ErrCurrency) {
		t.Fatalf("euros plus dollars error = %v, want ErrCurrency", err)
	}
}

func TestADecimalStaysAFloatUnlessMoneyAsks(t *testing.T) {
	t.Parallel()
	artifact, err := CompileExpr("risk < 0.5", moneyRegistry(t), CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got := artifact.Args()[0].Type(); !got.Equal(machine.FloatType) {
		t.Fatalf("risk < 0.5 gives risk %s, want float", got)
	}
}

func TestContractArgumentsDisagreeingIsBothErrors(t *testing.T) {
	t.Parallel()
	_, err := runMoney(t, "amount + fee", "amount: money<c>; fee: money<c>", map[string]any{"amount": "USD 1.00", "fee": "EUR 1.00"})
	if !errors.Is(err, machine.ErrContract) || !errors.Is(err, machine.ErrCurrency) {
		t.Fatalf("USD and EUR bound to c: error = %v, want ErrContract and ErrCurrency", err)
	}
}

// compileMoney compiles source on the money registry under the contract and,
// when result is not empty, the declared result type.
func compileMoney(t *testing.T, source, contract, result string) (*machine.Artifact, error) {
	t.Helper()
	options := CompileOptions{Args: moneyContract(t, contract)}
	if result != "" {
		typ, err := machine.ParseType(result)
		if err != nil {
			t.Fatalf("result %q: %v", result, err)
		}
		options.Result = &typ
	}
	return CompileExpr(source, moneyRegistry(t), options)
}

// runArtifact runs an artifact the money registry compiled, converting
// through rateTable; the result prints as runMoney's does.
func runArtifact(t *testing.T, artifact *machine.Artifact, args map[string]any) (string, error) {
	t.Helper()
	registry := moneyRegistry(t)
	runtime, err := machine.Instantiate(artifact, registry)
	if err != nil {
		t.Fatalf("Instantiate error = %v", err)
	}
	value, err := runtime.Run(t.Context(), args, machine.RunOptions{Fuel: 10_000, Rates: rateTable(t, registry)})
	if err != nil {
		return "", err
	}
	return fmt.Sprint(value.Any()), nil
}

// unitContract names one value in each state a unit can be in: a code, a
// contract variable, and unknown until the run.
const unitContract = "usd: money<USD>; eur: money<EUR>; a: money<c>; b: money<d>; u: money<?>; v: money<?>"

// unitMismatch binds unitContract so that every unknown operand the matrices
// run disagrees with the other side: c is USD and d is JPY, u is euros, v yen.
func unitMismatch() map[string]any {
	return map[string]any{"usd": "USD 1.00", "eur": "EUR 1.00", "a": "USD 1.00", "b": "JPY 1", "u": "EUR 1.00", "v": "JPY 1"}
}

// unitVerdict is what a program meeting two units comes to.
type unitVerdict int

const (
	unitProven   unitVerdict = iota // compiles, and the run needs no luck
	unitRefused                     // two proven currencies differ: a compile error
	unitDeferred                    // compiles; the run compares the currencies
)

// checkUnitVerdict compiles source under unitContract and holds it to the
// verdict: a refusal names no overload, a program that compiles has the
// result type want, and a deferred one fails with ErrCurrency when run on
// unitMismatch while a proven one runs.
func checkUnitVerdict(t *testing.T, source string, verdict unitVerdict, want string) {
	t.Helper()
	artifact, err := compileMoney(t, source, unitContract, "")
	if verdict == unitRefused {
		if err == nil || !strings.Contains(err.Error(), "no overload") {
			t.Fatalf("CompileExpr(%q) error = %v, want no overload", source, err)
		}
		return
	}
	if err != nil {
		t.Fatalf("CompileExpr(%q) error = %v, want it to compile", source, err)
	}
	if got := artifact.Result().String(); got != want {
		t.Fatalf("CompileExpr(%q) result = %s, want %s", source, got, want)
	}
	_, err = runArtifact(t, artifact, unitMismatch())
	if verdict == unitDeferred && !errors.Is(err, machine.ErrCurrency) {
		t.Fatalf("%s on mismatched currencies: error = %v, want ErrCurrency", source, err)
	}
	if verdict == unitProven && err != nil {
		t.Fatalf("%s on proven currencies: error = %v, want none", source, err)
	}
}

// Addition, subtraction and ordering demand one currency: two proven units
// that differ never compile — two codes, or a code and a contract variable —
// two proven units that agree need no luck, and two contract variables or an
// unknown one on either side leave the comparison to the run.
func TestUnitStatesMeetInAdditionAndOrdering(t *testing.T) {
	t.Parallel()
	pairs := []struct {
		left, right string
		verdict     unitVerdict
		sum         string
	}{
		{"usd", "usd", unitProven, "money<USD>"},
		{"usd", "eur", unitRefused, ""},
		{"usd", "a", unitRefused, ""},
		{"a", "usd", unitRefused, ""},
		{"a", "a", unitProven, "money<c>"},
		{"a", "b", unitDeferred, "money<?>"},
		{"usd", "u", unitDeferred, "money<?>"},
		{"u", "usd", unitDeferred, "money<?>"},
		{"a", "u", unitDeferred, "money<?>"},
		{"u", "a", unitDeferred, "money<?>"},
		{"u", "v", unitDeferred, "money<?>"},
	}
	for _, pair := range pairs {
		for _, op := range []string{"+", "-", "<", "<=", ">", ">="} {
			want := pair.sum
			if op != "+" && op != "-" && want != "" {
				want = "bool"
			}
			source := pair.left + " " + op + " " + pair.right
			t.Run(source, func(t *testing.T) {
				t.Parallel()
				checkUnitVerdict(t, source, pair.verdict, want)
			})
		}
	}
}

// A conversion's result is in the currency it converts to, whatever the
// amount's; combined with other money it meets that currency.
func TestUnitStatesMeetInConversion(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		source  string
		verdict unitVerdict
		want    string
	}{
		{"usd -> JPY", unitProven, "money<JPY>"},
		{"a -> EUR", unitProven, "money<EUR>"},
		{"u -> JPY", unitProven, "money<JPY>"},
		{"usd -> USD", unitProven, "money<USD>"},
		{"(usd -> EUR) + eur", unitProven, "money<EUR>"},
		{"(u -> USD) + usd", unitProven, "money<USD>"},
		{"(usd -> JPY) + usd", unitRefused, ""},
		{"(a -> EUR) < a", unitRefused, ""},
		{"(usd -> EUR) + v", unitDeferred, "money<?>"},
	} {
		t.Run(test.source, func(t *testing.T) {
			t.Parallel()
			checkUnitVerdict(t, test.source, test.verdict, test.want)
		})
	}
}

// Money over money is a rate when the currencies are one or unknown; proven
// apart it has no answer, since there is no exchange rate value.
func TestMoneyOverMoney(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		source  string
		verdict unitVerdict
		want    string
	}{
		{"usd / usd", unitProven, "rate"},
		{"a / a", unitProven, "rate"},
		{"u / v", unitDeferred, "rate"},
		{"usd / u", unitDeferred, "rate"},
		{"u / usd", unitDeferred, "rate"},
	} {
		t.Run(test.source, func(t *testing.T) {
			t.Parallel()
			checkUnitVerdict(t, test.source, test.verdict, test.want)
		})
	}
}

// Two amounts not proven to share a currency divide into the exchange rate
// between theirs: how much of the numerator's one unit of the other buys.
func TestMoneyOverOtherMoneyIsAnExchangeRate(t *testing.T) {
	t.Parallel()
	for source, want := range map[string]string{
		"usd / eur": "fxrate<EUR,USD>", "a / b": "fxrate<d,c>", "a / usd": "fxrate<USD,c>", "usd / a": "fxrate<c,USD>",
	} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			artifact, err := compileMoney(t, source, unitContract, "")
			if err != nil || artifact.Result().String() != want {
				t.Fatalf("CompileExpr(%q) = %v (%v), want %s", source, artifact, err, want)
			}
		})
	}
}

// -> takes the target's currency: a written code, a contract currency, or
// one known only at run time; the declared result holds it to a code.
func TestConversionTakesTheTargetsCurrency(t *testing.T) {
	t.Parallel()
	const contract = "usd: money<USD>; u: money<?>; a: money<c>; cur: currency<?>; to: currency<e>"
	for _, test := range []struct{ source, result, want string }{
		{"usd -> JPY", "", "money<JPY>"},
		{"u -> JPY", "", "money<JPY>"},
		{"usd -> cur", "", "money<?>"},
		{"a -> to", "", "money<e>"},
		{"usd + usd -> JPY", "", "money<JPY>"},
		{"usd -> JPY > 0", "", "bool"},
		{"[x -> EUR for x in [usd, u]]", "", "array<money<EUR>>"},
		{"usd -> JPY", "money<JPY>", "money<JPY>"},
		{"usd -> cur", "money<JPY>", "money<JPY>"},
		{"usd -> JPY", "money<EUR>", "error: the contract returns money<EUR> but the expression is in JPY"},
	} {
		t.Run(test.source+" as "+test.result, func(t *testing.T) {
			t.Parallel()
			if got := resultOrError(t, test.source, contract, test.result); !strings.HasPrefix(got, test.want) {
				t.Fatalf("CompileExpr(%q) as %s = %s, want %s", test.source, test.result, got, test.want)
			}
		})
	}
}

// resultOrError is the result type source compiles to, or "error: " and the
// type error's message.
func resultOrError(t *testing.T, source, contract, result string) string {
	t.Helper()
	artifact, err := compileMoney(t, source, contract, result)
	if err != nil {
		_, message, _ := strings.Cut(err.Error(), "type error: ")
		return "error: " + message
	}
	return artifact.Result().String()
}

// Equality with an unknown side is compared at run time like ordering is.
func TestEqualityWithAnUnknownCurrencyIsCheckedAtRunTime(t *testing.T) {
	t.Parallel()
	for _, source := range []string{"usd == usd", "usd == 0", "a != a", "0 == u"} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			checkUnitVerdict(t, source, unitProven, "bool")
		})
	}
	for source, want := range map[string]string{"usd == u": "bool", "u != a": "bool", "switch(u, case USD 1.00 => 1, else => 2)": "int"} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			checkUnitVerdict(t, source, unitDeferred, want)
		})
	}
}

// Two proven currencies that differ cannot be equal or unequal: the run
// refuses the question with ErrCurrency, so the compiler should refuse it
// the way it refuses usd < eur.
func TestEqualityOfProvenCurrenciesThatDiffer(t *testing.T) {
	t.Parallel()
	for _, source := range []string{
		"usd == eur", "usd != eur", "a == usd", "[usd] == [eur]", "[USD 1.00] == [EUR 1.00]",
		`{"k": usd} == {"k": eur}`, "{fee: usd} == {fee: eur}",
		"switch(usd, case EUR 1.00 => 1, else => 2)",
	} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			_, err := compileMoney(t, source, unitContract, "")
			if err == nil {
				t.Fatalf("CompileExpr(%q) compiled, want a currency mismatch", source)
			}
		})
	}
}

// Amounts inside containers are compared as the scalars are: dollars against
// euros has no answer, so the run should refuse it rather than say false.
func TestContainerEqualityAcrossCurrencies(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		source string
		args   map[string]any
	}{
		{"[usd] == [u]", map[string]any{"usd": "USD 1.00", "u": "EUR 1.00"}},
		{`{"k": usd} == {"k": u}`, map[string]any{"usd": "USD 1.00", "u": "EUR 1.00"}},
	} {
		t.Run(test.source, func(t *testing.T) {
			t.Parallel()
			got, err := runMoney(t, test.source, "usd: money<USD>; u: money<?>", test.args)
			if !errors.Is(err, machine.ErrCurrency) {
				t.Fatalf("%s = %s, %v, want ErrCurrency", test.source, got, err)
			}
		})
	}
}

// Where branches meet — if, every switch branch and its default, the items
// of an array or a dictionary, a reduce accumulator and its body, fallback —
// currencies that differ make one the run decides, not a compile error;
// currencies that agree stay proven.
func TestJunctionsForgetCurrenciesThatDiffer(t *testing.T) {
	t.Parallel()
	for source, want := range map[string]string{
		"switch(k, case 1 => usd, case 2 => eur, else => usd)": "money<?>",
		"switch(k, case 1 => usd, else => eur)":                "money<?>",
		"switch(case f => usd, else => eur)":                   "money<?>",
		"switch(k, case 1 => a, else => a)":                    "money<c>",
		"[usd, eur]":                                           "array<money<?>>",
		"[usd, usd]":                                           "array<money<USD>>",
		`{"x": usd, "y": eur}`:                                 "dict<money<?>>",
		`{"x": a, "y": a}`:                                     "dict<money<c>>",
		"reduce(x in eurs, last = usd, x)":                     "money<?>",
		"reduce(x in eurs, last = eur, x)":                     "money<EUR>",
		"fallback(usd, eur)":                                   "money<?>",
		"fallback(a, a, 0)":                                    "money<c>",
		"[if(f, usd, eur) for x in eurs]":                      "array<money<?>>",
		"{string(currency(x)): x for x in eurs}":               "dict<money<EUR>>",
		"{fee: if(f, usd, eur)}":                               "record{fee: money<?>}",
		"if(f, usd, 0)":                                        "money<USD>",
		"if(f, 0, a)":                                          "money<c>",
		"[0, usd]":                                             "array<money<USD>>",
	} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			artifact, err := compileMoney(t, source, "usd: money<USD>; eur: money<EUR>; a: money<c>; f: bool; k: int; eurs: array<money<EUR>>", "")
			if err != nil {
				t.Fatalf("CompileExpr(%q) error = %v, want it to compile", source, err)
			}
			if got := artifact.Result().String(); got != want {
				t.Fatalf("CompileExpr(%q) result = %s, want %s", source, got, want)
			}
		})
	}
}

// What a junction forgot is checked where the value is next combined.
func TestAForgottenCurrencyIsCheckedWhereItIsUsed(t *testing.T) {
	t.Parallel()
	const contract = "usd: money<USD>; eur: money<EUR>; f: bool; k: int; eurs: array<money<EUR>>"
	one := []any{"EUR 1.00"}
	for _, test := range []struct {
		source string
		args   map[string]any
		want   string // "" for ErrCurrency
	}{
		{"if(f, usd, eur) + money(1, EUR)", map[string]any{"f": false}, "{EUR 101}"},
		{"if(f, usd, eur) + money(1, EUR)", map[string]any{"f": true}, ""},
		{"switch(k, case 1 => usd, else => eur) + eur", map[string]any{"k": 2}, "{EUR 200}"},
		{"switch(k, case 1 => usd, else => eur) + eur", map[string]any{"k": 1}, ""},
		{"reduce(x in eurs, last = usd, x) + usd", map[string]any{"eurs": []any{}}, "{USD 200}"},
		{"reduce(x in eurs, last = usd, x) + usd", map[string]any{"eurs": one}, ""},
		{"fallback(usd, eur) < eur", map[string]any{}, ""},
		{"[usd, eur][1] - eur", map[string]any{}, "{EUR 0}"},
	} {
		t.Run(fmt.Sprint(test.source, test.args), func(t *testing.T) {
			t.Parallel()
			args := map[string]any{"usd": "USD 1.00", "eur": "EUR 1.00", "f": false, "k": 0, "eurs": one}
			maps.Copy(args, test.args)
			got, err := runMoney(t, test.source, contract, args)
			if test.want == "" && !errors.Is(err, machine.ErrCurrency) {
				t.Fatalf("%s = %s, %v, want ErrCurrency", test.source, got, err)
			}
			if test.want != "" && (err != nil || got != test.want) {
				t.Fatalf("%s = %s, %v, want %s", test.source, got, err, test.want)
			}
		})
	}
}

// Each read of a contract argument has units of its own: a junction that
// forgets one read's currency leaves the next read proven.
func TestContractReadsDoNotTaintEachOther(t *testing.T) {
	t.Parallel()
	const contract = "usd: money<USD>; eur: money<EUR>; a: money<c>; f: bool"
	artifact, err := compileMoney(t, "let(m = if(f, usd, eur), {mixed: m, own: usd})", contract, "")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := artifact.Result().String(), "record{mixed: money<?>, own: money<USD>}"; got != want {
		t.Fatalf("result = %s, want %s", got, want)
	}
	for _, source := range []string{
		"let(m = if(f, usd, eur), usd + eur)",
		"let(m = if(f, a, usd), a + money(1, USD))",
		"if(f, usd, eur) == 0 || usd < eur",
		"[if(f, a, usd), a + usd]",
	} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			if _, err := compileMoney(t, source, contract, ""); err == nil || !strings.Contains(err.Error(), "no overload") {
				t.Fatalf("CompileExpr(%q) error = %v, want the later read still proven", source, err)
			}
		})
	}
}

// Every currency variable an inferred result names is one an argument binds:
// a variable nothing binds publishes as unknown.
func TestResultUnitsComeFromArguments(t *testing.T) {
	t.Parallel()
	const contract = "a: money<c>; b: money<d>; cur: currency<e>; minor: int; s: string"
	for source, want := range map[string]string{
		"a -> cur":                        "money<e>",
		"b -> currency(a)":                "money<c>",
		"like(a, 5)":                      "money<c>",
		"currency(b)":                     "currency<d>",
		"money(minor, cur)":               "money<e>",
		"money(minor, currency(a))":       "money<c>",
		"money(minor, currency_of(s))":    "money<?>",
		"allocate(0, 3)":                  "array<money<?>>",
		"allocate(a, [1, 2])":             "array<money<c>>",
		"{d: currency(b), fee: a -> cur}": "record{d: currency<d>, fee: money<e>}",
	} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			artifact, err := compileMoney(t, source, contract, "")
			if err != nil {
				t.Fatalf("CompileExpr(%q) error = %v", source, err)
			}
			if got := artifact.Result().String(); got != want {
				t.Fatalf("CompileExpr(%q) result = %s, want %s", source, got, want)
			}
			if unit := unboundResultUnit(artifact); unit != "" {
				t.Fatalf("CompileExpr(%q) result names %s, which no argument of %v binds", source, unit, artifact.Args())
			}
		})
	}
}

// unboundResultUnit is a currency variable the artifact's result names and
// no argument binds, or "".
func unboundResultUnit(artifact *machine.Artifact) string {
	bound := machine.UnitVariables(artifact.Args())
	for _, unit := range machine.UnitVariables([]machine.Parameter{machine.NewParameter("", artifact.Result(), "")}) {
		if !slices.Contains(bound, unit) {
			return unit
		}
	}
	return ""
}

func TestUnitInfoJoins(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name        string
		left, right unitInfo
		want        unitInfo
		published   string
	}{
		{"one code", unitInfo{known: "USD"}, unitInfo{known: "USD"}, unitInfo{known: "USD"}, "USD"},
		{"two codes", unitInfo{known: "USD"}, unitInfo{known: "EUR"}, unitInfo{known: "USD", dyn: true}, ""},
		{"a variable and a code", unitInfo{known: "c"}, unitInfo{known: "USD"}, unitInfo{known: "c", dyn: true}, ""},
		{"nothing known takes the other", unitInfo{}, unitInfo{known: "c"}, unitInfo{known: "c"}, "c"},
		{"unknown stays unknown", unitInfo{dyn: true}, unitInfo{known: "USD"}, unitInfo{known: "USD", dyn: true}, ""},
		{"both open", unitInfo{}, unitInfo{}, unitInfo{}, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got := test.left.join(test.right)
			if got != test.want || got.published() != test.published {
				t.Fatalf("%+v.join(%+v) = %+v publishing %q, want %+v publishing %q", test.left, test.right, got, got.published(), test.want, test.published)
			}
			if flipped := test.right.join(test.left); flipped.dyn != got.dyn || flipped.published() != got.published() {
				t.Fatalf("join is not symmetric: %+v and %+v", got, flipped)
			}
		})
	}
	if got := unitFor(""); got != (unitInfo{dyn: true}) {
		t.Fatalf(`unitFor("") = %+v, want unknown`, got)
	}
}

func TestProvenUnitsListsWhatMeets(t *testing.T) {
	t.Parallel()
	usd, eur, unknown := unitInfo{known: "USD"}, unitInfo{known: "EUR"}, unitInfo{known: "EUR", dyn: true}
	for _, test := range []struct {
		name  string
		unit  string
		infos []unitInfo
		want  string
	}{
		{"an empty unit demands nothing", "", []unitInfo{usd, eur}, "[]"},
		{"a code counts itself", "USD", []unitInfo{eur}, "[USD EUR]"},
		{"a code met by itself", "USD", []unitInfo{usd}, "[USD]"},
		{"a variable met once each", "u", []unitInfo{usd, eur}, "[USD EUR]"},
		{"a variable met twice by one", "u", []unitInfo{usd, usd}, "[USD]"},
		{"unknown operands prove nothing", "u", []unitInfo{usd, unknown}, "[USD]"},
		{"a type variable is no unit", "T", []unitInfo{usd, eur}, "[]"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := fmt.Sprint(provenUnits(test.unit, test.infos)); got != test.want {
				t.Fatalf("provenUnits(%q, %v) = %s, want %s", test.unit, test.infos, got, test.want)
			}
		})
	}
}

// Membership compares a value with the items it looks among, as == compares
// two values: proven currencies that differ are a compile error, and an
// unproven one is checked when the run meets it.
func TestMembershipComparesCurrenciesAsEqualityDoes(t *testing.T) {
	t.Parallel()
	const contract = "usd: money<USD>; eur: money<EUR>; xs: array<money<?>>"
	for _, source := range []string{"usd in [eur]", "member(usd, [eur, eur])"} {
		if _, err := compileMoney(t, source, contract, ""); err == nil {
			t.Errorf("CompileExpr(%q) compiled, want a currency mismatch", source)
		}
	}
	if _, err := compileMoney(t, "usd in xs", contract, ""); err != nil {
		t.Fatalf("CompileExpr(usd in xs) error = %v, want it left to the run", err)
	}
	_, err := runMoney(t, "usd in xs", "usd: money<USD>; xs: array<money<?>>", map[string]any{"usd": "USD 1.00", "xs": []any{"EUR 1.00"}})
	if !errors.Is(err, machine.ErrCurrency) {
		t.Fatalf("USD 1.00 in [EUR 1.00] error = %v, want ErrCurrency", err)
	}
}

// Two contract variables are two currencies the run may bind alike: a
// payment refunded in its own currency passes c and d the same. So where
// they meet compiles and the run compares them — a host function's
// arguments with a group check, the kernel in its own operation, the result
// with the check at the end — and money<c> / money<d> is still read as the
// exchange rate it was written as.
func TestTwoContractVariablesMeetAtRunTime(t *testing.T) {
	t.Parallel()
	const contract = "a: money<c>; b: money<d>"
	for _, source := range []string{"a + b", "a < b", "a == b", "if(a > b, a, b)"} {
		if got, err := runMoney(t, source, contract, map[string]any{"a": "USD 1.00", "b": "USD 2.00"}); err != nil {
			t.Errorf("%s with c and d both USD = %s, %v, want it to run", source, got, err)
		}
		if _, err := runMoney(t, source, contract, map[string]any{"a": "USD 1.00", "b": "EUR 2.00"}); !errors.Is(err, machine.ErrCurrency) {
			t.Errorf("%s with c USD and d EUR: error = %v, want ErrCurrency", source, err)
		}
	}
	artifact, err := compileMoney(t, "a / b", contract, "")
	if err != nil || artifact.Result().String() != "fxrate<d,c>" {
		t.Fatalf("CompileExpr(a / b) = %v, %v, want fxrate<d,c>", artifact.Result(), err)
	}
	if _, err := compileMoney(t, "a + b", contract, "money<c>"); err != nil {
		t.Fatalf("CompileExpr(a + b) returning money<c>: %v, want the check left to the run", err)
	}
	if _, err := compileMoney(t, "a + USD 1.00", contract, ""); err == nil {
		t.Fatal("CompileExpr(a + USD 1.00) compiled, want a code against a variable refused")
	}
}
