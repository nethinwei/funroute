package lang_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"testing"
	"time"

	"funroute/lang"
)

func TestPhaseZeroPublicContract(t *testing.T) {
	registry := lang.CoreRegistry()
	if _, err := lang.CompileExpr("if(", registry, lang.CompileOptions{}); !errors.Is(err, lang.ErrCompile) {
		t.Fatalf("compile error = %v", err)
	}
	if err := lang.ValidateContract(lang.CompileOptions{
		Args: []lang.ArgSpec{{Name: "x", Type: lang.IntType}, {Name: "x", Type: lang.IntType}},
	}); !errors.Is(err, lang.ErrContract) {
		t.Fatalf("contract error = %v", err)
	}
	if got := lang.EnumOf("channel", "stripe", "adyen").String(); got != `enum<channel>{adyen,stripe}` {
		t.Fatalf("enum type = %s", got)
	}
}

// A host says only what a machine cannot work out. The signature comes from
// the Go types, and the category falls out of the name's namespace.
func TestDocOnlyCarriesWhatAHostMustSay(t *testing.T) {
	registry := lang.CoreRegistry()
	if err := lang.Logic(registry, "payout.settle_v1", lang.Doc{Label: "结算"},
		func(amount int64) (int64, error) { return amount, nil }); err != nil {
		t.Fatal(err)
	}
	var settle lang.FunctionDescriptor
	for _, function := range lang.Catalog(registry).Functions {
		if function.Name == "payout.settle_v1" {
			settle = function
		}
	}
	if settle.Doc.Category != "payout" {
		t.Fatalf("category = %q, want the name's namespace", settle.Doc.Category)
	}
	if settle.Signature != "payout.settle_v1(int)->int" {
		t.Fatalf("signature = %q", settle.Signature)
	}
	if rendered := fmt.Sprint(settle.Doc); strings.Contains(rendered, "#") {
		t.Fatalf("display carries styling: %s", rendered)
	}
}

