package compile

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/nethinwei/funroute/internal/machine"
	"github.com/nethinwei/funroute/internal/syntax"
)

// A contract is checked where it is handed over: names, duplicates, concrete
// types and a limit that makes sense.
func TestAContractIsCheckedWhereItIsHandedOver(t *testing.T) {
	t.Parallel()
	for name, options := range map[string]CompileOptions{
		"a reserved name":         {Args: []ArgSpec{{Name: "case", Type: machine.IntType}}},
		"a name twice":            {Args: []ArgSpec{{Name: "a", Type: machine.IntType}, {Name: "a", Type: machine.IntType}}},
		"an open type":            {Args: []ArgSpec{{Name: "a", Type: machine.TypeVar("T")}}},
		"a negative limit":        {MaxInstructions: -1},
		"an open declared result": {Result: new(machine.TypeVar("T"))},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if err := ValidateContract(options); !errors.Is(err, machine.ErrContract) {
				t.Fatalf("ValidateContract(%s) = %v, want ErrContract", name, err)
			}
		})
	}
}

func TestDisabledFormsAreRejectedWhenTheyArriveAsExprJSON(t *testing.T) {
	t.Parallel()
	expr, err := syntax.Parse(`[reduce(p in row, t = 0, add(t,p)) for row in rows]`)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := syntax.ExportExprJSON(expr)
	if err != nil {
		t.Fatal(err)
	}
	imported, err := syntax.ImportExprJSON(encoded)
	if err != nil {
		t.Fatal(err)
	}
	switchAndFor := machine.CoreRegistry()
	if err := switchAndFor.EnableForm(machine.SwitchForm, machine.ForForm); err != nil {
		t.Fatal(err)
	}
	if _, err := CompileAST(imported, switchAndFor, CompileOptions{}); err == nil ||
		!strings.Contains(err.Error(), "reduce is not enabled") {
		t.Fatalf("CompileAST bypassed the registry forms: %v, want reduce is not enabled", err)
	}
	if _, err := CompileAST(imported, machine.CoreRegistry(), CompileOptions{}); err == nil ||
		!strings.Contains(err.Error(), "for is not enabled") {
		t.Fatalf("CompileAST accepted a disabled form: %v, want for is not enabled", err)
	}
}

// A selector answers, and fails, as the comprehension it stands for, with
// its list run once; the artifact keeps the selector as written.
func TestASelectorAnswersAsItsComprehension(t *testing.T) {
	t.Parallel()
	registry := machine.CoreRegistry()
	if err := registry.EnableForm(machine.ForForm); err != nil {
		t.Fatal(err)
	}
	channel, err := machine.ParseType("record{fee: int, name: string, meta: record{rank: int}}")
	if err != nil {
		t.Fatal(err)
	}
	options := CompileOptions{Args: []ArgSpec{{Name: "cs", Type: machine.ArrayOf(channel)}, {Name: "n", Type: machine.IntType}}}
	for selected, written := range map[string]string{
		`sort_by(cs, .fee)`:                          `sort_by(cs, [c.fee for c in cs])`,
		`sort_by_desc(cs, .meta.rank)`:               `sort_by_desc(cs, [c.meta.rank for c in cs])`,
		`top_k(take(cs, n), .fee, 2)`:                `let(t = take(cs, n), top_k(t, [c.fee for c in t], 2))`,
		`let(t = 1, bottom_k(take(cs, n), .fee, t))`: `let(t = 1, u = take(cs, n), bottom_k(u, [c.fee for c in u], t))`,
		`min_by(cs, .name).fee`:                      `min_by(cs, [c.name for c in cs]).fee`,
		`max_by(sort_by(cs, .name), .fee).name`:      `let(s = sort_by(cs, [c.name for c in cs]), max_by(s, [c.fee for c in s]).name)`,
	} {
		t.Run(selected, func(t *testing.T) {
			t.Parallel()
			got, want := run(t, selected, registry, options), run(t, written, registry, options)
			for _, args := range selectorInputs() {
				assertAnswersAlike(t, got, want, args)
			}
		})
	}
}

func assertAnswersAlike(t *testing.T, got, want *machine.Runtime, args map[string]any) {
	t.Helper()
	answer, err := got.Run(t.Context(), args)
	wanted, wantedErr := want.Run(t.Context(), args)
	if fmt.Sprint(err) != fmt.Sprint(wantedErr) || err == nil && !answer.Equal(wanted) {
		t.Errorf("with %v: %v, %v; written out %v, %v", args, answer.Any(), err, wanted.Any(), wantedErr)
	}
}

func selectorInputs() []map[string]any {
	channel := func(fee int, name string, rank int) map[string]any {
		return map[string]any{"fee": fee, "name": name, "meta": map[string]any{"rank": rank}}
	}
	channels := []any{channel(3, "b", 2), channel(1, "c", 3), channel(3, "a", 1)}
	return []map[string]any{{"cs": []any{}, "n": 0}, {"cs": channels, "n": 2}, {"cs": channels, "n": 3}}
}

func run(t *testing.T, source string, registry *machine.Registry, options CompileOptions) *machine.Runtime {
	t.Helper()
	artifact, err := CompileExpr(source, registry, options)
	if err != nil {
		t.Fatalf("%s: %v", source, err)
	}
	runtime, err := machine.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	return runtime
}

// A selector is where it reads a list, it counts as a comprehension, and the
// artifact keeps it as it was written.
func TestASelectorIsAComprehensionWrittenShort(t *testing.T) {
	t.Parallel()
	channels, err := machine.ParseType("array<record{fee: int}>")
	if err != nil {
		t.Fatal(err)
	}
	list := CompileOptions{Args: []ArgSpec{{Name: "cs", Type: channels}}}
	if _, err := CompileExpr(`sort_by(cs, .fee)`, machine.CoreRegistry(), list); err == nil || !strings.Contains(err.Error(), "for is not enabled") {
		t.Fatalf("a selector without comprehensions: %v, want for is not enabled", err)
	}
	registry := machine.CoreRegistry()
	if err := registry.EnableForm(machine.ForForm); err != nil {
		t.Fatal(err)
	}
	_, err = CompileExpr(`len(.fee)`, registry, list)
	if line, column, ok := syntax.LineColumn(err, `len(.fee)`); !ok || line != 1 || column != 5 || !strings.Contains(err.Error(), "the selector .fee must be an argument after a call's first") {
		t.Fatalf("a selector as the list: %v at %d:%d, want it placed at 1:5", err, line, column)
	}
	artifact, err := CompileExpr(`sort_by(cs, .fee)`, registry, list)
	if err != nil {
		t.Fatal(err)
	}
	if encoded, _ := json.Marshal(artifact); !strings.Contains(string(encoded), `{"node":"selector","path":"fee"}`) {
		t.Fatalf("the artifact is %s, want the selector as written", encoded)
	}
}
