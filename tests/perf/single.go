package main

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/nethinwei/funroute"
	"github.com/nethinwei/funroute/extensions/std"
)

// newRegistry is the kernel with its forms, money in dollars, euros and yen,
// and the standard pack.
func newRegistry() (*funroute.Registry, error) {
	registry := funroute.CoreRegistry()
	if err := registry.EnableForm(funroute.SwitchForm, funroute.ForForm, funroute.ReduceForm); err != nil {
		return nil, err
	}
	err := registry.DeclareMoney(funroute.MoneySpec{Currencies: []funroute.CurrencySpec{{Code: "USD", Digits: 2}, {Code: "EUR", Digits: 2}, {Code: "JPY"}}})
	if err != nil {
		return nil, err
	}
	return registry, std.Register(registry)
}

// loaded compiles source against args and loads it.
func loaded(registry *funroute.Registry, source string, args ...funroute.ArgSpec) (*funroute.Runtime, error) {
	artifact, err := funroute.CompileExpr(source, registry, funroute.CompileOptions{Args: args})
	if err != nil {
		return nil, fmt.Errorf("%s: %w", source, err)
	}
	return funroute.Instantiate(artifact, registry)
}

// measured is one benchmark's time and allocations per run, and the first
// error a run met.
type measured struct {
	nanoseconds float64
	allocations int64
	err         error
}

// bench measures run the way go test -bench does.
func bench(run func(ctx context.Context) error) measured {
	var failed error
	result := testing.Benchmark(func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if err := run(b.Context()); err != nil && failed == nil {
				failed = err
			}
		}
	})
	return measured{perOp(result), result.AllocsPerOp(), failed}
}

// runValues measures runtime on args.
func runValues(runtime *funroute.Runtime, args ...funroute.Value) measured {
	return bench(func(ctx context.Context) error {
		_, err := runtime.RunValues(ctx, args)
		return err
	})
}

func singleRuns(out *strings.Builder) error {
	registry, err := newRegistry()
	if err != nil {
		return err
	}
	rows, err := singleRows(registry)
	if err != nil {
		return err
	}
	out.WriteString("\n## 单次执行\n\n\"原生 Go\"是同一件事直接用 Go 写：宿主惯常的写法，不查溢出、不过边界；换汇调用的就是规则里用的 `Currencies.Convert`。耗时比是 FunRoute 的耗时除以它的：大于 1 表示 FunRoute 更慢，小于 1 表示更快，全文同一口径。\n\n" +
		"| 场景 | FunRoute | 分配 | 原生 Go | 分配 | 耗时比 |\n|---|---|---|---|---|---|\n")
	for _, row := range rows {
		if row.err != nil {
			return fmt.Errorf("%s: %w", row.name, row.err)
		}
		fmt.Fprintf(out, "| %s | %s | %d 次 | %s | %d 次 | %s |\n", row.name, duration(row.nanoseconds), row.allocations,
			duration(row.native.nanoseconds), row.native.allocations, ratio(row.nanoseconds, row.native.nanoseconds))
	}
	return nil
}

// row is one scenario: its time in FunRoute and the same work in Go.
type row struct {
	name string
	measured
	native measured
}

// ratio is FunRoute's time over the other's, written to two figures: above
// 1 FunRoute is slower, below 1 faster. Every table of the report says it
// this one way (tests/perf/expr writes it the same).
func ratio(funroute, other float64) string {
	if other <= 0 {
		return "—"
	}
	switch times := funroute / other; {
	case times >= 10:
		return fmt.Sprintf("%.0f×", times)
	case times >= 1:
		return fmt.Sprintf("%.1f×", times)
	default:
		return fmt.Sprintf("%.2g×", times)
	}
}
