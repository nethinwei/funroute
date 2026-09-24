package hosttest

import (
	"encoding/json"
	"errors"
	"slices"
	"testing"

	"github.com/nethinwei/funroute"
	"github.com/nethinwei/funroute/extensions/std"
)

type engineTensor struct{}

// The host's registry, written as a manifest, stands in for it where its
// functions cannot run: programs compile to the same artifact, and running one
// says which calls it could not make.
func TestManifestStandsInForTheHostsFunctions(t *testing.T) {
	t.Parallel()
	host := withStandard(t)
	if err := funroute.DefineHandle[*engineTensor](host, "demo.tensor"); err != nil {
		t.Fatal(err)
	}
	doc := funroute.Doc{Label: "风险分", Cost: 25, Params: []string{"国家", "金额"}}
	if err := host.Register(funroute.FunctionSpec{
		Name: "risk.score_v1",
		Doc:  doc,
		Go:   func(country string, amount int64) (float64, error) { return 0.9, nil },
	}); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(host.Manifest())
	if err != nil {
		t.Fatal(err)
	}
	var manifest funroute.Manifest
	if err := json.Unmarshal(encoded, &manifest); err != nil {
		t.Fatal(err)
	}
	browser := withStandard(t)
	if err := manifest.Apply(browser); err != nil {
		t.Fatal(err)
	}
	contract := funroute.CompileOptions{Args: []funroute.ArgSpec{{Name: "country", Type: funroute.StringType}, {Name: "amount", Type: funroute.IntType}}}
	source := `fallback(risk.score_v1(country, amount), 0.5) > 0.8`
	atHost, err := funroute.CompileExpr(source, host, contract)
	if err != nil {
		t.Fatal(err)
	}
	inBrowser, err := funroute.CompileExpr(source, browser, contract)
	if err != nil {
		t.Fatalf("CompileExpr(%q) against the manifest: %v", source, err)
	}
	if inBrowser.Digest() != atHost.Digest() {
		t.Fatalf("against the manifest %q compiles to digest %s, want the host's %s", source, inBrowser.Digest(), atHost.Digest())
	}
	checkUnavailableCall(t, inBrowser, browser, host)
}

func checkUnavailableCall(t *testing.T, artifact *funroute.Artifact, browser, host *funroute.Registry) {
	t.Helper()
	args := map[string]any{"country": "SG", "amount": 100}
	runtime, err := funroute.Instantiate(artifact, browser)
	if err != nil {
		t.Fatal(err)
	}
	ctx, calls := funroute.TrackUnavailable(t.Context())
	value, err := runtime.Run(ctx, args, funroute.RunOptions{Fuel: 1000})
	if err != nil || value.Any() != false || !slices.Equal(calls(), []string{"risk.score_v1"}) {
		t.Fatalf("in the browser: %v, %v, calls %v; want false, no error, calls [risk.score_v1]", value.Any(), err, calls())
	}
	hosted, err := funroute.Instantiate(artifact, host)
	if err != nil {
		t.Fatal(err)
	}
	if value, err := hosted.Run(t.Context(), args, funroute.RunOptions{Fuel: 1000}); err != nil || value.Any() != true {
		t.Fatalf("at the host: %v, %v; want true", value.Any(), err)
	}
	bare, err := funroute.CompileExpr(`risk.score_v1("SG", 1)`, browser, funroute.CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	direct, _ := funroute.Instantiate(bare, browser)
	if _, err := direct.Run(t.Context(), nil, funroute.RunOptions{Fuel: 1000}); !errors.Is(err, funroute.ErrUnavailable) || !errors.Is(err, funroute.ErrExtension) {
		t.Fatalf("Run(risk.score_v1(\"SG\", 1)) error = %v, want ErrUnavailable and ErrExtension", err)
	}
}

// raiseAddCost sets the cost of add in a manifest's JSON document to 99.
func raiseAddCost(t *testing.T, document map[string]any) {
	t.Helper()
	functions, ok := document["functions"].([]any)
	if !ok {
		t.Fatalf("manifest functions = %T, want an array", document["functions"])
	}
	for _, function := range functions {
		entry, ok := function.(map[string]any)
		if !ok {
			t.Fatalf("manifest function = %T, want an object", function)
		}
		if entry["name"] != "add" {
			continue
		}
		doc, ok := entry["doc"].(map[string]any)
		if !ok {
			t.Fatalf("add's doc = %T, want an object", entry["doc"])
		}
		doc["cost"] = 99.0
		return
	}
}

// A function the registry already has must cost what the manifest says, or
// what one compiles the other would refuse to bind.
func TestManifestRefusesADisagreement(t *testing.T) {
	t.Parallel()
	edited := func(edit func(map[string]any)) funroute.Manifest {
		encoded, err := json.Marshal(withStandard(t).Manifest())
		if err != nil {
			t.Fatal(err)
		}
		var document map[string]any
		if err := json.Unmarshal(encoded, &document); err != nil {
			t.Fatal(err)
		}
		edit(document)
		encoded, _ = json.Marshal(document)
		var manifest funroute.Manifest
		if err := json.Unmarshal(encoded, &manifest); err != nil {
			t.Fatal(err)
		}
		return manifest
	}
	costlier := edited(func(document map[string]any) { raiseAddCost(t, document) })
	if err := costlier.Apply(withStandard(t)); err == nil {
		t.Fatal("a manifest that disagrees on a cost was applied")
	}
	newer := edited(func(document map[string]any) { document["version"] = float64(funroute.ManifestVersion + 1) })
	if err := newer.Apply(withStandard(t)); err == nil {
		t.Fatal("a manifest of another version was applied")
	}
}

// withStandard is a registry with the kernel, every form and the standard
// library: what a tool that runs anywhere has compiled in.
func withStandard(t *testing.T) *funroute.Registry {
	t.Helper()
	registry := funroute.CoreRegistry()
	if err := registry.EnableForm(funroute.SwitchForm, funroute.ForForm, funroute.ReduceForm); err != nil {
		t.Fatal(err)
	}
	if err := std.Register(registry); err != nil {
		t.Fatal(err)
	}
	return registry
}

// A manifest says which shape it is.
func TestManifestNamesItsVersion(t *testing.T) {
	t.Parallel()
	if got := withStandard(t).Manifest().Version(); got != funroute.ManifestVersion {
		t.Fatalf("Manifest().Version() = %d, want %d", got, funroute.ManifestVersion)
	}
}
