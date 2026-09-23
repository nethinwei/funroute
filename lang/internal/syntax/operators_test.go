package syntax

import "testing"

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
