package syntax

import (
	"strings"
	"testing"
)

// The scope rules live in the binds tags: for and reduce bind into their
// bodies, let binds each name into the later bindings and the body.
func TestFreeVariablesFollowTheBindsTags(t *testing.T) {
	for _, test := range []struct{ source, want string }{
		{`[x + y for x in xs if x > z]`, "xs,z,y"},
		{`[v + k for k, v in d]`, "d"},
		{`reduce(x in xs, acc = seed, acc + x + w)`, "xs,seed,w"},
		{`let(a = b, c = a + d, c + e)`, "b,d,e"},
		{`let(a = c, c = a, c)`, "c"},
		{`{"z": z, "a": a}`, "a,z"},
		{`switch(s, case p, q => r, else t)`, "s,p,q,r,t"},
	} {
		expr, err := Parse(test.source)
		if err != nil {
			t.Fatalf("%s: %v", test.source, err)
		}
		if got := strings.Join(FreeVariables(expr), ","); got != test.want {
			t.Errorf("%s: free variables = %s, want %s", test.source, got, test.want)
		}
	}
}

// Children is the one traversal the compiler's structural passes use.
func TestChildrenAreListedInDefinitionOrder(t *testing.T) {
	expr, err := Parse(`switch(s, case p, q => r, else t)`)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, child := range Children(expr) {
		names = append(names, child.(*VariableExpr).Name)
	}
	if got := strings.Join(names, ","); got != "s,p,q,r,t" {
		t.Fatalf("children = %s", got)
	}
	if form, ok := FormOf(expr); !ok || string(form) != "switch" {
		t.Fatalf("form = %q, %v", form, ok)
	}
}

// Import applies the same rules as the parser, from the same definitions.
func TestImportEnforcesTheNodeDefinitions(t *testing.T) {
	for _, test := range []struct{ document, want string }{
		{`{"version":1,"expr":{"node":"for","source":{"node":"var","name":"xs"},"yield":{"node":"var","name":"x"}}}`, "for node is missing variable"},
		{`{"version":1,"expr":{"node":"let","bindings":[],"body":{"node":"int","int":1}}}`, "needs at least 1 item"},
		{`{"version":1,"expr":{"node":"var","name":"x","extra":1}}`, `unknown field "extra"`},
		{`{"version":1,"expr":{"node":"switch","cases":[{"match":[{"node":"bool","bool":true}],"result":{"node":"int","int":1}}],"default":{"node":"int","int":2},"value":null}}`, "null node"},
		{`{"version":1,"expr":{"node":"dict","entries":[{"key":"a","value":{"node":"int","int":1}},{"key":"a","value":{"node":"int","int":2}}]}}`, `duplicate dictionary key "a"`},
		{`{"version":1,"expr":{"node":"reduce","source":{"node":"var","name":"xs"},"variable":"x","accumulator":"x","init":{"node":"int","int":0},"body":{"node":"var","name":"x"}}}`, `"x" is bound twice`},
		{`{"version":1,"expr":{"node":"for","source":{"node":"var","name":"xs"},"variable":"in","yield":{"node":"var","name":"x"}}}`, `invalid local variable name "in"`},
		{`{"version":1,"expr":{"node":"float","float":"nan"}}`, "non-finite floats"},
		// A reserved word would print as syntax and read back as something else.
		{`{"version":1,"expr":{"node":"var","name":"case"}}`, `invalid variable name "case"`},
		{`{"version":1,"expr":{"node":"call","name":"let","args":[]}}`, `invalid function name "let"`},
		{`{"version":1,"expr":{"node":"loop"}}`, `unknown expression node "loop"`},
	} {
		_, err := ImportExprJSON([]byte(test.document))
		if err == nil || !strings.Contains(err.Error(), test.want) {
			t.Errorf("%s\nerror = %v, want %q", test.document, err, test.want)
		}
	}
}

// Every node the walker knows is listed, the literal kinds included.
func TestNodeKindsListEveryNode(t *testing.T) {
	want := "int float string bool var enum array dict call record field switch for reduce let record_update"
	if got := strings.Join(NodeKinds(), " "); got != want {
		t.Fatalf("node kinds = %s", got)
	}
}
