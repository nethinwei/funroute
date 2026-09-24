package compile

import (
	"strings"
	"testing"

	"github.com/nethinwei/funroute/internal/machine"
)

func TestAnalyzeKnowsEachNode(t *testing.T) {
	t.Parallel()
	source := `let(rate = fee * 2, [rate + x for x in xs if x > 0.5])`
	options := CompileOptions{Args: []ArgSpec{{Name: "fee", Type: machine.IntType}, {Name: "xs", Type: machine.ArrayOf(machine.FloatType)}}}
	registry := machine.CoreRegistry()
	if err := registry.EnableForm(machine.ForForm); err != nil {
		t.Fatal(err)
	}
	analysis, err := Analyze(source, registry, options)
	if err != nil {
		t.Fatal(err)
	}
	fact := func(text string) NodeFact {
		t.Helper()
		for _, node := range analysis.Nodes {
			if source[node.Span.Start:node.Span.End] == text {
				return node
			}
		}
		t.Fatalf("no node spans %q", text)
		return NodeFact{}
	}
	if inner, _ := analysis.At(strings.Index(source, "rate + x")); source[inner.Span.Start:inner.Span.End] != "rate" {
		t.Errorf("the innermost node at rate + x is %+v", inner)
	}
	if typ := fact("rate + x").Type; typ == nil || typ.String() != "float" {
		t.Errorf("rate + x is %v", typ)
	}
	if got := fact("rate + x").Signature; got != "add(int,float)->float" {
		t.Errorf("rate + x resolved to %q", got)
	}
	if got := fact("fee").Reference; got != "argument" {
		t.Errorf("fee is %q", got)
	}
	if analysis.Result.String() != "array<float>" {
		t.Errorf("result is %s", analysis.Result)
	}
}

// A program that type-checks but fails later still says what was inferred.
func TestAnalyzeKeepsWhatWasLearnt(t *testing.T) {
	t.Parallel()
	analysis, err := Analyze(`n + 1 / 0`, machine.CoreRegistry(), CompileOptions{Args: []ArgSpec{{Name: "n", Type: machine.IntType}}})
	if err == nil || analysis == nil || len(analysis.Nodes) == 0 {
		t.Fatalf("analysis %v, error %v", analysis, err)
	}
	if _, err := Analyze(`n +`, machine.CoreRegistry(), CompileOptions{}); err == nil {
		t.Error("a syntax error was not reported")
	}
}
