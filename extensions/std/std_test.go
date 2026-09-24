package std_test

import (
	"strings"
	"testing"

	"funroute/extensions/std"
	"funroute/lang"
)

func TestAggregationsReplaceTheFoldConstruct(t *testing.T) {
	t.Parallel()
	prices := lang.ArgSpec{Name: "prices", Type: lang.ArrayOf(lang.IntType)}
	rates := lang.ArgSpec{Name: "rates", Type: lang.ArrayOf(lang.FloatType)}
	for _, test := range []struct {
		name, source string
		specs        []lang.ArgSpec
		args         map[string]any
		want         any
	}{
		{"总额", "sum(prices)", []lang.ArgSpec{prices}, map[string]any{"prices": []any{3, 4, 5}}, int64(12)},
		{"筛选后求和", "sum([p for p in prices if p >= 4])", []lang.ArgSpec{prices}, map[string]any{"prices": []any{3, 4, 5}}, int64(9)},
		{"最大值", "max(rates)", []lang.ArgSpec{rates}, map[string]any{"rates": []any{0.2, 0.9, 0.5}}, 0.9},
		{"最小值", "min(prices)", []lang.ArgSpec{prices}, map[string]any{"prices": []any{3, 4, 5}}, int64(3)},
		{"任一为真", "any([p > 4 for p in prices])", []lang.ArgSpec{prices}, map[string]any{"prices": []any{3, 4, 5}}, true},
		{"全部为真", "all([p > 4 for p in prices])", []lang.ArgSpec{prices}, map[string]any{"prices": []any{3, 4, 5}}, false},
		{"空数组求和是零", "sum([p for p in prices if p > 99])", []lang.ArgSpec{prices}, map[string]any{"prices": []any{3}}, int64(0)},
		{"序列求和", "sum(range(4))", nil, nil, int64(6)},
		{"带步长的序列", "len(range(1, 10, 3))", nil, nil, int64(3)},
		{"常量绑定可以喂给序列", "sum(let(n = 2 + 1, range(n)))", nil, nil, int64(3)},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := run(t, test.source, test.args, test.specs...)
			if err != nil {
				t.Fatalf("run %s: %v", test.source, err)
			}
			if got != test.want {
				t.Fatalf("%s = %v (%T), want %v", test.source, got, got, test.want)
			}
		})
	}
}

func TestEmptyArrayHasNoExtreme(t *testing.T) {
	t.Parallel()
	prices := lang.ArgSpec{Name: "prices", Type: lang.ArrayOf(lang.IntType)}
	_, err := run(t, "max([p for p in prices if p > 99])", map[string]any{"prices": []any{3}}, prices)
	if err == nil {
		t.Fatal("max of an empty array must fail rather than invent a value")
	}
}

// TestRangeTakesBoundedLengths is the bound this pack promises: a length the
// inputs already bound is fine, an arbitrary run-time scalar is not — that is
// the one that would let a single argument stand for an arbitrarily long array
// and break docs/termination.md.
func TestRangeTakesBoundedLengths(t *testing.T) {
	t.Parallel()
	fees := lang.ArgSpec{Name: "fees", Type: lang.ArrayOf(lang.IntType)}
	bounded := map[string]any{"fees": []any{1, 2, 3}}
	for _, source := range []string{
		`len(range(3))`,
		`len(range(len(fees)))`,
		`len(range(len(fees) * 2))`,
	} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			if _, err := run(t, source, bounded, fees); err != nil {
				t.Fatalf("%s should be allowed: %v", source, err)
			}
		})
	}
	_, err := run(t, "sum(range(n))", map[string]any{"n": 3}, lang.ArgSpec{Name: "n", Type: lang.IntType})
	if err == nil {
		t.Fatal("range must refuse a length that is any scalar at run time")
	}
	if !strings.Contains(err.Error(), "bound") {
		t.Fatalf("sum(range(n)) error = %v, want it to say the length is not bound", err)
	}
}

