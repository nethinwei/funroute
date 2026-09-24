package compile

import (
	"errors"
	"testing"

	"funroute/lang/internal/machine"
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
