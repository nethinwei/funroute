package machine

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func declared(t *testing.T, rounding Rounding, currencies ...CurrencySpec) *Registry {
	t.Helper()
	registry := CoreRegistry()
	if err := registry.DeclareMoney(MoneySpec{Rounding: rounding, Currencies: currencies}); err != nil {
		t.Fatal(err)
	}
	return registry
}

func TestDeclareMoneyRefusesWhatCannotBeRepresented(t *testing.T) {
	t.Parallel()
	for name, test := range map[string]struct {
		spec MoneySpec
		want string
	}{
		"no rounding":       {MoneySpec{Currencies: []CurrencySpec{{Code: "USD", Digits: 2}}}, "rounding"},
		"no currency":       {MoneySpec{Rounding: RoundHalfUp}, "at least one"},
		"a lower-case code": {MoneySpec{Rounding: RoundHalfUp, Currencies: []CurrencySpec{{Code: "usd", Digits: 2}}}, "invalid currency code"},
		"too many places":   {MoneySpec{Rounding: RoundHalfUp, Currencies: []CurrencySpec{{Code: "BTC", Digits: 9}}}, "0 to 8"},
		"a code twice":      {MoneySpec{Rounding: RoundHalfUp, Currencies: []CurrencySpec{{Code: "USD", Digits: 2}, {Code: "USD", Digits: 0}}}, "twice"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if err := CoreRegistry().DeclareMoney(test.spec); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("DeclareMoney error = %v, want one containing %q", err, test.want)
			}
		})
	}
	registry := declared(t, RoundHalfUp, CurrencySpec{Code: "USD", Digits: 2})
	if err := registry.DeclareMoney(MoneySpec{Rounding: RoundHalfUp, Currencies: []CurrencySpec{{Code: "EUR", Digits: 2}}}); err == nil {
		t.Fatal("a second DeclareMoney succeeded, want an error")
	}
}

// A table's identity is its currencies and their places: the order they are
// listed in does not change it, nor does the rounding, which a rate table
// and the registry it converts for need not share; the places do.
func TestTheIdentityNamesTheCurrencies(t *testing.T) {
	t.Parallel()
	usd, eur := CurrencySpec{Code: "USD", Digits: 2}, CurrencySpec{Code: "EUR", Digits: 2}
	identity := func(rounding Rounding, currencies ...CurrencySpec) string {
		return declared(t, rounding, currencies...).currencies().identity
	}
	first := identity(RoundHalfUp, usd, eur)
	if first != identity(RoundHalfUp, eur, usd) || first != identity(RoundHalfEven, usd, eur) || first == identity(RoundHalfUp, usd, CurrencySpec{Code: "EUR", Digits: 3}) {
		t.Fatal("identity: want the same for another order or rounding, another for other places")
	}
	digest, ok := strings.CutPrefix(first, "sha256:")
	if !ok || len(digest) != 64 {
		t.Fatalf("identity = %s, want sha256: with 64 hex digits", first)
	}
}

func TestMoneyTypesNeedTheDeclaration(t *testing.T) {
	t.Parallel()
	spec := FunctionSpec{Name: "fee_v1", Params: []Type{MoneyOf("u")}, Result: MoneyOf("u"), Eval: evalIntAdd}
	if err := CoreRegistry().Register(spec); err == nil {
		t.Fatal("a money signature registered without money declared")
	}
	registry := declared(t, RoundHalfUp, CurrencySpec{Code: "USD", Digits: 2})
	if err := registry.Register(spec); err != nil {
		t.Fatal(err)
	}
	spec.Name, spec.Params, spec.Result = "fee_v2", []Type{MoneyOf("GBP")}, MoneyOf("GBP")
	if err := registry.Register(spec); !errors.Is(err, ErrCurrency) {
		t.Fatalf("an undeclared currency in a signature: error = %v, want ErrCurrency", err)
	}
}

