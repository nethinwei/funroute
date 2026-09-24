package compile

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"funroute/lang/internal/machine"
	"funroute/lang/internal/syntax"
)

func TestExprJSONCanonicalRoundTrip(t *testing.T) {
	t.Parallel()
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
	if len(before.Args()) != 2 || before.Args()[0].Name() != "a" || before.Args()[1].Name() != "z" {
		t.Fatalf("canonical args = %#v, want a then z", before.Args())
	}
	if before.Digest() != after.Digest() {
		t.Fatalf("compile digest changed across AST round trip: %s != %s", before.Digest(), after.Digest())
	}
}

func TestIfIsLazyAndFuelIsEnforced(t *testing.T) {
	t.Parallel()
	registry := machine.CoreRegistry()
	artifact, err := CompileExpr(`if(flag,7,div(1,zero))`, registry, CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := machine.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	result, err := runtime.Run(t.Context(), map[string]any{"flag": true, "zero": 0}, machine.RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if value, ok := result.Int(); !ok || value != 7 {
		t.Fatalf("Run(flag=true, zero=0) = %#v, want 7", result.Any())
	}
	_, err = runtime.Run(t.Context(), map[string]any{"flag": true, "zero": 0}, machine.RunOptions{Fuel: 1})
	if err == nil || !strings.Contains(err.Error(), "fuel") {
		t.Fatalf("Run with Fuel 1 error = %v, want a fuel error", err)
	}
}

func TestFunctionalForBindsLocalFiltersAndMaps(t *testing.T) {
	t.Parallel()
	registry := consoleRegistry(t)
	artifact, err := CompileExpr(
		`[string(channel) for channel in channels if eq(channel,"UP")]`,
		registry,
		CompileOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}
	wantArgs := []machine.Parameter{machine.NewParameter("channels", machine.ArrayOf(machine.StringType), "")}
	if len(artifact.Args()) != len(wantArgs) {
		t.Fatalf("args = %#v, want %#v", artifact.Args(), wantArgs)
	}
	for i := range wantArgs {
		if artifact.Args()[i].Name() != wantArgs[i].Name() || !artifact.Args()[i].Type().Equal(wantArgs[i].Type()) {
			t.Fatalf("arg %d = %#v, want %#v", i, artifact.Args()[i], wantArgs[i])
		}
	}
	runtime, err := machine.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	result, err := runtime.Run(t.Context(), map[string]any{
		"channels": []any{"UP", "DOWN", "UP"},
	}, machine.RunOptions{Fuel: 10_000})
	if err != nil {
		t.Fatal(err)
	}
	items, ok := result.Array()
	if !ok || len(items) != 2 {
		t.Fatalf("result = %#v, want [UP UP]", result.Any())
	}
	for i, item := range items {
		if value, ok := item.String(); !ok || value != "UP" {
			t.Fatalf("result item %d = %#v, want UP", i, item.Any())
		}
	}
	if machine.PartsOf(artifact).Locals != 1 {
		t.Fatalf("locals = %d, want 1", machine.PartsOf(artifact).Locals)
	}
}

func TestFunctionalSwitchIsLazyAndTyped(t *testing.T) {
	t.Parallel()
	registry := consoleRegistry(t)
	artifact, err := CompileExpr(`switch(country, case "SG" => 1, case "MY" => 2, else => div(1,zero))`, registry, CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := machine.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	result, err := runtime.Run(t.Context(), map[string]any{"country": "MY", "zero": 0}, machine.RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if value, ok := result.Int(); !ok || value != 2 {
		t.Fatalf("Run(country=MY) = %#v, want 2", result.Any())
	}
}

func TestArtifactJSONRoundTripAndTamperDetection(t *testing.T) {
	t.Parallel()
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
	machine.PartsOf(artifact).Instructions[0].A = 999
	result, err := runtime.Run(t.Context(), map[string]any{"n": 1}, machine.RunOptions{})
	if err != nil {
		t.Fatalf("runtime retained mutable artifact: %v", err)
	}
	if value, ok := result.Int(); !ok || value != 3 {
		t.Fatalf("snapshotted runtime Run(n=1) = %#v, want 3", result.Any())
	}
	machine.PartsOf(artifact).Calls[0].Cost++
	if _, err := machine.Instantiate(artifact, registry); err == nil {
		t.Fatal("mutated artifact was accepted")
	}
}

func TestReduceFoldsArrayWithLocalAccumulator(t *testing.T) {
	t.Parallel()
	value, runtime := compileAndRun(t,
		`reduce(price in prices, total = 0, add(total,price))`,
		consoleRegistry(t), map[string]any{"prices": []any{10, 20, 30}}, machine.RunOptions{})
	params := runtime.Args()
	if len(params) != 1 || params[0].Name() != "prices" || !params[0].Type().Equal(machine.ArrayOf(machine.IntType)) {
		t.Fatalf("args = %#v, want prices: array<int>", params)
	}
	if !runtime.ResultType().Equal(machine.IntType) {
		t.Fatalf("result type = %s, want int", runtime.ResultType())
	}
	if got, _ := value.Int(); got != 60 {
		t.Fatalf("value = %v, want 60", value.Any())
	}

	// An empty source yields the initial accumulator without running the body.
	empty, _ := compileAndRun(t,
		`reduce(price in prices, total = 7, add(total,price))`,
		consoleRegistry(t), map[string]any{"prices": []any{}}, machine.RunOptions{})
	if got, _ := empty.Int(); got != 7 {
		t.Fatalf("empty reduce = %v, want 7", empty.Any())
	}
}

// TestReduceSkipsFilteredItems is the "if" clause the fold shares with the
// comprehension: a rejected item is not folded, so the accumulator keeps the
// value the previous step left.
func TestReduceSkipsFilteredItems(t *testing.T) {
	t.Parallel()
	value, _ := compileAndRun(t,
		`reduce(price in prices if price >= 1000, total = 0, total + price)`,
		consoleRegistry(t), map[string]any{"prices": []any{100, 2500, 900, 4000}},
		machine.RunOptions{})
	if got, _ := value.Int(); got != 6500 {
		t.Fatalf("filtered fold = %v, want 6500", value.Any())
	}

	// The condition sees the loop variables but not the accumulator, which is
	// what makes filtering happen before folding rather than inside it.
	contract := CompileOptions{Args: []ArgSpec{{Name: "prices", Type: machine.ArrayOf(machine.IntType)}}}
	_, err := CompileExpr(`reduce(price in prices if total > 0, total = 0, total + price)`, consoleRegistry(t), contract)
	if err == nil || !strings.Contains(err.Error(), "total") {
		t.Fatalf("the filter must not see the accumulator: %v", err)
	}

	// A dictionary walk filters on either loop variable.
	dictValue, _ := compileAndRun(t,
		`reduce(name, weight in weights if weight > 0.1, total = 0.0, total + weight)`,
		consoleRegistry(t), map[string]any{"weights": map[string]any{"a": 0.5, "b": 0.05, "c": 0.2}},
		machine.RunOptions{})
	if got, _ := dictValue.Float(); got < 0.69 || got > 0.71 {
		t.Fatalf("filtered dictionary fold = %v, want 0.7", dictValue.Any())
	}
}

func TestReduceRejectsBodyThatChangesAccumulatorType(t *testing.T) {
	t.Parallel()
	registry := consoleRegistry(t)
	_, err := CompileExpr(`reduce(price in prices, total = 0, string(total))`, registry, CompileOptions{})
	if err == nil || !strings.Contains(err.Error(), "accumulator") {
		t.Fatalf("CompileExpr(string(total) as the body) error = %v, want an accumulator error", err)
	}
	if _, err := CompileExpr(`reduce(price in prices, price = 0, price)`, registry, CompileOptions{}); err == nil {
		t.Fatal("reduce accepted the same name for item and accumulator")
	}
}

func TestReduceAndComprehensionSurviveExprJSONRoundTrip(t *testing.T) {
	t.Parallel()
	for _, source := range []string{
		`reduce(price in prices, total = 0, add(total,price))`,
		`reduce(price in prices if gt(price,minimum), total = 0, add(total,price))`,
		`[channel for channel in channels if eq(channel,"UP")]`,
		`[add(x,1) for x in [mul(y,2) for y in items]]`,
	} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			assertExprJSONIsCanonical(t, source)
		})
	}
}

// assertExprJSONIsCanonical parses source, and checks that exporting,
// importing and exporting again gives the same ExprJSON. It returns it.
func assertExprJSONIsCanonical(t *testing.T, source string) []byte {
	t.Helper()
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
	return first
}

func TestSwitchEvaluatesItsSubjectOnce(t *testing.T) {
	t.Parallel()
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
	artifact, err := CompileExpr(`switch(probe_v1(n), case 1 => "one", case 2 => "two", else => "other")`, registry, CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := machine.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	result, err := runtime.Run(t.Context(), map[string]any{"n": 2}, machine.RunOptions{Fuel: 100})
	if err != nil {
		t.Fatal(err)
	}
	if text, _ := result.String(); text != "two" || calls != 1 {
		t.Fatalf("Run(n=2) = %q with %d subject calls, want \"two\" with 1", text, calls)
	}
}

// A call does not pay for checking its own arguments. Both halves of that were
// once false and both showed up right here: hasType built a whole Type just to
// compare a record against one, and every RunValues rescanned float backings
// for NaN — 16µs for a 65536-element vector, on a value whose constructor had
// already rejected them.
//
// It is not parallel: AllocsPerRun counts every goroutine's allocations.
func TestArgumentChecksDoNotAllocate(t *testing.T) {
	registry := machine.CoreRegistry()
	order := machine.RecordOf(
		machine.FieldOf("amount", machine.IntType),
		machine.FieldOf("currency", machine.StringType),
	)
	record, err := machine.Record(order, []machine.Value{machine.Int(1200), machine.String("SGD")})
	if err != nil {
		t.Fatal(err)
	}
	vector, err := machine.ToValue(make([]float64, 4096))
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, source string
		arg          ArgSpec
		value        machine.Value
	}{
		{"record 参数", `order.amount + 1`, ArgSpec{Name: "order", Type: order}, record},
		{"向量参数", `len(features)`, ArgSpec{Name: "features", Type: machine.ArrayOf(machine.FloatType)}, vector},
	} {
		t.Run(test.name, func(t *testing.T) {
			assertCallDoesNotAllocate(t, registry, test.source, test.arg, test.value)
		})
	}
}

func assertCallDoesNotAllocate(t *testing.T, registry *machine.Registry, source string, arg ArgSpec, value machine.Value) {
	t.Helper()
	artifact, err := CompileExpr(source, registry, CompileOptions{Args: []ArgSpec{arg}})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := machine.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	args := []machine.Value{value}
	var failure error
	allocs := testing.AllocsPerRun(100, func() {
		if _, err := runtime.RunValues(ctx, args, machine.RunOptions{Fuel: 1000}); err != nil {
			failure = err
		}
	})
	if failure != nil {
		t.Fatal(failure)
	}
	if allocs != 0 {
		t.Fatalf("%s allocated %.0f times per call, want 0", source, allocs)
	}
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

// benchRegistry is the full language: kernel, list primitives and every form.
func benchRegistry(b *testing.B) *machine.Registry {
	b.Helper()
	registry := machine.CoreRegistry()
	if err := registry.EnableForm(machine.SwitchForm, machine.ForForm, machine.ReduceForm); err != nil {
		b.Fatal(err)
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
	value, err := runtime.Run(t.Context(), args, options)
	if err != nil {
		t.Fatalf("run %s: %v", source, err)
	}
	return value, runtime
}

func BenchmarkCompile(b *testing.B) {
	registry := benchRegistry(b)
	source := `switch(country, case "SG" => reduce(p in prices, t = 0, add(t,p)), case "MY" => reduce(p in prices, t = 1, mul(t,p)), else => 0)`
	b.ReportAllocs()
	for b.Loop() {
		if _, err := CompileExpr(source, registry, CompileOptions{
			Args: []ArgSpec{{Name: "country", Type: machine.StringType}, {Name: "prices", Type: machine.ArrayOf(machine.IntType)}},
		}); err != nil {
			b.Fatal(err)
		}
	}
}

func TestFallbackIsLazyOnSuccess(t *testing.T) {
	t.Parallel()
	registry := machine.CoreRegistry()
	called := 0
	if err := machine.Logic(registry, "default_v1", machine.Doc{}, func(value int64) (int64, error) {
		called++
		return 9, nil
	}); err != nil {
		t.Fatal(err)
	}
	value, err := compileAndRunInt(t, `fallback(x, default_v1(x))`, registry, 100)
	if err != nil || value != 3 || called != 0 {
		t.Fatalf("result = %d, calls = %d, err = %v, want 3 with no calls", value, called, err)
	}
}

func TestFallbackTriesAnyNumberOfCandidatesInOrder(t *testing.T) {
	t.Parallel()
	registry := machine.CoreRegistry()
	var calls []string
	register := func(name string, succeed bool) {
		err := machine.Logic(registry, name, machine.Doc{}, func(value int64) (int64, error) {
			calls = append(calls, name)
			if !succeed {
				return 0, errors.New("provider unavailable")
			}
			return value + 10, nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	register("first_v1", false)
	register("second_v1", false)
	register("third_v1", true)
	register("unused_v1", true)
	value, err := compileAndRunInt(t, `fallback(first_v1(x),second_v1(x),third_v1(x),unused_v1(x))`, registry, 100)
	if err != nil || value != 13 || strings.Join(calls, ",") != "first_v1,second_v1,third_v1" {
		t.Fatalf("result = %d, calls = %v, err = %v, want 13 after first_v1,second_v1,third_v1", value, calls, err)
	}
	calls = nil
	value, err = compileAndRunInt(t, `fallback(first_v1(x),second_v1(x),x + 9)`, registry, 100)
	if err != nil || value != 12 || strings.Join(calls, ",") != "first_v1,second_v1" {
		t.Fatalf("final result = %d, calls = %v, err = %v, want 12 after first_v1,second_v1", value, calls, err)
	}
	if _, err := compileAndRunInt(t, `fallback(first_v1(x),second_v1(x))`, registry, 100); !errors.Is(err, machine.ErrExtension) {
		t.Fatalf("last candidate error = %v, want ErrExtension", err)
	}
	if _, err := CompileExpr(`fallback(x)`, registry, CompileOptions{}); err == nil || !strings.Contains(err.Error(), "at least 2") {
		t.Fatalf("single candidate error = %v, want one saying at least 2", err)
	}
	if _, err := CompileExpr(`fallback(x,"bad",7)`, registry, CompileOptions{}); err == nil {
		t.Fatal("fallback accepted candidates with different types")
	}
}

func compileAndRunInt(t *testing.T, source string, registry *machine.Registry, fuel uint64) (int64, error) {
	t.Helper()
	artifact, err := CompileExpr(source, registry, CompileOptions{
		Args: []ArgSpec{{Name: "x", Type: machine.IntType}},
	})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := machine.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	value, err := runtime.Run(t.Context(), map[string]any{"x": int64(3)}, machine.RunOptions{Fuel: fuel})
	if err != nil {
		return 0, err
	}
	result, _ := value.Int()
	return result, nil
}

func TestSugarSemantics(t *testing.T) {
	t.Parallel()
	registry := consoleRegistry(t)
	for _, test := range []struct {
		source string
		args   map[string]any
		want   any
	}{
		{`a + b * 2`, map[string]any{"a": 1, "b": 3}, int64(7)},
		{`(a + b) * 2`, map[string]any{"a": 1, "b": 3}, int64(8)},
		{`a - b - 1`, map[string]any{"a": 10, "b": 3}, int64(6)},
		{`-a + 1`, map[string]any{"a": 5}, int64(-4)},
		// A float literal now pulls the variable to float instead of picking the
		// cheaper mixed-numeric overload.
		{`a * 1.5`, map[string]any{"a": 2.0}, 3.0},
		{`a < b`, map[string]any{"a": 1, "b": 2}, true},
		{`a >= b`, map[string]any{"a": 1, "b": 2}, false},
		{`a != 2`, map[string]any{"a": 1}, true},
		{`a == 1`, map[string]any{"a": 1}, true},
		{`"ab" < "b"`, map[string]any{}, true},
		// The right side would divide by zero, so this also proves && and ||
		// short circuit.
		{`a > 0 && b > 0`, map[string]any{"a": 1, "b": 2}, true},
		{`a > 0 || div(1,zero) > 0`, map[string]any{"a": 1, "zero": 0}, true},
		{`a < 0 && div(1,zero) > 0`, map[string]any{"a": 1, "zero": 0}, false},
		{`!(a > b)`, map[string]any{"a": 1, "b": 2}, true},
		{"// 选主渠道\n a + 1 // 加一", map[string]any{"a": 1}, int64(2)},
		{`1_000_000 + 1`, map[string]any{}, int64(1000001)},
		{`[x * 2 for x in items if x > 1]`, map[string]any{"items": []any{1, 2, 3}}, []any{int64(4), int64(6)}},
		{`reduce(x in items, total = 0, total + x)`, map[string]any{"items": []any{1, 2, 3}}, int64(6)},
	} {
		t.Run(test.source, func(t *testing.T) {
			t.Parallel()
			value, _ := compileAndRun(t, test.source, registry, test.args, machine.RunOptions{Fuel: 100_000})
			if fmt.Sprint(value.Any()) != fmt.Sprint(test.want) {
				t.Fatalf("%s = %v, want %v", test.source, value.Any(), test.want)
			}
		})
	}
}

var switchCases = []struct {
	name   string
	source string
	args   map[string]any
	want   any
}{
	{
		name:   "多值 case",
		source: `switch(country, case "MY", "TH" => "adyen_asia", case "SG" => "adyen_sg", else => "global")`,
		args:   map[string]any{"country": "TH"},
		want:   "adyen_asia",
	},
	{
		name:   "多值 case 未命中走默认",
		source: `switch(country, case "MY", "TH" => "adyen_asia", else => "global")`,
		args:   map[string]any{"country": "JP"},
		want:   "global",
	},
	{
		name:   "条件链取第一个成立的分支",
		source: `switch(case amount > 10000 => "manual", case risk > 0.8 => "reject", else => "auto")`,
		args:   map[string]any{"amount": 20000, "risk": 0.1},
		want:   "manual",
	},
	{
		name:   "条件链按顺序短路",
		source: `switch(case amount > 10000 => "manual", case risk > 0.8 => "reject", else => "auto")`,
		args:   map[string]any{"amount": 5, "risk": 0.9},
		want:   "reject",
	},
	{
		name:   "条件链多条件任一成立",
		source: `switch(case amount > 10000, risk > 0.8 => "review", else => "auto")`,
		args:   map[string]any{"amount": 5, "risk": 0.9},
		want:   "review",
	},
	{
		name:   "条件链默认分支",
		source: `switch(case amount > 10000 => "manual", else => "auto")`,
		args:   map[string]any{"amount": 5},
		want:   "auto",
	},
}

func TestSwitchBranchesAndConditions(t *testing.T) {
	t.Parallel()
	registry := consoleRegistry(t)
	for _, test := range switchCases {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			value, _ := compileAndRun(t, test.source, registry, test.args, machine.RunOptions{Fuel: 100_000})
			if fmt.Sprint(value.Any()) != fmt.Sprint(test.want) {
				t.Fatalf("%s: %s = %v, want %v", test.name, test.source, value.Any(), test.want)
			}
		})
	}
}

func TestSwitchIsStillLazyAndTypeChecked(t *testing.T) {
	t.Parallel()
	registry := consoleRegistry(t)
	// The untaken branch must not be evaluated, in both shapes.
	for _, source := range []string{
		`switch(country, case "SG" => 1, else => div(1,zero))`,
		`switch(case country == "SG" => 1, else => div(1,zero))`,
	} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			value, _ := compileAndRun(t, source, registry, map[string]any{"country": "SG", "zero": 0}, machine.RunOptions{Fuel: 1000})
			if got, _ := value.Int(); got != 1 {
				t.Fatalf("%s = %v, want 1", source, value.Any())
			}
		})
	}
	// A bare variable as a condition is fine — it just types as bool.
	artifact, err := CompileExpr(`switch(case healthy => "yes", else => "no")`, registry, CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(artifact.Args()) != 1 || !artifact.Args()[0].Type().Equal(machine.BoolType) {
		t.Fatalf("args = %#v, want healthy: bool", artifact.Args())
	}
	// A non-bool condition, and results of different types, must be rejected.
	for _, source := range []string{
		`switch(case 1 => "yes", else => "no")`,
		`switch(country, case "SG" => 1, else => "two")`,
	} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			if _, err := CompileExpr(source, registry, CompileOptions{}); err == nil {
				t.Fatalf("%s compiled, want a type error", source)
			}
		})
	}
	// A condition branch with a subject still compares values.
	if _, err := CompileExpr(`switch(country, case amount => "yes", else => "no")`, registry, CompileOptions{}); err == nil {
		t.Fatal("subject and match of different types compiled")
	}
}

