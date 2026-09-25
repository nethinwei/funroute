package limits

import (
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/nethinwei/funroute"
)

// repeated is n copies of item, comma separated.
func repeated(item string, n int) string {
	return strings.TrimSuffix(strings.Repeat(item+", ", n), ", ")
}

// sum is a added n times, one addition inside the next: n levels of calls
// and a's one more. Its types stay int however deep it goes, so it measures
// the nesting alone.
func sum(n int) string { return "a" + strings.Repeat(" + a", n) }

// instructions is how many instructions source compiles to, or its error.
func instructions(t *testing.T, registry *funroute.Registry, source string) string {
	t.Helper()
	artifact, err := funroute.CompileExpr(source, registry, options(t, "a: int"))
	if err != nil {
		return "编译期 `" + errorName(err) + "`"
	}
	return strconv.Itoa(artifact.InstructionCount()) + " 条指令"
}

func programTable(t *testing.T, registry *funroute.Registry) string {
	t.Helper()
	one := `{"a": 1}`
	rows := [][3]string{
		{"嵌套 1000 层：`a + a + …` 999 个加法，加上最里面的 `a` / 再多一层", outcome(t, registry, sum(999), "a: int", one), outcome(t, registry, sum(1000), "a: int", one)},
		{"指令数 10000：`len([a, …])` 9998 项 / 9999 项", instructions(t, registry, "len(["+repeated("a", 9998)+"])"), instructions(t, registry, "len(["+repeated("a", 9999)+"])")},
		{"运行时栈深 1024：`len([a, …])` 1024 项 / 1025 项", outcome(t, registry, "len(["+repeated("a", 1024)+"])", "a: int", one), outcome(t, registry, "len(["+repeated("a", 1025)+"])", "a: int", one)},
		{"常量数组也一样：`len([1, …])` 1024 项 / 1025 项", outcome(t, registry, "len(["+repeated("1", 1024)+"])", "", ""), outcome(t, registry, "len(["+repeated("1", 1025)+"])", "", "")},
		{"编译期折叠的栈 256：`len([1, …])` 256 项 / 257 项", instructions(t, registry, "len(["+repeated("1", 256)+"])"), instructions(t, registry, "len(["+repeated("1", 257)+"])")},
	}
	var out strings.Builder
	out.WriteString("| 边界 | 刚好在内 | 越过一点 |\n|---|---|---|\n")
	for _, row := range rows {
		fmt.Fprintf(&out, "| %s | %s | %s |\n", row[0], row[1], row[2])
	}
	return out.String()
}

// sized runs one program on arrays of any length: the first n of one array,
// which a Value wraps without copying.
type sized struct {
	t       *testing.T
	runtime *funroute.Runtime
	items   []int64
}

func newSized(t *testing.T, registry *funroute.Registry, source string) sized {
	t.Helper()
	artifact, err := funroute.CompileExpr(source, registry, options(t, "xs: array<int>"))
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := funroute.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	items := make([]int64, 1<<20)
	for i := range items {
		items[i] = int64(i + 1)
	}
	return sized{t, runtime, items}
}

// runs reports whether the program runs on 1…n within fuel, 0 the default.
func (s sized) runs(n int, fuel uint64) bool {
	arg, err := funroute.ToValue(s.items[:n])
	if err != nil {
		s.t.Fatal(err)
	}
	_, err = s.runtime.RunValues(s.t.Context(), []funroute.Value{arg}, funroute.RunOptions{Fuel: fuel})
	return err == nil
}

// fuel is the least fuel the program runs on 1…n with.
func (s sized) fuel(n int) uint64 {
	low, high := uint64(1), uint64(1<<40)
	for low < high {
		if middle := (low + high) / 2; s.runs(n, middle) {
			high = middle
		} else {
			low = middle + 1
		}
	}
	return low
}

// capacity is the longest array the default fuel runs the program on, for
// one it does not run on all of items: doubling to past it, then halving.
func (s sized) capacity() int {
	high := 1
	for s.runs(high, 0) {
		high *= 2
	}
	low := high / 2
	for low < high-1 {
		if middle := (low + high) / 2; s.runs(middle, 0) {
			low = middle
		} else {
			high = middle
		}
	}
	return low
}

func fuelTable(t *testing.T, registry *funroute.Registry) string {
	t.Helper()
	var out strings.Builder
	out.WriteString("| 程序 | fuel 随长度增加多少 | 默认 10000 fuel 能处理的最长数组 |\n|---|---|---|\n")
	for _, c := range []struct {
		source string
		pairs  bool
	}{
		{source: "[x + 1 for x in xs]"}, {source: "reduce(x in xs, acc = 0, acc + x)"}, {source: "{string(x): x for x in xs}"},
		{source: "sum([x * 2 for x in xs if x > 3])"}, {source: "[x + y for x in xs for y in xs]", pairs: true},
		{source: "sum(xs)"}, {source: "sort(xs)"},
	} {
		program := newSized(t, registry, c.source)
		short, long := program.fuel(20), program.fuel(40)
		growth := fmt.Sprintf("每个元素 %d", (long-short)/20)
		if c.pairs {
			growth = fmt.Sprintf("每对元素 %d", (long-short)/(40*40-20*20))
		}
		if short == long {
			growth = fmt.Sprintf("不增加：整个调用 %d", short)
		}
		reach := fmt.Sprintf("不受 fuel 限制（%d 个元素也只要 %d）", len(program.items), short)
		if !program.runs(len(program.items), 0) {
			reach = strconv.Itoa(program.capacity()) + " 个元素"
		}
		fmt.Fprintf(&out, "| `%s` | %s | %s |\n", c.source, growth, reach)
	}
	return out.String()
}

func catalogTable(t *testing.T, registry *funroute.Registry) string {
	t.Helper()
	core := funroute.CoreRegistry()
	if err := core.EnableForm(funroute.SwitchForm, funroute.ForForm, funroute.ReduceForm); err != nil {
		t.Fatal(err)
	}
	count := func(of *funroute.Registry) (names, overloads int) {
		seen := map[string]bool{}
		for _, function := range of.Catalog().Functions() {
			seen[function.Name()] = true
			overloads++
		}
		return len(seen), overloads
	}
	coreNames, coreOverloads := count(core)
	allNames, allOverloads := count(registry)
	return fmt.Sprintf("| 注册表 | 函数名 | 重载 | 形式 |\n|---|---|---|---|\n"+
		"| 内核（`CoreRegistry`） | %d | %d | %d |\n| 内核 + 金额 + std | %d | %d | %d |\n",
		coreNames, coreOverloads, len(core.Catalog().SpecialForms()), allNames, allOverloads, len(registry.Catalog().SpecialForms()))
}