// A manifest carries the money feature, and applying it declares the same
// table the host has.
func TestManifestCarriesMoney(t *testing.T) {
	t.Parallel()
	host := declared(t, RoundHalfUp, CurrencySpec{Code: "USD", Digits: 2}, CurrencySpec{Code: "JPY", Digits: 0})
	manifest := host.Manifest()
	server := CoreRegistry()
	if err := manifest.Apply(server); err != nil {
		t.Fatal(err)
	}
	want, _ := host.Money()
	if got, ok := server.Money(); !ok || !reflect.DeepEqual(got, want) {
		t.Fatalf("money after Apply = %v, want %v", got, want)
	}
	if err := manifest.Apply(declared(t, RoundDown, CurrencySpec{Code: "USD", Digits: 2})); err == nil {
		t.Fatal("a manifest applied over other money, want an error")
	}
}

// The checks before one call keep a slot per currency variable, so a
// signature with more of them than that is refused where it is registered.
func TestASignatureHasAtMostEightCurrencyVariables(t *testing.T) {
	t.Parallel()
	registry := declared(t, RoundHalfUp, CurrencySpec{Code: "USD", Digits: 2})
	params := make([]Type, 9)
	for i := range params {
		params[i] = MoneyOf(string(rune('a' + i)))
	}
	err := registry.Register(FunctionSpec{Name: "wide_v1", Params: params, Result: BoolType, Eval: evalIntAdd})
	if err == nil || !strings.Contains(err.Error(), "currency variables") {
		t.Fatalf("nine currency variables: error = %v, want a refusal", err)
	}
}

func TestDeclareMoneyChecksEveryCurrency(t *testing.T) {
	t.Parallel()
	for name, test := range map[string]struct {
		rounding Rounding
		currency CurrencySpec
		want     string
	}{
		"a rounding past the modes":  {Rounding(99), CurrencySpec{Code: "USD", Digits: 2}, "rounding"},
		"no code":                    {RoundHalfUp, CurrencySpec{Digits: 2}, "invalid currency code"},
		"a two-letter code":          {RoundHalfUp, CurrencySpec{Code: "US", Digits: 2}, "invalid currency code"},
		"a nine-letter code":         {RoundHalfUp, CurrencySpec{Code: "ABCDEFGHI", Digits: 2}, "invalid currency code"},
		"a code starting in a digit": {RoundHalfUp, CurrencySpec{Code: "1US", Digits: 2}, "invalid currency code"},
		"a code with a lower case":   {RoundHalfUp, CurrencySpec{Code: "USd", Digits: 2}, "invalid currency code"},
		"a code with a dash":         {RoundHalfUp, CurrencySpec{Code: "US-D", Digits: 2}, "invalid currency code"},
		"negative places":            {RoundHalfUp, CurrencySpec{Code: "USD", Digits: -1}, "0 to 8"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			registry := CoreRegistry()
			err := registry.DeclareMoney(MoneySpec{Rounding: test.rounding, Currencies: []CurrencySpec{test.currency}})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("DeclareMoney(%v, %+v) error = %v, want one containing %q", test.rounding, test.currency, err, test.want)
			}
			if _, declared := registry.Money(); declared {
				t.Fatalf("DeclareMoney(%v, %+v) failed but left money declared", test.rounding, test.currency)
			}
		})
	}
}

// The edges of what a table holds are in it: no places and eight, a code of
// three characters and of eight, digits after the first letter, and every
// rounding mode.
func TestDeclareMoneyAcceptsTheEdges(t *testing.T) {
	t.Parallel()
	currencies := []CurrencySpec{{Code: "JPY", Digits: 0}, {Code: "BTC", Digits: 8}, {Code: "ABCDEFGH", Digits: 2}, {Code: "U5D", Digits: 2}}
	for mode := RoundHalfEven; mode <= RoundFloor; mode++ {
		t.Run(mode.String(), func(t *testing.T) {
			t.Parallel()
			registry := declared(t, mode, currencies...)
			spec, _ := registry.Money()
			if spec.Rounding != mode || len(spec.Currencies) != len(currencies) || spec.Currencies[0].Code != "ABCDEFGH" {
				t.Fatalf("Money() = %+v, want rounding %s and the currencies sorted by code", spec, mode)
			}
		})
	}
}

