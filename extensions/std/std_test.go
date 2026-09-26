package std_test

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/nethinwei/funroute"
	"github.com/nethinwei/funroute/extensions/std"
)

func TestAggregationsReplaceTheFoldConstruct(t *testing.T) {
	t.Parallel()
	prices := funroute.ArgSpec{Name: "prices", Type: funroute.ArrayOf(funroute.IntType)}
	rates := funroute.ArgSpec{Name: "rates", Type: funroute.ArrayOf(funroute.FloatType)}
	for _, test := range []struct {
		name, source string
		specs        []funroute.ArgSpec
		args         map[string]any
		want         any
	}{
		{"总额", "sum(prices)", []funroute.ArgSpec{prices}, map[string]any{"prices": []any{3, 4, 5}}, int64(12)},
		{"筛选后求和", "sum([p for p in prices if p >= 4])", []funroute.ArgSpec{prices}, map[string]any{"prices": []any{3, 4, 5}}, int64(9)},
		{"最大值", "max(rates)", []funroute.ArgSpec{rates}, map[string]any{"rates": []any{0.2, 0.9, 0.5}}, 0.9},
		{"最小值", "min(prices)", []funroute.ArgSpec{prices}, map[string]any{"prices": []any{3, 4, 5}}, int64(3)},
		{"任一为真", "any([p > 4 for p in prices])", []funroute.ArgSpec{prices}, map[string]any{"prices": []any{3, 4, 5}}, true},
		{"全部为真", "all([p > 4 for p in prices])", []funroute.ArgSpec{prices}, map[string]any{"prices": []any{3, 4, 5}}, false},
		{"空数组求和是零", "sum([p for p in prices if p > 99])", []funroute.ArgSpec{prices}, map[string]any{"prices": []any{3}}, int64(0)},
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
	prices := funroute.ArgSpec{Name: "prices", Type: funroute.ArrayOf(funroute.IntType)}
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
	fees := funroute.ArgSpec{Name: "fees", Type: funroute.ArrayOf(funroute.IntType)}
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
	_, err := run(t, "sum(range(n))", map[string]any{"n": 3}, funroute.ArgSpec{Name: "n", Type: funroute.IntType})
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
	artifact, err := funroute.CompileExpr(`sum(range(4)) + len(unique(["a", "a"]))`, registry(t), funroute.CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if artifact.InstructionCount() != 1 {
		t.Fatalf("compiled to %d instructions, want one load", artifact.InstructionCount())
	}
}

// Out-of-range and empty cases are errors, not quiet answers, and the
// rule's or the data's own: data with no answer is ErrDomain, a number out
// of its range ErrArithmetic, and fallback takes neither. The arguments make
// each one fail at run time, not while compiling.
func TestPackRefusesWhatHasNoAnswer(t *testing.T) {
	t.Parallel()
	xs := funroute.ArgSpec{Name: "xs", Type: funroute.ArrayOf(funroute.IntType)}
	n := funroute.ArgSpec{Name: "n", Type: funroute.IntType}
	s := funroute.ArgSpec{Name: "s", Type: funroute.StringType}
	input := map[string]any{"xs": []int64{}, "n": -1, "s": "abc"}
	for source, want := range map[string]error{
		`fallback(slice(s, 0, 9), "x")`:               funroute.ErrDomain,
		`fallback(first(xs), 7)`:                      funroute.ErrDomain,
		`fallback(avg(xs), 7.0)`:                      funroute.ErrDomain,
		`fallback(arg_min(xs), 7)`:                    funroute.ErrDomain,
		`fallback(index_of(xs, 1), 7)`:                funroute.ErrDomain,
		`fallback(len(sort_by([1, 2], xs)), 7)`:       funroute.ErrDomain,
		`fallback(len(split(s, slice(s, 0, 0))), 7)`:  funroute.ErrDomain,
		`fallback(len(take([1, 2], n)), 7)`:           funroute.ErrArithmetic,
		`fallback(len(windows([1, 2], n)), 7)`:        funroute.ErrArithmetic,
		`fallback(percentile([1, 2], float(n)), 7.0)`: funroute.ErrArithmetic,
	} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			if value, err := run(t, source, input, xs, n, s); !errors.Is(err, want) {
				t.Fatalf("%s = %v, %v, want %v", source, value, err, want)
			}
		})
	}
}

