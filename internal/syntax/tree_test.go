package syntax

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

// The tree has ExprJSON's shape, with the source of every node and of every
// name the node writes.
func TestSyntaxTreeLocatesNodesAndNames(t *testing.T) {
	t.Parallel()
	source := `let(amount = order.amount.amount, [amount + x for x in xs if x != 0])`
	tree, err := SyntaxTree(source)
	if err != nil {
		t.Fatal(err)
	}
	text := func(span Span) string { return source[span.Start:span.End] }
	if tree.Node != "let" || text(tree.Span) != source {
		t.Fatalf("root is %s over %q, want let over %q", tree.Node, text(tree.Span), source)
	}
	binding := tree.Fields[0].Items[0]
	if binding[0].Text != "amount" || binding[0].TextSpan == nil || binding[0].TextSpan.Start != 4 {
		t.Errorf("the binding's name is %+v, want %q at 4", binding[0], "amount")
	}
	value := binding[1].Nodes[0]
	if text(value.Span) != "order.amount.amount" || value.Fields[1].TextSpan.Start != len("let(amount = order.amount.") {
		t.Errorf("the outer field read is %+v, want %q with its last name at %d", value, "order.amount.amount", len("let(amount = order.amount."))
	}
	loop := tree.Fields[1].Nodes[0]
	if loop.Node != "for" || text(*loop.Fields[1].TextSpan) != "x" || loop.Fields[1].TextSpan.Start <= len("let(amount = order.amount.amount, [amount + x") {
		t.Errorf("the loop variable is %+v, want the x after the yield", loop.Fields[1])
	}
	where := loop.Fields[2].Nodes[0]
	if where.Operator != "!=" || text(where.Span) != "x != 0" {
		t.Errorf("the filter is %+v, want != over %q", where, "x != 0")
	}
	if _, err := json.Marshal(tree); err != nil {
		t.Fatal(err)
	}
	if _, err := SyntaxTree(`let(x = 1)`); err == nil {
		t.Errorf("SyntaxTree(%q) error = nil, want an error", `let(x = 1)`)
	}
}

// Every clause of a nested comprehension covers what was written, and each
// clause's name is found where its own clause writes it: the outer x as
// well as the inner y.
func TestEveryClauseOfAComprehensionLocatesItsName(t *testing.T) {
	t.Parallel()
	source := `[x + y for x in xs for y in ys]`
	tree, err := SyntaxTree(source)
	if err != nil {
		t.Fatal(err)
	}
	named := func(node Tree, name string) *Span {
		for _, field := range node.Fields {
			if field.Name == "variable" && field.Text == name {
				return field.TextSpan
			}
		}
		return nil
	}
	outer := *tree
	var inner Tree
	for _, field := range outer.Fields {
		if field.Name == "yield" {
			inner = field.Nodes[0]
		}
	}
	for node, want := range map[string]struct {
		tree Tree
		at   int
	}{"x": {outer, len("[x + y for ")}, "y": {inner, len("[x + y for x in xs for ")}} {
		if span := named(want.tree, node); span == nil || span.Start != want.at {
			t.Errorf("%s is at %v, want %d", node, span, want.at)
		}
	}
	if inner.Span != outer.Span {
		t.Errorf("the inner clause covers %v, the comprehension %v; want the same", inner.Span, outer.Span)
	}
}

// An operator's tree holds the operands that were written, and only them:
// the false that a && b expands to, the zero of -b, are no one's source.
func TestAnOperatorsTreeHoldsWhatWasWritten(t *testing.T) {
	t.Parallel()
	for source, want := range map[string][]string{
		"a && b": {"a", "b"},
		"a || b": {"a", "b"},
		"!a":     {"a"},
		"a != b": {"a", "b"},
		"-(b)":   {"(b)"},
	} {
		tree, err := SyntaxTree(source)
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, field := range tree.Fields {
			for _, node := range field.Nodes {
				got = append(got, source[node.Span.Start:node.Span.End])
			}
		}
		if !slices.Equal(got, want) {
			t.Errorf("%s: the operands in the tree are %q, want %q", source, got, want)
		}
	}
}

// A chain of operators is read once: each operator's operands are not read
// again for its expansion, which held them, so the tree takes as long as the
// chain is long and not twice as long for every operator more.
func TestALongChainOfOperatorsIsReadOnce(t *testing.T) {
	t.Parallel()
	for _, source := range []string{strings.Repeat("!", 200) + "a", "a" + strings.Repeat(" + a", 500)} {
		tree, err := SyntaxTree(source)
		if err != nil {
			t.Fatal(err)
		}
		if tree.Operator == "" {
			t.Errorf("%.20s…: the root is %s, want the outermost operator", source, tree.Node)
		}
	}
}

// A postfix field's name is located where it is written, after blanks too.
func TestAFieldsNameIsLocatedAfterItsDot(t *testing.T) {
	t.Parallel()
	for _, source := range []string{"order.amount", "order . amount", "f(x).amount", "f(x) . amount", "f(x).\n  amount"} {
		tree, err := SyntaxTree(source)
		if err != nil {
			t.Fatal(err)
		}
		name := tree.Fields[1]
		if name.TextSpan == nil || source[name.TextSpan.Start:name.TextSpan.End] != "amount" {
			t.Errorf("%q: the field's name is at %v, want where amount is written", source, name.TextSpan)
		}
	}
}

// A selector's path is located whole, one name or several.
func TestASelectorsPathIsLocated(t *testing.T) {
	t.Parallel()
	for _, source := range []string{"sort_by(xs, .fee)", "sort_by(xs, .customer.fee)", "sort_by(xs, (.fee))", "sort_by(xs, . fee)", "sort_by(xs, .\n  fee)"} {
		tree, err := SyntaxTree(source)
		if err != nil {
			t.Fatal(err)
		}
		selector := tree.Fields[1].Nodes[1]
		path := selector.Fields[0]
		if path.TextSpan == nil || source[path.TextSpan.Start:path.TextSpan.End] != path.Text {
			t.Errorf("%s: the selector's path %q is at %v, want where it is written", source, path.Text, path.TextSpan)
		}
	}
}