// A host stores artifacts and loads them later; the loader is what checks they
// were not edited in between. This also exercises HandleOf, the way a contract
// declares an engine value.
func TestHostChecksArtifactIdentity(t *testing.T) {
	registry := lang.CoreRegistry()
	type tensor struct{ Values []float64 }
	if err := lang.DefineHandle[*tensor](registry, "demo.tensor"); err != nil {
		t.Fatal(err)
	}
	if err := lang.Logic(registry, "demo.size_v1", lang.Doc{Label: "尺寸", Cost: 2},
		func(value *tensor) (int64, error) { return int64(len(value.Values)), nil }); err != nil {
		t.Fatal(err)
	}
	result := lang.IntType
	artifact, err := lang.CompileExpr("demo.size_v1(features)", registry, lang.CompileOptions{
		Args:   []lang.ArgSpec{{Name: "features", Type: lang.HandleOf("demo.tensor")}},
		Result: &result,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lang.Instantiate(artifact, registry); err != nil {
		t.Fatal(err)
	}
	// The digest is the identity: an artifact edited after compilation no
	// longer loads, which is what a host relies on when it stores them.
	artifact.Args[0].Name = "other"
	if _, err := lang.Instantiate(artifact, registry); err == nil {
		t.Fatal("an edited artifact still instantiated")
	}
}

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
	err := lang.Logic(registry, "route.is_healthy_v1", lang.Doc{
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
	result, err := runtime.Run(context.Background(), map[string]any{"health": "UP", "doubled": 500}, lang.RunOptions{Fuel: 1000})
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
	if catalog.Source.ExprJSONVersion != lang.ExprJSONVersion || catalog.ArtifactVersion != lang.ArtifactVersion || len(catalog.Source.Operators) == 0 {
		t.Fatalf("catalog versions/source = %+v", catalog)
	}
}

func TestPublicFloatRejectsNonFiniteValues(t *testing.T) {
	if _, err := lang.Float(math.NaN()); err == nil {
		t.Fatal("public Float accepted NaN")
	}
	if value, err := lang.Float(0.75); err != nil {
		t.Fatal(err)
	} else if number, _ := value.Float(); number != 0.75 {
		t.Fatalf("float = %v", number)
	}
}

// The deep-learning shape: a feature vector goes host → VM → extension and
// a score comes back. The extension must receive the host's own slice.
func TestHostVectorsReachExtensionsWithoutCopying(t *testing.T) {
	registry := lang.CoreRegistry()
	var received []float64
	err := lang.Logic(registry, "model.score_v1", lang.Doc{Cost: 10}, func(xs []float64) (float64, error) {
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
	result, err := runtime.RunValues(context.Background(), []lang.Value{value}, lang.RunOptions{Fuel: 100})
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
	if _, err := runtime.Run(context.Background(), map[string]any{"features": features}, lang.RunOptions{Fuel: 100}); err != nil {
		t.Fatal(err)
	}
	if &received[0] != &features[0] {
		t.Fatal("Run copied the host's vector")
	}
}

// A model's tensor crosses the expression as an opaque handle, and a batch of
// requests calls the model once. This is the deep-learning integration a host
// writes, end to end, with the public API only.
// tensor stands in for an engine's tensor type.
type tensor struct{ rows [][]float64 }

// modelRegistry defines the engine's type as a handle and registers a model
// with a batch implementation that counts how often it ran.
func modelRegistry(t *testing.T, batches *int) *lang.Registry {
	t.Helper()
	registry := lang.CoreRegistry()
	if err := lang.DefineHandle[*tensor](registry, "engine.tensor"); err != nil {
		t.Fatal(err)
	}
	err := lang.Model(registry, "model.embed_v1", lang.Doc{Cost: 10},
		func(features []float64) (*tensor, error) { return &tensor{rows: [][]float64{features}}, nil },
		func(features [][]float64) ([]*tensor, error) {
			*batches++
			out := make([]*tensor, len(features))
			for i, row := range features {
				out[i] = &tensor{rows: [][]float64{row}}
			}
			return out, nil
		})
	if err != nil {
		t.Fatal(err)
	}
	err = lang.Logic(registry, "model.score_v1", lang.Doc{Cost: 10}, func(t *tensor) (float64, error) { return t.rows[0][0], nil })
	if err != nil {
		t.Fatal(err)
	}
	return registry
}

func TestHostBatchesModelCallsAcrossRequests(t *testing.T) {
	var batches int
	registry := modelRegistry(t, &batches)
	artifact, err := lang.CompileExpr(`let(e = model.embed_v1(features), if(model.score_v1(e) > 0.5, "review", "accept"))`, registry,
		lang.CompileOptions{Args: []lang.ArgSpec{{Name: "features", Type: lang.ArrayOf(lang.FloatType)}}})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := lang.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	batch := lang.NewBatch(runtime, lang.BatchOptions{MaxSize: 2, MaxWait: time.Second})
	defer batch.Close()
	results := make([]string, 2)
	var wg sync.WaitGroup
	for i, score := range []float64{0.9, 0.1} {
		wg.Add(1)
		go func(i int, score float64) {
			defer wg.Done()
			features, _ := lang.ToValue([]float64{score})
			result, err := batch.Run(context.Background(), []lang.Value{features})
			if err != nil {
				t.Error(err)
				return
			}
			results[i], _ = result.String()
		}(i, score)
	}
	wg.Wait()
	if results[0] != "review" || results[1] != "accept" || batches != 1 {
		t.Fatalf("results = %v, batches = %d", results, batches)
	}
	// An expired budget is refused at the first model call, typed.
	expired, cancel := context.WithCancel(context.Background())
	cancel()
	features, _ := lang.ToValue([]float64{0.9})
	if _, err := runtime.RunValues(expired, []lang.Value{features}, lang.RunOptions{}); !errors.Is(err, lang.ErrDeadline) {
		t.Fatalf("expired error = %v", err)
	}
	if catalog := lang.Catalog(registry); catalog.ValueTypes[len(catalog.ValueTypes)-1].Type.String() != "handle<engine.tensor>" {
		t.Fatalf("value types = %+v", catalog.ValueTypes)
	}
}