// Arithmetic with no answer — an int overflow, an int no float fits, a
// float no int fits — is ErrArithmetic wherever it happens, so fallback does
// not take it: the arguments make each one fail at run time, not while
// compiling.
func TestArithmeticWithNoAnswerIsNotTakenByFallback(t *testing.T) {
	t.Parallel()
	ints := funroute.ArgSpec{Name: "xs", Type: funroute.ArrayOf(funroute.IntType)}
	x := funroute.ArgSpec{Name: "x", Type: funroute.IntType}
	f := funroute.ArgSpec{Name: "f", Type: funroute.FloatType}
	for _, test := range []struct {
		source string
		args   map[string]any
		spec   funroute.ArgSpec
	}{
		{`fallback(abs(x), 7)`, map[string]any{"x": int64(math.MinInt64)}, x},
		{`fallback(pow(x, 0 - 1), 7)`, map[string]any{"x": 2}, x},
		{`fallback(round(f), 7)`, map[string]any{"f": 1e300}, f},
		{`fallback(deltas(xs), [7])`, map[string]any{"xs": []int64{math.MaxInt64, -2}}, ints},
		{`fallback(bool(s), false)`, map[string]any{"s": "yes"}, funroute.ArgSpec{Name: "s", Type: funroute.StringType}},
	} {
		t.Run(test.source, func(t *testing.T) {
			t.Parallel()
			value, err := run(t, test.source, test.args, test.spec)
			if !errors.Is(err, funroute.ErrArithmetic) {
				t.Fatalf("%s with %v = %v, %v, want ErrArithmetic", test.source, test.args, value, err)
			}
		})
	}
}

// A float past its range is an infinity, as IEEE 754 has it, in std as in
// the kernel: nothing fails.
func TestFloatsPastTheirRangeAreInfinities(t *testing.T) {
	t.Parallel()
	floats := funroute.ArgSpec{Name: "fs", Type: funroute.ArrayOf(funroute.FloatType)}
	huge := map[string]any{"fs": []float64{1e308, 1e308}}
	apart := map[string]any{"fs": []float64{1e308, -1e308}}
	for _, test := range []struct {
		source string
		args   map[string]any
		want   float64
	}{
		{`pow(fs[0], 2.0)`, huge, math.Inf(1)},
		{`avg(fs)`, huge, math.Inf(1)},
		{`median(fs)`, huge, math.Inf(1)},
		{`sum(fs)`, huge, math.Inf(1)},
		{`stddev(fs)`, apart, math.Inf(1)},
		{`deltas(fs)[0]`, apart, math.Inf(-1)},
	} {
		t.Run(test.source, func(t *testing.T) {
			t.Parallel()
			value, err := run(t, test.source, test.args, floats)
			if got, _ := value.(float64); err != nil || got != test.want {
				t.Fatalf("%s with %v = %v, %v; want %v", test.source, test.args, value, err, test.want)
			}
		})
	}
}

// A NaN is what Go makes of it: min and max answer it and arg_min and
// arg_max its position, sorting puts it first, and no comparison — in,
// index_of, unique — finds it equal to anything, itself included.
func TestNaNIsAsGoHasIt(t *testing.T) {
	t.Parallel()
	floats := funroute.ArgSpec{Name: "fs", Type: funroute.ArrayOf(funroute.FloatType)}
	args := map[string]any{"fs": []float64{1, math.NaN(), -1}}
	nan := func(v any) bool { f, ok := v.(float64); return ok && math.IsNaN(f) }
	is := func(want any) func(any) bool { return func(v any) bool { return v == want } }
	for source, holds := range map[string]func(any) bool{
		`min(fs)`: nan, `max(fs)`: nan, `min(fs[1], 2.0)`: nan,
		`arg_min(fs)`: is(int64(1)), `arg_max(fs)`: is(int64(1)),
		`sort(fs)[0]`: nan, `sort(fs)[1]`: is(-1.0),
		`fs[1] in fs`: is(false), `len(unique(fs))`: is(int64(3)), `fs[1] != fs[1]`: is(true),
	} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			if value, err := run(t, source, args, floats); err != nil || !holds(value) {
				t.Fatalf("%s with fs = [1 NaN -1] = %v, %v", source, value, err)
			}
		})
	}
}

