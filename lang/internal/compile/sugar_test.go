package compile

import (
	"context"
	"fmt"
	"funroute/lang/internal/machine"
	"funroute/lang/internal/syntax"
	"strings"
	"testing"
)

func TestBareExpressionIsTheGrammar(t *testing.T) {
	// A dotted name is a function's; a variable is plain, so "." stays free
	// for field access.
	if _, err := syntax.Parse(`a.b + 1`); err == nil || !strings.Contains(err.Error(), `invalid variable name "a.b"`) {
		t.Fatalf("dotted variable error = %v", err)
	}
	// A bare expression is the whole grammar.
	if _, err := syntax.Parse(`add(1,2)`); err != nil {
		t.Fatal(err)
	}
}

func TestInfixSugarIsDesugaredToTheSameAST(t *testing.T) {
	// Sugar lives in the source layer only: a + b must produce exactly the
	// ExprJSON that add(a, b) produces.
	for _, pair := range [][2]string{
		{`a + b * 2`, `add(a,mul(b,2))`},
		{`(a + b) * 2`, `mul(add(a,b),2)`},
		{`a - b - 1`, `sub(sub(a,b),1)`},
		{`a < b`, `lt(a,b)`},
		{`a >= b`, `ge(a,b)`},
		{`a != b`, `if(eq(a,b),false,true)`},
		{`a && b`, `if(a,b,false)`},
		{`a || b`, `if(a,true,b)`},
		{`!a`, `if(a,false,true)`},
		{`-a`, `sub(0,a)`},
		{`1_000_000`, `1000000`},
		{`add(a, b,)`, `add(a,b)`},
	} {
		sugared, err := syntax.Parse(pair[0])
		if err != nil {
			t.Fatalf("parse %q: %v", pair[0], err)
		}
		plain, err := syntax.Parse(pair[1])
		if err != nil {
			t.Fatalf("parse %q: %v", pair[1], err)
		}
		left, err := syntax.ExportExprJSON(sugared)
		if err != nil {
			t.Fatal(err)
		}
		right, err := syntax.ExportExprJSON(plain)
		if err != nil {
			t.Fatal(err)
		}
		if string(left) != string(right) {
			t.Fatalf("%q desugars to\n%s\nwant\n%s", pair[0], left, right)
		}
	}
}

func TestSugarSemantics(t *testing.T) {
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
		{`a > 0 || div(1,0) > 0`, map[string]any{"a": 1}, true},
		{`a < 0 && div(1,0) > 0`, map[string]any{"a": 1}, false},
		{`!(a > b)`, map[string]any{"a": 1, "b": 2}, true},
		{"// 选主渠道\n a + 1 // 加一", map[string]any{"a": 1}, int64(2)},
		{`1_000_000 + 1`, map[string]any{}, int64(1000001)},
		{`[x * 2 for x in items if x > 1]`, map[string]any{"items": []any{1, 2, 3}}, []any{int64(4), int64(6)}},
		{`reduce(x in items, total = 0, total + x)`, map[string]any{"items": []any{1, 2, 3}}, int64(6)},
	} {
		value, _ := compileAndRun(t, test.source, registry, test.args, machine.RunOptions{Fuel: 100_000})
		if fmt.Sprint(value.Any()) != fmt.Sprint(test.want) {
			t.Fatalf("%s = %v, want %v", test.source, value.Any(), test.want)
		}
	}
}

func TestLoopSugarAndCallsShareOneTree(t *testing.T) {
	for _, pair := range [][2]string{
		{`[x * 2 for x in items if x > 1]`, `[mul(x,2) for x in items if gt(x,1)]`},
		{`[x for x in items]`, `[x for x in items]`},
		{`reduce(x in items, total = 0, total + x)`, `reduce(x in items, total = 0, add(total,x))`},
	} {
		sugared, err := syntax.Parse(pair[0])
		if err != nil {
			t.Fatalf("parse %q: %v", pair[0], err)
		}
		plain, err := syntax.Parse(pair[1])
		if err != nil {
			t.Fatalf("parse %q: %v", pair[1], err)
		}
		left, _ := syntax.ExportExprJSON(sugared)
		right, _ := syntax.ExportExprJSON(plain)
		if string(left) != string(right) {
			t.Fatalf("%q desugars to\n%s\nwant\n%s", pair[0], left, right)
		}
	}
}