// A declaration that fails leaves the registry as it was, so the host can
// correct it and declare again.
func TestAFailedDeclarationCanBeRetried(t *testing.T) {
	t.Parallel()
	registry := CoreRegistry()
	if err := registry.DeclareMoney(MoneySpec{Rounding: RoundHalfUp, Currencies: []CurrencySpec{{Code: "usd", Digits: 2}}}); err == nil {
		t.Fatal("DeclareMoney with a lower-case code succeeded, want an error")
	}
	if err := registry.DeclareMoney(MoneySpec{Rounding: RoundHalfUp, Currencies: []CurrencySpec{{Code: "USD", Digits: 2}}}); err != nil {
		t.Fatalf("DeclareMoney after a failed one: %v", err)
	}
	if len(registry.Overloads("allocate")) != 4 {
		t.Fatalf("Overloads(allocate) = %d after declaring, want 4", len(registry.Overloads("allocate")))
	}
}

func TestAnUndeclaredRegistryHasNoMoney(t *testing.T) {
	t.Parallel()
	registry := CoreRegistry()
	if _, ok := registry.Money(); ok {
		t.Fatal("Money() on an undeclared registry reported a spec")
	}
	if err := DeclaredCurrency(registry, "USD"); err == nil || !strings.Contains(err.Error(), "not declared for this registry") {
		t.Fatalf("DeclaredCurrency(USD) error = %v, want money not declared", err)
	}
	if _, err := ParseMoneyAmount(registry, "USD", "1.70"); err == nil || !strings.Contains(err.Error(), "not declared for this registry") {
		t.Fatalf("ParseMoneyAmount(USD, 1.70) error = %v, want money not declared", err)
	}
	var none *Registry
	if none.currencies() != nil {
		t.Fatal("a nil registry reported a currency table")
	}
	if len(registry.Overloads("allocate")) != 0 || len(registry.Overloads("round")) != 0 {
		t.Fatal("an undeclared registry has the money kernel's functions")
	}
}

// Money hands out a copy: a host that edits it does not edit the registry.
func TestMoneyReportsACopyOfTheSpec(t *testing.T) {
	t.Parallel()
	registry := declared(t, RoundHalfUp, CurrencySpec{Code: "USD", Digits: 2})
	spec, _ := registry.Money()
	spec.Currencies[0].Digits = 5
	again, _ := registry.Money()
	if again.Currencies[0].Digits != 2 {
		t.Fatalf("Money() after editing a copy = %+v, want USD with 2 places", again)
	}
}

func TestDeclaredCurrenciesAndAmounts(t *testing.T) {
	t.Parallel()
	registry := declared(t, RoundHalfUp, CurrencySpec{Code: "USD", Digits: 2}, CurrencySpec{Code: "JPY", Digits: 0}, CurrencySpec{Code: "KWD", Digits: 3})
	if err := DeclaredCurrency(registry, "GBP"); !errors.Is(err, ErrCurrency) {
		t.Fatalf("DeclaredCurrency(GBP) error = %v, want ErrCurrency", err)
	}
	for name, test := range map[string]struct {
		code, amount string
		want         int64
		fails        string
	}{
		"dollars and cents":   {"USD", "1.70", 170, ""},
		"fewer places":        {"USD", "1.7", 170, ""},
		"whole yen":           {"JPY", "100", 100, ""},
		"dinars and fils":     {"KWD", "1.5", 1500, ""},
		"a negative amount":   {"USD", "-0.01", -1, ""},
		"a cent too fine":     {"USD", "1.701", 0, "3 decimal places"},
		"a fraction of a yen": {"JPY", "1.5", 0, "1 decimal places"},
		"an undeclared code":  {"GBP", "1", 0, "not declared"},
		"not a number":        {"USD", "one", 0, "is not a decimal"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			expectParsed(t, registry, test.code, test.amount, test.want, test.fails)
		})
	}
}