// Everything in this pack is pure, so a call that reads no argument is done
// while the rule compiles.
func TestPackIsConstexpr(t *testing.T) {
	t.Parallel()
	artifact, err := lang.CompileExpr(`sum(range(4)) + len(unique(["a", "a"]))`, registry(t), lang.CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if artifact.InstructionCount() != 1 {
		t.Fatalf("compiled to %d instructions, want one load", artifact.InstructionCount())
	}
}

// Out-of-range and empty cases are errors, not quiet answers.
func TestPackRefusesWhatHasNoAnswer(t *testing.T) {
	t.Parallel()
	for _, source := range []string{
		`slice("abc", 0, 9)`,
		`first([n for n in [1] if n > 9])`,
		`split("a,b", "")`,
		`take([1, 2], 0 - 1)`,
	} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			if _, err := run(t, source, nil); err == nil {
				t.Fatalf("%s must fail", source)
			}
		})
	}
}

// The corners where one type used to have an operation and its neighbour did
// not, and the one place a dictionary could still be built with a repeated key.
func TestPackIsSymmetric(t *testing.T) {
	t.Parallel()
	names := lang.ArgSpec{Name: "names", Type: lang.ArrayOf(lang.StringType)}
	fees := lang.ArgSpec{Name: "fees", Type: lang.ArrayOf(lang.IntType)}
	for _, test := range []struct {
		name, source string
		specs        []lang.ArgSpec
		args         map[string]any
		want         any
	}{
		{"字符串取最小", `min(names)`, []lang.ArgSpec{names}, map[string]any{"names": []any{"b", "a"}}, "a"},
		{"字符串取最大", `max(names)`, []lang.ArgSpec{names}, map[string]any{"names": []any{"b", "a"}}, "b"},
		{"数组取一段", `len(slice(fees, 1, 3))`, []lang.ArgSpec{fees}, map[string]any{"fees": []any{1, 2, 3, 4}}, int64(2)},
		{"整数绝对值", `abs(0 - 5)`, nil, nil, int64(5)},
		{"浮点绝对值", `abs(0.0 - 1.5)`, nil, nil, 1.5},
		// Rounding answers with an int: the caller wanted an integer, and
		// having to write int(floor(x)) to get one is a tax on every use.
		{"向上取整", `ceil(1.2)`, nil, nil, int64(2)},
		{"向下取整", `floor(1.8)`, nil, nil, int64(1)},
		{"四舍五入", `round(1.5)`, nil, nil, int64(2)},
		{"负数四舍五入", `round(0.0 - 1.5)`, nil, nil, int64(-2)},
		{"取整后直接算整数", `floor(1250.0 / 100.0) * 100`, nil, nil, int64(1200)},
		{"要 float 就显式转", `float(floor(1.8)) / 2.0`, nil, nil, 0.5},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := run(t, test.source, test.args, test.specs...)
			if err != nil {
				t.Fatalf("run %s: %v", test.source, err)
			}
			if got != test.want {
				t.Fatalf("%s = %v (%T), want %v", test.source, got, got, test.want)
			}
		})
	}
	// A dictionary refuses a repeated key whichever door it came in by.
	if _, err := run(t, `{k: 1 for k in names}`, map[string]any{"names": []any{"a", "a"}}, names); err == nil {
		t.Fatal("a comprehension that produces one key twice must fail")
	}
	// round takes a float to a whole float; turning it into an int is the
	// caller's explicit step, like every other cross-type move.
	got, err := run(t, `int(round(2.5))`, nil)
	if err != nil || got != int64(3) {
		t.Fatalf("int(round(2.5)) = %v, %v; want 3", got, err)
	}
}

