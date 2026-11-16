package syntax

import (
	"strings"
	"testing"
)

func TestFormatCommentsInsideExpressions(t *testing.T) {
	t.Parallel()
	for source, want := range map[string]string{
		"f(a, // why\n b)":                                  "f(\n  a, // why\n  b\n)",
		"f(a // why\n,b)":                                   "f(\n  a, // why\n  b\n)",
		"f(a\n// next\n,b)":                                 "f(\n  a,\n  // next\n  b\n)",
		"let(// input\nx=1,// value\nx+2)":                  "let(\n  // input\n  x = 1, // value\n  x + 2\n)",
		"{fee:USD -1.70,// refund\nrate:2.9%}":              "{\n  fee: USD -1.70, // refund\n  rate: 2.9%\n}",
		"f([1,// first\n2],g(3,4))":                         "f(\n  [\n    1, // first\n    2\n  ],\n  g(3, 4)\n)",
		"switch(x,case 1=>a,// primary\nelse=>b)":           "switch(\n  x,\n  case 1 => a, // primary\n  else => b\n)",
		"[x*2// yield\nfor x in xs if x>0]":                 "[\n  x * 2 // yield\n  for x in xs if x > 0\n]",
		"a+// why\nb":                                       "a + // why\nb",
		"f(// empty\n)":                                     "f(\n  // empty\n)",
		"f(\r\na,// 中文\r\nb\r\n)\r\n":                       "f(\n  a, // 中文\n  b\n)\n",
		"// contract\n\nf(\"// text\",// real\nx) // end\n": "// contract\n\nf(\n  \"// text\", // real\n  x\n) // end\n",
		"f(@a .b,// field\nUSD-1)":                          "f(\n  @a .b, // field\n  USD - 1\n)",
		"f(USD\n-1,// subtract\nx)":                         "f(\n  USD\n  - 1, // subtract\n  x\n)",
	} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			if got := checkCommentFormat(t, source); got != want {
				t.Errorf("FormatSource(%q) =\n%s\nwant:\n%s", source, got, want)
			}
		})
	}
}

// Exercise every token boundary in real programs, including punctuation,
// expanded operators, signed amounts, enum fields and comprehension clauses.
func TestFormatCommentsAtEveryTokenBoundary(t *testing.T) {
	t.Parallel()
	for _, source := range fuzzSources(t) {
		r := read(source, false)
		if r.err != nil {
			continue
		}
		for _, tok := range r.tokens {
			commented := source[:tok.end] + " // boundary\n" + source[tok.end:]
			if _, err := Parse(commented); err != nil {
				continue // money and exchange-rate literals cannot contain comments
			}
			checkCommentFormat(t, commented)
		}
	}
}

func checkCommentFormat(t *testing.T, source string) string {
	t.Helper()
	before := read(source, false)
	if before.err != nil {
		t.Fatal(before.err)
	}
	got, err := FormatSource(source)
	if err != nil {
		t.Fatalf("FormatSource(%q): %v", source, err)
	}
	checkReparses(t, "FormatSource", source, got, exportFuzzed(t, source, before.expr))
	if again, err := FormatSource(got); err != nil || again != got {
		t.Fatalf("FormatSource is not idempotent for %q:\n%s\nthen:\n%s\n%v", source, got, again, err)
	}
	after := read(got, false)
	if len(before.lex.comments) != len(after.lex.comments) {
		t.Fatalf("FormatSource(%q) changed its comments: %q", source, got)
	}
	for i, comment := range before.lex.comments {
		if strings.TrimRight(comment.text, " \t\r") != strings.TrimRight(after.lex.comments[i].text, " \t\r") {
			t.Fatalf("FormatSource(%q) changed comment %d: %q", source, i, got)
		}
	}
	return got
}
