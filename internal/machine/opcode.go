package machine

import (
	"fmt"
	"slices"
)

// What makes an opcode well-formed lives in one row: the name it takes in
// JSON and the operands it must have. How it moves the typed stack is
// verify.go's, and what it becomes in the register form lower_instr.go's.
// Adding an opcode means a row here, a case in each of the two;
// TestEveryOpcodeIsExecutableAndNamed checks they agree, so there is no
// fourth place to remember.
//
// The table is indexed by the opcode itself, which is what keeps the names
// aligned: a list in a different order used to be a silent mismatch.
type opcodeSpec struct {
	name string
	// validate rejects a malformed instruction. Nil means the opcode has no
	// operands worth checking.
	validate func(Instruction, *Artifact, failFunc) error
}

var opcodes = [...]opcodeSpec{
	OpInvalid: {name: "invalid"},
	OpConstant: {
		name: "const",
		validate: func(in Instruction, a *Artifact, fail failFunc) error {
			if in.A < 0 || in.A >= len(a.parts.Constants) {
				return fail("constant index %d", in.A)
			}
			return nil
		},
	},
	OpLoadArg: {
		name: "load_arg",
		validate: func(in Instruction, a *Artifact, fail failFunc) error {
			if in.A < 0 || in.A >= len(a.parts.Args) {
				return fail("argument index %d", in.A)
			}
			return nil
		},
	},
	OpLoadLocal:  {name: "load_local", validate: validateLocalSlot},
	OpStoreLocal: {name: "store_local", validate: validateLocalSlot},
	OpMakeArray: {
		name:     "make_array",
		validate: validateMakeInstruction,
	},
	OpMakeDict: {
		name:     "make_dict",
		validate: validateMakeInstruction,
	},
	// Pops the field values and pushes the record.
	OpMakeRecord: {
		name:     "make_record",
		validate: validateMakeInstruction,
	},
	// Pops a record and pushes the field at A. The compiler resolved the name
	// to that index, so nothing is looked up here.
	OpField: {
		name:     "field",
		validate: validateFieldInstruction,
	},
	// Pops two operands and pushes the comparison.
	OpEqual: {name: "equal"},
	OpCall: {
		name:     "call",
		validate: validateCallInstruction,
	},
	// A loop pops its source — and its seed when it folds — and pushes
	// nothing; the result arrives at OpLoopNext's last iteration.
	OpLoopInit: {
		name:     "loop_init",
		validate: validateLoopInstruction,
	},
	// A = 1 when the comprehension builds a dictionary, and the instruction
	// then takes the key as well as the value.
	OpLoopCollect: {
		name:     "loop_collect",
		validate: validateLoopInstruction,
	},
	OpLoopNext: {name: "loop_next", validate: validateLoopInstruction},
	// A nested comprehension yields one array per outer item; spread collects
	// their elements rather than the arrays, which is what makes
	// [f(x, y) for x in xs for y in ys] one flat array.
	OpLoopSpread:  {name: "loop_spread", validate: validateLoopInstruction},
	OpJumpIfFalse: {name: "jump_if_false", validate: validateJumpTarget},
	OpJump:        {name: "jump", validate: validateJumpTarget},
	OpBeginFallback: {
		name:     "begin_fallback",
		validate: validateJumpTarget,
	},
	OpEndFallback: {name: "end_fallback"},
	// Pops a record and one value per name in Keys, and pushes the record with
	// those fields replaced — a copy, since values are immutable. The names are
	// resolved to indexes when the artifact is lowered (rmake.fields).
	OpRecordWith: {
		name:     "record_with",
		validate: validateRecordWith,
	},
	// Opens a using: pops B quotes, each an exchange rate or an array of
	// them, and runs what follows with them alone. OpFxPop closes it.
	OpFxPush: {
		name:     "fx_push",
		validate: func(in Instruction, _ *Artifact, fail failFunc) error { return validateFxPush(in, fail) },
	},
	OpFxPop: {name: "fx_pop"},
	// Pops the answer of the innermost loop, a fold's, and ends the loop
	// with it: a fold that stops — any at the first true — goes to A, past
	// its loop_next, as the loop's last iteration would.
	OpLoopBreak: {name: "loop_break", validate: validateLoopInstruction},
	// Pops an item of the innermost loop, a fold's, and folds it into the
	// answer with the kernel function Calls[A]: answer = f(answer, item). It
	// costs what loop_collect does, as collecting the item for a function
	// of the whole array did.
	OpLoopFold: {name: "loop_fold", validate: validateLoopFold},
}

func validateLoopFold(in Instruction, a *Artifact, fail failFunc) error {
	if in.A < 0 || in.A >= len(a.parts.Calls) || in.Type == nil {
		return fail("malformed loop fold")
	}
	return nil
}

func (o OpCode) spec() opcodeSpec {
	if int(o) < len(opcodes) && opcodes[o].name != "" {
		return opcodes[o]
	}
	return opcodes[OpInvalid]
}

func (o OpCode) String() string { return o.spec().name }

func (o OpCode) MarshalText() ([]byte, error) { return []byte(o.String()), nil }

func (o *OpCode) UnmarshalText(text []byte) error {
	i := slices.IndexFunc(opcodes[:], func(spec opcodeSpec) bool { return spec.name == string(text) })
	if i < 0 {
		return fmt.Errorf("unknown opcode %q", text)
	}
	*o = OpCode(i)
	return nil
}

func validateLocalSlot(in Instruction, a *Artifact, fail failFunc) error {
	if in.A < 0 || in.A >= a.parts.Locals {
		return fail("local index %d", in.A)
	}
	return nil
}

func validateJumpTarget(in Instruction, a *Artifact, fail failFunc) error {
	if in.A < 0 || in.A > len(a.parts.Instructions) {
		return fail("jump target %d", in.A)
	}
	return nil
}
