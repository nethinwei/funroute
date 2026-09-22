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
		{"条件计数", "count([p for p in prices if p >= 4])", []lang.ArgSpec{prices}, map[string]any{"prices": []any{3, 4, 5}}, int64(2)},
		{"最大值", "max(rates)", []lang.ArgSpec{rates}, map[string]any{"rates": []any{0.2, 0.9, 0.5}}, 0.9},
		{"最小值", "min(prices)", []lang.ArgSpec{prices}, map[string]any{"prices": []any{3, 4, 5}}, int64(3)},
		{"任一为真", "any([p > 4 for p in prices])", []lang.ArgSpec{prices}, map[string]any{"prices": []any{3, 4, 5}}, true},
		{"全部为真", "all([p > 4 for p in prices])", []lang.ArgSpec{prices}, map[string]any{"prices": []any{3, 4, 5}}, false},
		{"空数组求和是零", "sum([p for p in prices if p > 99])", []lang.ArgSpec{prices}, map[string]any{"prices": []any{3}}, int64(0)},
		{"序列求和", "sum(range(4))", nil, nil, int64(6)},
		{"带步长的序列", "count(range(1, 10, 3))", nil, nil, int64(3)},
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
