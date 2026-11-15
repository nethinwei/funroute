package syntax

import (
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/nethinwei/funroute/internal/kit"
)

func TestInfixSugarIsDesugaredToTheSameAST(t *testing.T) {
	t.Parallel()
	// Sugar lives in the source layer only: a + b must produce exactly the
	// ExprJSON that add(a, b) produces.
	for _, pair := range [][2]string{
		{`a + b * 2`, `add(a,mul(b,2))`},
		{`(a + b) * 2`, `mul(add(a,b),2)`},
		{`a - b - 1`, `sub(sub(a,b),1)`},
		{`a < b`, `lt(a,b)`},
		{`a >= b`, `ge(a,b)`},
		{`a != b`, `if(eq(a,b),false,true)`},
		{`a && b`, `if(a,b,false)`},
		{`a || b`, `if(a,true,b)`},
		{`!a`, `if(a,false,true)`},
		{`-a`, `sub(0,a)`},
		{`1_000_000`, `1000000`},
		{`add(a, b,)`, `add(a,b)`},
	} {
		t.Run(pair[0], func(t *testing.T) {
			t.Parallel()
			assertSameExprJSON(t, pair[0], pair[1])
		})
	}
}

// assertSameExprJSON parses both sources and checks that they are one tree:
// the same ExprJSON.
func assertSameExprJSON(t *testing.T, sugaredSource, plainSource string) {
	t.Helper()
	sugared, err := Parse(sugaredSource)
	if err != nil {
		t.Fatalf("parse %q: %v", sugaredSource, err)
	}
	plain, err := Parse(plainSource)
	if err != nil {
		t.Fatalf("parse %q: %v", plainSource, err)
	}
	left, err := ExportExprJSON(sugared)
	if err != nil {
		t.Fatal(err)
	}
	right, err := ExportExprJSON(plain)
	if err != nil {
		t.Fatal(err)
	}
	if string(left) != string(right) {
		t.Fatalf("%q desugars to\n%s\nwant\n%s", sugaredSource, left, right)
	}
}

func TestLoopSugarAndCallsShareOneTree(t *testing.T) {
	t.Parallel()
	for _, pair := range [][2]string{
		{`[x * 2 for x in items if x > 1]`, `[mul(x,2) for x in items if gt(x,1)]`},
		{`[x for x in items]`, `[x for x in items]`},
		{`reduce(x in items, total = 0, total + x)`, `reduce(x in items, total = 0, add(total,x))`},
	} {
		t.Run(pair[0], func(t *testing.T) {
			t.Parallel()
			assertSameExprJSON(t, pair[0], pair[1])
		})
	}
}

// docs/language.md's table of operators is sourceOperators written for a
// reader: each level of precedence lists exactly the operators of that level,
// and names the function each one is. An operator added, moved or renamed
// fails here until the table says so.
func TestTheOperatorTableIsTheOperators(t *testing.T) {
	t.Parallel()
	doc, err := os.ReadFile("../../docs/language.md")
	if err != nil {
		t.Fatal(err)
	}
	start := strings.Index(string(doc), "| 优先级 | 运算符 | 对应函数 |")
	if start < 0 {
		t.Fatal("docs/language.md has no table of operators")
	}
	table, _, _ := strings.Cut(string(doc)[start:], "\n\n")
	rows := map[int][2]string{} // precedence: the operators written, the functions named
	for _, row := range regexp.MustCompile(`(?m)^\| (\d+) \| (.+?) \| (.+) \|$`).FindAllStringSubmatch(table, -1) {
		level, _ := strconv.Atoi(row[1])
		rows[level] = [2]string{strings.ReplaceAll(row[2], `\|`, "|"), row[3]}
	}
	for _, spec := range sourceOperators {
		row, ok := rows[spec.precedence]
		written := "`" + spec.token + "`"
		if spec.fixity == "index" {
			written = "[" // written as its uses, xs[i]
		}
		if !ok || !strings.Contains(row[0], written) {
			t.Errorf("the operator %s %s is not at level %d of the table", spec.fixity, spec.token, spec.precedence)
		}
		if spec.function != "" && !regexp.MustCompile("`"+spec.function+"[`(]").MatchString(row[1]) { // add, or sub(0, a)
			t.Errorf("level %d of the table does not name %s, the function %s is", spec.precedence, spec.function, spec.token)
		}
	}
	if len(rows) != slices.Max(kit.Map(sourceOperators, func(spec operatorSpec) int { return spec.precedence })) {
		t.Errorf("the table has %d levels of precedence, the operators %d", len(rows), slices.Max(kit.Map(sourceOperators, func(spec operatorSpec) int { return spec.precedence })))
	}
}
