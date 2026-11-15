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

// The value flow has a rule for every opcode (flowRules): an opcode added
// without one would leave the stack it keeps out of step with the program's.
func TestEveryOpcodeHasAFlowRule(t *testing.T) {
	t.Parallel()
	for code := 1; code < len(opcodes); code++ {
		if !flowRules[OpCode(code)] {
			t.Errorf("opcode %s has no rule in the value flow (lower_escape.go)", opcodes[code].name)
		}
	}
}

// jumpAnalyses is what the analyses that ask their own question of a jump
// make of each one: whether it ends a block (leadersOf; a fallback's begin
// is counted there instead) and whether it opens a range up to its target
// (guardedInstructions; a loop's next goes back, and a break leaves).
var jumpAnalyses = map[OpCode]struct{ endsBlock, opensRange bool }{
	OpJump: {true, true}, OpJumpIfFalse: {true, true}, OpLoopInit: {true, true},
	OpLoopNext: {true, false}, OpLoopBreak: {true, false}, OpBeginFallback: {false, true},
}

// Every opcode whose A is an instruction is placed in each analysis that
// asks its own question of a jump, and what is written of it is what the
// analysis does; an opcode that does not jump is no jump to any of them. A
// new jump fails here until it is read into each.
func TestEveryJumpIsPlacedInEachAnalysis(t *testing.T) {
	t.Parallel()
	for op := OpCode(1); int(op) < len(opcodes); op++ {
		placed, listed := jumpAnalyses[op]
		if listed != opcodes[op].jumps {
			t.Errorf("%s: jumps %v, placed in jumpAnalyses %v", opcodes[op].name, opcodes[op].jumps, listed)
			continue
		}
		if op == OpCall || op == OpEndFallback {
			continue // leadersOf reads these for the fallback they are in
		}
		code := []Instruction{{Op: op, A: 2}, {Op: OpConstant}, {Op: OpConstant}}
		if ends := leadersOf(code, nil)[1]; ends != placed.endsBlock {
			t.Errorf("%s: leadersOf ends a block after it %v, want %v", opcodes[op].name, ends, placed.endsBlock)
		}
		if opens := guardedInstructions(code)[1]; opens != placed.opensRange {
			t.Errorf("%s: guardedInstructions opens a range %v, want %v", opcodes[op].name, opens, placed.opensRange)
		}
		if joins := joinsOf(code)[2]; joins != listed {
			t.Errorf("%s: joinsOf marks its A as a join %v, want %v", opcodes[op].name, joins, listed)
		}
	}
}
