package main

import (
	"fmt"
	"math/rand/v2"
	"strings"
	"time"

	"github.com/nethinwei/funroute"
)

// largeInputs runs programs on arrays of ten thousand to a million, fuel
// unbounded, to show what each element costs.
func largeInputs(out *strings.Builder) error {
	registry, err := newRegistry()
	if err != nil {
		return err
	}
	out.WriteString("\n## 大输入的吞吐\n\nfuel 不设限，参数是固定种子打乱的 0…n−1，两边用同一份。\"原生 Go\"是同一件事的 Go 循环，在 100 万个元素上测：筛选求和不建中间数组；排序用 `slices.Sort`，std 的 `sort` 在没有 0 与 -0 这种相等却可区分的元素时也用它。\n\n" +
		"| 程序 | n = 1 万 | n = 10 万 | n = 100 万 | 每个元素 | 原生 Go 每个元素 | 倍数 |\n|---|---|---|---|---|---|---|\n")
	for _, c := range []struct {
		source string
		native func([]int64)
	}{
		{"[x + 1 for x in xs]", func(xs []int64) { keep(nativeIncrement(xs)) }},
		{"sum([x * 2 for x in xs if x % 3 == 0])", func(xs []int64) { keep(nativeFilterSum(xs)) }},
		{"{string(x): x for x in xs}", func(xs []int64) { keep(nativeIndex(xs)) }},
		{"sort(xs)", func(xs []int64) { keep(nativeSort(xs)) }},
		{"sum(xs)", func(xs []int64) { keep(nativeSum(xs)) }},
	} {
		line, err := throughput(registry, c.source, c.native)
		if err != nil {
			return err
		}
		out.WriteString(line)
	}
	return nestedThroughput(out, registry)
}

// throughput is one row: source on 10⁴, 10⁵ and 10⁶ elements, and native on
// 10⁶.
func throughput(registry *funroute.Registry, source string, native func([]int64)) (string, error) {
	runtime, err := loaded(registry, source, funroute.ArgSpec{Name: "xs", Type: funroute.ArrayOf(funroute.IntType)})
	if err != nil {
		return "", err
	}
	cells := []string{"`" + source + "`"}
	var last time.Duration
	for _, n := range []int{10_000, 100_000, 1_000_000} {
		if last, err = timeOn(runtime, n); err != nil {
			return "", err
		}
		cells = append(cells, duration(float64(last.Nanoseconds())))
	}
	perElement := float64(last.Nanoseconds()) / 1e6
	nativePerElement := float64(nativeTime(1_000_000, native).Nanoseconds()) / 1e6
	cells = append(cells, fmt.Sprintf("%.1f ns", perElement), fmt.Sprintf("%.2f ns", nativePerElement), ratio(perElement, nativePerElement))
	return "| " + strings.Join(cells, " | ") + " |\n", nil
}

// nativeTime is the fastest of three runs of native on inputs(n), the
// arguments timeOn gives FunRoute.
func nativeTime(n int, native func([]int64)) time.Duration {
	items := inputs(n)
	took, _ := fastest(func() error {
		native(items)
		return nil
	})
	return took
}

// inputs is 0 … n-1 shuffled, the same every run: distinct, so they can key
// a dictionary, and in no order, so sorting has work to do.
func inputs(n int) []int64 {
	random := rand.New(rand.NewPCG(uint64(n), 1))
	items := make([]int64, n)
	for i, value := range random.Perm(n) {
		items[i] = int64(value)
	}
	return items
}

// timeOn is the fastest of three runs on inputs(n).
func timeOn(runtime *funroute.Runtime, n int) (time.Duration, error) {
	arg, err := funroute.ToValue(inputs(n))
	if err != nil {
		return 0, err
	}
	return fastest(func() error {
		_, err := runtime.RunValues(backgroundContext(), []funroute.Value{arg}, funroute.RunOptions{Fuel: 1 << 50})
		return err
	})
}

func nestedThroughput(out *strings.Builder, registry *funroute.Registry) error {
	runtime, err := loaded(registry, "len([x + y for x in xs for y in xs])", funroute.ArgSpec{Name: "xs", Type: funroute.ArrayOf(funroute.IntType)})
	if err != nil {
		return err
	}
	took, err := timeOn(runtime, 1000)
	if err != nil {
		return err
	}
	native := float64(nativeTime(1000, func(xs []int64) { keep(nativePairs(xs, xs)) }).Nanoseconds()) / 1e6
	perPair := float64(took.Nanoseconds()) / 1e6
	fmt.Fprintf(out, "| 1000 × 1000 的嵌套推导式 | — | — | %s（100 万对） | %.1f ns / 对 | %.2f ns / 对 | %s |\n", duration(float64(took.Nanoseconds())), perPair, native, ratio(perPair, native))
	return nil
}

// compiling times compiling programs of growing width, with the contract
// given and with every type inferred.
func compiling(out *strings.Builder) error {
	registry, err := newRegistry()
	if err != nil {
		return err
	}
	out.WriteString("\n## 编译\n\n`[a * 0 + b, a * 1 + b, …]`：很多重载调用共用同一对参数。\n\n| 项数 | 源码 | 有契约 | 无契约（全部推导） |\n|---|---|---|---|\n")
	ints := []funroute.ArgSpec{{Name: "a", Type: funroute.IntType}, {Name: "b", Type: funroute.IntType}}
	for _, n := range []int{100, 500, 1000, 1900} {
		items := make([]string, n)
		for i := range items {
			items[i] = fmt.Sprintf("a * %d + b", i)
		}
		source := "[" + strings.Join(items, ", ") + "]"
		declared, err := compileTime(registry, source, ints)
		if err != nil {
			return err
		}
		inferred, err := compileTime(registry, source, nil)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "| %d | %.1f KB | %s | %s |\n", n, float64(len(source))/1024, duration(float64(declared)), duration(float64(inferred)))
	}
	return inferenceChain(out, registry)
}

func compileTime(registry *funroute.Registry, source string, args []funroute.ArgSpec) (time.Duration, error) {
	return fastest(func() error {
		_, err := funroute.CompileExpr(source, registry, funroute.CompileOptions{Args: args})
		return err
	})
}

// inferenceChain is the other case: every term its own variable.
func inferenceChain(out *strings.Builder, registry *funroute.Registry) error {
	out.WriteString("\n`x0 + x1 + …`，变量互不相同，无契约：\n\n| 项数 | 编译 |\n|---|---|\n")
	for _, n := range []int{10, 100, 1000} {
		terms := make([]string, n)
		for i := range terms {
			terms[i] = fmt.Sprintf("x%d", i)
		}
		took, err := compileTime(registry, strings.Join(terms, " + "), nil)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "| %d | %s |\n", n, duration(float64(took)))
	}
	return nil
}
