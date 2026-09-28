package syntax

import (
	"strings"
	"testing"
)

// A selector is written out as the comprehension over its call's first
// argument, which a let names when it is not a name already; everything
// else is the tree it was, shared, and the new nodes are numbered after it.
func TestExpandSelectorsWritesOutTheComprehension(t *testing.T) {
	t.Parallel()
	for source, want := range map[string]string{
		`sort_by(xs, .fee)`:              `sort_by(xs, [$item.fee for $item in xs])`,
		`top_k(xs, .a.b, n)`:             `top_k(xs, [$item.a.b for $item in xs], n)`,
		`f(g(y), .fee)`:                  `let($items = g(y), f($items, [$item.fee for $item in $items]))`,
		`[sort_by(r, .k) for r in rows]`: `[sort_by(r, [$item.k for $item in r]) for r in rows]`,
		`f(.fee)`:                        `the selector .fee must be an argument after a call's first`,
		`xs[.fee]`:                       `xs[[$item.fee for $item in xs]]`,
	} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			expr, err := Parse(source)
			if err != nil {
				t.Fatal(err)
			}
			var got string
			if expanded, err := ExpandSelectors(expr); err != nil {
				got = err.Error()
			} else {
				got = inline(expanded, 0)
			}
			if !strings.Contains(got, want) {
				t.Fatalf("ExpandSelectors(%s) = %s, want %s", source, got, want)
			}
		})
	}
}

func TestExpandSelectorsSharesWhatItKeeps(t *testing.T) {
	t.Parallel()
	expr, err := Parse(`let(a = f(x, 1), b = sort_by(xs, .fee), [a, b])`)
	if err != nil {
		t.Fatal(err)
	}
	before := inline(expr, 0)
	expanded, err := ExpandSelectors(expr)
	if err != nil {
		t.Fatal(err)
	}
	if inline(expr, 0) != before {
		t.Fatalf("expanding changed the program to %s", inline(expr, 0))
	}
	kept, _ := expr.(*LetExpr)
	made, ok := expanded.(*LetExpr)
	if !ok || kept.Bindings[0].Value != made.Bindings[0].Value {
		t.Fatalf("the binding with no selector was copied")
	}
	last := 0
	for node := range Nodes(expr) {
		last = max(last, node.NodeID())
	}
	seen := map[int]bool{}
	for node := range Nodes(expanded) {
		if seen[node.NodeID()] {
			t.Fatalf("node %d is in the expanded tree twice", node.NodeID())
		}
		seen[node.NodeID()] = true
	}
	if !seen[last+1] {
		t.Fatalf("no new node is numbered %d, the one after the program's", last+1)
	}
}
