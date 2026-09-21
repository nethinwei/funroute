package machine

// A prefetch site is a call a Batch may run for many requests at once, before
// any of their programs start. Hoisting it must change nothing observable, so
// the bytecode has to prove three things about the call:
//
//   - its arguments are program arguments or constants, so they are known
//     before the program runs;
//   - it is not inside a loop, so it runs at most once;
//   - no conditional jump can skip it, so it runs at least once — the lazy
//     branches of if and switch stay lazy.
//
// Everything else stays a per-request call. The analysis reads the bytecode
// rather than the AST, so it needs no second look at the program's meaning:
// what the compiler emitted is what runs.

// PrefetchSite is one hoistable call.
type PrefetchSite struct {
	PC       int       // the call instruction
	Call     int       // index into Artifact.Calls
	Operands []Operand // one per argument, in order
}

// Operand is where a hoisted call's argument comes from: a program argument,
// or a constant when Arg is NoArgument.
type Operand struct {
	Arg      int
	Constant int
}

// NoArgument marks an Operand that is a constant.
const NoArgument = -1

// PrefetchSites lists the hoistable calls of an artifact in program order.
func PrefetchSites(artifact *Artifact) []PrefetchSite {
	guarded := guardedInstructions(artifact.Instructions)
	var sites []PrefetchSite
	for pc, instruction := range artifact.Instructions {
		if instruction.Op != OpCall || guarded[pc] {
			continue
		}
		operands, ok := callOperands(artifact.Instructions, pc, instruction.B)
		if !ok {
			continue
		}
		sites = append(sites, PrefetchSite{PC: pc, Call: instruction.A, Operands: operands})
	}
	return sites
}

// guardedInstructions marks every instruction that a forward jump can skip or
// a loop can repeat: the body of an if branch, a switch case, a for or a
// reduce. A backward jump is a loop's return edge and marks nothing new.
func guardedInstructions(code []Instruction) []bool {
	guarded := make([]bool, len(code))
	for pc, instruction := range code {
		switch instruction.Op {
		case OpJumpIfFalse, OpJump, OpLoopInit:
			markRange(guarded, pc+1, instruction.A)
		}
	}
	return guarded
}

func markRange(guarded []bool, from, to int) {
	for pc := from; pc < to && pc < len(guarded); pc++ {
		guarded[pc] = true
	}
}

// callOperands reads the count instructions before a call, which on a stack
// machine are exactly its arguments, and accepts them only when each is an
// argument load or a constant.
func callOperands(code []Instruction, pc, count int) ([]Operand, bool) {
	if count > pc {
		return nil, false
	}
	operands := make([]Operand, count)
	for i := range operands {
		instruction := code[pc-count+i]
		switch instruction.Op {
		case OpLoadArg:
			operands[i] = Operand{Arg: instruction.A, Constant: -1}
		case OpConstant:
			operands[i] = Operand{Arg: NoArgument, Constant: instruction.A}
		default:
			return nil, false
		}
	}
	return operands, true
}

// Prefetched is what a Batch hands a program for one hoisted call: the value
// the engine produced, or the error it reported. The error surfaces only if the
// program actually reaches the call.
type Prefetched struct {
	Value Value
	Err   error
}
