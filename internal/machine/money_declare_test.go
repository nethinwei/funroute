package machine

import (
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/nethinwei/funroute/internal/money"
)

func declared(t *testing.T, currencies ...money.CurrencySpec) *Registry {
	t.Helper()
	registry := CoreRegistry()
	if err := registry.DeclareMoney(money.MoneySpec{Currencies: currencies}); err != nil {
		t.Fatal(err)
	}
	return registry
}

func TestDeclareMoneyRefusesWhatCannotBeRepresented(t *testing.T) {
	t.Parallel()
	for name, test := range map[string]struct {
		spec money.MoneySpec
		want string
	}{
		"no currency":       {money.MoneySpec{}, "at least one"},
		"a lower-case code": {money.MoneySpec{Currencies: []money.CurrencySpec{{Code: "usd", Digits: 2}}}, "invalid currency code"},
		"too many places":   {money.MoneySpec{Currencies: []money.CurrencySpec{{Code: "BTC", Digits: 9}}}, "0 to 8"},
		"a code twice":      {money.MoneySpec{Currencies: []money.CurrencySpec{{Code: "USD", Digits: 2}, {Code: "USD", Digits: 0}}}, "twice"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if err := CoreRegistry().DeclareMoney(test.spec); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("DeclareMoney error = %v, want one containing %q", err, test.want)
			}
		})
	}
	registry := declared(t, money.CurrencySpec{Code: "USD", Digits: 2})
	if err := registry.DeclareMoney(money.MoneySpec{Currencies: []money.CurrencySpec{{Code: "EUR", Digits: 2}}}); err == nil {
		t.Fatal("a second DeclareMoney succeeded, want an error")
	}
}

// A table's identity is its currencies and their places: the order they are
// listed in does not change it; the places do.
func TestTheIdentityNamesTheCurrencies(t *testing.T) {
	t.Parallel()
	usd, eur := money.CurrencySpec{Code: "USD", Digits: 2}, money.CurrencySpec{Code: "EUR", Digits: 2}
	identity := func(currencies ...money.CurrencySpec) string {
		return money.Identity(declared(t, currencies...).currencies())
	}
	first := identity(usd, eur)
	if first != identity(eur, usd) || first == identity(usd, money.CurrencySpec{Code: "EUR", Digits: 3}) {
		t.Fatal("identity: want the same for another order, another for other places")
	}
	digest, ok := strings.CutPrefix(first, "sha256:")
	if !ok || len(digest) != 64 {
		t.Fatalf("identity = %s, want sha256: with 64 hex digits", first)
	}
}

func TestMoneyTypesNeedTheDeclaration(t *testing.T) {
	t.Parallel()
	spec := FunctionSpec{Name: "fee_v1", Params: []Type{MoneyType}, Result: MoneyType, Eval: evalIntAdd}
	if err := CoreRegistry().Register(spec); err == nil {
		t.Fatal("a money signature registered without money declared")
	}
	registry := declared(t, money.CurrencySpec{Code: "USD", Digits: 2})
	if err := registry.Register(spec); err != nil {
		t.Fatal(err)
	}
}

// A manifest carries the money feature, and applying it declares the same
// table the host has.
func TestManifestCarriesMoney(t *testing.T) {
	t.Parallel()
	host := declared(t, money.CurrencySpec{Code: "USD", Digits: 2}, money.CurrencySpec{Code: "JPY", Digits: 0})
	manifest := host.Manifest()
	server := CoreRegistry()
	if err := manifest.Apply(server); err != nil {
		t.Fatal(err)
	}
	want, _ := host.Money()
	if got, ok := server.Money(); !ok || !reflect.DeepEqual(got, want) {
		t.Fatalf("money after Apply = %v, want %v", got, want)
	}
	if err := manifest.Apply(declared(t, money.CurrencySpec{Code: "USD", Digits: 2})); err == nil {
		t.Fatal("a manifest applied over other money, want an error")
	}
}

