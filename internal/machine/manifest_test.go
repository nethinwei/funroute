package machine_test

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/nethinwei/funroute/internal/compile"
	"github.com/nethinwei/funroute/internal/machine"
)

// A manifest crosses from a host to a language service as JSON, so its
// shape is fixed, by digest, and it reads back to itself.
func TestManifestJSONShape(t *testing.T) {
	t.Parallel()
	encoded, err := json.Marshal(snapshotRegistry(t).Manifest())
	if err != nil {
		t.Fatal(err)
	}
	const want = "2b15b2c71dd608e15fb09f843f7c0b26dee785c6d0845374bb20ac4d4ca982b9"
	if got := fmt.Sprintf("%x", sha256.Sum256(encoded)); got != want {
		t.Fatalf("sha256(json.Marshal(manifest)) = %s, want %s", got, want)
	}
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
