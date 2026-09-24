package syntax

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/nethinwei/funroute/internal/machine"
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
	const source = `switch(s, case p, q => r, else => t)`
	expr, err := Parse(source)
	if err != nil {
		t.Fatal(err)
	}
	children := Children(expr)
	names := make([]string, 0, len(children))
	for _, child := range children {
		variable, ok := child.(*VariableExpr)
		if !ok {
			t.Fatalf("Children(Parse(%q)) has a %T, want only variables", source, child)
		}
		names = append(names, variable.Name)
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
	want := "int float string bool var enum array dict call record field switch for reduce let record_update money ratio using fxrate currency"
	if got := strings.Join(NodeKinds(), " "); got != want {
		t.Fatalf("node kinds = %s, want %s", got, want)
	}
}

// A form is named by the kind tag, so its machine.Form constant must be
// spelled as the node's kind: the three nodes a registry can turn off are
// the three forms it knows, and no other node is one.
func TestFormsAreNamedAsTheirNodeKinds(t *testing.T) {
	t.Parallel()
	want := map[string]machine.Form{"switch": machine.SwitchForm, "for": machine.ForForm, "reduce": machine.ReduceForm}
	for _, node := range nodeTypes {
		form, ok := FormOf(node)
		kind := kindOf(node, planOf(node))
		if _, literal := node.(*LiteralExpr); literal {
			kind = "literal"
		}
		if wanted, isForm := want[kind]; ok != isForm || form != wanted {
			t.Errorf("FormOf(%s) = %q, %v, want %q, %v", kind, form, ok, wanted, isForm)
		}
	}
}

// An amount and a ratio are leaves whose fields are text: they name nothing,
// so they add no free variable, and a code that is not before a number is
// a variable like any other.
func TestMoneyAndRatiosAreLeaves(t *testing.T) {
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
// and its amount as text, a ratio its value and unit, each over the source it
// was read from.
func TestSyntaxTreeShowsMoneyAndRatios(t *testing.T) {
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
	checkLeaf(t, source, args[1].Fields[1].Nodes[1], "ratio", "2.9%", "value=2.9", "unit=%")
	checkLeaf(t, source, args[2], "ratio", "0.5bps", "value=0.5", "unit=bps")
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
	got := make([]string, 0, len(node.Fields))
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

// The forms a node's tag marks optional are the forms a registry can turn
// on, in the machine's order: one list, read from both sides.
func TestTheOptionalFormsAreTheMachines(t *testing.T) {
	t.Parallel()
	var tagged []machine.Form
	for _, node := range nodeTypes {
		if plan := plans[reflect.TypeOf(node).Elem()]; plan != nil && plan.form != "" {
			tagged = append(tagged, plan.form)
		}
	}
	if want := machine.OptionalForms(); !slices.Equal(tagged, want) {
		t.Fatalf("the nodes tag %v optional, the machine turns on %v", tagged, want)
	}
}

// FreeReads is every read no form inside binds, not only the first of each
// name.
func TestFreeReadsAreEveryUnboundRead(t *testing.T) {
	t.Parallel()
	expr, err := Parse("let(y = x, x + y + [z for z in x])")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	reads := FreeReads(expr)
	eachVariable(expr, func(variable *VariableExpr, _ bool) {
		if reads[variable.ID] {
			got = append(got, variable.Name)
		}
	})
	if want := []string{"x", "x", "x"}; !slices.Equal(got, want) {
		t.Fatalf("FreeReads reads %v, want %v", got, want)
	}
}