// A range stops where int64 does: the value after the last one is past stop.
func TestARangeEndsAtTheEdgeOfInt(t *testing.T) {
	t.Parallel()
	for source, want := range map[string]string{
		`range(9223372036854775806, 9223372036854775807, 10)`:                 "[9223372036854775806]",
		`range(0 - 9223372036854775807, 0 - 9223372036854775807 - 1, 0 - 10)`: "[-9223372036854775807]",
	} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			value, err := run(t, source, nil)
			if got := fmt.Sprint(value); err != nil || got != want {
				t.Fatalf("%s = %s, %v, want %s", source, got, err, want)
			}
		})
	}
}

// The corners where one type used to have an operation and its neighbour did
// not, and the one place a dictionary could still be built with a repeated key.
func TestPackIsSymmetric(t *testing.T) {
	t.Parallel()
	names := funroute.ArgSpec{Name: "names", Type: funroute.ArrayOf(funroute.StringType)}
	fees := funroute.ArgSpec{Name: "fees", Type: funroute.ArrayOf(funroute.IntType)}
	for _, test := range []struct {
		name, source string
		specs        []funroute.ArgSpec
		args         map[string]any
		want         any
	}{
		{"字符串取最小", `min(names)`, []funroute.ArgSpec{names}, map[string]any{"names": []any{"b", "a"}}, "a"},
		{"字符串取最大", `max(names)`, []funroute.ArgSpec{names}, map[string]any{"names": []any{"b", "a"}}, "b"},
		{"数组取一段", `len(slice(fees, 1, 3))`, []funroute.ArgSpec{fees}, map[string]any{"fees": []any{1, 2, 3, 4}}, int64(2)},
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
func registry(t *testing.T) *funroute.Registry {
	t.Helper()
	registry := funroute.CoreRegistry()
	if err := registry.EnableForm(funroute.SwitchForm, funroute.ForForm); err != nil {
		t.Fatalf("enable forms: %v", err)
	}
	if err := std.Register(registry); err != nil {
		t.Fatalf("register std: %v", err)
	}
	return registry
}

func run(t *testing.T, source string, args map[string]any, specs ...funroute.ArgSpec) (any, error) {
	t.Helper()
	artifact, err := funroute.CompileExpr(source, registry(t), funroute.CompileOptions{Args: specs})
	if err != nil {
		return nil, err
	}
	runtime, err := funroute.Instantiate(artifact, registry(t))
	if err != nil {
		return nil, err
	}
	value, err := runtime.Run(t.Context(), args)
	if err != nil {
		return nil, err
	}
	return value.Any(), nil
}

// Declaring money adds exactly one money overload to each name that has one
// (min and max two: the array form and the pair; avg and median one, which
// names its rounding), and nothing to the rest.
func TestMoneyDeclarationAddsOneOverloadPerName(t *testing.T) {
	t.Parallel()
	count := func(registry *funroute.Registry) map[string]int {
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
		"min": 2, "max": 2, "avg": 1, "median": 1,
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

// any and all of a comprehension stop at the item that decides them, as ||
// and && do: nothing after it is computed, so nothing after it fails.
func TestQuantifiersStopAtTheItemThatDecides(t *testing.T) {
	t.Parallel()
	xs := funroute.ArgSpec{Name: "xs", Type: funroute.ArrayOf(funroute.IntType)}
	for _, test := range []struct {
		source string
		want   bool
	}{
		{`any([10 / x > 2 for x in xs])`, true},
		{`all([10 / x > 5 for x in xs])`, false},
	} {
		got, err := run(t, test.source, map[string]any{"xs": []any{1, 5, 0}}, xs)
		if err != nil || got != test.want {
			t.Errorf("%s over [1, 5, 0] = %v, %v, want %v: the 0 is past the item that decides", test.source, got, err, test.want)
		}
	}
	if _, err := run(t, `any([10 / x > 20 for x in xs])`, map[string]any{"xs": []any{1, 0}}, xs); !errors.Is(err, funroute.ErrArithmetic) {
		t.Errorf("any with nothing true before the 0 = %v, want its division by zero", err)
	}
}
