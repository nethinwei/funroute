package compile

import (
	"errors"
	"strings"
	"testing"

	"funroute/lang/internal/machine"
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
	options, err := (&TextContract{Args: []TextArg{{Name: "amount", Type: "money<c>"}}}).Options()
	if err != nil || len(options.Args) != 1 {
		t.Fatalf("Options(amount: money<c>) = %+v, %v", options, err)
	}
}

// A text contract's tables are the rate tables it declares.
func TestATextContractDeclaresRateTables(t *testing.T) {
	t.Parallel()
	contract := &TextContract{Tables: []string{"settlement"}}
	options, err := contract.Options()
	if err != nil || strings.Join(options.RateTables, ",") != "settlement" {
		t.Fatalf("Options() = %+v, %v, want the table settlement", options, err)
	}
	if _, err := (&TextContract{Tables: []string{"x", "x"}}).Options(); err == nil {
		t.Fatal("a table declared twice was accepted")
	}
}
