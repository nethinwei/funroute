package lang_test

import (
	"encoding/json"
	"errors"
	"slices"
	"testing"

	"funroute/extensions/std"
	"funroute/lang"
)

type engineTensor struct{}

// The host's registry, written as a manifest, stands in for it where its
// functions cannot run: programs compile to the same artifact, and running one
// says which calls it could not make.
func TestManifestStandsInForTheHostsFunctions(t *testing.T) {
	t.Parallel()
	host := withStandard(t)
	if err := lang.DefineHandle[*engineTensor](host, "demo.tensor"); err != nil {
		t.Fatal(err)
	}
	doc := lang.Doc{Label: "风险分", Cost: 25, Params: []string{"国家", "金额"}}
	if err := lang.Logic(host, "risk.score_v1", doc, func(country string, amount int64) (float64, error) { return 0.9, nil }); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(host.Manifest())
	if err != nil {
		t.Fatal(err)
	}
	var manifest lang.Manifest
	if err := json.Unmarshal(encoded, &manifest); err != nil {
		t.Fatal(err)
	}
	browser := withStandard(t)
	if err := manifest.Apply(browser); err != nil {
		t.Fatal(err)
	}
	contract := lang.CompileOptions{Args: []lang.ArgSpec{{Name: "country", Type: lang.StringType}, {Name: "amount", Type: lang.IntType}}}
	source := `fallback(risk.score_v1(country, amount), 0.5) > 0.8`
	atHost, err := lang.CompileExpr(source, host, contract)
	if err != nil {
		t.Fatal(err)
	}
	inBrowser, err := lang.CompileExpr(source, browser, contract)
	if err != nil {
		t.Fatalf("CompileExpr(%q) against the manifest: %v", source, err)
	}
	if inBrowser.Digest != atHost.Digest {
		t.Fatalf("against the manifest %q compiles to digest %s, want the host's %s", source, inBrowser.Digest, atHost.Digest)
	}
	checkUnavailableCall(t, inBrowser, browser, host)
}

func checkUnavailableCall(t *testing.T, artifact *lang.Artifact, browser, host *lang.Registry) {
	t.Helper()
	args := map[string]any{"country": "SG", "amount": 100}
	runtime, err := lang.Instantiate(artifact, browser)
	if err != nil {
		t.Fatal(err)
	}
	ctx, calls := lang.TrackUnavailable(t.Context())
	value, err := runtime.Run(ctx, args, lang.RunOptions{Fuel: 1000})
	if err != nil || value.Any() != false || !slices.Equal(calls(), []string{"risk.score_v1"}) {
		t.Fatalf("in the browser: %v, %v, calls %v; want false, no error, calls [risk.score_v1]", value.Any(), err, calls())
	}
	real, err := lang.Instantiate(artifact, host)
	if err != nil {
		t.Fatal(err)
	}
	if value, err := real.Run(t.Context(), args, lang.RunOptions{Fuel: 1000}); err != nil || value.Any() != true {
		t.Fatalf("at the host: %v, %v; want true", value.Any(), err)
	}
	bare, err := lang.CompileExpr(`risk.score_v1("SG", 1)`, browser, lang.CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	direct, _ := lang.Instantiate(bare, browser)
	if _, err := direct.Run(t.Context(), nil, lang.RunOptions{Fuel: 1000}); !errors.Is(err, lang.ErrUnavailable) || !errors.Is(err, lang.ErrExtension) {
		t.Fatalf("Run(risk.score_v1(\"SG\", 1)) error = %v, want ErrUnavailable and ErrExtension", err)
	}
}

// A function the registry already has must cost what the manifest says, or
// what one compiles the other would refuse to bind.
func TestManifestRefusesADisagreement(t *testing.T) {
	t.Parallel()
	manifest := withStandard(t).Manifest()
	for i, function := range manifest.Functions {
		if function.Name == "add" {
			manifest.Functions[i].Doc.Cost += 1
			break
		}
	}
	if err := manifest.Apply(withStandard(t)); err == nil {
		t.Fatal("a manifest that disagrees on a cost was applied")
	}
	manifest.Version = lang.ManifestVersion + 1
	if err := manifest.Apply(withStandard(t)); err == nil {
		t.Fatal("a manifest of another version was applied")
	}
}

// withStandard is a registry with the kernel, every form and the standard
// library: what a tool that runs anywhere has compiled in.
func withStandard(t *testing.T) *lang.Registry {
	t.Helper()
	registry := lang.CoreRegistry()
	if err := registry.EnableForm(lang.SwitchForm, lang.ForForm, lang.ReduceForm); err != nil {
		t.Fatal(err)
	}
	if err := std.Register(registry); err != nil {
		t.Fatal(err)
	}
	return registry
}
