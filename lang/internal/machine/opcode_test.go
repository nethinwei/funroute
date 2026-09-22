package machine

import (
	"strings"
	"testing"
)

// The opcode table and frame.step are the two places an opcode exists. This
// test is what makes that pair safe: a new row with no case, or a case with no
// row, fails here instead of at run time in a customer's routing decision.
func TestEveryOpcodeIsExecutableAndNamed(t *testing.T) {
	seen := map[string]bool{}
	for code := 1; code < len(opcodes); code++ {
		op := OpCode(code)
		spec := opcodes[code]
		if spec.name == "" {
			t.Fatalf("opcode %d has no row in the table", code)
		}
		if seen[spec.name] {
			t.Fatalf("two opcodes share the name %q", spec.name)
		}
		seen[spec.name] = true

		// The name must survive the JSON round trip, which is how an artifact
		// stores it.
		var decoded OpCode
		if err := decoded.UnmarshalText([]byte(spec.name)); err != nil || decoded != op {
			t.Fatalf("%s did not round trip: %v, got %v", spec.name, err, decoded)
		}

		if !stepHandles(op) {
			t.Fatalf("%s has a table row but no case in step", spec.name)
		}
	}
}

// Instructions that carry no operands must still be rejected when they are
// malformed, and the table is where that check now lives.
func TestInstructionValidationUsesTheTable(t *testing.T) {
	artifact := &Artifact{Instructions: make([]Instruction, 3)}
	for _, test := range []struct {
		name        string
		instruction Instruction
		wantError   bool
	}{
		{"unknown opcode", Instruction{Op: OpCode(200)}, true},
		{"invalid opcode", Instruction{Op: OpInvalid}, true},
		{"constant out of range", Instruction{Op: OpConstant, A: 7}, true},
		{"jump past the end", Instruction{Op: OpJump, A: 99}, true},
		{"fallback handler past the end", Instruction{Op: OpBeginFallback, A: 99}, true},
		{"jump to the end is the normal exit", Instruction{Op: OpJump, A: 3}, false},
		{"equal takes no operands", Instruction{Op: OpEqual}, false},
	} {
		err := validateInstruction(0, test.instruction, artifact)
		if (err != nil) != test.wantError {
			t.Fatalf("%s: err = %v, want error = %v", test.name, err, test.wantError)
		}
	}
}

// stepHandles reports whether step has a case for op. An unmatched opcode is
// the one thing step reports without touching the frame, so anything else —
// another error, or a panic from reaching into an empty frame — means the case
// is there.
func stepHandles(op OpCode) (handled bool) {
	defer func() {
		if recover() != nil {
			handled = true
		}
	}()
	f := &frame{}
	_, err := f.step(0, Instruction{Op: op})
	return err == nil || !strings.Contains(err.Error(), "unknown opcode")
}
