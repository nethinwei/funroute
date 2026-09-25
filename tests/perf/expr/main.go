// Command expr measures FunRoute beside expr (github.com/expr-lang/expr) on
// the same work and writes the comparison as a Markdown section on standard
// output: make perf appends it to docs/perf.md. It is a module of its own so
// that expr stays out of FunRoute's go.mod, which has no dependencies.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"runtime/debug"
	"strings"
	"testing"
)

// benchTime is how long each side of a row is measured: about 100 rows,
// two sides each, keep make perf within a few minutes.
const benchTime = "300ms"

func main() {
	testing.Init()
	if err := flag.Set("test.benchtime", benchTime); err != nil {
		fail(err)
	}
	groups := []group{scalarGroup(), textGroup(), loopGroup(), arrayGroup(), recordGroup(), boundaryGroup(), compileGroup()}
	tables, err := measure(groups)
	if err != nil {
		fail(err)
	}
	var out strings.Builder
	fmt.Fprintf(&out, "\n## 与 expr 对照\n\n由 `tests/perf/expr` 生成：它是单独的 module，只有它依赖 expr（%s），FunRoute 的 go.mod 仍然为空。"+
		"两边从同一个宿主 struct 读参数，先编译好再反复执行，每边测 %s。FunRoute 用 `Program.Run`（可并发调用）；expr 用复用的 `vm.VM`，这是它最快的用法，但一个 `VM` 不能并发；宿主函数在 expr 里经 `expr.Function` 注册。"+
		"每一行都先经 JSON 核对两边答案相同。倍数是 expr 的耗时除以 FunRoute 的，大于 1 表示 FunRoute 更快。\n", exprVersion(), benchTime)
	for i, rows := range tables {
		fmt.Fprintf(&out, "\n### %s\n\n| FunRoute 写法 | expr 写法 | FunRoute | 分配 | expr | 分配 | 倍数 |\n|---|---|---|---|---|---|---|\n", groups[i].title)
		for _, row := range rows {
			fmt.Fprintf(&out, "| %s | %s | %s | %d 次 | %s | %d 次 | %s |\n", cell(row.name), cell(row.expr), duration(row.ours.nanoseconds), row.ours.allocations,
				duration(row.theirs.nanoseconds), row.theirs.allocations, ratio(row.theirs.nanoseconds, row.ours.nanoseconds))
		}
	}
	registry, err := newRegistry()
	if err != nil {
		fail(err)
	}
	section, err := parallelSection(registry)
	if err != nil {
		fail(err)
	}
	out.WriteString(section)
	out.WriteString(notes)
	fmt.Print(out.String())
}

// notes say what the rows do not: where the two write the same work
// differently, and what has no counterpart to measure.
const notes = "\n写法的差别：expr 的 `/` 总是浮点除法，整数除法写成 `int(a / b)`；expr 的 `sort` 不接受 `[]int64`，排序的几行 expr 读同样内容的 `[]int`；FunRoute 的推导式在 expr 里是带谓词的 `map`/`filter`/`sum`/`count`/`any`，`switch` 是连写的 `?:`；" +
	"expr 没有 `intersect`/`except`/`arg_max`/`top_k`/`with`/`index_of`，写成它能写的等价形式。\n\n" +
	"没有对照的：金额（`money`/`ratio`/`fxrate`、`round(…, @mode)`、`using` 与 `->`、`allocate`）、`fallback`、枚举与穷尽的 `switch`、句柄与模型批处理，expr 没有对应；" +
	"`windows`、`chunk`、`deltas`、`cumsum`、`take_while`、`drop_while`、`stddev`、`percentile`、`rank`、`pad_left`/`pad_right`、`merge` 在 expr 里没有内置函数。\n"

func fail(err error) {
	fmt.Fprintln(os.Stderr, "perf/expr:", err)
	os.Exit(1)
}

// cell escapes the pipes in a table cell, which Markdown would otherwise
// read as the end of the cell: a || b, join(…, "|").
func cell(source string) string { return strings.ReplaceAll(source, "|", `\|`) }

// exprVersion is the version of expr this program was built with.
func exprVersion() string {
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, module := range info.Deps {
			if module.Path == "github.com/expr-lang/expr" {
				return module.Version
			}
		}
	}
	return "版本未知"
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
	return measured{float64(result.T.Nanoseconds()) / float64(result.N), result.AllocsPerOp(), failed}
}

// duration writes nanoseconds the way a person reads them.
func duration(nanoseconds float64) string {
	switch {
	case nanoseconds < 10:
		return fmt.Sprintf("%.2f ns", nanoseconds)
	case nanoseconds < 1e3:
		return fmt.Sprintf("%.0f ns", nanoseconds)
	case nanoseconds < 1e6:
		return fmt.Sprintf("%.1f µs", nanoseconds/1e3)
	}
	return fmt.Sprintf("%.1f ms", nanoseconds/1e6)
}

// ratio is how many times the first time is the second, to two figures.
func ratio(theirs, ours float64) string {
	times := theirs / ours
	if times >= 10 {
		return fmt.Sprintf("%.0f×", times)
	}
	return fmt.Sprintf("%.1f×", times)
}