func TestDeclareMoneyChecksEveryCurrency(t *testing.T) {
	t.Parallel()
	for name, test := range map[string]struct {
		currency money.CurrencySpec
		want     string
	}{
		"no code":                    {money.CurrencySpec{Digits: 2}, "invalid currency code"},
		"a two-letter code":          {money.CurrencySpec{Code: "US", Digits: 2}, "invalid currency code"},
		"a nine-letter code":         {money.CurrencySpec{Code: "ABCDEFGHI", Digits: 2}, "invalid currency code"},
		"a code starting in a digit": {money.CurrencySpec{Code: "1US", Digits: 2}, "invalid currency code"},
		"a code with a lower case":   {money.CurrencySpec{Code: "USd", Digits: 2}, "invalid currency code"},
		"a code with a dash":         {money.CurrencySpec{Code: "US-D", Digits: 2}, "invalid currency code"},
		"negative places":            {money.CurrencySpec{Code: "USD", Digits: -1}, "0 to 8"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			registry := CoreRegistry()
			err := registry.DeclareMoney(money.MoneySpec{Currencies: []money.CurrencySpec{test.currency}})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("DeclareMoney(%+v) error = %v, want one containing %q", test.currency, err, test.want)
			}
			if _, declared := registry.Money(); declared {
				t.Fatalf("DeclareMoney(%+v) failed but left money declared", test.currency)
			}
		})
	}
}

// The edges of what a table holds are in it: no places and eight, a code of
// three characters and of eight, digits after the first letter, and every
// rounding mode.
func TestDeclareMoneyAcceptsTheEdges(t *testing.T) {
	t.Parallel()
	currencies := []money.CurrencySpec{{Code: "JPY", Digits: 0}, {Code: "BTC", Digits: 8}, {Code: "ABCDEFGH", Digits: 2}, {Code: "U5D", Digits: 2}}
	registry := declared(t, currencies...)
	spec, _ := registry.Money()
	if len(spec.Currencies) != len(currencies) || spec.Currencies[0].Code != "ABCDEFGH" {
		t.Fatalf("Money() = %+v, want the currencies sorted by code", spec)
	}
}

