package lang_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"funroute/lang"
)

// A host says only what a machine cannot work out. The signature comes from
// the Go types, and the category falls out of the name's namespace.
func TestDocOnlyCarriesWhatAHostMustSay(t *testing.T) {
	t.Parallel()
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
		t.Fatalf("signature = %q, want %q", settle.Signature, "payout.settle_v1(int)->int")
	}
	if rendered := fmt.Sprint(settle.Doc); strings.Contains(rendered, "#") {
		t.Fatalf("display carries styling: %s, want no colours", rendered)
	}
}

// The catalog lists what a registry offers.
func TestHostCanListTheCatalog(t *testing.T) {
	t.Parallel()
	catalog := hostRegistry(t).Catalog()
	if len(catalog.Functions) == 0 || len(catalog.SpecialForms) == 0 || catalog.ArtifactVersion != lang.ArtifactVersion {
		t.Fatalf("catalog = %+v, want functions, special forms and artifact version %d", catalog, lang.ArtifactVersion)
	}
}

func TestHostNamesTheRegistryAndArtifactTypes(t *testing.T) {
	t.Parallel()
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
	form := lang.SwitchForm
	if err := registry.EnableForm(form); err != nil {
		t.Fatal(err)
	}
	artifact, err := lang.CompileExpr(`demo.double(21)`, registry, lang.CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	constant := artifact.Constants[0]
	instruction := artifact.Instructions[0]
	// The call reads no argument and the host marked it constexpr, so folding
	// ran it at compile time and the pool holds the answer, not the input.
	if constant.Int == nil || *constant.Int != 42 {
		t.Fatalf("constant pool[0] = %+v, want the folded result 42", constant)
	}
	op := instruction.Op
	runtime := mustInstantiate(t, artifact, registry)
	batch := lang.NewBatch(runtime, lang.BatchOptions{MaxSize: 1})
	defer batch.Close()
	if op.String() == "" {
		t.Fatalf("OpCode(%d).String() is empty, want its name", uint8(op))
	}
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
	catalog := lang.CoreRegistry().Catalog()
	function := catalog.Functions[0]
	form := catalog.SpecialForms[0]
	if function.Signature == "" || form.Syntax == "" || form.Doc.Label == "" {
		t.Fatalf("catalog entries are unlabelled: %+v %+v", function, form)
	}
}
