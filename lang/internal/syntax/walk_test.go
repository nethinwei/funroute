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
		{`reduce(x in xs, acc from seed, acc + x + w)`, "xs,seed,w"},
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
		{`{"version":1,"expr":{"node":"loop"}}`, `unknown expression node "loop"`},
	} {
		_, err := ImportExprJSON([]byte(test.document))
		if err == nil || !strings.Contains(err.Error(), test.want) {
			t.Errorf("%s\nerror = %v, want %q", test.document, err, test.want)
		}
	}
}

// Every node the walker knows is described to the front end, with the tags it
// was declared with.
func TestNodeSchemasMirrorTheDefinitions(t *testing.T) {
	schemas := map[string]bool{}
	var forNode, letNode int
	for i, schema := range NodeSchemas() {
		schemas[schema.Node] = true
		switch schema.Node {
		case "for":
			forNode = i
		case "let":
			letNode = i
		}
	}
	for _, want := range []string{"int", "float", "string", "bool", "var", "array", "dict", "call", "switch", "for", "reduce", "let"} {
		if !schemas[want] {
			t.Errorf("schema for %s is missing", want)
		}
	}
	variable := NodeSchemas()[forNode].Fields[1]
	if variable.Name != "variable" || variable.Kind != "name" || variable.Role != "local" || variable.Default != "item" {
		t.Fatalf("for.variable = %+v", variable)
	}
	if NodeSchemas()[forNode].Form != "for" {
		t.Fatalf("for.form = %q", NodeSchemas()[forNode].Form)
	}
	bindings := NodeSchemas()[letNode].Fields[0]
	if bindings.Kind != "list" || bindings.Min != 1 || len(bindings.Fields) != 2 || bindings.Fields[1].Kind != "expr" {
		t.Fatalf("let.bindings = %+v", bindings)
	}
}
