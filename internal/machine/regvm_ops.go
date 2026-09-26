package machine

// The register form's operations. An artifact never holds them: lowering
// makes them from the verified stack bytecode when the artifact is loaded,
// so they can change with the engine and leave every digest as it is.
//
// An operation reads registers a and b and writes register c, unless its
// comment says otherwise. The ones a kernel function became do what the
// function does and nothing else; when the fast path cannot answer — an
// overflow, a division by zero — the operation stops and the function itself
// is asked, so every failure is the function's own, word for word.
type rop uint8

const (
	rInvalid rop = iota
	rMove        // c = a
	rJump        // to a
	// rBranch goes to b unless register a is true.
	rBranch
	rAddI
	rSubI
	rMulI
	rDivI
	rModI
	rAddF
	rSubF
	rMulF
	rDivF
	rConcat
	// The orderings: gt and ge are lt and le with the operands swapped.
	rLtI
	rLeI
	rLtF
	rLeF
	rLtS
	rLeS
	// The branches an ordering and the jump after it make together: to c
	// unless the ordering holds.
	rBranchLtI
	rBranchLeI
	rBranchLtF
	rBranchLeF
	rBranchLtS
	rBranchLeS
	rBranchEq
	rEq
	rLen    // c = the length of the array or dictionary in a
	rAt     // c = item b of the array in a
	rIntToF // c = float(a), refusing an int past 2^53
	rLenA   // rLen of an array in an arena slot
	rAtA    // rAt of an array in an arena slot
	// rCall is a call: calls[a] says of what, from which registers, into
	// which.
	rCall
	rField // c = field b of the record in a
	// The builders take b registers from a, and put what they build in a;
	// makes[c] has the type and the names.
	rMakeArray
	rMakeDict
	rMakeRecord
	rRecordWith // the record in a with the b-1 registers after it as fields
	// rLoopInit starts loops[c] over the source in a, the seed in b for a
	// fold. An empty source goes to the loop's exit with its result.
	rLoopInit
	rCollect // adds register a, under the key in b unless b is -1
	rSpread  // adds the items of the array in a
	// rLoopNext goes back to a while items are left, and otherwise puts
	// loops[c]'s result in its register and goes on.
	rLoopNext
	// rLoopBreak ends loops[c] with register a as its answer, and goes to
	// b: the loop's exit, or wherever the stack instruction went on to.
	rLoopBreak
	// rCollectNext is rCollect of register a and then rLoopNext back to b —
	// c is the loop, for a listing —:
	// the last two operations of most comprehensions' bodies.
	rCollectNext
	rBeginFallback // a failure fallback takes goes to a
	rEndFallback
	rFxPush // opens a using of the b quotes from a
	rFxPop
	rHalt // the program's result is register a
	ropCount
)

var ropNames = [ropCount]string{
	rInvalid: "invalid", rMove: "move", rJump: "jump", rBranch: "branch",
	rAddI: "add_i", rSubI: "sub_i", rMulI: "mul_i", rDivI: "div_i", rModI: "mod_i",
	rAddF: "add_f", rSubF: "sub_f", rMulF: "mul_f", rDivF: "div_f", rConcat: "concat",
	rLtI: "lt_i", rLeI: "le_i", rLtF: "lt_f", rLeF: "le_f", rLtS: "lt_s", rLeS: "le_s",
	rBranchLtI: "branch_lt_i", rBranchLeI: "branch_le_i", rBranchLtF: "branch_lt_f",
	rBranchLeF: "branch_le_f", rBranchLtS: "branch_lt_s", rBranchLeS: "branch_le_s", rBranchEq: "branch_eq",
	rEq: "eq", rLen: "len", rAt: "at", rLenA: "len_a", rIntToF: "int_to_f", rAtA: "at_a", rCall: "call", rField: "field",
	rMakeArray: "make_array", rMakeDict: "make_dict", rMakeRecord: "make_record", rRecordWith: "record_with",
	rLoopInit: "loop_init", rCollect: "collect", rSpread: "spread", rLoopNext: "loop_next", rLoopBreak: "loop_break", rCollectNext: "collect_next",
	rBeginFallback: "begin_fallback", rEndFallback: "end_fallback", rFxPush: "fx_push", rFxPop: "fx_pop",
	rHalt: "halt",
}

func (o rop) String() string {
	if o < ropCount {
		return ropNames[o]
	}
	return ropNames[rInvalid]
}

// rinstr is one register operation: sixteen bytes, and read in place, not
// copied.
type rinstr struct {
	op      rop
	a, b, c int32
}

// regProgram is a program in register form, and the tables its operations
// index.
//
// The registers are one array: the constants, the arguments, the stack —
// position k of the stack is one register throughout — and the locals.
type regProgram struct {
	code []rinstr
	// origins is, for each operation, the stack instruction it came from:
	// what a failure is reported at, and how a kernel operation finds its
	// function.
	origins []int32
	calls   []rcall
	loops   []rloop
	makes   []rmake
	// args is where the arguments start, and size how many registers there
	// are.
	args, size int32
	// scalar is set when no register ever holds a pointer, and scalarReads
	// when none does but the slots of arguments the program never reads,
	// which a Program leaves unwritten; hosts is set when the program calls
	// a host's function that is not pure, whose call looks at the deadline,
	// and foreign when it calls a host's function at all, which may panic.
	scalar, scalarReads, hosts, foreign bool
	// nesting is how many loops deep the program goes; arenas and dests
	// how many arena and answer slots it builds in.
	nesting       int
	arenas, dests int
	// fieldOnly is, by argument, a record the program only reads field by
	// field, and viewOnly an array it only walks, measures or indexes.
	fieldOnly, viewOnly []bool
	// promotions are the fields read straight off a record argument, each
	// in a register of its own (lower_promote.go).
	promotions []promotion
	// starts marks, by operation, where a basic block starts.
	starts []bool
}

// rcall is one call: the function, the call's type, its arguments' first
// register and how many, where the result goes, and the stack instruction a
// Batch knows it by.
type rcall struct {
	fn   *RegisteredFunction
	typ  *Type
	args int32
	argc int32
	dst  int32
	pc   int32
	// pure calls a pure host function straight from the registers.
	pure pureCall
	// kernel marks a kernel function that reads nothing of the run: it is
	// called straight away, with none of what a host's call is held to.
	// direct marks a host's function no Batch hoists — it has no batch
	// form — with no Timeout and not Detached: called with nothing to ask
	// but the deadline.
	kernel, direct bool
}

// rloop is one loop: its type, the registers of its names — -1 for none —
// where its result goes, and its exit. spread marks the inner clause of a
// nested comprehension, which puts its items straight into the outer
// clause's array rather than building one of its own to be spread.
type rloop struct {
	typ            *Type
	item, key, acc int32
	dst            int32
	exit           int32
	spread         bool
	// itemsInPlace is set for a loop over an array of plain records whose
	// item is only read field by field (lower_escape.go).
	itemsInPlace bool
	built
	// vec is the plan a vector runs the loop's body by, when it can.
	vec *vecPlan
}

// built is where an array is built: in an arena slot, in the answer's slot
// the host lends, or — both -1 — in memory of its own.
type built struct {
	arena, dest int32
}

// rmake is what a builder needs besides its registers: the type, and a
// dictionary's keys or the fields a record update replaces.
type rmake struct {
	typ    *Type
	keys   []string
	fields []int
	built
	// answer marks the record the program answers, built in the frame's
	// own record when the host reads it before the frame goes.
	answer bool
}
