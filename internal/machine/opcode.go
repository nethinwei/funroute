package machine

import "fmt"

// Everything the machine knows about an opcode lives in one row: the name it
// takes in JSON, how it moves the operand stack, and what makes it
// well-formed. Adding an opcode means adding a row here and a case in
// frame.step; TestEveryOpcodeIsExecutable checks the two agree, so there is no
// third place to remember.
//
// The table is indexed by the opcode itself, which is what keeps the names
// aligned: a list in a different order used to be a silent mismatch.
type opcodeSpec struct {
	name string
	// effect is how much the instruction grows the stack. Nil means zero.
	effect func(Instruction) int
	// validate rejects a malformed instruction. Nil means the opcode has no
	// operands worth checking.
	validate func(Instruction, *Artifact, failFunc) error
}

var opcodes = [...]opcodeSpec{
	OpInvalid: {name: "invalid"},
	OpConstant: {
		name:   "const",
		effect: pushes(1),
		validate: func(in Instruction, a *Artifact, fail failFunc) error {
			if in.A < 0 || in.A >= len(a.parts.Constants) {
				return fail("constant index %d", in.A)
			}
			return nil
		},
	},
	OpLoadArg: {
		name:   "load_arg",
		effect: pushes(1),
		validate: func(in Instruction, a *Artifact, fail failFunc) error {
			if in.A < 0 || in.A >= len(a.parts.Args) {
				return fail("argument index %d", in.A)
			}
			return nil
		},
	},
	OpLoadLocal:  {name: "load_local", effect: pushes(1), validate: validateLocalSlot},
	OpStoreLocal: {name: "store_local", effect: pushes(-1), validate: validateLocalSlot},
	OpMakeArray: {
		name:     "make_array",
		effect:   func(in Instruction) int { return 1 - in.A },
		validate: validateMakeInstruction,
	},
	OpMakeDict: {
		name:     "make_dict",
		effect:   func(in Instruction) int { return 1 - in.A },
		validate: validateMakeInstruction,
	},
	// Pops the field values and pushes the record.
	OpMakeRecord: {
		name:     "make_record",
		effect:   func(in Instruction) int { return 1 - in.A },
		validate: validateMakeInstruction,
	},
	// Pops a record and pushes the field at A. The compiler resolved the name
	// to that index, so nothing is looked up here.
	OpField: {
		name:     "field",
		effect:   pushes(0),
		validate: validateFieldInstruction,
	},
	// Pops two operands and pushes the comparison.
	OpEqual: {name: "equal", effect: pushes(-1)},
	OpCall: {
		name:     "call",
		effect:   func(in Instruction) int { return 1 - in.B },
		validate: validateCallInstruction,
	},
	// A loop pops its source — and its seed when it folds — and pushes
	// nothing; the result arrives at OpLoopNext's last iteration.
	OpLoopInit: {
		name: "loop_init",
		effect: func(in Instruction) int {
			if in.C != NoAccumulator {
				return -2
			}
			return -1
		},
		validate: validateLoopInstruction,
	},
	// A = 1 when the comprehension builds a dictionary, and the instruction
	// then takes the key as well as the value.
	OpLoopCollect: {
		name: "loop_collect",
		effect: func(in Instruction) int {
			if in.A == 1 {
				return -2
			}
			return -1
		},
		validate: validateLoopInstruction,
	},
	OpLoopNext: {name: "loop_next", effect: pushes(1), validate: validateLoopInstruction},
	// A nested comprehension yields one array per outer item; spread collects
	// their elements rather than the arrays, which is what makes
	// [f(x, y) for x in xs for y in ys] one flat array.
	OpLoopSpread:  {name: "loop_spread", effect: pushes(-1), validate: validateLoopInstruction},
	OpJumpIfFalse: {name: "jump_if_false", effect: pushes(-1), validate: validateJumpTarget},
	OpJump:        {name: "jump", validate: validateJumpTarget},
	OpBeginFallback: {
		name:     "begin_fallback",
		validate: validateJumpTarget,
	},
	OpEndFallback: {name: "end_fallback"},
	// Pops a record and one value per name in Keys, and pushes the record with
	// those fields replaced — a copy, since values are immutable. The names are
	// resolved to indexes when the artifact is loaded (Runtime.updates).
	OpRecordWith: {
		name:     "record_with",
		effect:   func(in Instruction) int { return -len(in.Keys) },
		validate: validateRecordWith,
	},
	// Opens a using: pops B quotes, each an exchange rate or an array of
	// them, and runs what follows with them alone. OpFxPop closes it.
	OpFxPush: {
		name:     "fx_push",
		effect:   func(in Instruction) int { return -in.B },
		validate: func(in Instruction, _ *Artifact, fail failFunc) error { return validateFxPush(in, fail) },
	},
	OpFxPop: {name: "fx_pop"},
}

func pushes(n int) func(Instruction) int {
	return func(Instruction) int { return n }
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
	for i := range opcodes {
		if opcodes[i].name == string(text) {
			*o = OpCode(i)
			return nil
		}
	}
	return fmt.Errorf("unknown opcode %q", text)
}

// StackEffect is how much an instruction grows the operand stack. The compiler
// sums it to size the frame's reservation; a wrong figure costs the
// optimisation but never correctness, because push still checks the limit.
func StackEffect(instruction Instruction) int {
	effect := instruction.Op.spec().effect
	if effect == nil {
		return 0
	}
	return effect(instruction)
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