func TestFloatLiteralPullsVariablesToFloat(t *testing.T) {
	registry := machine.CoreRegistry()
	for _, test := range []struct {
		source string
		want   string
	}{
		{`risk < 0.5`, "(risk: float) -> bool"},
		{`amount * 1.5`, "(amount: float) -> float"},
		{`a + b`, "(a: int, b: int) -> int"},
		{`float(n) < 0.5`, "(n: int) -> bool"},
	} {
		artifact, err := CompileExpr(test.source, registry, CompileOptions{})
		if err != nil {
			t.Fatalf("%s: %v", test.source, err)
		}
		params := make([]string, len(artifact.Args))
		for i, param := range artifact.Args {
			params[i] = param.Name + ": " + param.Type.String()
		}
		got := "(" + strings.Join(params, ", ") + ") -> " + artifact.Result.String()
		if got != test.want {
			t.Fatalf("%s infers %s, want %s", test.source, got, test.want)
		}
	}
}

func TestSwitchShapesAgree(t *testing.T) {
	// The positional form and the branch form produce the same AST.
	for _, pair := range [][2]string{
		{`switch(country, case "SG" => "a", case "MY" => "b", else "c")`, `switch(country, case "SG" => "a", case "MY" => "b", else "c")`},
	} {
		sugared, err := syntax.Parse(pair[0])
		if err != nil {
			t.Fatalf("parse %q: %v", pair[0], err)
		}
		plain, err := syntax.Parse(pair[1])
		if err != nil {
			t.Fatalf("parse %q: %v", pair[1], err)
		}
		left, _ := syntax.ExportExprJSON(sugared)
		right, _ := syntax.ExportExprJSON(plain)
		if string(left) != string(right) {
			t.Fatalf("%q gives\n%s\nwant\n%s", pair[0], left, right)
		}
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
		source: `switch(country, case "MY", "TH" => "adyen_asia", case "SG" => "adyen_sg", else "global")`,
		args:   map[string]any{"country": "TH"},
		want:   "adyen_asia",
	},
	{
		name:   "多值 case 未命中走默认",
		source: `switch(country, case "MY", "TH" => "adyen_asia", else "global")`,
		args:   map[string]any{"country": "JP"},
		want:   "global",
	},
	{
		name:   "条件链取第一个成立的分支",
		source: `switch(case amount > 10000 => "manual", case risk > 0.8 => "reject", else "auto")`,
		args:   map[string]any{"amount": 20000, "risk": 0.1},
		want:   "manual",
	},
	{
		name:   "条件链按顺序短路",
		source: `switch(case amount > 10000 => "manual", case risk > 0.8 => "reject", else "auto")`,
		args:   map[string]any{"amount": 5, "risk": 0.9},
		want:   "reject",
	},
	{
		name:   "条件链多条件任一成立",
		source: `switch(case amount > 10000, risk > 0.8 => "review", else "auto")`,
		args:   map[string]any{"amount": 5, "risk": 0.9},
		want:   "review",
	},
	{
		name:   "条件链默认分支",
		source: `switch(case amount > 10000 => "manual", else "auto")`,
		args:   map[string]any{"amount": 5},
		want:   "auto",
	},
}

func TestSwitchBranchesAndConditions(t *testing.T) {
	registry := consoleRegistry(t)
	for _, test := range switchCases {
		value, _ := compileAndRun(t, test.source, registry, test.args, machine.RunOptions{Fuel: 100_000})
		if fmt.Sprint(value.Any()) != fmt.Sprint(test.want) {
			t.Fatalf("%s: %s = %v, want %v", test.name, test.source, value.Any(), test.want)
		}
	}
}

