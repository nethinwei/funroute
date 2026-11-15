package machine

import (
	"testing"
	"unsafe"
)

// Every register operation has a name of its own: ropNames is keyed by the
// operation, so one added without a name would print as "" in every trace
// and disassembly, and two sharing a name would read as one.
func TestEveryRegisterOperationIsNamed(t *testing.T) {
	t.Parallel()
	seen := map[string]rop{}
	for op := range ropCount {
		name := ropNames[op]
		if other, taken := seen[name]; name == "" || taken {
			t.Errorf("register operation %d is named %q, which is empty or %d's too", op, name, other)
			continue
		}
		seen[name] = op
	}
}

// A register instruction is an operation and three operands, 16 bytes: the
// register form's code is a slice of them, read in order, and a wider one
// would fit fewer to a cache line.
func TestARegisterInstructionIsSixteenBytes(t *testing.T) {
	t.Parallel()
	if got := unsafe.Sizeof(rinstr{}); got != 16 {
		t.Fatalf("a register instruction is %d bytes, want 16", got)
	}
}
