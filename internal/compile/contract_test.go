package compile

import (
	"errors"
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