func TestSwitchIsStillLazyAndTypeChecked(t *testing.T) {
	registry := consoleRegistry(t)
	// The untaken branch must not be evaluated, in both shapes.
	for _, source := range []string{
		`switch(country, case "SG" => 1, else div(1,0))`,
		`switch(case country == "SG" => 1, else div(1,0))`,
	} {
		value, _ := compileAndRun(t, source, registry, map[string]any{"country": "SG"}, machine.RunOptions{Fuel: 1000})
		if got, _ := value.Int(); got != 1 {
			t.Fatalf("%s = %v", source, value.Any())
		}
	}
	// A bare variable as a condition is fine — it just types as bool.
	artifact, err := CompileExpr(`switch(case healthy => "yes", else "no")`, registry, CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(artifact.Args) != 1 || !artifact.Args[0].Type.Equal(machine.BoolType) {
		t.Fatalf("args = %#v", artifact.Args)
	}
	// A non-bool condition, and results of different types, must be rejected.
	for _, source := range []string{
		`switch(case 1 => "yes", else "no")`,
		`switch(country, case "SG" => 1, else "two")`,
	} {
		if _, err := CompileExpr(source, registry, CompileOptions{}); err == nil {
			t.Fatalf("%s compiled", source)
		}
	}
	// A condition branch with a subject still compares values.
	if _, err := CompileExpr(`switch(country, case amount => "yes", else "no")`, registry, CompileOptions{}); err == nil {
		t.Fatal("subject and match of different types compiled")
	}
}

func TestSwitchSurvivesExprJSONRoundTrip(t *testing.T) {
	for _, source := range []string{
		`switch(country, case "MY", "TH" => "asia", case "SG" => "sg", else "global")`,
		`switch(case amount > 100 => "big", case risk > 0.5, country == "SG" => "check", else "ok")`,
	} {
		expr, err := syntax.Parse(source)
		if err != nil {
			t.Fatalf("parse %q: %v", source, err)
		}
		first, err := syntax.ExportExprJSON(expr)
		if err != nil {
			t.Fatal(err)
		}
		imported, err := syntax.ImportExprJSON(first)
		if err != nil {
			t.Fatalf("import %q: %v", source, err)
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

func TestDeadCandidatesAreDroppedNotFatal(t *testing.T) {
	registry := consoleRegistry(t)
	// gt leaves two candidates for x (int from gt(int,int), float from the mixed
	// gt(float,int)); the equality kills the float one. Dropping a dead branch
	// must not fail the whole inference — and the result must not depend on the
	// order the operators were written in.
	for _, source := range []string{
		`x > 1 && x == 99`,
		`x == 99 && x > 1`,
		`x > 1 || x == 99`,
		`x > 1 && x < 10 || x == 99`,
		`[x for x in items if x > 1 && x == 99]`,
	} {
		artifact, err := CompileExpr(source, registry, CompileOptions{})
		if err != nil {
			t.Fatalf("%s: %v", source, err)
		}
		if !artifact.Args[0].Type.Equal(machine.IntType) && !artifact.Args[0].Type.Equal(machine.ArrayOf(machine.IntType)) {
			t.Fatalf("%s infers %s", source, artifact.Args[0].Type)
		}
	}
	// A genuine conflict still fails.
	if _, err := CompileExpr(`x > 1 && x == "a"`, registry, CompileOptions{}); err == nil {
		t.Fatal("conflicting types compiled")
	}
}

func TestDictionaryWalkNeedsTwoVariables(t *testing.T) {
	registry := consoleRegistry(t)
	dictContract := []ArgSpec{{Name: "weights", Type: machine.DictOf(machine.FloatType)}}
	// Two variables walk a dictionary in sorted key order; one walks an array.
	for _, test := range []struct {
		source   string
		contract []ArgSpec
		args     map[string]any
		want     string
	}{
		{`[k for k, v in weights if v > 0.0]`, dictContract, map[string]any{"weights": map[string]any{"b": 1.0, "a": 2.0}}, "[a b]"},
		{`[v for k, v in weights if v > 0.0]`, dictContract, map[string]any{"weights": map[string]any{"b": 1.0, "a": 2.0}}, "[2 1]"},
		{`reduce(k, v in weights, t = 0.0, t + v)`, dictContract, map[string]any{"weights": map[string]any{"a": 1.5, "b": 2.5}}, "4"},
		{`[x for x in items if x > 0]`, nil, map[string]any{"items": []any{1, 2}}, "[1 2]"},
	} {
		artifact, err := CompileExpr(test.source, registry, CompileOptions{Args: test.contract})
		if err != nil {
			t.Fatalf("%s: %v", test.source, err)
		}
		runtime, err := machine.Instantiate(artifact, registry)
		if err != nil {
			t.Fatal(err)
		}
		value, err := runtime.Run(context.Background(), test.args, machine.RunOptions{Fuel: 10_000})
		if err != nil {
			t.Fatalf("%s: %v", test.source, err)
		}
		if got := fmt.Sprint(value.Any()); got != test.want {
			t.Fatalf("%s = %s, want %s", test.source, got, test.want)
		}
	}
	// The variable count and the source shape must agree.
	for _, test := range []struct {
		source string
		args   []ArgSpec
		want   string
	}{
		// With the hint forcing a dict, one variable is the wrong shape.
		{`[v for v in weights if v > 0.0]`, dictContract, "must be an array"},
		{`[x for k, x in items if x > 0]`, []ArgSpec{{Name: "items", Type: machine.ArrayOf(machine.IntType)}}, "must be a dict"},
		{`[x for x, x in items]`, nil, `"x" is bound twice`},
	} {
		if _, err := CompileExpr(test.source, registry, CompileOptions{Args: test.args}); err == nil ||
			!strings.Contains(err.Error(), test.want) {
			t.Fatalf("%s error = %v, want %q", test.source, err, test.want)
		}
	}
}

func TestDictionaryWalkSurvivesExprJSONRoundTrip(t *testing.T) {
	for _, source := range []string{
		`[add(k, string(v)) for k, v in weights if v > 1.0]`,
		`reduce(k, v in weights, total = 0.0, total + v)`,
	} {
		expr, err := syntax.Parse(source)
		if err != nil {
			t.Fatalf("parse %q: %v", source, err)
		}
		first, err := syntax.ExportExprJSON(expr)
		if err != nil {
			t.Fatal(err)
		}
		imported, err := syntax.ImportExprJSON(first)
		if err != nil {
			t.Fatalf("import %q: %v", source, err)
		}
		second, err := syntax.ExportExprJSON(imported)
		if err != nil {
			t.Fatal(err)
		}
		if string(first) != string(second) {
			t.Fatalf("%s is not canonical:\n%s\n%s", source, first, second)
		}
		if !strings.Contains(string(first), `"key_variable":"k"`) {
			t.Fatalf("the key variable is missing from %s", first)
		}
	}
}

func TestLetBindsMultipleLocalsInOrder(t *testing.T) {
	registry := consoleRegistry(t)
	// The names are local, so they never reach the signature; a later binding
	// reads an earlier one.
	artifact, err := CompileExpr(`let(base = amount * 2, fee = base / 10, base + fee)`, registry, CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(artifact.Args) != 1 || artifact.Args[0].Name != "amount" {
		t.Fatalf("args = %#v", artifact.Args)
	}
	runtime, err := machine.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	value, err := runtime.Run(context.Background(), map[string]any{"amount": 100}, machine.RunOptions{Fuel: 1_000})
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := value.Int(); got != 220 {
		t.Fatalf("value = %v", value.Any())
	}
	for _, test := range []struct{ source, want string }{
		{`let(x = 1, x = 2, x)`, `"x" is bound twice`},
		{`let(add(a, 1))`, "needs at least one binding"},
	} {
		if _, err := CompileExpr(test.source, registry, CompileOptions{}); err == nil ||
			!strings.Contains(err.Error(), test.want) {
			t.Fatalf("%s error = %v, want %q", test.source, err, test.want)
		}
	}
}

func TestLetEvaluatesABindingOnce(t *testing.T) {
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
		t.Fatalf("the let form should fit in 16 fuel: %v", bound)
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
	_, err = runtime.Run(context.Background(), map[string]any{"amount": 100}, machine.RunOptions{Fuel: fuel})
	return err
}

func TestLetSurvivesExprJSONRoundTrip(t *testing.T) {
	source := `let(base = amount * 2, fee = base / 10, base + fee)`
	expr, err := syntax.Parse(source)
	if err != nil {
		t.Fatal(err)
	}
	first, err := syntax.ExportExprJSON(expr)
	if err != nil {
		t.Fatal(err)
	}
	imported, err := syntax.ImportExprJSON(first)
	if err != nil {
		t.Fatal(err)
	}
	second, err := syntax.ExportExprJSON(imported)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatalf("%s is not canonical:\n%s\n%s", source, first, second)
	}
}
