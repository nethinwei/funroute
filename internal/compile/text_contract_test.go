package compile

import (
	"errors"
	"testing"

	"github.com/nethinwei/funroute/internal/machine"
)

// A contract written as text is checked the moment it becomes options, not
// only when something compiles against it.
func TestATextContractIsCheckedAsItBecomesOptions(t *testing.T) {
	t.Parallel()
	for name, contract := range map[string]TextContract{
		"a name twice":    {Args: []TextArg{{Name: "a", Type: "int"}, {Name: "a", Type: "int"}}},
		"a reserved name": {Args: []TextArg{{Name: "for", Type: "int"}}},
		"a bad type":      {Args: []TextArg{{Name: "a", Type: "money<usd dollars>"}}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := contract.Options(); !errors.Is(err, machine.ErrContract) {
				t.Fatalf("Options(%s) = %v, want ErrContract", name, err)
			}
		})
	}
	options, err := (&TextContract{Args: []TextArg{{Name: "amount", Type: "money"}}}).Options()
	if err != nil || len(options.Args) != 1 {
		t.Fatalf("Options(amount: money) = %+v, %v", options, err)
	}
}