func TestLetBindsMultipleLocalsInOrder(t *testing.T) {
	t.Parallel()
	registry := consoleRegistry(t)
	// The names are local, so they never reach the signature; a later binding
	// reads an earlier one.
	artifact, err := CompileExpr(`let(base = amount * 2, fee = base / 10, base + fee)`, registry, CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(artifact.Args()) != 1 || artifact.Args()[0].Name() != "amount" {
		t.Fatalf("args = %#v, want only amount", artifact.Args())
	}
	runtime, err := machine.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	value, err := runtime.Run(t.Context(), map[string]any{"amount": 100}, machine.RunOptions{Fuel: 1_000})
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := value.Int(); got != 220 {
		t.Fatalf("Run(amount=100) = %v, want 220", value.Any())
	}
	for _, test := range []struct{ source, want string }{
		{`let(x = 1, x = 2, x)`, `"x" is bound twice`},
		{`let(add(a, 1))`, "needs at least one binding"},
	} {
		t.Run(test.source, func(t *testing.T) {
			t.Parallel()
			if _, err := CompileExpr(test.source, registry, CompileOptions{}); err == nil ||
				!strings.Contains(err.Error(), test.want) {
				t.Fatalf("%s error = %v, want %q", test.source, err, test.want)
			}
		})
	}
}

func TestLetEvaluatesABindingOnce(t *testing.T) {
	t.Parallel()
	registry := consoleRegistry(t)
	// Both spell the same computation, but the repeated one calls mul twice, so
	// it costs more fuel. This is what let buys beyond readability — and with an
	// expensive extension (a model at Cost 500) the gap is that cost, not 2.
	repeated := runWithFuel(t, registry, `add(mul(amount,2), div(mul(amount,2), 10))`, 16)
	bound := runWithFuel(t, registry, `let(base = amount * 2, base + base / 10)`, 16)
	if repeated == nil {
		t.Fatal("expected the repeated form to exhaust 16 fuel")
	}
	if bound != nil {
		t.Fatalf("the let form should fit in 16 fuel: %v, want no error", bound)
	}
}

func runWithFuel(t *testing.T, registry *machine.Registry, source string, fuel uint64) error {
	t.Helper()
	artifact, err := CompileExpr(source, registry, CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := machine.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	_, err = runtime.Run(t.Context(), map[string]any{"amount": 100}, machine.RunOptions{Fuel: fuel})
	return err
}

// Several for clauses in one comprehension are the cartesian product: each
// loops inside the one before it, and every clause but the innermost splices
// what it yields, so the result is one flat array. The nesting lives in the
// AST — no new node type, one flag on the loop that splices.
func TestNestedComprehensionIsACartesianProduct(t *testing.T) {
	t.Parallel()
	registry := consoleRegistry(t)
	result, _ := compileAndRun(t,
		`[a * 10 + b for a in xs if a > 1 for b in ys if b < 9]`,
		registry,
		map[string]any{"xs": []any{1, 2, 3}, "ys": []any{7, 8, 9}},
		machine.RunOptions{Fuel: 10_000})
	items, ok := result.Array()
	if !ok {
		t.Fatalf("result = %#v, want an array", result.Any())
	}
	want := []int64{27, 28, 37, 38}
	if len(items) != len(want) {
		t.Fatalf("result = %#v, want %v", result.Any(), want)
	}
	for i, value := range items {
		if got, _ := value.Int(); got != want[i] {
			t.Fatalf("item %d = %v, want %d", i, value.Any(), want[i])
		}
	}
}

// The flag is what the printer and the compiler both read, so it has to
// survive ExprJSON — and it has to stay out of a single-clause comprehension,
// because a field that appeared there would move every existing digest.
func TestSplicingSurvivesExprJSONAndLeavesOneClauseAlone(t *testing.T) {
	t.Parallel()
	registry := consoleRegistry(t)
	nested, err := CompileExpr(`[a + b for a in xs for b in ys]`, registry, CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	encoded := machine.PartsOf(nested).ExprJSON
	if !strings.Contains(string(encoded), `"flatten":true`) {
		t.Fatalf("the outer clause did not record the splice: %s", encoded)
	}
	reloaded, err := CompileJSON(encoded, registry, CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Digest() != nested.Digest() {
		t.Fatalf("round trip changed the digest: %s vs %s", reloaded.Digest(), nested.Digest())
	}

	plain, err := CompileExpr(`[a + 1 for a in xs]`, registry, CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(machine.PartsOf(plain).ExprJSON), "flatten") {
		t.Fatalf("a one-clause comprehension gained a field: %s", machine.PartsOf(plain).ExprJSON)
	}
}
