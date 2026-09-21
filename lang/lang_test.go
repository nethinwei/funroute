package lang

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestSampleInfersArgumentsAndRuns(t *testing.T) {
	registry := CoreRegistry()
	artifact, err := CompileExpr(`if(a,b,add(1,1))`, registry, CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(artifact.Args) != 2 {
		t.Fatalf("args = %#v", artifact.Args)
	}
	if artifact.Args[0].Name != "a" || !artifact.Args[0].Type.Equal(BoolType) {
		t.Fatalf("first arg = %#v", artifact.Args[0])
	}
	if artifact.Args[1].Name != "b" || !artifact.Args[1].Type.Equal(IntType) {
		t.Fatalf("second arg = %#v", artifact.Args[1])
	}
	if !artifact.Result.Equal(IntType) {
		t.Fatalf("result = %s", artifact.Result)
	}
	runtime, err := Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	result, err := runtime.Run(map[string]any{"a": true, "b": 41}, RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if value, ok := result.Int(); !ok || value != 41 {
		t.Fatalf("true result = %#v", result.Any())
	}
	result, err = runtime.Run(map[string]any{"a": false, "b": 41}, RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if value, ok := result.Int(); !ok || value != 2 {
		t.Fatalf("false result = %#v", result.Any())
	}
}

func TestStrongTypesAllowNumericWideningButRejectMixedContainers(t *testing.T) {
	registry := CoreRegistry()
	artifact, err := CompileExpr(`add(1,1.5)`, registry, CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	result, err := runtime.Run(map[string]any{}, RunOptions{})
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
	registry := CoreRegistry()
	registerCollectionTestExtensions(t, registry)
	artifact, err := CompileExpr(
		`if(has(weights,key),get(weights,key),get([0.25,0.5],fallback))`,
		registry,
		CompileOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}
	want := []Parameter{
		{Name: "weights", Type: DictOf(FloatType)},
		{Name: "key", Type: StringType},
		{Name: "fallback", Type: IntType},
	}
	if len(artifact.Args) != len(want) {
		t.Fatalf("args = %#v", artifact.Args)
	}
	for i := range want {
		if artifact.Args[i].Name != want[i].Name || !artifact.Args[i].Type.Equal(want[i].Type) {
			t.Fatalf("arg %d = %#v, want %#v", i, artifact.Args[i], want[i])
		}
	}
	runtime, err := Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	result, err := runtime.Run(map[string]any{
		"weights":  map[string]any{"primary": 0.9},
		"key":      "missing",
		"fallback": 1,
	}, RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if value, ok := result.Float(); !ok || value != 0.5 {
		t.Fatalf("result = %#v", result.Any())
	}
}

func TestExtensionSignatureDrivesInference(t *testing.T) {
	registry := CoreRegistry()
	err := registry.Register(FunctionSpec{
		Name:   "risk.approved@1",
		Params: []Type{StringType, IntType},
		Result: BoolType,
		Cost:   25,
		Eval: func(args []Value) (Value, error) {
			return Bool(args[0].s == "US" && args[1].i > 100), nil
		},
		Display: FunctionDisplay{Label: "风险通过", Description: "演示扩展函数", Category: "风控", Color: "#DC2626"},
	})
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := CompileExpr(`risk.approved@1(country,amount)`, registry, CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got := artifact.Args[0].Type; !got.Equal(StringType) {
		t.Fatalf("country type = %s", got)
	}
	if got := artifact.Args[1].Type; !got.Equal(IntType) {
		t.Fatalf("amount type = %s", got)
	}
	runtime, err := Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	result, err := runtime.Run(map[string]any{"country": "US", "amount": 200}, RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if value, ok := result.Bool(); !ok || !value {
		t.Fatalf("result = %#v", result.Any())
	}
}

func registerCollectionTestExtensions(t *testing.T, registry *Registry) {
	t.Helper()
	typeT := TypeVar("T")
	for _, spec := range []FunctionSpec{
		{
			Name: "has", Params: []Type{DictOf(typeT), StringType}, Result: BoolType,
			Eval: func(args []Value) (Value, error) {
				_, ok := args[0].entries[args[1].s]
				return Bool(ok), nil
			},
		},
		{
			Name: "get", Params: []Type{DictOf(typeT), StringType}, Result: typeT,
			Eval: func(args []Value) (Value, error) {
				value, ok := args[0].entries[args[1].s]
				if !ok {
					return Value{}, fmt.Errorf("missing key")
				}
				return value, nil
			},
		},
		{
			Name: "get", Params: []Type{ArrayOf(typeT), IntType}, Result: typeT,
			Eval: func(args []Value) (Value, error) {
				index := args[1].i
				if index < 0 || index >= int64(len(args[0].items)) {
					return Value{}, fmt.Errorf("bad index")
				}
				return args[0].items[index], nil
			},
		},
	} {
		if err := registry.Register(spec); err != nil {
			t.Fatal(err)
		}
	}
}

func TestExprJSONCanonicalRoundTrip(t *testing.T) {
	expr, err := Parse(`{"z":add(z,1),"a":a}`)
	if err != nil {
		t.Fatal(err)
	}
	first, err := ExportExprJSON(expr)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Index(first, []byte(`"key":"a"`)) > bytes.Index(first, []byte(`"key":"z"`)) {
		t.Fatalf("dictionary keys are not canonical: %s", first)
	}
	imported, err := ImportExprJSON(first)
	if err != nil {
		t.Fatal(err)
	}
	second, err := ExportExprJSON(imported)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("round trip changed JSON\nfirst:  %s\nsecond: %s", first, second)
	}
	registry := CoreRegistry()
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
	registry := CoreRegistry()
	artifact, err := CompileExpr(`if(flag,7,div(1,0))`, registry, CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	result, err := runtime.Run(map[string]any{"flag": true}, RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if value, ok := result.Int(); !ok || value != 7 {
		t.Fatalf("result = %#v", result.Any())
	}
	_, err = runtime.Run(map[string]any{"flag": true}, RunOptions{Fuel: 1})
	if err == nil || !strings.Contains(err.Error(), "fuel") {
		t.Fatalf("fuel error = %v", err)
	}
}

func TestFunctionalForBindsLocalFiltersAndMaps(t *testing.T) {
	registry := consoleRegistry(t)
	artifact, err := CompileExpr(
		`for(channels,channel,eq(channel,"UP"),string(channel))`,
		registry,
		CompileOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}
	wantArgs := []Parameter{{Name: "channels", Type: ArrayOf(StringType)}}
	if len(artifact.Args) != len(wantArgs) {
		t.Fatalf("args = %#v", artifact.Args)
	}
	for i := range wantArgs {
		if artifact.Args[i].Name != wantArgs[i].Name || !artifact.Args[i].Type.Equal(wantArgs[i].Type) {
			t.Fatalf("arg %d = %#v, want %#v", i, artifact.Args[i], wantArgs[i])
		}
	}
	runtime, err := Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	result, err := runtime.Run(map[string]any{
		"channels": []any{"UP", "DOWN", "UP"},
	}, RunOptions{Fuel: 10_000})
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
	artifact, err := CompileExpr(`switch(country,"SG",1,"MY",2,div(1,0))`, registry, CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	result, err := runtime.Run(map[string]any{"country": "MY"}, RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if value, ok := result.Int(); !ok || value != 2 {
		t.Fatalf("result = %#v", result.Any())
	}
}

func TestImplicitNumericDefaultAndExplicitConversions(t *testing.T) {
	registry := CoreRegistry()
	artifact, err := CompileExpr(`add(a,b)`, registry, CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !artifact.Args[0].Type.Equal(IntType) || !artifact.Args[1].Type.Equal(IntType) || !artifact.Result.Equal(IntType) {
		t.Fatalf("implicit default = %#v -> %s", artifact.Args, artifact.Result)
	}
	artifact, err = CompileExpr(`add(a,b)`, registry, CompileOptions{
		ArgTypes: map[string]Type{"a": FloatType, "b": FloatType},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !artifact.Result.Equal(FloatType) {
		t.Fatalf("result = %s", artifact.Result)
	}
	artifact, err = CompileExpr(`add(float(amount),0.5)`, registry, CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(artifact.Args) != 1 || !artifact.Args[0].Type.Equal(IntType) || !artifact.Result.Equal(FloatType) {
		t.Fatalf("conversion inference = %#v -> %s", artifact.Args, artifact.Result)
	}
	if _, err := CompileExpr(`recur(n)`, registry, CompileOptions{}); err == nil || !strings.Contains(err.Error(), "recur is not enabled") {
		t.Fatalf("recur error = %v", err)
	}
}

func TestArtifactJSONRoundTripAndTamperDetection(t *testing.T) {
	registry := CoreRegistry()
	artifact, err := CompileExpr(`add(1,2)`, registry, CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(artifact)
	if err != nil {
		t.Fatal(err)
	}
	var restored Artifact
	if err := json.Unmarshal(encoded, &restored); err != nil {
		t.Fatal(err)
	}
	if _, err := Instantiate(&restored, registry); err != nil {
		t.Fatalf("round-tripped artifact: %v", err)
	}
	runtime, err := Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	artifact.Instructions[0].A = 999
	result, err := runtime.Run(map[string]any{}, RunOptions{})
	if err != nil {
		t.Fatalf("runtime retained mutable artifact: %v", err)
	}
	if value, ok := result.Int(); !ok || value != 3 {
		t.Fatalf("snapshotted runtime result = %#v", result.Any())
	}
	artifact.Calls[0].Cost++
	if _, err := Instantiate(artifact, registry); err == nil {
		t.Fatal("mutated artifact was accepted")
	}
}

func TestCoreCatalogIsMinimalAndCarriesDisplayMetadata(t *testing.T) {
	registry := CoreRegistry()
	catalog := registry.Catalog()
	names := map[string]bool{}
	for _, function := range catalog.Functions {
		names[function.Name] = true
		if function.Display.Label == "" || function.Display.Description == "" || function.Display.Category == "" {
			t.Fatalf("missing display metadata: %#v", function)
		}
		if len(function.Display.Parameters) != len(function.Params) {
			t.Fatalf("parameter display mismatch: %#v", function)
		}
	}
	for _, name := range []string{
		"if", "eq", "lt", "le", "gt", "ge",
		"add", "sub", "mul", "div", "int", "float", "string", "bool",
	} {
		if !names[name] {
			t.Fatalf("core function %q is missing", name)
		}
	}
	if len(names) != 14 {
		t.Fatalf("core registry is not minimal: %#v", names)
	}
	for _, hidden := range []string{"array.is_empty", "array.prepend", "array.head", "array.tail", "recur"} {
		if names[hidden] {
			t.Fatalf("low-level function %q leaked into operator catalog", hidden)
		}
	}
	// The kernel enables no lazy form at all; a console opts into them.
	if got := specialFormNames(catalog); got != "" {
		t.Fatalf("core special forms = %s", got)
	}
	if got := specialFormNames(consoleRegistry(t).Catalog()); got != "switch,for,reduce" {
		t.Fatalf("operator special forms = %s", got)
	}
	if got := specialFormNames(consoleRegistry(t, RecurForm).Catalog()); got != "switch,for,reduce,recur" {
		t.Fatalf("engineer special forms = %s", got)
	}
	if err := registry.EnableForm("lambda"); err == nil {
		t.Fatal("unknown form was accepted")
	}
	if err := registry.Register(FunctionSpec{
		Name: "bad.color@1", Params: []Type{IntType}, Result: IntType,
		Eval:    func(args []Value) (Value, error) { return args[0], nil },
		Display: FunctionDisplay{Color: "red"},
	}); err == nil {
		t.Fatal("invalid display color was accepted")
	}
}

func specialFormNames(catalog LanguageCatalog) string {
	names := make([]string, len(catalog.SpecialForms))
	for i, form := range catalog.SpecialForms {
		names[i] = form.Name
	}
	return strings.Join(names, ",")
}

// consoleRegistry is the operator console: the kernel plus the lazy forms an
// operator may use. Extra forms model a higher-privilege console.
func consoleRegistry(t *testing.T, extra ...Form) *Registry {
	t.Helper()
	registry := CoreRegistry()
	if err := registry.EnableForm(append([]Form{SwitchForm, ForForm, ReduceForm}, extra...)...); err != nil {
		t.Fatal(err)
	}
	return registry
}

func compileAndRun(t *testing.T, source string, registry *Registry, args map[string]any, options RunOptions) (Value, *Runtime) {
	t.Helper()
	artifact, err := CompileExpr(source, registry, CompileOptions{})
	if err != nil {
		t.Fatalf("compile %s: %v", source, err)
	}
	runtime, err := Instantiate(artifact, registry)
	if err != nil {
		t.Fatalf("instantiate %s: %v", source, err)
	}
	value, err := runtime.Run(args, options)
	if err != nil {
		t.Fatalf("run %s: %v", source, err)
	}
	return value, runtime
}

func TestReduceFoldsArrayWithLocalAccumulator(t *testing.T) {
	value, runtime := compileAndRun(t,
		`reduce(prices,price,total,0,add(total,price))`,
		consoleRegistry(t), map[string]any{"prices": []any{10, 20, 30}}, RunOptions{})
	params := runtime.Args()
	if len(params) != 1 || params[0].Name != "prices" || !params[0].Type.Equal(ArrayOf(IntType)) {
		t.Fatalf("args = %#v", params)
	}
	if !runtime.ResultType().Equal(IntType) {
		t.Fatalf("result type = %s", runtime.ResultType())
	}
	if got, _ := value.Int(); got != 60 {
		t.Fatalf("value = %v", value.Any())
	}

	// An empty source yields the initial accumulator without running the body.
	empty, _ := compileAndRun(t,
		`reduce(prices,price,total,7,add(total,price))`,
		consoleRegistry(t), map[string]any{"prices": []any{}}, RunOptions{})
	if got, _ := empty.Int(); got != 7 {
		t.Fatalf("empty reduce = %v", empty.Any())
	}
}

func TestReduceRejectsBodyThatChangesAccumulatorType(t *testing.T) {
	registry := consoleRegistry(t)
	_, err := CompileExpr(`reduce(prices,price,total,0,string(total))`, registry, CompileOptions{})
	if err == nil || !strings.Contains(err.Error(), "reduce") {
		t.Fatalf("error = %v", err)
	}
	if _, err := CompileExpr(`reduce(prices,price,price,0,price)`, registry, CompileOptions{}); err == nil {
		t.Fatal("reduce accepted the same name for item and accumulator")
	}
}

func TestRecurNeedsItsFormAndStaysBounded(t *testing.T) {
	source := `if(eq(n,0),acc,recur(sub(n,1),add(acc,n)))`
	value, _ := compileAndRun(t, source, consoleRegistry(t, RecurForm),
		map[string]any{"n": 10, "acc": 0}, RunOptions{Fuel: 100_000})
	if got, _ := value.Int(); got != 55 {
		t.Fatalf("sum = %v", value.Any())
	}
	if _, err := CompileExpr(source, consoleRegistry(t), CompileOptions{}); err == nil ||
		!strings.Contains(err.Error(), "recur is not enabled") {
		t.Fatalf("operator console accepted recur: %v", err)
	}

	// Tail recursion is a jump, so a non-terminating tail loop is caught by
	// fuel rather than by the recursion limit.
	registry := consoleRegistry(t, RecurForm)
	artifact, err := CompileExpr(`if(eq(n,0),n,recur(n))`, registry, CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Run(map[string]any{"n": 1}, RunOptions{Fuel: 60, MaxRecursion: 4}); err == nil ||
		!strings.Contains(err.Error(), "fuel") {
		t.Fatalf("tail recursion error = %v", err)
	}

	// A recur in argument position is a real nested activation and is bounded
	// by MaxRecursion.
	nested, err := CompileExpr(`if(eq(n,0),n,add(recur(sub(n,1)),0))`, registry, CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	nestedRuntime, err := Instantiate(nested, registry)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := nestedRuntime.Run(map[string]any{"n": 100}, RunOptions{Fuel: 100_000, MaxRecursion: 8}); err == nil ||
		!strings.Contains(err.Error(), "recursion limit") {
		t.Fatalf("nested recursion error = %v", err)
	}
	if value, err := nestedRuntime.Run(map[string]any{"n": 5}, RunOptions{Fuel: 100_000}); err != nil {
		t.Fatal(err)
	} else if got, _ := value.Int(); got != 0 {
		t.Fatalf("nested recursion result = %v", value.Any())
	}
}

func TestReduceAndRecurSurviveExprJSONRoundTrip(t *testing.T) {
	for _, source := range []string{
		`reduce(prices,price,total,0,add(total,price))`,
		`for(channels,channel,eq(channel,"UP"),channel)`,
		`if(eq(n,0),acc,recur(sub(n,1),add(acc,n)))`,
	} {
		expr, err := Parse(source)
		if err != nil {
			t.Fatalf("parse %s: %v", source, err)
		}
		first, err := ExportExprJSON(expr)
		if err != nil {
			t.Fatal(err)
		}
		imported, err := ImportExprJSON(first)
		if err != nil {
			t.Fatalf("import %s: %v", source, err)
		}
		second, err := ExportExprJSON(imported)
		if err != nil {
			t.Fatal(err)
		}
		if string(first) != string(second) {
			t.Fatalf("%s is not canonical:\n%s\n%s", source, first, second)
		}
	}
}

func TestDisabledFormsAreRejectedWhenTheyArriveAsExprJSON(t *testing.T) {
	expr, err := Parse(`for(channels,channel,recur(channel))`)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := ExportExprJSON(expr)
	if err != nil {
		t.Fatal(err)
	}
	imported, err := ImportExprJSON(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := CompileAST(imported, consoleRegistry(t), CompileOptions{}); err == nil ||
		!strings.Contains(err.Error(), "recur is not enabled") {
		t.Fatalf("CompileAST bypassed the registry forms: %v", err)
	}
	if _, err := CompileAST(imported, CoreRegistry(), CompileOptions{}); err == nil ||
		!strings.Contains(err.Error(), "for is not enabled") {
		t.Fatalf("CompileAST accepted a disabled form: %v", err)
	}
}

func TestPanickingExtensionIsContained(t *testing.T) {
	registry := CoreRegistry()
	if err := registry.Register(FunctionSpec{
		Name: "boom@1", Params: []Type{IntType}, Result: IntType, Cost: 1,
		Eval:    func(args []Value) (Value, error) { panic("extension exploded") },
		Display: FunctionDisplay{Label: "炸弹", Description: "总是 panic 的扩展", Category: "测试"},
	}); err != nil {
		t.Fatal(err)
	}
	artifact, err := CompileExpr(`boom@1(n)`, registry, CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	_, err = runtime.Run(map[string]any{"n": 1}, RunOptions{Fuel: 100})
	if err == nil || !strings.Contains(err.Error(), "extension panicked") {
		t.Fatalf("panic was not contained: %v", err)
	}
	// The runtime stays usable afterwards.
	if _, err := runtime.Run(map[string]any{"n": 2}, RunOptions{Fuel: 100}); err == nil ||
		!strings.Contains(err.Error(), "extension panicked") {
		t.Fatalf("second run: %v", err)
	}
}
