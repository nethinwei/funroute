package compile

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/nethinwei/funroute/internal/machine"
)

func TestSampleInfersArgumentsAndRuns(t *testing.T) {
	t.Parallel()
	registry := machine.CoreRegistry()
	artifact, err := CompileExpr(`if(a,b,add(1,1))`, registry, CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(artifact.Args()) != 2 {
		t.Fatalf("args = %#v, want a and b", artifact.Args())
	}
	if artifact.Args()[0].Name() != "a" || !artifact.Args()[0].Type().Equal(machine.BoolType) {
		t.Fatalf("first arg = %#v, want a: bool", artifact.Args()[0])
	}
	if artifact.Args()[1].Name() != "b" || !artifact.Args()[1].Type().Equal(machine.IntType) {
		t.Fatalf("second arg = %#v, want b: int", artifact.Args()[1])
	}
	if !artifact.Result().Equal(machine.IntType) {
		t.Fatalf("result = %s, want int", artifact.Result())
	}
	runtime, err := machine.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	result, err := runtime.Run(t.Context(), map[string]any{"a": true, "b": 41}, machine.RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if value, ok := result.Int(); !ok || value != 41 {
		t.Fatalf("Run(a=true, b=41) = %#v, want 41", result.Any())
	}
	result, err = runtime.Run(t.Context(), map[string]any{"a": false, "b": 41}, machine.RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if value, ok := result.Int(); !ok || value != 2 {
		t.Fatalf("Run(a=false, b=41) = %#v, want 2", result.Any())
	}
}

func TestStrongTypesAllowNumericWideningButRejectMixedContainers(t *testing.T) {
	t.Parallel()
	registry := machine.CoreRegistry()
	artifact, err := CompileExpr(`add(1,1.5)`, registry, CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := machine.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	result, err := runtime.Run(t.Context(), map[string]any{}, machine.RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if value, ok := result.Float(); !ok || value != 2.5 {
		t.Fatalf("add(1,1.5) = %#v, want 2.5", result.Any())
	}
	for _, source := range []string{
		`[1,"two"]`,
		`{"one":1,"two":2.0}`,
	} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			if _, err := CompileExpr(source, registry, CompileOptions{}); err == nil {
				t.Fatalf("%s compiled without a type error", source)
			}
		})
	}
}

func TestImplicitNumericDefaultAndExplicitConversions(t *testing.T) {
	t.Parallel()
	registry := machine.CoreRegistry()
	artifact, err := CompileExpr(`add(a,b)`, registry, CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !artifact.Args()[0].Type().Equal(machine.IntType) || !artifact.Args()[1].Type().Equal(machine.IntType) || !artifact.Result().Equal(machine.IntType) {
		t.Fatalf("implicit default = %#v -> %s, want (int, int) -> int", artifact.Args(), artifact.Result())
	}
	artifact, err = CompileExpr(`add(a,b)`, registry, CompileOptions{
		Args: []ArgSpec{{Name: "a", Type: machine.FloatType}, {Name: "b", Type: machine.FloatType}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !artifact.Result().Equal(machine.FloatType) {
		t.Fatalf("add(a,b) with float arguments returns %s, want float", artifact.Result())
	}
	artifact, err = CompileExpr(`add(float(amount),0.5)`, registry, CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(artifact.Args()) != 1 || !artifact.Args()[0].Type().Equal(machine.IntType) || !artifact.Result().Equal(machine.FloatType) {
		t.Fatalf("conversion inference = %#v -> %s, want (amount: int) -> float", artifact.Args(), artifact.Result())
	}
}

func TestFloatLiteralPullsVariablesToFloat(t *testing.T) {
	t.Parallel()
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
		t.Run(test.source, func(t *testing.T) {
			t.Parallel()
			artifact, err := CompileExpr(test.source, registry, CompileOptions{})
			if err != nil {
				t.Fatalf("%s: %v", test.source, err)
			}
			params := make([]string, len(artifact.Args()))
			for i, param := range artifact.Args() {
				params[i] = param.Name() + ": " + param.Type().String()
			}
			got := "(" + strings.Join(params, ", ") + ") -> " + artifact.Result().String()
			if got != test.want {
				t.Fatalf("%s infers %s, want %s", test.source, got, test.want)
			}
		})
	}
}

func TestDeadCandidatesAreDroppedNotFatal(t *testing.T) {
	t.Parallel()
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
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			artifact, err := CompileExpr(source, registry, CompileOptions{})
			if err != nil {
				t.Fatalf("%s: %v", source, err)
			}
			if !artifact.Args()[0].Type().Equal(machine.IntType) && !artifact.Args()[0].Type().Equal(machine.ArrayOf(machine.IntType)) {
				t.Fatalf("%s infers %s, want int or array<int>", source, artifact.Args()[0].Type())
			}
		})
	}
	// A genuine conflict still fails.
	if _, err := CompileExpr(`x > 1 && x == "a"`, registry, CompileOptions{}); err == nil {
		t.Fatal("conflicting types compiled")
	}
}

// A type error never shows inference's own variables: an open literal is
// named by the kinds it may be, anything else as unknown.
func TestTypeErrorsDoNotShowInferenceVariables(t *testing.T) {
	t.Parallel()
	numbered := regexp.MustCompile(`\?[0-9]`)
	for source, want := range map[string]string{
		`0 % "a"`:   "int|ratio|money",
		`0.5 % "a"`: "float|ratio",
		`[] + 1`:    "?",
	} {
		_, err := CompileExpr(source, moneyRegistry(t), CompileOptions{})
		if err == nil || numbered.MatchString(err.Error()) || !strings.Contains(err.Error(), want) {
			t.Errorf("CompileExpr(%q) error = %v, want one naming %s and no numbered variable", source, err, want)
		}
	}
}

// chain is n variables joined by op, which nothing but the operators types.
func chain(n int, op string) string {
	parts := make([]string, n)
	for i := range parts {
		parts[i] = fmt.Sprintf("x%d", i)
	}
	return strings.Join(parts, op)
}

// A long expression with no contract types in time proportional to its
// length: every operator is overloaded, and a reading per combination of
// overloads would never finish.
func TestAnUncontractedChainTypesWithoutEnumerating(t *testing.T) {
	t.Parallel()
	registry := moneyRegistry(t)
	for _, test := range []struct{ op, want string }{
		{" + ", "int"}, {" * ", "int"}, {" < 1 && ", "bool"},
	} {
		t.Run(test.op, func(t *testing.T) {
			t.Parallel()
			artifact, err := CompileExpr(chain(300, test.op), registry, CompileOptions{})
			if err != nil {
				t.Fatalf("CompileExpr(x0%s…x299) error = %v", test.op, err)
			}
			if got := artifact.Result().String(); got != test.want || artifact.Args()[0].Type().String() != "int" {
				t.Fatalf("CompileExpr(x0%s…x299) = %v -> %s, want int arguments -> %s", test.op, artifact.Args()[0].Type(), got, test.want)
			}
		})
	}
}

func BenchmarkInferChain(b *testing.B) {
	registry := moneyRegistry(b)
	for _, n := range []int{10, 100, 1000} {
		source := chain(n, " + ")
		b.Run(strconv.Itoa(n), func(b *testing.B) {
			for b.Loop() {
				mustCompile(b, source, registry)
			}
		})
	}
}

func mustCompile(b *testing.B, source string, registry *machine.Registry) {
	b.Helper()
	if _, err := CompileExpr(source, registry, CompileOptions{}); err != nil {
		b.Fatal(err)
	}
}

// A type error found once every choice is settled keeps the place it was
// found at, and says so once: x.a is what has no known record type, not the
// sum around it.
func TestASettledTypeErrorKeepsItsPlace(t *testing.T) {
	t.Parallel()
	assertErrorCovers(t, "1 + x.a", CompileOptions{}, "x.a")
	_, err := CompileExpr("1 + x.a", machine.CoreRegistry(), CompileOptions{})
	if err == nil || strings.Count(err.Error(), "type error") != 1 {
		t.Fatalf("CompileExpr(1 + x.a) error = %v, want one saying type error once", err)
	}
}