// expectParsed requires ParseMoneyAmount to read amount as want minor units
// of code, or to fail with fails in its error.
func expectParsed(t *testing.T, registry *Registry, code, amount string, want int64, fails string) {
	t.Helper()
	value, err := ParseMoneyAmount(registry, code, amount)
	if fails != "" {
		if err == nil || !strings.Contains(err.Error(), fails) {
			t.Fatalf("ParseMoneyAmount(%s, %s) error = %v, want one containing %q", code, amount, err, fails)
		}
		return
	}
	if money, _ := value.Money(); err != nil || money != (Money{currency: code, minor: want}) {
		t.Fatalf("ParseMoneyAmount(%s, %s) = %v, %v, want %s %d", code, amount, money, err, code, want)
	}
}

// A signature is admitted only in a registry that declares money, and only
// with currencies it declares — wherever in the signature they are.
func TestASignatureNamesOnlyDeclaredCurrencies(t *testing.T) {
	t.Parallel()
	registry := declared(t, RoundHalfUp, CurrencySpec{Code: "USD", Digits: 2})
	for name, test := range map[string]struct {
		params []Type
		result Type
		ok     bool
	}{
		"a declared code":              {[]Type{MoneyOf("USD")}, MoneyOf("USD"), true},
		"variables and unknowns":       {[]Type{MoneyOf("u"), CurrencyOf("u"), MoneyOf("")}, MoneyOf(""), true},
		"an undeclared parameter":      {[]Type{MoneyOf("GBP")}, RateType, false},
		"an undeclared result":         {[]Type{IntType}, MoneyOf("GBP"), false},
		"an undeclared array element":  {[]Type{ArrayOf(MoneyOf("GBP"))}, IntType, false},
		"an undeclared record field":   {[]Type{RecordOf(Field{name: "fee", typ: MoneyOf("GBP")})}, IntType, false},
		"an undeclared currency":       {[]Type{DictOf(CurrencyOf("GBP"))}, IntType, false},
		"a code nested two levels":     {[]Type{ArrayOf(DictOf(MoneyOf("GBP")))}, IntType, false},
		"a declared code nested twice": {[]Type{ArrayOf(DictOf(MoneyOf("USD")))}, IntType, true},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			err := registry.Register(FunctionSpec{Name: "fees.check_v1", Params: test.params, Result: test.result, Eval: evalIntAdd})
			if test.ok != (err == nil) || (err != nil && !errors.Is(err, ErrCurrency)) {
				t.Fatalf("Register(%v -> %s) error = %v, want ok %v or ErrCurrency", test.params, test.result, err, test.ok)
			}
		})
	}
}

// Before DeclareMoney every money kind is refused, a rate included.
func TestEveryMoneyKindNeedsTheDeclaration(t *testing.T) {
	t.Parallel()
	for name, typ := range map[string]Type{
		"a rate": RateType, "a currency": CurrencyOf(""),
		"money in an array": ArrayOf(MoneyOf("")), "a rate in a record": RecordOf(Field{name: "fee", typ: RateType}),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			err := CoreRegistry().Register(FunctionSpec{Name: "fees.check_v1", Params: []Type{typ}, Result: IntType, Eval: evalIntAdd})
			if err == nil || !strings.Contains(err.Error(), "declares no money") {
				t.Fatalf("Register(%s) error = %v, want declares no money", typ, err)
			}
		})
	}
}

