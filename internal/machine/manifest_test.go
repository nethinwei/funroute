package machine_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/nethinwei/funroute/internal/compile"
	"github.com/nethinwei/funroute/internal/machine"
	"github.com/nethinwei/funroute/internal/money"
)

// A manifest crosses from a host to a language service as JSON, so its
// shape is fixed byte for byte (testdata/manifest.json), and it reads back to
// itself.
func TestManifestJSONShape(t *testing.T) {
	t.Parallel()
	encoded, err := json.Marshal(snapshotRegistry(t).Manifest())
	if err != nil {
		t.Fatal(err)
	}
	expectSnapshot(t, "manifest.json", encoded)
	var back machine.Manifest
	if err := json.Unmarshal(encoded, &back); err != nil {
		t.Fatal(err)
	}
	if again, _ := json.Marshal(back); string(again) != string(encoded) {
		t.Fatal("a manifest read back writes something else")
	}
}

// A fold changes how a call on a comprehension runs — a first stops at its
// item — so a manifest carries it: a program compiled against the manifest
// is the program compiled against the registry, digest and all, and the
// catalog says the function short-circuits.
func TestAManifestCarriesTheFold(t *testing.T) {
	t.Parallel()
	host := machine.CoreRegistry()
	if err := host.EnableForm(machine.ForForm); err != nil {
		t.Fatal(err)
	}
	first := func(xs []int64) (int64, error) { return xs[0], nil }
	if err := host.Register(machine.FunctionSpec{Name: "pick.first_v1", Go: first, Fold: &machine.Fold{First: true}}); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(host.Manifest())
	if err != nil {
		t.Fatal(err)
	}
	var manifest machine.Manifest
	if err := json.Unmarshal(encoded, &manifest); err != nil {
		t.Fatal(err)
	}
	service := machine.CoreRegistry()
	if err := manifest.Apply(service); err != nil {
		t.Fatal(err)
	}
	options := compile.CompileOptions{Args: []compile.ArgSpec{{Name: "xs", Type: machine.ArrayOf(machine.IntType)}}}
	source := "pick.first_v1([10 / x for x in xs])"
	byHost, err := compile.CompileExpr(source, host, options)
	if err != nil {
		t.Fatal(err)
	}
	byManifest, err := compile.CompileExpr(source, service, options)
	if err != nil {
		t.Fatal(err)
	}
	if byHost.Digest() != byManifest.Digest() {
		t.Fatalf("%s compiles to %s against the registry and %s against its manifest", source, byHost.Digest(), byManifest.Digest())
	}
	for _, function := range service.Catalog().Functions() {
		if function.Name() == "pick.first_v1" && !function.ShortCircuit() {
			t.Fatal("the catalog does not say pick.first_v1 short-circuits")
		}
	}
}

// A function the registry has already must compile as the manifest says it
// does: the same fold, the same arguments bounded or fixed. One that would
// compile otherwise is refused by name, rather than quietly kept.
func TestAManifestRefusesAFunctionThatWouldCompileOtherwise(t *testing.T) {
	t.Parallel()
	first := func(xs []int64) (int64, error) { return xs[0], nil }
	host := machine.CoreRegistry()
	if err := host.Register(machine.FunctionSpec{Name: "pick.first_v1", Go: first, Fold: &machine.Fold{First: true}}); err != nil {
		t.Fatal(err)
	}
	for name, spec := range map[string]machine.FunctionSpec{
		"the same":     {Name: "pick.first_v1", Go: first, Fold: &machine.Fold{First: true}},
		"fold":         {Name: "pick.first_v1", Go: first},
		"bounded_args": {Name: "pick.first_v1", Go: first, Fold: &machine.Fold{First: true}, Doc: machine.Doc{BoundedArgs: true}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			service := machine.CoreRegistry()
			if err := service.Register(spec); err != nil {
				t.Fatal(err)
			}
			err := host.Manifest().Apply(service)
			if name == "the same" && err != nil || name != "the same" && (err == nil || !strings.Contains(err.Error(), name)) {
				t.Fatalf("Apply onto a registry whose pick.first_v1 differs in %q: error = %v", name, err)
			}
		})
	}
}

// A manifest the registry refuses leaves it as it was: the forms, the money,
// the handle and the functions the manifest describes ahead of the one
// refused are not in it, and the same registry takes a manifest it accepts
// afterwards as a fresh one would.
func TestAManifestIsAppliedWholeOrNotAtAll(t *testing.T) {
	t.Parallel()
	first := func(xs []int64) (int64, error) { return xs[0], nil }
	host := machine.CoreRegistry()
	if err := host.EnableForm(machine.ForForm, machine.ReduceForm); err != nil {
		t.Fatal(err)
	}
	if err := host.DeclareMoney(money.MoneySpec{Currencies: []money.CurrencySpec{{Code: "USD", Digits: 2}}}); err != nil {
		t.Fatal(err)
	}
	if err := machine.DefineHandle[*strings.Builder](host, "host.session"); err != nil {
		t.Fatal(err)
	}
	for _, spec := range []machine.FunctionSpec{
		{Name: "a.early_v1", Go: func(int64) int64 { return 0 }},
		{Name: "pick.first_v1", Go: first, Fold: &machine.Fold{First: true}},
	} {
		if err := host.Register(spec); err != nil {
			t.Fatal(err)
		}
	}
	service := machine.CoreRegistry()
	if err := service.Register(machine.FunctionSpec{Name: "pick.first_v1", Go: first}); err != nil {
		t.Fatal(err)
	}
	before, _ := json.Marshal(service.Manifest())
	if err := host.Manifest().Apply(service); err == nil {
		t.Fatal("Apply onto a registry whose pick.first_v1 folds otherwise succeeded")
	}
	if after, _ := json.Marshal(service.Manifest()); string(after) != string(before) {
		t.Errorf("a refused manifest left the registry changed:\n%s\nwant\n%s", after, before)
	}
	other := machine.CoreRegistry()
	if err := other.EnableForm(machine.ForForm); err != nil {
		t.Fatal(err)
	}
	if err := other.Manifest().Apply(service); err != nil || !service.FormEnabled(machine.ForForm) || service.FormEnabled(machine.ReduceForm) {
		t.Errorf("Apply after a refused one: %v, for enabled %v, reduce enabled %v; want only for", err, service.FormEnabled(machine.ForForm), service.FormEnabled(machine.ReduceForm))
	}
}
