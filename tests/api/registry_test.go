package api

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"strings"
	"testing"

	"github.com/nethinwei/funroute"
)

// A host says only what a machine cannot work out. The signature comes from
// the Go types, and the category falls out of the name's namespace.
func TestDocOnlyCarriesWhatAHostMustSay(t *testing.T) {
	t.Parallel()
	registry := funroute.CoreRegistry()
	if err := registry.Register(funroute.FunctionSpec{
		Name: "payout.settle_v1",
		Doc:  funroute.Doc{Label: "结算"},
		Go:   func(amount int64) (int64, error) { return amount, nil },
	}); err != nil {
		t.Fatal(err)
	}
	var settle funroute.FunctionDescriptor
	for _, function := range registry.Catalog().Functions() {
		if function.Name() == "payout.settle_v1" {
			settle = function
		}
	}
	if settle.Doc().Category != "payout" {
		t.Fatalf("category = %q, want the name's namespace", settle.Doc().Category)
	}
	if settle.Signature() != "payout.settle_v1(int)->int" {
		t.Fatalf("signature = %q, want %q", settle.Signature(), "payout.settle_v1(int)->int")
	}
	if rendered := fmt.Sprint(settle.Doc()); strings.Contains(rendered, "#") {
		t.Fatalf("display carries styling: %s, want no colours", rendered)
	}
}

// The catalog lists what a registry offers.
func TestHostCanListTheCatalog(t *testing.T) {
	t.Parallel()
	catalog := hostRegistry(t).Catalog()
	if len(catalog.Functions()) == 0 || len(catalog.SpecialForms()) == 0 {
		t.Fatalf("catalog has %d functions and %d special forms, want some of each", len(catalog.Functions()), len(catalog.SpecialForms()))
	}
	encoded, err := json.Marshal(catalog)
	var shape struct {
		ArtifactVersion int `json:"artifact_version"`
	}
	if err != nil || json.Unmarshal(encoded, &shape) != nil || shape.ArtifactVersion != funroute.ArtifactVersion {
		t.Fatalf("the catalog's JSON names artifact version %d (%v), want %d", shape.ArtifactVersion, err, funroute.ArtifactVersion)
	}
}

func TestHostNamesTheRegistryAndArtifactTypes(t *testing.T) {
	t.Parallel()
	registry := funroute.NewRegistry()
	var double funroute.EvalFunc = func(_ context.Context, args []funroute.Value) (funroute.Value, error) {
		value, _ := args[0].Int()
		return funroute.Int(value * 2), nil
	}
	if err := registry.Register(funroute.FunctionSpec{
		Name: "demo.double", Params: []funroute.Type{funroute.IntType}, Result: funroute.IntType, Eval: double,
		// Pure arithmetic, so the host lets folding run it while compiling.
		Doc: funroute.Doc{
			Label: "翻倍", Category: "演示", Params: []string{"值"}, Result: "两倍",
			Constexpr: true,
		},
	}); err != nil {
		t.Fatal(err)
	}
	form := funroute.SwitchForm
	if err := registry.EnableForm(form); err != nil {
		t.Fatal(err)
	}
	artifact, err := funroute.CompileExpr(`demo.double(21)`, registry, funroute.CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	// The call reads no argument and the host marked it constexpr, so folding
	// ran it at compile time: the program is one load of the answer.
	if artifact.InstructionCount() != 1 {
		t.Fatalf("demo.double(21) compiled to %d instructions, want the folded answer alone", artifact.InstructionCount())
	}
	runtime := mustInstantiate(t, artifact, registry)
	batch := funroute.NewBatch(runtime, funroute.BatchOptions{MaxSize: 1})
	defer batch.Close()
	value, err := batch.Run(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := value.Int(); got != 42 {
		t.Fatalf("batch.Run(demo.double(21)) = %v, want 42", value.Any())
	}
}

func TestHostNamesTheCatalogTypes(t *testing.T) {
	t.Parallel()
	catalog := funroute.CoreRegistry().Catalog()
	function := catalog.Functions()[0]
	form := catalog.SpecialForms()[0]
	if function.Signature() == "" || form.Syntax() == "" || form.Doc().Label == "" {
		t.Fatalf("catalog entries are unlabelled: %+v %+v", function, form)
	}
}

// The catalog says what each function returns, and which of them are the
// kernel's lazy constructs rather than ordinary calls.
func TestCatalogNamesResultsAndSpecials(t *testing.T) {
	t.Parallel()
	results := map[string]string{}
	specials := map[string]string{}
	for _, function := range funroute.CoreRegistry().Catalog().Functions() {
		results[function.Signature()] = function.Result().String()
		if function.Special() != "" {
			specials[function.Name()] = function.Special()
		}
	}
	for signature, want := range map[string]string{
		"add(int,int)->int":     "int",
		"add(float,int)->float": "float",
		"if(bool,T,T)->T":       "T",
		"fallback(T,T,...)->T":  "T",
	} {
		if got := results[signature]; got != want {
			t.Errorf("%s: Result() = %q, want %q", signature, got, want)
		}
	}
	if want := map[string]string{"if": "if", "fallback": "fallback"}; !maps.Equal(specials, want) {
		t.Fatalf("Special() = %v, want %v", specials, want)
	}
}

// A host's function of an array may say it is a fold, and a call of it on a
// comprehension is then the comprehension folding as it goes: the answer the
// function gives, and the function itself is never handed an array.
func TestAHostFoldFoldsTheComprehension(t *testing.T) {
	t.Parallel()
	registry := funroute.CoreRegistry()
	if err := registry.EnableForm(funroute.ForForm); err != nil {
		t.Fatal(err)
	}
	var handed int
	total := func(fees []int64) int64 {
		handed++
		sum := int64(0)
		for _, fee := range fees {
			sum += fee
		}
		return sum
	}
	if err := registry.Register(funroute.FunctionSpec{
		Name: "fees.total_v1", Go: total, Fold: &funroute.Fold{Step: "add", Init: funroute.Int(0)},
	}); err != nil {
		t.Fatal(err)
	}
	fees := funroute.ArgSpec{Name: "fees", Type: funroute.ArrayOf(funroute.IntType)}
	artifact, err := funroute.CompileExpr(`fees.total_v1([fee * 2 for fee in fees if fee > 10])`, registry, funroute.CompileOptions{Args: []funroute.ArgSpec{fees}})
	if err != nil {
		t.Fatal(err)
	}
	value, err := mustInstantiate(t, artifact, registry).Run(t.Context(), map[string]any{"fees": []any{5, 20, 30}})
	if got, _ := value.Int(); err != nil || got != 100 || handed != 0 {
		t.Fatalf("the fold = %v, %v, the function handed %d arrays; want 100 and none", value.Any(), err, handed)
	}
}
