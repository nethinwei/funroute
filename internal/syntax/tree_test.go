package syntax

import (
	"encoding/json"
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
