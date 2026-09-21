package lang

import (
	"fmt"
	"strings"
	"testing"
)

func TestPrefixSyntaxIsRejected(t *testing.T) {
	if _, err := Parse(`expr->add(1,2)`); err == nil || !strings.Contains(err.Error(), "expr-> prefix") {
		t.Fatalf("expr-> prefix error = %v", err)
	}
	// A bare expression is the whole grammar.
	if _, err := Parse(`add(1,2)`); err != nil {
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
		sugared, err := Parse(pair[0])
		if err != nil {
			t.Fatalf("parse %q: %v", pair[0], err)
		}
		plain, err := Parse(pair[1])
		if err != nil {
			t.Fatalf("parse %q: %v", pair[1], err)
		}
		left, err := ExportExprJSON(sugared)
		if err != nil {
			t.Fatal(err)
		}
		right, err := ExportExprJSON(plain)
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
		{`reduce(x in items, total from 0, total + x)`, map[string]any{"items": []any{1, 2, 3}}, int64(6)},
	} {
		value, _ := compileAndRun(t, test.source, registry, test.args, RunOptions{Fuel: 100_000})
		if fmt.Sprint(value.Any()) != fmt.Sprint(test.want) {
			t.Fatalf("%s = %v, want %v", test.source, value.Any(), test.want)
		}
	}
}

func TestLoopKeywordFormsMatchPositionalForms(t *testing.T) {
	for _, pair := range [][2]string{
		{`[x * 2 for x in items if x > 1]`, `[mul(x,2) for x in items if gt(x,1)]`},
		{`[x for x in items]`, `[x for x in items]`},
		{`reduce(x in items, total from 0, total + x)`, `reduce(items,x,total,0,add(total,x))`},
	} {
		sugared, err := Parse(pair[0])
		if err != nil {
			t.Fatalf("parse %q: %v", pair[0], err)
		}
		plain, err := Parse(pair[1])
		if err != nil {
			t.Fatalf("parse %q: %v", pair[1], err)
		}
		left, _ := ExportExprJSON(sugared)
		right, _ := ExportExprJSON(plain)
		if string(left) != string(right) {
			t.Fatalf("%q desugars to\n%s\nwant\n%s", pair[0], left, right)
		}
	}
}

func TestFloatLiteralPullsVariablesToFloat(t *testing.T) {
	registry := CoreRegistry()
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
		{`switch(country, case "SG" => "a", case "MY" => "b", else "c")`, `switch(country,"SG","a","MY","b","c")`},
	} {
		sugared, err := Parse(pair[0])
		if err != nil {
			t.Fatalf("parse %q: %v", pair[0], err)
		}
		plain, err := Parse(pair[1])
		if err != nil {
			t.Fatalf("parse %q: %v", pair[1], err)
		}
		left, _ := ExportExprJSON(sugared)
		right, _ := ExportExprJSON(plain)
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
		value, _ := compileAndRun(t, test.source, registry, test.args, RunOptions{Fuel: 100_000})
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
		value, _ := compileAndRun(t, source, registry, map[string]any{"country": "SG"}, RunOptions{Fuel: 1000})
		if got, _ := value.Int(); got != 1 {
			t.Fatalf("%s = %v", source, value.Any())
		}
	}
	// A bare variable as a condition is fine — it just types as bool.
	artifact, err := CompileExpr(`switch(case healthy => "yes", else "no")`, registry, CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(artifact.Args) != 1 || !artifact.Args[0].Type.Equal(BoolType) {
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
		expr, err := Parse(source)
		if err != nil {
			t.Fatalf("parse %q: %v", source, err)
		}
		first, err := ExportExprJSON(expr)
		if err != nil {
			t.Fatal(err)
		}
		imported, err := ImportExprJSON(first)
		if err != nil {
			t.Fatalf("import %q: %v", source, err)
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
