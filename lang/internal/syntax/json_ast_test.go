package syntax

import (
	"strings"
	"testing"
)

// Import applies the same rules as the parser, from the same definitions.
func TestImportEnforcesTheNodeDefinitions(t *testing.T) {
	t.Parallel()
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
		// The document's own keys are as exact as a node's.
		{`{"VERSION":1,"expr":{"node":"var","name":"x"}}`, `unknown field "VERSION"`},
		{`{"version":1,"eXpr":{"node":"var","name":"x"}}`, `unknown field "eXpr"`},
	} {
		t.Run(test.want, func(t *testing.T) {
			t.Parallel()
			_, err := ImportExprJSON([]byte(test.document))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Errorf("%s\nerror = %v, want %q", test.document, err, test.want)
			}
		})
	}
}

// FuzzImportExprJSON holds the importer to its rules on any bytes: it never
// panics, and what it accepts exports to a document that imports back to the
// same program — and prints as source that parses back to it too.
func FuzzImportExprJSON(f *testing.F) {
	for _, source := range fuzzSources(f) {
		if expr, err := Parse(source); err == nil {
			f.Add([]byte(mustExport(f, expr)))
		}
	}
	for _, document := range []string{
		``, `{}`, `null`, `{"version":1}`, `{"version":2,"expr":{"node":"int","int":1}}`,
		`{"version":1,"expr":{"node":"float","float":"nan"}}`,
		`{"version":1,"expr":{"node":"var","name":"case"}}`,
		`{"version":1,"expr":{"node":"record_update","base":{"node":"var","name":"r"},"fields":[]}}`,
	} {
		f.Add([]byte(document))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		expr, err := ImportExprJSON(data)
		if err != nil {
			return
		}
		want := exportFuzzed(t, string(data), expr)
		again, err := ImportExprJSON([]byte(want))
		if err != nil {
			t.Fatalf("ImportExprJSON(ExportExprJSON(ImportExprJSON(%q))) error = %v, want nil", data, err)
		}
		if got := exportFuzzed(t, want, again); got != want {
			t.Fatalf("ImportExprJSON(%q) exports to %s after another round trip, want %s", data, got, want)
		}
		checkReparses(t, "Inline", string(data), Inline(expr), want)
	})
}

func mustExport(t testing.TB, expr Expr) string {
	t.Helper()
	encoded, err := ExportExprJSON(expr)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

func TestSwitchSurvivesExprJSONRoundTrip(t *testing.T) {
	t.Parallel()
	for _, source := range []string{
		`switch(country, case "MY", "TH" => "asia", case "SG" => "sg", else "global")`,
		`switch(case amount > 100 => "big", case risk > 0.5, country == "SG" => "check", else "ok")`,
	} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			assertExprJSONIsCanonical(t, source)
		})
	}
}

func TestDictionaryWalkSurvivesExprJSONRoundTrip(t *testing.T) {
	t.Parallel()
	for _, source := range []string{
		`[add(k, string(v)) for k, v in weights if v > 1.0]`,
		`reduce(k, v in weights, total = 0.0, total + v)`,
	} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			if first := assertExprJSONIsCanonical(t, source); !strings.Contains(string(first), `"key_variable":"k"`) {
				t.Fatalf("the key variable is missing from %s", first)
			}
		})
	}
}

func TestLetSurvivesExprJSONRoundTrip(t *testing.T) {
	t.Parallel()
	source := `let(base = amount * 2, fee = base / 10, base + fee)`
	expr, err := Parse(source)
	if err != nil {
		t.Fatal(err)
	}
	first, err := ExportExprJSON(expr)
	if err != nil {
		t.Fatal(err)
	}
	imported, err := ImportExprJSON(first)
	if err != nil {
		t.Fatal(err)
	}
	second, err := ExportExprJSON(imported)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatalf("%s is not canonical:\n%s\n%s", source, first, second)
	}
}

// assertExprJSONIsCanonical parses source, and checks that exporting,
// importing and exporting again gives the same ExprJSON. It returns it.
func assertExprJSONIsCanonical(t *testing.T, source string) []byte {
	t.Helper()
	expr, err := Parse(source)
	if err != nil {
		t.Fatalf("parse %s: %v", source, err)
	}
	first, err := ExportExprJSON(expr)
	if err != nil {
		t.Fatal(err)
	}
	imported, err := ImportExprJSON(first)
	if err != nil {
		t.Fatalf("import %s: %v", source, err)
	}
	second, err := ExportExprJSON(imported)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatalf("%s is not canonical:\n%s\n%s", source, first, second)
	}
	return first
}
