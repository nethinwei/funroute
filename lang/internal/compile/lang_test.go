package compile

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"funroute/lang/internal/machine"
	"funroute/lang/internal/syntax"
	"strings"
	"testing"
)

func TestSampleInfersArgumentsAndRuns(t *testing.T) {
	registry := machine.CoreRegistry()
	artifact, err := CompileExpr(`if(a,b,add(1,1))`, registry, CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(artifact.Args) != 2 {
		t.Fatalf("args = %#v", artifact.Args)
	}
	if artifact.Args[0].Name != "a" || !artifact.Args[0].Type.Equal(machine.BoolType) {
		t.Fatalf("first arg = %#v", artifact.Args[0])
	}
	if artifact.Args[1].Name != "b" || !artifact.Args[1].Type.Equal(machine.IntType) {
		t.Fatalf("second arg = %#v", artifact.Args[1])
	}
	if !artifact.Result.Equal(machine.IntType) {
		t.Fatalf("result = %s", artifact.Result)
	}
	runtime, err := machine.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	result, err := runtime.Run(context.Background(), map[string]any{"a": true, "b": 41}, machine.RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if value, ok := result.Int(); !ok || value != 41 {
		t.Fatalf("true result = %#v", result.Any())
	}
	result, err = runtime.Run(context.Background(), map[string]any{"a": false, "b": 41}, machine.RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if value, ok := result.Int(); !ok || value != 2 {
		t.Fatalf("false result = %#v", result.Any())
	}
}

func TestStrongTypesAllowNumericWideningButRejectMixedContainers(t *testing.T) {
	registry := machine.CoreRegistry()
	artifact, err := CompileExpr(`add(1,1.5)`, registry, CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := machine.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	result, err := runtime.Run(context.Background(), map[string]any{}, machine.RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if value, ok := result.Float(); !ok || value != 2.5 {
		t.Fatalf("numeric widening result = %#v", result.Any())
	}
	for _, source := range []string{
		`[1,"two"]`,
		`{"one":1,"two":2.0}`,
	} {
		if _, err := CompileExpr(source, registry, CompileOptions{}); err == nil {
			t.Fatalf("%s compiled without a type error", source)
		}
	}
}

func TestArrayDictionaryAndGenericFunctions(t *testing.T) {
	registry := machine.CoreRegistry()
	registerCollectionTestExtensions(t, registry)
	artifact, err := CompileExpr(
		`if(has(weights,key),get(weights,key),get([0.25,0.5],fallback))`,
		registry,
		CompileOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}
	want := []machine.Parameter{
		{Name: "weights", Type: machine.DictOf(machine.FloatType)},
		{Name: "key", Type: machine.StringType},
		{Name: "fallback", Type: machine.IntType},
	}
	if len(artifact.Args) != len(want) {
		t.Fatalf("args = %#v", artifact.Args)
	}
	for i := range want {
		if artifact.Args[i].Name != want[i].Name || !artifact.Args[i].Type.Equal(want[i].Type) {
			t.Fatalf("arg %d = %#v, want %#v", i, artifact.Args[i], want[i])
		}
	}
	runtime, err := machine.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	result, err := runtime.Run(context.Background(), map[string]any{
		"weights":  map[string]any{"primary": 0.9},
		"key":      "missing",
		"fallback": 1,
	}, machine.RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if value, ok := result.Float(); !ok || value != 0.5 {
		t.Fatalf("result = %#v", result.Any())
	}
}

func TestExtensionSignatureDrivesInference(t *testing.T) {
	registry := machine.CoreRegistry()
	err := registry.Register(machine.FunctionSpec{
		Name:   "risk.approved_v1",
		Params: []machine.Type{machine.StringType, machine.IntType},
		Result: machine.BoolType,
		// An extension sees values the way a host does: through the public
		// accessors, never the private fields.
		Eval: func(_ context.Context, args []machine.Value) (machine.Value, error) {
			country, _ := args[0].String()
			amount, _ := args[1].Int()
			return machine.Bool(country == "US" && amount > 100), nil
		},
		Doc: machine.Doc{Label: "风险通过", Description: "演示扩展函数", Category: "风控", Cost: 25},
	})
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := CompileExpr(`risk.approved_v1(country,amount)`, registry, CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got := artifact.Args[0].Type; !got.Equal(machine.StringType) {
		t.Fatalf("country type = %s", got)
	}
	if got := artifact.Args[1].Type; !got.Equal(machine.IntType) {
		t.Fatalf("amount type = %s", got)
	}
	runtime, err := machine.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	result, err := runtime.Run(context.Background(), map[string]any{"country": "US", "amount": 200}, machine.RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if value, ok := result.Bool(); !ok || !value {
		t.Fatalf("result = %#v", result.Any())
	}
}

func registerCollectionTestExtensions(t *testing.T, registry *machine.Registry) {
	t.Helper()
	typeT := machine.TypeVar("T")
	for _, spec := range []machine.FunctionSpec{
		{
			Name: "has", Params: []machine.Type{machine.DictOf(typeT), machine.StringType}, Result: machine.BoolType,
			Eval: func(_ context.Context, args []machine.Value) (machine.Value, error) {
				entries, _ := args[0].Dict()
				key, _ := args[1].String()
				_, ok := entries[key]
				return machine.Bool(ok), nil
			},
		},
		{
			Name: "get", Params: []machine.Type{machine.DictOf(typeT), machine.StringType}, Result: typeT,
			Eval: func(_ context.Context, args []machine.Value) (machine.Value, error) {
				entries, _ := args[0].Dict()
				key, _ := args[1].String()
				value, ok := entries[key]
				if !ok {
					return machine.Value{}, fmt.Errorf("missing key")
				}
				return value, nil
			},
		},
		{
			Name: "get", Params: []machine.Type{machine.ArrayOf(typeT), machine.IntType}, Result: typeT,
			Eval: func(_ context.Context, args []machine.Value) (machine.Value, error) {
				items, _ := args[0].Array()
				index, _ := args[1].Int()
				if index < 0 || index >= int64(len(items)) {
					return machine.Value{}, fmt.Errorf("bad index")
				}
				return items[index], nil
			},
		},
	} {
		if err := registry.Register(spec); err != nil {
			t.Fatal(err)
		}
	}
}

func TestExprJSONCanonicalRoundTrip(t *testing.T) {
	expr, err := syntax.Parse(`{"z":add(z,1),"a":a}`)
	if err != nil {
		t.Fatal(err)
	}
	first, err := syntax.ExportExprJSON(expr)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Index(first, []byte(`"key":"a"`)) > bytes.Index(first, []byte(`"key":"z"`)) {
		t.Fatalf("dictionary keys are not canonical: %s", first)
	}
	imported, err := syntax.ImportExprJSON(first)
	if err != nil {
		t.Fatal(err)
	}
	second, err := syntax.ExportExprJSON(imported)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("round trip changed JSON\nfirst:  %s\nsecond: %s", first, second)
	}
	registry := machine.CoreRegistry()
	before, err := CompileAST(expr, registry, CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	after, err := CompileAST(imported, registry, CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(before.Args) != 2 || before.Args[0].Name != "a" || before.Args[1].Name != "z" {
		t.Fatalf("canonical args = %#v", before.Args)
	}
	if before.Digest != after.Digest {
		t.Fatalf("compile digest changed across AST round trip: %s != %s", before.Digest, after.Digest)
	}
}

func TestIfIsLazyAndFuelIsEnforced(t *testing.T) {
	registry := machine.CoreRegistry()
	artifact, err := CompileExpr(`if(flag,7,div(1,0))`, registry, CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := machine.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	result, err := runtime.Run(context.Background(), map[string]any{"flag": true}, machine.RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if value, ok := result.Int(); !ok || value != 7 {
		t.Fatalf("result = %#v", result.Any())
	}
	_, err = runtime.Run(context.Background(), map[string]any{"flag": true}, machine.RunOptions{Fuel: 1})
	if err == nil || !strings.Contains(err.Error(), "fuel") {
		t.Fatalf("fuel error = %v", err)
	}
}

func TestFunctionalForBindsLocalFiltersAndMaps(t *testing.T) {
	registry := consoleRegistry(t)
	artifact, err := CompileExpr(
		`[string(channel) for channel in channels if eq(channel,"UP")]`,
		registry,
		CompileOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}
	wantArgs := []machine.Parameter{{Name: "channels", Type: machine.ArrayOf(machine.StringType)}}
	if len(artifact.Args) != len(wantArgs) {
		t.Fatalf("args = %#v", artifact.Args)
	}
	for i := range wantArgs {
		if artifact.Args[i].Name != wantArgs[i].Name || !artifact.Args[i].Type.Equal(wantArgs[i].Type) {
			t.Fatalf("arg %d = %#v, want %#v", i, artifact.Args[i], wantArgs[i])
		}
	}
	runtime, err := machine.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	result, err := runtime.Run(context.Background(), map[string]any{
		"channels": []any{"UP", "DOWN", "UP"},
	}, machine.RunOptions{Fuel: 10_000})
	if err != nil {
		t.Fatal(err)
	}
	items, ok := result.Array()
	if !ok || len(items) != 2 {
		t.Fatalf("result = %#v", result.Any())
	}
	for i, item := range items {
		if value, ok := item.String(); !ok || value != "UP" {
			t.Fatalf("result item %d = %#v", i, item.Any())
		}
	}
	if artifact.Locals != 1 {
		t.Fatalf("locals = %d", artifact.Locals)
	}
}

func TestFunctionalSwitchIsLazyAndTyped(t *testing.T) {
	registry := consoleRegistry(t)
	artifact, err := CompileExpr(`switch(country, case "SG" => 1, case "MY" => 2, else div(1,0))`, registry, CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := machine.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	result, err := runtime.Run(context.Background(), map[string]any{"country": "MY"}, machine.RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if value, ok := result.Int(); !ok || value != 2 {
		t.Fatalf("result = %#v", result.Any())
	}
}

func TestImplicitNumericDefaultAndExplicitConversions(t *testing.T) {
	registry := machine.CoreRegistry()
	artifact, err := CompileExpr(`add(a,b)`, registry, CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !artifact.Args[0].Type.Equal(machine.IntType) || !artifact.Args[1].Type.Equal(machine.IntType) || !artifact.Result.Equal(machine.IntType) {
		t.Fatalf("implicit default = %#v -> %s", artifact.Args, artifact.Result)
	}
	artifact, err = CompileExpr(`add(a,b)`, registry, CompileOptions{
		Args: []ArgSpec{{Name: "a", Type: machine.FloatType}, {Name: "b", Type: machine.FloatType}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !artifact.Result.Equal(machine.FloatType) {
		t.Fatalf("result = %s", artifact.Result)
	}
	artifact, err = CompileExpr(`add(float(amount),0.5)`, registry, CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(artifact.Args) != 1 || !artifact.Args[0].Type.Equal(machine.IntType) || !artifact.Result.Equal(machine.FloatType) {
		t.Fatalf("conversion inference = %#v -> %s", artifact.Args, artifact.Result)
	}
}

func TestArtifactJSONRoundTripAndTamperDetection(t *testing.T) {
	registry := machine.CoreRegistry()
	// The expression reads an argument on purpose: a closed one would be folded
	// to a single constant at compile time, leaving no call to tamper with.
	artifact, err := CompileExpr(`add(n,2)`, registry, CompileOptions{Args: []ArgSpec{{Name: "n", Type: machine.IntType}}})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(artifact)
	if err != nil {
		t.Fatal(err)
	}
	var restored machine.Artifact
	if err := json.Unmarshal(encoded, &restored); err != nil {
		t.Fatal(err)
	}
	if _, err := machine.Instantiate(&restored, registry); err != nil {
		t.Fatalf("round-tripped artifact: %v", err)
	}
	runtime, err := machine.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	artifact.Instructions[0].A = 999
	result, err := runtime.Run(context.Background(), map[string]any{"n": 1}, machine.RunOptions{})
	if err != nil {
		t.Fatalf("runtime retained mutable artifact: %v", err)
	}
	if value, ok := result.Int(); !ok || value != 3 {
		t.Fatalf("snapshotted runtime result = %#v", result.Any())
	}
	artifact.Calls[0].Cost++
	if _, err := machine.Instantiate(artifact, registry); err == nil {
		t.Fatal("mutated artifact was accepted")
	}
}

func TestCoreCatalogIsMinimalAndCarriesDisplayMetadata(t *testing.T) {
	registry := machine.CoreRegistry()
	catalog := registry.Catalog()
	names := map[string]bool{}
	var fallback machine.FunctionDescriptor
	for _, function := range catalog.Functions {
		names[function.Name] = true
		if function.Name == "fallback" {
			fallback = function
		}
		if function.Doc.Label == "" || function.Doc.Description == "" || function.Doc.Category == "" {
			t.Fatalf("missing display metadata: %#v", function)
		}
		if len(function.Doc.Params) != len(function.Params) {
			t.Fatalf("parameter display mismatch: %#v", function)
		}
	}
	for _, name := range []string{
		"if", "fallback", "eq", "lt", "le", "gt", "ge",
		"add", "sub", "mul", "div", "int", "float", "string", "bool",
	} {
		if !names[name] {
			t.Fatalf("core function %q is missing", name)
		}
	}
	if len(names) != 15 {
		t.Fatalf("core registry is not minimal: %#v", names)
	}
	if !fallback.Variadic || fallback.Signature != "fallback(T,T,...)->T" || len(fallback.Params) != 2 {
		t.Fatalf("fallback catalog = %#v", fallback)
	}
	assertDisplayCarriesNoStyling(t, catalog)
	assertSignaturesNameTheirForm(t, catalog)
	assertLabelCountIsChecked(t, registry)
}

// A special form's signature is the one hand-written string in the catalog:
// it is a syntax shape, not a type signature, so it cannot be generated. It
// can at least be checked for naming the form it describes.
func assertSignaturesNameTheirForm(t *testing.T, catalog machine.LanguageCatalog) {
	t.Helper()
	for _, form := range append(append([]machine.FunctionDescriptor(nil), catalog.SpecialForms...), catalog.Functions...) {
		if !strings.HasPrefix(form.Signature, form.Name+"(") && !strings.Contains(form.Signature, form.Name) {
			t.Fatalf("signature %q does not name %q", form.Signature, form.Name)
		}
	}
}

func assertDisplayCarriesNoStyling(t *testing.T, catalog machine.LanguageCatalog) {
	t.Helper()
	for _, function := range catalog.Functions {
		if strings.Contains(fmt.Sprint(function.Doc), "#") {
			t.Fatalf("function %s display carries styling: %+v", function.Name, function.Doc)
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
		t.Fatalf("mismatched label count error = %v", err)
	}
}

func specialFormNames(catalog machine.LanguageCatalog) string {
	names := make([]string, len(catalog.SpecialForms))
	for i, form := range catalog.SpecialForms {
		names[i] = form.Name
	}
	return strings.Join(names, ",")
}

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

func compileAndRun(t *testing.T, source string, registry *machine.Registry, args map[string]any, options machine.RunOptions) (machine.Value, *machine.Runtime) {
	t.Helper()
	artifact, err := CompileExpr(source, registry, CompileOptions{})
	if err != nil {
		t.Fatalf("compile %s: %v", source, err)
	}
	runtime, err := machine.Instantiate(artifact, registry)
	if err != nil {
		t.Fatalf("instantiate %s: %v", source, err)
	}
	value, err := runtime.Run(context.Background(), args, options)
	if err != nil {
		t.Fatalf("run %s: %v", source, err)
	}
	return value, runtime
}

func TestReduceFoldsArrayWithLocalAccumulator(t *testing.T) {
	value, runtime := compileAndRun(t,
		`reduce(price in prices, total from 0, add(total,price))`,
		consoleRegistry(t), map[string]any{"prices": []any{10, 20, 30}}, machine.RunOptions{})
	params := runtime.Args()
	if len(params) != 1 || params[0].Name != "prices" || !params[0].Type.Equal(machine.ArrayOf(machine.IntType)) {
		t.Fatalf("args = %#v", params)
	}
	if !runtime.ResultType().Equal(machine.IntType) {
		t.Fatalf("result type = %s", runtime.ResultType())
	}
	if got, _ := value.Int(); got != 60 {
		t.Fatalf("value = %v", value.Any())
	}

	// An empty source yields the initial accumulator without running the body.
	empty, _ := compileAndRun(t,
		`reduce(price in prices, total from 7, add(total,price))`,
		consoleRegistry(t), map[string]any{"prices": []any{}}, machine.RunOptions{})
	if got, _ := empty.Int(); got != 7 {
		t.Fatalf("empty reduce = %v", empty.Any())
	}
}

func TestReduceRejectsBodyThatChangesAccumulatorType(t *testing.T) {
	registry := consoleRegistry(t)
	_, err := CompileExpr(`reduce(price in prices, total from 0, string(total))`, registry, CompileOptions{})
	if err == nil || !strings.Contains(err.Error(), "accumulator") {
		t.Fatalf("error = %v", err)
	}
	if _, err := CompileExpr(`reduce(price in prices, price from 0, price)`, registry, CompileOptions{}); err == nil {
		t.Fatal("reduce accepted the same name for item and accumulator")
	}
}

func TestReduceAndComprehensionSurviveExprJSONRoundTrip(t *testing.T) {
	for _, source := range []string{
		`reduce(price in prices, total from 0, add(total,price))`,
		`[channel for channel in channels if eq(channel,"UP")]`,
		`[add(x,1) for x in [mul(y,2) for y in items]]`,
	} {
		expr, err := syntax.Parse(source)
		if err != nil {
			t.Fatalf("parse %s: %v", source, err)
		}
		first, err := syntax.ExportExprJSON(expr)
		if err != nil {
			t.Fatal(err)
		}
		imported, err := syntax.ImportExprJSON(first)
		if err != nil {
			t.Fatalf("import %s: %v", source, err)
		}
		second, err := syntax.ExportExprJSON(imported)
		if err != nil {
			t.Fatal(err)
		}
		if string(first) != string(second) {
			t.Fatalf("%s is not canonical:\n%s\n%s", source, first, second)
		}
	}
}

func TestDisabledFormsAreRejectedWhenTheyArriveAsExprJSON(t *testing.T) {
	expr, err := syntax.Parse(`[reduce(p in row, t from 0, add(t,p)) for row in rows]`)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := syntax.ExportExprJSON(expr)
	if err != nil {
		t.Fatal(err)
	}
	imported, err := syntax.ImportExprJSON(encoded)
	if err != nil {
		t.Fatal(err)
	}
	switchAndFor := machine.CoreRegistry()
	if err := switchAndFor.EnableForm(machine.SwitchForm, machine.ForForm); err != nil {
		t.Fatal(err)
	}
	if _, err := CompileAST(imported, switchAndFor, CompileOptions{}); err == nil ||
		!strings.Contains(err.Error(), "reduce is not enabled") {
		t.Fatalf("CompileAST bypassed the registry forms: %v", err)
	}
	if _, err := CompileAST(imported, machine.CoreRegistry(), CompileOptions{}); err == nil ||
		!strings.Contains(err.Error(), "for is not enabled") {
		t.Fatalf("CompileAST accepted a disabled form: %v", err)
	}
}

func TestPanickingExtensionIsContained(t *testing.T) {
	registry := machine.CoreRegistry()
	if err := registry.Register(machine.FunctionSpec{
		Name: "boom_v1", Params: []machine.Type{machine.IntType}, Result: machine.IntType,
		Eval: func(_ context.Context, args []machine.Value) (machine.Value, error) { panic("extension exploded") },
		Doc:  machine.Doc{Label: "炸弹", Description: "总是 panic 的扩展", Category: "测试", Cost: 1},
	}); err != nil {
		t.Fatal(err)
	}
	artifact, err := CompileExpr(`boom_v1(n)`, registry, CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := machine.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	_, err = runtime.Run(context.Background(), map[string]any{"n": 1}, machine.RunOptions{Fuel: 100})
	if !errors.Is(err, machine.ErrExtension) || !strings.Contains(err.Error(), "extension panicked") {
		t.Fatalf("panic was not contained: %v", err)
	}
	// The runtime stays usable afterwards.
	if _, err := runtime.Run(context.Background(), map[string]any{"n": 2}, machine.RunOptions{Fuel: 100}); err == nil ||
		!strings.Contains(err.Error(), "extension panicked") {
		t.Fatalf("second run: %v", err)
	}
}

func TestSwitchEvaluatesItsSubjectOnce(t *testing.T) {
	registry := machine.CoreRegistry()
	if err := registry.EnableForm(machine.SwitchForm); err != nil {
		t.Fatal(err)
	}
	var calls int
	if err := machine.Logic(registry, "probe_v1", machine.Doc{Cost: 1}, func(value int64) (int64, error) {
		calls++
		return value, nil
	}); err != nil {
		t.Fatal(err)
	}
	artifact, err := CompileExpr(`switch(probe_v1(n), case 1 => "one", case 2 => "two", else "other")`, registry, CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := machine.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	result, err := runtime.Run(context.Background(), map[string]any{"n": 2}, machine.RunOptions{Fuel: 100})
	if err != nil {
		t.Fatal(err)
	}
	if text, _ := result.String(); text != "two" || calls != 1 {
		t.Fatalf("result = %q, subject calls = %d", text, calls)
	}
}

func TestCatalogListsSwitchableAndDerivedForms(t *testing.T) {
	// The kernel enables no switchable form; let and the derived forms are
	// always there, because let only binds names and the derived forms expand
	// to if, which the kernel always has.
	if got := specialFormNames(machine.CoreRegistry().Catalog()); got != "let,and,or,not" {
		t.Fatalf("core special forms = %s", got)
	}
	if got := specialFormNames(consoleRegistry(t).Catalog()); got != "switch,for,reduce,let,and,or,not" {
		t.Fatalf("operator special forms = %s", got)
	}
	// Only the known forms can be enabled.
	if err := machine.CoreRegistry().EnableForm("lambda"); err == nil {
		t.Fatal("unknown form was accepted")
	}
}

func TestCatalogCarriesTheParsersSourceContract(t *testing.T) {
	catalog := Catalog(consoleRegistry(t))
	if catalog.Source.ExprJSONVersion != syntax.ExprJSONVersion || catalog.Source.VariableNamePattern == "" {
		t.Fatalf("source syntax = %+v", catalog.Source)
	}
	found := map[string]int{}
	for _, operator := range catalog.Source.Operators {
		found[operator.Token] = operator.Precedence
		if operator.Template.Node == "" {
			t.Fatalf("operator %q has no canonical template", operator.Token)
		}
	}
	if found["||"] != 1 || found["<"] != 4 || found["+"] != 5 || found["!"] != 7 {
		t.Fatalf("operator precedences = %v", found)
	}
}
