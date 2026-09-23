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
	for _, function := range registry.Catalog().Functions {
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
	// Formatting keeps the contract comments and is still the same program.
	formatted, err := lang.Format(text)
	if err != nil || !strings.HasPrefix(formatted, "// health:") {
		t.Fatalf("formatted view = %s (%v)", formatted, err)
	}
	if again, err := lang.CompileExpr(formatted, registry, hostContract()); err != nil || again.Digest != artifact.Digest {
		t.Fatalf("formatting changed the program: %v", err)
	}
}

// The catalog lists what a registry offers.
func TestHostCanListTheCatalog(t *testing.T) {
	catalog := hostRegistry(t).Catalog()
	if len(catalog.Functions) == 0 || len(catalog.SpecialForms) == 0 || catalog.ArtifactVersion != lang.ArtifactVersion {
		t.Fatalf("catalog = %+v", catalog)
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
	if handles := registry.Handles(); len(handles) != 1 || handles[0].String() != "handle<engine.tensor>" {
		t.Fatalf("handles = %+v", handles)
	}
}

// The SDK's job is to let a host write these types down. Code that only ever
// uses := never notices a missing alias, so the three tests below name every
// public type explicitly: drop one from lang.go and this file stops compiling.
func TestHostNamesTheValueTypes(t *testing.T) {
	var flag lang.Value = lang.Bool(true)
	var text lang.Value = lang.String("SGD")
	var kind lang.Kind = flag.Kind()
	var boolType lang.Type = lang.BoolType
	numbers, err := lang.Array(lang.IntType, []lang.Value{lang.Int(1), lang.Int(2)})
	if err != nil {
		t.Fatal(err)
	}
	weights, err := lang.Dict(lang.FloatType, map[string]lang.Value{"adyen": mustFloat(t, 0.6)})
	if err != nil {
		t.Fatal(err)
	}
	var dictType lang.Type = lang.DictOf(lang.FloatType)
	// A record is named fields with their own types, in an order that is part
	// of the type; the host writes both down.
	var orderType lang.Type = lang.RecordOf(
		lang.Field{Name: "amount", Type: lang.IntType},
		lang.Field{Name: "currency", Type: lang.StringType},
	)
	order, err := lang.Record(orderType, []lang.Value{lang.Int(1200), lang.String("SGD")})
	if err != nil {
		t.Fatal(err)
	}
	if amount, _ := order.Field(0).Int(); amount != 1200 || !order.Type().Equal(orderType) {
		t.Fatalf("record = %v", order.Any())
	}
	if kind.String() != "bool" || !flag.Type().Equal(boolType) {
		t.Fatalf("kind = %s", kind)
	}
	if got, _ := text.String(); got != "SGD" {
		t.Fatalf("string value = %q", got)
	}
	if length, _ := numbers.Length(); length != 2 {
		t.Fatalf("array length = %d", length)
	}
	if !weights.Type().Equal(dictType) {
		t.Fatalf("dict type = %s", weights.Type())
	}
}

func TestHostNamesTheRegistryAndArtifactTypes(t *testing.T) {
	registry := lang.NewRegistry()
	var double lang.EvalFunc = func(_ context.Context, args []lang.Value) (lang.Value, error) {
		value, _ := args[0].Int()
		return lang.Int(value * 2), nil
	}
	if err := registry.Register(lang.FunctionSpec{
		Name: "demo.double", Params: []lang.Type{lang.IntType}, Result: lang.IntType, Eval: double,
		// Pure arithmetic, so the host lets folding run it while compiling.
		Doc: lang.Doc{
			Label: "翻倍", Category: "演示", Cost: 2, Params: []string{"值"}, Result: "两倍",
			Constexpr: true,
		},
	}); err != nil {
		t.Fatal(err)
	}
	var form lang.Form = lang.SwitchForm
	if err := registry.EnableForm(form); err != nil {
		t.Fatal(err)
	}
	artifact, err := lang.CompileExpr(`demo.double(21)`, registry, lang.CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var constant lang.Constant = artifact.Constants[0]
	var instruction lang.Instruction = artifact.Instructions[0]
	// The call reads no argument and the host marked it constexpr, so folding
	// ran it at compile time and the pool holds the answer, not the input.
	if constant.Int == nil || *constant.Int != 42 {
		t.Fatalf("the folded result should be in the constant pool: %+v", constant)
	}
	var op lang.OpCode = instruction.Op
	var runtime *lang.Runtime = mustInstantiate(t, artifact, registry)
	var batch *lang.Batch = lang.NewBatch(runtime, lang.BatchOptions{MaxSize: 1})
	defer batch.Close()
	if op.String() == "" {
		t.Fatal("an opcode must name itself")
	}
	value, err := batch.Run(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := value.Int(); got != 42 {
		t.Fatalf("value = %v", value.Any())
	}
}

func TestHostNamesTheCatalogTypes(t *testing.T) {
	var catalog lang.LanguageCatalog = lang.CoreRegistry().Catalog()
	var function lang.FunctionDescriptor = catalog.Functions[0]
	var form lang.FormDescriptor = catalog.SpecialForms[0]
	if function.Signature == "" || form.Syntax == "" || form.Doc.Label == "" {
		t.Fatalf("catalog entries are unlabelled: %+v %+v", function, form)
	}
}

func mustFloat(t *testing.T, value float64) lang.Value {
	t.Helper()
	out, err := lang.Float(value)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func mustInstantiate(t *testing.T, artifact *lang.Artifact, registry *lang.Registry) *lang.Runtime {
	t.Helper()
	runtime, err := lang.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	return runtime
}

// A compile error carries where it happened; the text stays with the host, so
// turning the offset into a line and a column is a call the host makes.
func TestHostLocatesACompileError(t *testing.T) {
	source := "amount\n  + \"x\""
	_, err := lang.CompileExpr(source, lang.CoreRegistry(), lang.CompileOptions{
		Args: []lang.ArgSpec{{Name: "amount", Type: lang.IntType}},
	})
	if err == nil {
		t.Fatal("adding a string to an int must fail")
	}
	var positioned *lang.PositionError
	if !errors.As(err, &positioned) {
		t.Fatalf("the error should carry a position: %v", err)
	}
	line, column, ok := lang.LineColumn(err, source)
	if !ok || line != 2 || column != 3 {
		t.Fatalf("position = %d:%d (ok=%v)", line, column, ok)
	}
	if !errors.Is(err, lang.ErrCompile) {
		t.Fatal("a positioned error is still a compile error")
	}
}

// The pieces a host reaches for around a compile, used the way a console
// would: a contract written as data, arguments decoded from what was typed,
// the artifact's own description of what it takes and calls, and the budget.
func TestAHostDrivesARuleFromDataToResult(t *testing.T) {
	contract := &lang.TextContract{
		Types:  map[string]string{"Order": "record{amount: int}"},
		Args:   []lang.TextArg{{Name: "order", Type: "Order", Doc: "订单"}},
		Result: &lang.TextResult{Type: "int", Doc: "手续费"},
	}
	options, err := contract.Options()
	if err != nil {
		t.Fatal(err)
	}
	registry := lang.CoreRegistry()
	artifact, err := lang.CompileExpr(`order.amount * 25 / 10000`, registry, options)
	if err != nil {
		t.Fatal(err)
	}
	var params []lang.Parameter = artifact.Args
	var calls []lang.CallReference = artifact.Calls
	if len(params) != 1 || params[0].Name != "order" || len(calls) == 0 {
		t.Fatalf("the artifact takes %v and calls %v", params, calls)
	}
	args, err := lang.DecodeArgs([]byte(`{"order": {"amount": 9007199254740993}}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lang.DecodeArgs([]byte(`{} {}`)); !errors.Is(err, lang.ErrContract) {
		t.Fatalf("trailing data must be refused: %v", err)
	}
	runtime, err := lang.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Run(context.Background(), args, lang.RunOptions{Fuel: lang.DefaultFuel}); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Run(context.Background(), args, lang.RunOptions{Fuel: 1}); !errors.Is(err, lang.ErrFuel) {
		t.Fatalf("a run past its budget is ErrFuel: %v", err)
	}
}

// What a host exchanges besides text: the ExprJSON document names its
// version, a handle carries an engine's value by name, and a manifest lists
// each signature.
func TestTheDocumentsAHostExchanges(t *testing.T) {
	encoded, err := lang.ParseToJSON(`1 + 2`)
	if err != nil {
		t.Fatal(err)
	}
	var document struct{ Version int }
	if err := json.Unmarshal(encoded, &document); err != nil || document.Version != lang.ExprJSONVersion {
		t.Fatalf("the document is version %d, want %d (%v)", document.Version, lang.ExprJSONVersion, err)
	}
	if handle := lang.NewHandle("onnx.tensor", []float32{1}); handle.Type().String() != "handle<onnx.tensor>" {
		t.Fatalf("a handle is %s", handle.Type())
	}
	var functions []lang.ManifestFunction = lang.CoreRegistry().Manifest().Functions
	if len(functions) == 0 || functions[0].Name == "" {
		t.Fatalf("the manifest lists %v", functions)
	}
}
