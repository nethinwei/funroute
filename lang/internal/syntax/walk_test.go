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
		{`switch(s, case p, q => r, else t)`, "s,p,q,r,t"},
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
	expr, err := Parse(`switch(s, case p, q => r, else t)`)
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
	want := "int float string bool var enum array dict call record field switch for reduce let record_update"
	if got := strings.Join(NodeKinds(), " "); got != want {
		t.Fatalf("node kinds = %s, want %s", got, want)
	}
}
