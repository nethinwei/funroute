package machine

import (
	"strings"
	"testing"
)

// The opcode table and frame.step are the two places an opcode exists. This
// test is what makes that pair safe: a new row with no case, or a case with no
// row, fails here instead of at run time in a customer's routing decision.
func TestEveryOpcodeIsExecutableAndNamed(t *testing.T) {
	t.Parallel()
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
			t.Fatalf("UnmarshalText(%q) = %v, %v, want %v, nil", spec.name, decoded, err, op)
		}

		if !stepHandles(op) {
			t.Fatalf("%s has a table row but no case in step", spec.name)
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