// Eight currency variables fit, counted across every unit a parameter has.
func TestASignatureMayHaveEightCurrencyVariables(t *testing.T) {
	t.Parallel()
	registry := declared(t, RoundHalfUp, CurrencySpec{Code: "USD", Digits: 2})
	var params []Type
	for i := range 8 {
		params = append(params, MoneyOf(string(rune('a'+i))))
	}
	if err := registry.Register(FunctionSpec{Name: "fees.eight_v1", Params: params, Result: BoolType, Eval: evalIntAdd}); err != nil {
		t.Fatalf("eight currency variables in eight amounts: %v", err)
	}
	params = append(params, CurrencyOf("i"))
	err := registry.Register(FunctionSpec{Name: "fees.nine_v1", Params: params, Result: BoolType, Eval: evalIntAdd})
	if err == nil || !strings.Contains(err.Error(), "currency variables") {
		t.Fatalf("nine currency variables: error = %v, want a refusal", err)
	}
	repeated := []Type{MoneyOf("a"), MoneyOf("a"), CurrencyOf("a"), ArrayOf(MoneyOf("a"))}
	if err := registry.Register(FunctionSpec{Name: "fees.same_v1", Params: repeated, Result: BoolType, Eval: evalIntAdd}); err != nil {
		t.Fatalf("one currency variable used four times: %v", err)
	}
}

// A manifest carries the table through JSON, and applying it again, or over
// the same table, changes nothing.
func TestAManifestsMoneyCrossesJSON(t *testing.T) {
	t.Parallel()
	host := declared(t, RoundHalfDown, CurrencySpec{Code: "USD", Digits: 2}, CurrencySpec{Code: "KWD", Digits: 3})
	encoded, err := json.Marshal(host.Manifest())
	if err != nil {
		t.Fatal(err)
	}
	var manifest Manifest
	if err := json.Unmarshal(encoded, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.parts.Money == nil || manifest.parts.Money.Rounding != RoundHalfDown || manifest.parts.Money.Currencies[0].Code != "KWD" {
		t.Fatalf("manifest money = %+v, want half_down with KWD first", manifest.parts.Money)
	}
	server := CoreRegistry()
	for range 2 {
		if err := manifest.Apply(server); err != nil {
			t.Fatalf("Apply: %v", err)
		}
	}
	want, _ := host.Money()
	if got, _ := server.Money(); !reflect.DeepEqual(got, want) {
		t.Fatalf("money after Apply = %v, want %v", got, want)
	}
	if err := manifest.Apply(declared(t, RoundHalfDown, CurrencySpec{Code: "KWD", Digits: 3}, CurrencySpec{Code: "USD", Digits: 2})); err != nil {
		t.Fatalf("Apply over the same table declared in another order: %v", err)
	}
}

func TestAManifestsMoneyMustBeATable(t *testing.T) {
	t.Parallel()
	manifest := CoreRegistry().Manifest()
	if manifest.parts.Money != nil {
		t.Fatalf("an undeclared registry's manifest has money %+v", manifest.parts.Money)
	}
	if err := manifest.Apply(declared(t, RoundHalfUp, CurrencySpec{Code: "USD", Digits: 2})); err != nil {
		t.Fatalf("a manifest without money over a registry with it: %v", err)
	}
	manifest.parts.Money = &MoneySpec{Rounding: RoundHalfUp, Currencies: []CurrencySpec{{Code: "usd", Digits: 2}}}
	for name, registry := range map[string]*Registry{"undeclared": CoreRegistry(), "declared": declared(t, RoundHalfUp, CurrencySpec{Code: "USD", Digits: 2})} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if err := manifest.Apply(registry); err == nil || !strings.Contains(err.Error(), "invalid currency code") {
				t.Fatalf("Apply of a manifest with a lower-case code: error = %v, want invalid currency code", err)
			}
		})
	}
}

// The manifest's spec is a copy: editing it leaves the registry alone.
func TestAManifestCopiesTheTable(t *testing.T) {
	t.Parallel()
	registry := declared(t, RoundHalfUp, CurrencySpec{Code: "USD", Digits: 2})
	manifest := registry.Manifest()
	manifest.parts.Money.Currencies[0].Digits = 0
	if spec, _ := registry.Money(); spec.Currencies[0].Digits != 2 {
		t.Fatalf("Money() after editing the manifest = %+v, want USD with 2 places", spec)
	}
}
