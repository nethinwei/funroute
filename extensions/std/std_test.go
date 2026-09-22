package std_test

import (
	"context"
	"strings"
	"testing"

	"funroute/extensions/std"
	"funroute/lang"
)

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
	value, err := runtime.Run(context.Background(), args, lang.RunOptions{Fuel: 100_000})
	if err != nil {
		return nil, err
	}
	return value.Any(), nil
}

func TestAggregationsReplaceTheFoldConstruct(t *testing.T) {
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
	prices := lang.ArgSpec{Name: "prices", Type: lang.ArrayOf(lang.IntType)}
	_, err := run(t, "max([p for p in prices if p > 99])", map[string]any{"prices": []any{3}}, prices)
	if err == nil {
		t.Fatal("max of an empty array must fail rather than invent a value")
	}
}

// TestRangeRefusesRunTimeLength is the bound this pack promises: a range whose
// length is only known at run time would let one scalar argument stand for an
// arbitrarily long array, which is what docs/termination.md rules out.
func TestRangeRefusesRunTimeLength(t *testing.T) {
	_, err := run(t, "sum(range(n))", map[string]any{"n": 3}, lang.ArgSpec{Name: "n", Type: lang.IntType})
	if err == nil {
		t.Fatal("range must refuse an argument that is only known at run time")
	}
	if !strings.Contains(err.Error(), "compile time") {
		t.Fatalf("error should say why: %v", err)
	}
}

// The string and array functions, each shown doing the job it exists for.
func TestStringsAndArrays(t *testing.T) {
	card := lang.ArgSpec{Name: "card", Type: lang.StringType}
	fees := lang.ArgSpec{Name: "fees", Type: lang.ArrayOf(lang.IntType)}
	names := lang.ArgSpec{Name: "names", Type: lang.ArrayOf(lang.StringType)}
	for _, test := range []struct {
		name, source string
		specs        []lang.ArgSpec
		args         map[string]any
		want         any
	}{
		{"取卡 BIN", `slice(trim(card), 0, 6)`, []lang.ArgSpec{card}, map[string]any{"card": " 4111111111111111 "}, "411111"},
		{"判前缀", `starts_with(card, "4")`, []lang.ArgSpec{card}, map[string]any{"card": "4111"}, true},
		{"规范化", `upper(card) == "AB"`, []lang.ArgSpec{card}, map[string]any{"card": "ab"}, true},
		{"拆分再拼", `join(split(card, ","), "|")`, []lang.ArgSpec{card}, map[string]any{"card": "a,b"}, "a|b"},
		{"替换", `replace(card, "_", ":")`, []lang.ArgSpec{card}, map[string]any{"card": "a_b"}, "a:b"},
		{"最便宜", `first(sort(fees))`, []lang.ArgSpec{fees}, map[string]any{"fees": []any{30, 10, 20}}, int64(10)},
		{"最贵", `last(sort(fees))`, []lang.ArgSpec{fees}, map[string]any{"fees": []any{30, 10, 20}}, int64(30)},
		{"取前两个", `len(take(fees, 2))`, []lang.ArgSpec{fees}, map[string]any{"fees": []any{1, 2, 3}}, int64(2)},
		{"去重后计数", `len(unique(names))`, []lang.ArgSpec{names}, map[string]any{"names": []any{"a", "a", "b"}}, int64(2)},
		{"拉平", `len(flatten([[f] for f in fees]))`, []lang.ArgSpec{fees}, map[string]any{"fees": []any{1, 2}}, int64(2)},
	} {
		t.Run(test.name, func(t *testing.T) {
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

// Everything in this pack is pure, so a call that reads no argument is done
// while the rule compiles.
func TestPackIsConstexpr(t *testing.T) {
	artifact, err := lang.CompileExpr(`sum(range(4)) + len(unique(["a", "a"]))`, registry(t), lang.CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(artifact.Instructions) != 1 {
		t.Fatalf("compiled to %d instructions, want one load", len(artifact.Instructions))
	}
}

// Out-of-range and empty cases are errors, not quiet answers.
func TestPackRefusesWhatHasNoAnswer(t *testing.T) {
	for _, source := range []string{
		`slice("abc", 0, 9)`,
		`first([n for n in [1] if n > 9])`,
		`split("a,b", "")`,
		`take([1, 2], 0 - 1)`,
	} {
		if _, err := run(t, source, nil); err == nil {
			t.Fatalf("%s must fail", source)
		}
	}
}
