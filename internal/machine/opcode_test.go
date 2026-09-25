package machine

import (
	"strings"
	"testing"
)

// An opcode exists in three places: the table, the verifier and the
// lowering. This test is what keeps them together: a new row with no rule in
// either fails here instead of at load time in a customer's routing decision.
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

		if !lowers(op) {
			t.Fatalf("%s has a table row but no lowering", spec.name)
		}
		if !verifierTypes(op) {
			t.Fatalf("%s has a table row but no rule in the verifier", spec.name)
		}
	}
}

// lowers reports whether the lowering has a rule for op. An unmatched opcode
// is the one thing the lowering reports without touching its state, so
// anything else — a panic from reaching into an empty lowerer — means the
// rule is there.
func lowers(op OpCode) (handled bool) {
	defer func() {
		if recover() != nil {
			handled = true
		}
	}()
	l := &lowerer{code: []Instruction{{Op: op}}}
	l.instruction(Instruction{Op: op})
	return l.err == nil
}

// verifierTypes reports whether the verifier has a rule for op, the way
// lowers does for the lowering.
func verifierTypes(op OpCode) (typed bool) {
	defer func() {
		if recover() != nil {
			typed = true
		}
	}()
	v := &verifier{artifact: &Artifact{parts: ArtifactParts{Instructions: []Instruction{{Op: op}}}}}
	_, err := v.step(0, &vstate{})
	return err == nil || !strings.Contains(err.Error(), "unknown opcode")
}
