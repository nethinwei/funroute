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
	}
	var out strings.Builder
	out.WriteString("| 边界 | 刚好在内 | 越过一点 |\n|---|---|---|\n")
	for _, row := range rows {
		fmt.Fprintf(&out, "| %s | %s | %s |\n", row[0], row[1], row[2])
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
