package lang_test

import (
	"encoding/json"
	"strings"
	"testing"

	"funroute/lang"
)

// These tests are written the way a host writes one: they may only use what
// package lang exports, so they also guard the public surface. If one of them
// needs something from internal/core, the surface is too narrow — or the change
// belongs elsewhere.

// hostRegistry is the console a host builds: the kernel, the forms it allows,
// and its own domain functions.
func hostRegistry(t *testing.T) *lang.Registry {
	t.Helper()
	registry := lang.CoreRegistry()
	if err := registry.EnableForm(lang.SwitchForm); err != nil {
		t.Fatal(err)
	}
	err := lang.Fn1(registry, "route.is_healthy_v1", lang.Doc{
		Label:       "渠道是否健康",
		Description: "UP 表示可用",
		Category:    "路由",
	}, func(status string) (bool, error) { return status == "UP", nil })
	if err != nil {
		t.Fatal(err)
	}
	return registry
}

const hostSource = `switch(case route.is_healthy_v1(health) && doubled > 100 => "adyen", else "stripe")`

// hostContract is what a console reads from its rule record: the arguments in
// ABI order, their types and the prose it shows operators.
func hostContract() lang.CompileOptions {
	result := lang.StringType
	return lang.CompileOptions{
		Args: []lang.ArgSpec{
			{Name: "health", Type: lang.StringType, Doc: "渠道健康状态"},
			{Name: "doubled", Type: lang.IntType, Doc: "订单金额的两倍，单位：分"},
		},
		Result:    &result,
		ResultDoc: "渠道号",
	}
}

func TestHostCanCompileAndShipAnArtifact(t *testing.T) {
	registry := hostRegistry(t)
	artifact, err := lang.CompileExpr(hostSource, registry, hostContract())
	if err != nil {
		t.Fatal(err)
	}
	if len(artifact.Args) != 2 || artifact.Args[0].Name != "health" {
		t.Fatalf("args = %#v", artifact.Args)
	}
	if !artifact.Result.Equal(lang.StringType) || artifact.Digest == "" {
		t.Fatalf("result = %s digest = %q", artifact.Result, artifact.Digest)
	}

	// An artifact is meant to be stored and shipped as JSON.
	encoded, err := json.Marshal(artifact)
	if err != nil {
		t.Fatal(err)
	}
	var shipped lang.Artifact
	if err := json.Unmarshal(encoded, &shipped); err != nil {
		t.Fatal(err)
	}
	runtime, err := lang.Instantiate(&shipped, registry)
	if err != nil {
		t.Fatal(err)
	}
	result, err := runtime.Run(map[string]any{"health": "UP", "doubled": 500}, lang.RunOptions{Fuel: 1000})
	if err != nil {
		t.Fatal(err)
	}
	if text, _ := result.String(); text != "adyen" {
		t.Fatalf("result = %#v", result.Any())
	}
}

// Programs are exchanged as ExprJSON: that is the contract with a front end,
// and it must be lossless down to the digest. The contract itself travels
// separately, in the host's own record.
func TestHostCanRoundTripAProgramThroughJSON(t *testing.T) {
	registry := hostRegistry(t)
	artifact, err := lang.CompileExpr(hostSource, registry, hostContract())
	if err != nil {
		t.Fatal(err)
	}
	document, err := lang.ParseToJSON(hostSource)
	if err != nil {
		t.Fatal(err)
	}
	again, err := lang.CompileJSON(document, registry, hostContract())
	if err != nil {
		t.Fatal(err)
	}
	if again.Digest != artifact.Digest {
		t.Fatalf("round trip changed the digest:\n%s\n%s", artifact.Digest, again.Digest)
	}
	// The artifact carries the contract back, so a host that stored only the
	// artifact can still render the rule for a human.
	args, result, resultDoc := lang.ContractFromArtifact(artifact)
	text := lang.RenderWithContract(hostSource, args, result, resultDoc)
	if !strings.Contains(text, "health:") || !strings.Contains(text, "渠道健康状态") {
		t.Fatalf("rendered view = %s", text)
	}
	// Comments are not syntax: the rendered text is the same program.
	rendered, err := lang.CompileExpr(text, registry, hostContract())
	if err != nil {
		t.Fatal(err)
	}
	if rendered.Digest != artifact.Digest {
		t.Fatal("the rendered view is not the same program")
	}
}

// The catalog is what a front end renders.
func TestHostCanRenderTheCatalog(t *testing.T) {
	catalog := lang.Catalog(hostRegistry(t))
	if len(catalog.Functions) == 0 || len(catalog.SpecialForms) == 0 || len(catalog.Nodes) == 0 {
		t.Fatalf("catalog = %+v", catalog)
	}
}

// The deep-learning shape: a feature vector goes host → VM → extension and
// a score comes back. The extension must receive the host's own slice.
func TestHostVectorsReachExtensionsWithoutCopying(t *testing.T) {
	registry := lang.CoreRegistry()
	var received []float64
	err := lang.Fn1(registry, "model.score_v1", lang.Doc{Cost: 10}, func(xs []float64) (float64, error) {
		received = xs
		return xs[0], nil
	})
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := lang.CompileExpr(`model.score_v1(features)`, registry, lang.CompileOptions{
		Args: []lang.ArgSpec{{Name: "features", Type: lang.ArrayOf(lang.FloatType)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := lang.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	features := []float64{0.25, 0.5}
	value, err := lang.ToValue(features)
	if err != nil {
		t.Fatal(err)
	}
	result, err := runtime.RunValues([]lang.Value{value}, lang.RunOptions{Fuel: 100})
	if err != nil {
		t.Fatal(err)
	}
	if score, _ := lang.FromValue[float64](result); score != 0.25 {
		t.Fatalf("score = %v", score)
	}
	if &received[0] != &features[0] {
		t.Fatal("the extension received a copy of the host's vector")
	}
	// Run's by-name path takes the same shortcut for a Go slice.
	if _, err := runtime.Run(map[string]any{"features": features}, lang.RunOptions{Fuel: 100}); err != nil {
		t.Fatal(err)
	}
	if &received[0] != &features[0] {
		t.Fatal("Run copied the host's vector")
	}
}
