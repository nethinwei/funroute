package syntax

import (
	"encoding/json"
	"testing"
)

// The tree has ExprJSON's shape, with the source of every node and of every
// name the node writes.
func TestSyntaxTreeLocatesNodesAndNames(t *testing.T) {
	source := `let(amount = order.amount.amount, [amount + x for x in xs if x != 0])`
	tree, err := SyntaxTree(source)
	if err != nil {
		t.Fatal(err)
	}
	text := func(span Span) string { return source[span.Start:span.End] }
	if tree.Node != "let" || text(tree.Span) != source {
		t.Fatalf("root is %s over %q", tree.Node, text(tree.Span))
	}
	binding := tree.Fields[0].Items[0]
	if binding[0].Text != "amount" || binding[0].TextSpan == nil || binding[0].TextSpan.Start != 4 {
		t.Errorf("the binding's name is %+v", binding[0])
	}
	value := binding[1].Nodes[0]
	if text(value.Span) != "order.amount.amount" || value.Fields[1].TextSpan.Start != len("let(amount = order.amount.") {
		t.Errorf("the outer field read is %+v", value)
	}
	loop := tree.Fields[1].Nodes[0]
	if loop.Node != "for" || text(*loop.Fields[1].TextSpan) != "x" || loop.Fields[1].TextSpan.Start <= int(len("let(amount = order.amount.amount, [amount + x")) {
		t.Errorf("the loop variable is %+v", loop.Fields[1])
	}
	where := loop.Fields[2].Nodes[0]
	if where.Operator != "!=" || text(where.Span) != "x != 0" {
		t.Errorf("the filter is %+v", where)
	}
	if _, err := json.Marshal(tree); err != nil {
		t.Fatal(err)
	}
	if _, err := SyntaxTree(`let(x = 1)`); err == nil {
		t.Error("an unfinished let has a tree")
	}
}