// A name that serves several element types has to serve all of them. Dropping
// one registration is invisible: the function still exists and still works for
// the types it kept, so nothing fails until a rule reaches the missing type in
// production — and what it gets then is "no overload", which reads like the
// rule is wrong rather than the pack incomplete.
//
// The counts below are the point of the test, so each one says where it comes
// from. A number that changes here is either a deliberate extension or the
// registration that went missing.
func TestNamesCoverEveryElementTypeTheyClaim(t *testing.T) {
	t.Parallel()
	overloads := map[string]int{}
	for _, function := range registry(t).Catalog().Functions() {
		overloads[function.Name()]++
	}
	for _, expected := range []struct {
		name  string
		count int
		why   string
	}{
		{"sum", 2, "int / float"},
		{"avg", 2, "int / float"},
		{"median", 2, "int / float"},
		{"stddev", 2, "int / float"},
		{"percentile", 2, "int / float"},
		{"cumsum", 2, "int / float"},
		{"deltas", 2, "int / float"},
		{"abs", 2, "int / float"},
		{"pow", 2, "int / float"},
		{"sort", 3, "int / float / string"},
		{"sort_desc", 3, "int / float / string"},
		{"rank", 3, "int / float / string"},
		{"arg_min", 3, "键是 int / float / string"},
		{"arg_max", 3, "键是 int / float / string"},
		{"sort_by", 3, "键是 int / float / string"},
		{"sort_by_desc", 3, "键是 int / float / string"},
		{"top_k", 3, "键是 int / float / string"},
		{"bottom_k", 3, "键是 int / float / string"},
		{"min", 6, "数组版 3 种元素类型，二元版 3 种"},
		{"max", 6, "数组版 3 种元素类型，二元版 3 种"},
	} {
		t.Run(expected.name, func(t *testing.T) {
			t.Parallel()
			if got := overloads[expected.name]; got != expected.count {
				t.Errorf("%s 有 %d 个重载，应为 %d（%s）", expected.name, got, expected.count, expected.why)
			}
		})
	}
}

// registry is a console with the structural forms and this pack: what a rule
// needs to map over an input and collapse the result.
func registry(t *testing.T) *lang.Registry {
	t.Helper()
	registry := lang.CoreRegistry()
	if err := registry.EnableForm(lang.SwitchForm, lang.ForForm); err != nil {
		t.Fatalf("enable forms: %v", err)
	}
	if err := std.Register(registry); err != nil {
		t.Fatalf("register std: %v", err)
	}
	return registry
}

func run(t *testing.T, source string, args map[string]any, specs ...lang.ArgSpec) (any, error) {
	t.Helper()
	artifact, err := lang.CompileExpr(source, registry(t), lang.CompileOptions{Args: specs})
	if err != nil {
		return nil, err
	}
	runtime, err := lang.Instantiate(artifact, registry(t))
	if err != nil {
		return nil, err
	}
	value, err := runtime.Run(t.Context(), args, lang.RunOptions{Fuel: 100_000})
	if err != nil {
		return nil, err
	}
	return value.Any(), nil
}

// Declaring money adds exactly one money overload to each name that has one
// (min and max two: the array form and the pair), and nothing to the rest.
func TestMoneyDeclarationAddsOneOverloadPerName(t *testing.T) {
	t.Parallel()
	count := func(registry *lang.Registry) map[string]int {
		overloads := map[string]int{}
		for _, function := range registry.Catalog().Functions() {
			overloads[function.Name()]++
		}
		return overloads
	}
	without, with := count(registry(t)), count(moneyPack(t))
	for name, added := range map[string]int{
		"sum": 1, "abs": 1, "sort": 1, "sort_desc": 1, "cumsum": 1, "deltas": 1,
		"arg_min": 1, "arg_max": 1, "sort_by": 1, "sort_by_desc": 1, "top_k": 1, "bottom_k": 1,
		"min": 2, "max": 2, "avg": 2, "median": 2,
		"stddev": 0, "percentile": 0, "rank": 0, "pow": 0, "unique": 0, "take": 0,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := with[name] - without[name]; got != added {
				t.Errorf("declaring money adds %d overloads of %s (%d → %d), want %d", got, name, without[name], with[name], added)
			}
		})
	}
}

// A registry that never declared money has no money overload anywhere.
func TestPackWithoutMoneyHasNoMoneySignature(t *testing.T) {
	t.Parallel()
	for _, function := range registry(t).Catalog().Functions() {
		if strings.Contains(function.Signature(), "money") || function.Doc().Category == "金额" {
			t.Errorf("%s is in a registry without money", function.Signature())
		}
	}
}
