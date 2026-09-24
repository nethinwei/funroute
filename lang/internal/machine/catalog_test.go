package machine_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"funroute/lang/internal/machine"
	"funroute/lang/internal/syntax"
)

// The catalog is the machine's, but what it lists is what the compiler
// accepts: these tests hold the two to each other.

// consoleRegistry is the operator console: the kernel plus the lazy forms an
// operator may use. Extra forms model a higher-privilege console.
func consoleRegistry(t *testing.T, extra ...machine.Form) *machine.Registry {
	t.Helper()
	registry := machine.CoreRegistry()
	if err := registry.EnableForm(append([]machine.Form{machine.SwitchForm, machine.ForForm, machine.ReduceForm}, extra...)...); err != nil {
		t.Fatal(err)
	}
	return registry
}

func TestCoreCatalogIsMinimalAndCarriesDisplayMetadata(t *testing.T) {
	t.Parallel()
	registry := machine.CoreRegistry()
	catalog := registry.Catalog()
	names := map[string]bool{}
	var fallback machine.FunctionDescriptor
	for _, function := range catalog.Functions() {
		names[function.Name()] = true
		if function.Name() == "fallback" {
			fallback = function
		}
		if function.Doc().Label == "" || function.Doc().Description == "" || function.Doc().Category == "" {
			t.Fatalf("missing display metadata: %#v", function)
		}
		if len(function.Doc().Params) != len(function.Params()) {
			t.Fatalf("parameter display mismatch: %#v", function)
		}
	}
	for _, name := range []string{
		"if", "fallback", "eq", "lt", "le", "gt", "ge",
		"add", "sub", "mul", "div", "mod", "int", "float", "string", "bool",
		"at", "member", "len",
	} {
		if !names[name] {
			t.Fatalf("core function %q is missing", name)
		}
	}
	if len(names) != 19 {
		t.Fatalf("core registry is not minimal: %#v, want 19 names", names)
	}
	if !fallback.Variadic() || fallback.Signature() != "fallback(T,T,...)->T" || len(fallback.Params()) != 2 {
		t.Fatalf("fallback catalog = %#v, want variadic fallback(T,T,...)->T", fallback)
	}
	assertDisplayCarriesNoStyling(t, catalog)
	assertSignaturesNameTheirForm(t, catalog)
	assertLabelCountIsChecked(t, registry)
}

// A form's syntax is the one hand-written string in the catalog: it is how
// the form is spelled, not a type signature, so it cannot be generated. It
// can at least be checked to parse, which is what makes it true.
func assertSignaturesNameTheirForm(t *testing.T, catalog machine.LanguageCatalog) {
	t.Helper()
	for _, function := range catalog.Functions() {
		if !strings.HasPrefix(function.Signature(), function.Name()+"(") {
			t.Fatalf("signature %q does not name %q", function.Signature(), function.Name())
		}
	}
	for _, form := range catalog.SpecialForms() {
		if _, err := syntax.Parse(form.Syntax()); err != nil {
			t.Fatalf("the syntax of %s does not parse: %q: %v", form.Name(), form.Syntax(), err)
		}
	}
}

func assertDisplayCarriesNoStyling(t *testing.T, catalog machine.LanguageCatalog) {
	t.Helper()
	for _, function := range catalog.Functions() {
		if strings.Contains(fmt.Sprint(function.Doc()), "#") {
			t.Fatalf("function %s display carries styling: %+v", function.Name(), function.Doc())
		}
	}
}

// Labels are the only hand-written part of a signature, so a miscount is
// refused instead of being padded with a generated "参数 2".
func assertLabelCountIsChecked(t *testing.T, registry *machine.Registry) {
	t.Helper()
	err := registry.Register(machine.FunctionSpec{
		Name: "bad.labels_v1", Params: []machine.Type{machine.IntType, machine.IntType}, Result: machine.IntType,
		Eval: func(_ context.Context, args []machine.Value) (machine.Value, error) { return args[0], nil },
		Doc:  machine.Doc{Label: "标签数不符", Params: []string{"只有一个"}},
	})
	if err == nil || !strings.Contains(err.Error(), "parameter labels") {
		t.Fatalf("mismatched label count error = %v, want a parameter labels error", err)
	}
}

