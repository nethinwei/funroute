package syntax

import (
	"strings"
	"testing"
)

// The scope rules live in the binds tags: for and reduce bind into their
// bodies, let binds each name into the later bindings and the body.
func TestFreeVariablesFollowTheBindsTags(t *testing.T) {
	t.Parallel()
	for _, test := range []struct{ source, want string }{
		{`[x + y for x in xs if x > z]`, "xs,z,y"},
		{`[v + k for k, v in d]`, "d"},
		{`reduce(x in xs, acc = seed, acc + x + w)`, "xs,seed,w"},
		{`let(a = b, c = a + d, c + e)`, "b,d,e"},
		{`let(a = c, c = a, c)`, "c"},
		{`{"z": z, "a": a}`, "a,z"},
		{`switch(s, case p, q => r, else => t)`, "s,p,q,r,t"},
	} {
		t.Run(test.source, func(t *testing.T) {
			t.Parallel()
			if got := strings.Join(FreeVariables(mustParse(t, test.source)), ","); got != test.want {
				t.Errorf("%s: free variables = %s, want %s", test.source, got, test.want)
			}
		})
	}
}

// Children is the one traversal the compiler's structural passes use.
func TestChildrenAreListedInDefinitionOrder(t *testing.T) {
	t.Parallel()
	expr, err := Parse(`switch(s, case p, q => r, else => t)`)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, child := range Children(expr) {
		names = append(names, child.(*VariableExpr).Name)
	}
	if got := strings.Join(names, ","); got != "s,p,q,r,t" {
		t.Fatalf("children = %s, want s,p,q,r,t", got)
	}
	if form, ok := FormOf(expr); !ok || string(form) != "switch" {
		t.Fatalf("FormOf(switch) = %q, %v, want \"switch\", true", form, ok)
	}
}

// Every node the walker knows is listed, the literal kinds included.
func TestNodeKindsListEveryNode(t *testing.T) {
	t.Parallel()
	want := "int float string bool var enum array dict call record field switch for reduce let record_update money rate using fxrate currency"
	if got := strings.Join(NodeKinds(), " "); got != want {
		t.Fatalf("node kinds = %s, want %s", got, want)
	}
}

// An amount and a rate are leaves whose fields are text: they name nothing,
// so they add no free variable, and a code that is not before a number is
// a variable like any other.
func TestMoneyAndRatesAreLeaves(t *testing.T) {
	t.Parallel()
	for _, test := range []struct{ source, want string }{
		{`amount * 2.9% + USD 0.30`, "amount"},
		{`USD -1 + 25bps`, ""},
		{`USD + USD 1`, ""},
		{`amount -> JPY`, "amount"},
		{`let(fee = JPY 5, [fee + x for x in xs if x > 0.5bps])`, "xs"},
	} {
		t.Run(test.source, func(t *testing.T) {
			t.Parallel()
			if got := strings.Join(FreeVariables(mustParse(t, test.source)), ","); got != test.want {
				t.Errorf("%s: free variables = %q, want %q", test.source, got, test.want)
			}
		})
	}
	for _, source := range []string{`USD 1.70`, `JPY -5`, `2.9%`, `25bps`} {
		if children := Children(mustParse(t, source)); len(children) != 0 {
			t.Errorf("Children(%q) = %v, want none", source, children)
		}
		if form, ok := FormOf(mustParse(t, source)); ok {
			t.Errorf("FormOf(%q) = %q, true, want no form", source, form)
		}
	}
}

// The syntax tree comes from the same plan: a money node shows its currency
// and its amount as text, a rate its value and unit, each over the source it
// was read from.
func TestSyntaxTreeShowsMoneyAndRates(t *testing.T) {
	t.Parallel()
	source := `f(USD -1.70, amount * 2.9%, 0.5bps)`
	tree, err := SyntaxTree(source)
	if err != nil {
		t.Fatal(err)
	}
	args := tree.Fields[1].Nodes
	if len(args) != 3 {
		t.Fatalf("SyntaxTree(%q) has arguments %+v, want three", source, args)
	}
	checkLeaf(t, source, args[0], "money", "USD -1.70", "currency=USD", "amount=-1.70")
	checkLeaf(t, source, args[1].Fields[1].Nodes[1], "rate", "2.9%", "value=2.9", "unit=%")
	checkLeaf(t, source, args[2], "rate", "0.5bps", "value=0.5", "unit=bps")
	if args[1].Operator != "*" {
		t.Errorf("the second argument is %+v, want the * over the rate", args[1])
	}
}

// checkLeaf fails unless node is kind over text with exactly the named text
// fields, in order.
func checkLeaf(t *testing.T, source string, node Tree, kind, text string, fields ...string) {
	t.Helper()
	if node.Node != kind || source[node.Span.Start:node.Span.End] != text {
		t.Errorf("%q: node %s over %q, want %s over %q", source, node.Node, source[node.Span.Start:node.Span.End], kind, text)
	}
	var got []string
	for _, field := range node.Fields {
		got = append(got, field.Name+"="+field.Text)
		if len(field.Nodes) != 0 || len(field.Items) != 0 {
			t.Errorf("%q: %s field %s has children %+v, want text only", source, kind, field.Name, field)
		}
	}
	if strings.Join(got, " ") != strings.Join(fields, " ") {
		t.Errorf("%q: %s fields = %v, want %v", source, kind, got, fields)
	}
}
