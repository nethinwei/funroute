package main

import (
	"strings"
	"testing"
)

// A type alias named twice is refused, as an argument named twice is: the
// second one would otherwise quietly replace the first.
func TestATypeAliasIsDeclaredOnce(t *testing.T) {
	t.Parallel()
	if _, err := textContract("T=record{b: int},T=record{b: string}", "a=T"); err == nil || !strings.Contains(err.Error(), `type "T" is declared twice`) {
		t.Fatalf("textContract with T declared twice error = %v, want it refused", err)
	}
	contract, err := textContract("T=record{b: int},U=int", "a=T,b=U")
	if err != nil || len(contract.Types) != 2 || len(contract.Args) != 2 {
		t.Fatalf("textContract(T, U) = %+v, %v, want two types and two arguments", contract, err)
	}
}