// A declaration that fails leaves the registry as it was, so the host can
// correct it and declare again.
func TestAFailedDeclarationCanBeRetried(t *testing.T) {
	t.Parallel()
	registry := CoreRegistry()
	if err := registry.DeclareMoney(money.MoneySpec{Currencies: []money.CurrencySpec{{Code: "usd", Digits: 2}}}); err == nil {
		t.Fatal("DeclareMoney with a lower-case code succeeded, want an error")
	}
	if err := registry.DeclareMoney(money.MoneySpec{Currencies: []money.CurrencySpec{{Code: "USD", Digits: 2}}}); err != nil {
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
	if _, err := ParseMoneyAmount(registry, "USD", "1.70"); err == nil || !strings.Contains(err.Error(), "not declared for this registry") {
		t.Fatalf("ParseMoneyAmount(USD, 1.70) error = %v, want money not declared", err)
	}
	var none *Registry
	if none.currencies() != nil {
		t.Fatal("a nil registry reported a currency table")
	}
	// round rounds a float to an int without money; with money it is a
	// rounding scope too.
	if len(registry.Overloads("allocate")) != 0 || slices.ContainsFunc(registry.Overloads("round"), (*RegisteredFunction).IsRoundingScope) {
		t.Fatal("an undeclared registry has the money kernel's functions")
	}
}

// Money hands out a copy: a host that edits it does not edit the registry.
func TestMoneyReportsACopyOfTheSpec(t *testing.T) {
	t.Parallel()
	registry := declared(t, money.CurrencySpec{Code: "USD", Digits: 2})
	spec, _ := registry.Money()
	spec.Currencies[0].Digits = 5
	again, _ := registry.Money()
	if again.Currencies[0].Digits != 2 {
		t.Fatalf("Money() after editing a copy = %+v, want USD with 2 places", again)
	}
}

func TestDeclaredCurrenciesAndAmounts(t *testing.T) {
	t.Parallel()
	registry := declared(t, money.CurrencySpec{Code: "USD", Digits: 2}, money.CurrencySpec{Code: "JPY", Digits: 0}, money.CurrencySpec{Code: "KWD", Digits: 3})
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
	if got, _ := value.Money(); err != nil || got != money.Make(code, want) {
		t.Fatalf("ParseMoneyAmount(%s, %s) = %v, %v, want %s %d", code, amount, got, err, code, want)
	}
}

// Before DeclareMoney every money kind is refused, a ratio included.
func TestEveryMoneyKindNeedsTheDeclaration(t *testing.T) {
	t.Parallel()
	for name, typ := range map[string]Type{
		"a ratio": RatioType, "a currency": CurrencyType,
		"money in an array": ArrayOf(MoneyType), "a ratio in a record": RecordOf(Field{name: "fee", typ: RatioType}),
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

// A manifest carries the table through JSON, and applying it again, or over
// the same table, changes nothing.
func TestAManifestsMoneyCrossesJSON(t *testing.T) {
	t.Parallel()
	host := declared(t, money.CurrencySpec{Code: "USD", Digits: 2}, money.CurrencySpec{Code: "KWD", Digits: 3})
	encoded, err := json.Marshal(host.Manifest())
	if err != nil {
		t.Fatal(err)
	}
	var manifest Manifest
	if err := json.Unmarshal(encoded, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.parts.Money == nil || manifest.parts.Money.Currencies[0].Code != "KWD" {
		t.Fatalf("manifest money = %+v, want KWD first", manifest.parts.Money)
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
	if err := manifest.Apply(declared(t, money.CurrencySpec{Code: "KWD", Digits: 3}, money.CurrencySpec{Code: "USD", Digits: 2})); err != nil {
		t.Fatalf("Apply over the same table declared in another order: %v", err)
	}
}

func TestAManifestsMoneyMustBeATable(t *testing.T) {
	t.Parallel()
	manifest := CoreRegistry().Manifest()
	if manifest.parts.Money != nil {
		t.Fatalf("an undeclared registry's manifest has money %+v", manifest.parts.Money)
	}
	if err := manifest.Apply(declared(t, money.CurrencySpec{Code: "USD", Digits: 2})); err != nil {
		t.Fatalf("a manifest without money over a registry with it: %v", err)
	}
	manifest.parts.Money = &money.MoneySpec{Currencies: []money.CurrencySpec{{Code: "usd", Digits: 2}}}
	for name, registry := range map[string]*Registry{"undeclared": CoreRegistry(), "declared": declared(t, money.CurrencySpec{Code: "USD", Digits: 2})} {
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
	registry := declared(t, money.CurrencySpec{Code: "USD", Digits: 2})
	manifest := registry.Manifest()
	manifest.parts.Money.Currencies[0].Digits = 0
	if spec, _ := registry.Money(); spec.Currencies[0].Digits != 2 {
		t.Fatalf("Money() after editing the manifest = %+v, want USD with 2 places", spec)
	}
}

// The registry's table is the one it declared: a service that builds its own
// from the same spec agrees with the rules.
func TestARegistryHandsOutItsTable(t *testing.T) {
	t.Parallel()
	registry := CoreRegistry()
	if _, ok := registry.Currencies(); ok {
		t.Fatal("a registry without money handed out a table")
	}
	spec := money.MoneySpec{Currencies: []money.CurrencySpec{{Code: "USD", Digits: 2}}}
	if err := registry.DeclareMoney(spec); err != nil {
		t.Fatal(err)
	}
	table, ok := registry.Currencies()
	if text, err := table.Format(money.Make("USD", 5)); !ok || err != nil || text != "USD 0.05" {
		t.Fatalf("registry table = %v, %v", table, ok)
	}
}

func TestARegistryWithoutMoneyHasNoTable(t *testing.T) {
	t.Parallel()
	var none *Registry
	if _, ok := none.Currencies(); ok {
		t.Fatal("a nil registry handed out a table")
	}
	// No table is nil, not a table that panics on its first use.
	if table, ok := CoreRegistry().Currencies(); ok || table != nil {
		t.Fatalf("Currencies() without money = %v, %v, want nil, false", table, ok)
	}
	registry := declared(t, money.CurrencySpec{Code: "USD", Digits: 2}, money.CurrencySpec{Code: "EUR", Digits: 2})
	table, ok := registry.Currencies()
	own, err := money.NewCurrencies(money.MoneySpec{Currencies: []money.CurrencySpec{{Code: "EUR", Digits: 2}, {Code: "USD", Digits: 2}}})
	if !ok || err != nil || !slices.Equal(table.Spec().Currencies, own.Spec().Currencies) {
		t.Fatalf("registry table %+v and a table built from its spec %+v disagree (%v)", table.Spec(), own.Spec(), err)
	}
}