func specialFormNames(catalog machine.LanguageCatalog) string {
	names := make([]string, len(catalog.SpecialForms()))
	for i, form := range catalog.SpecialForms() {
		names[i] = form.Name()
	}
	return strings.Join(names, ",")
}

func TestCatalogListsSwitchableAndDerivedForms(t *testing.T) {
	t.Parallel()
	// The kernel enables no switchable form; let and the derived forms are
	// always there, because let only binds names and the derived forms expand
	// to if, which the kernel always has.
	if got, want := specialFormNames(machine.CoreRegistry().Catalog()), "let,and,or,not"; got != want {
		t.Fatalf("core special forms = %s, want %s", got, want)
	}
	if got, want := specialFormNames(consoleRegistry(t).Catalog()), "switch,for,reduce,let,and,or,not"; got != want {
		t.Fatalf("operator special forms = %s, want %s", got, want)
	}
	// Only the known forms can be enabled.
	if err := machine.CoreRegistry().EnableForm("lambda"); err == nil {
		t.Fatal("unknown form was accepted")
	}
}

// A doc cut by bytes — "左侧补齐"[:2] — is not UTF-8, and is refused where it
// is registered rather than shown broken in a hover.
func TestDocTextMustBeUTF8(t *testing.T) {
	t.Parallel()
	broken := "左侧补齐"[:2]
	err := machine.CoreRegistry().Register(machine.FunctionSpec{
		Name: "pad_v1", Params: []machine.Type{machine.StringType}, Result: machine.StringType,
		Eval: func(_ context.Context, args []machine.Value) (machine.Value, error) { return args[0], nil },
		Doc:  machine.Doc{Description: "在" + broken + "补"},
	})
	if err == nil || !strings.Contains(err.Error(), "UTF-8") {
		t.Fatalf("Register with a broken doc: error = %v, want one naming UTF-8", err)
	}
}

// The catalog carries the declared money feature, for completing USD and
// @half_up, and lists the money kernel under its own category.
func TestTheCatalogCarriesMoney(t *testing.T) {
	t.Parallel()
	if money, declared := machine.CoreRegistry().Catalog().Money(); declared {
		t.Fatalf("an undeclared registry's catalog has money %+v", money)
	}
	registry := moneyRegistry(t)
	catalog := registry.Catalog()
	money, declared := catalog.Money()
	if !declared || money.Rounding != machine.RoundHalfEven || len(money.Currencies) != 4 || money.Currencies[0].Code != "EUR" {
		t.Fatalf("catalog money = %+v, want half_even with four currencies sorted by code", money)
	}
	money.Currencies[0].Digits = 7
	if again, _ := catalog.Money(); again.Currencies[0].Digits != 2 {
		t.Fatalf("catalog money after editing a copy = %+v, want EUR with 2 places", again)
	}
	found := map[string]bool{}
	for _, function := range catalog.Functions() {
		if function.Doc().Category == "金额" {
			found[function.Name()] = true
		}
	}
	for _, name := range []string{"money", "currency_of", "like", "minor", "currency", "sign", "allocate", "rate", "round"} {
		if !found[name] {
			t.Errorf("catalog lists no money function %s; money functions: %v", name, found)
		}
	}
}

// The catalog reaches a front end only as JSON, so its shape is fixed, by
// digest.
func TestCatalogJSONShape(t *testing.T) {
	t.Parallel()
	encoded, err := json.Marshal(snapshotRegistry(t).Catalog())
	if err != nil {
		t.Fatal(err)
	}
	const want = "64161f55bdafbb207fa84fb61cb76e9b2b982e012c5431ce0b069c64151d8491"
	if got := fmt.Sprintf("%x", sha256.Sum256(encoded)); got != want {
		t.Fatalf("sha256(json.Marshal(catalog)) = %s, want %s", got, want)
	}
}
