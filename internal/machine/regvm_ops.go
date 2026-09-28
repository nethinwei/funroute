package machine

// The register form's operations. An artifact never holds them: lowering
// makes them from the verified stack bytecode when the artifact is loaded,
// so they can change with the engine and leave every digest as it is.
//
// An operation reads registers a and b and writes register c, unless its
// comment says otherwise, each in the file its kind says (regvm_banks.go):
// an _i operation's ints, an _f one's floats — a comparison's answer, a bool,
// is an int — and the others' values. The ones a kernel function became do what the
// function does and nothing else; when the fast path cannot answer — an
// overflow, a division by zero — the operation stops and the function itself
// is asked, so every failure is the function's own, word for word.
type rop uint8

const (
	rInvalid rop = iota
	// The hot loop's kernel operations first, and its others right after
	// rCall: its switch is then a jump table, which a range of values more
	// than four times its cases is not.
	rMove // c = a
	rMoveI
	rMoveF
	rJump // to a
	// rBranch goes to b unless register a is true.
	rBranch
	rAddI
	rSubI
	rMulI
	rAddF
	// The branches an ordering and the jump after it make together: to c
	// unless the ordering holds.
	rBranchLtI
	rBranchLeI
	rLen // c = the length of the array or dictionary in a
	rDivI
	rModI
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
	rBranchLtF
	rBranchLeF
	rBranchLtS
	rBranchLeS
	rBranchEq
	rBranchEqI
	rBranchEqF
	rEq
	rEqI
	rEqF
	rAt // c = item b of the array in a
	rAtI
	rAtF
	rAtB
	rAtD // c = the entry of the dictionary in a under the key in b
	rAtDI
	rAtDF
	rAtDB
	rIntToF   // c = float(a), refusing an int past 2^53
	rFloatToI // c = int(a), refusing a float that is not a whole int
	rAtA      // rAt of an array in an arena slot
	rTake     // c = the first b items of the array in a
	rHasKey   // c = whether the dictionary in b has the key in a
	// rCall is a call: calls[a] says of what, from which registers, into
	// which.
	rCall
	// rLoopNext goes back to a while items are left, and otherwise puts
	// loops[c]'s result in its register and goes on.
	rLoopNext
	// rCollectNext is rCollect of register a and then rLoopNext back to b,
	// going on at c after the last item: the last two operations of most
	// comprehensions' bodies.
	rCollectNext
	rCollectNextI
	rCollectNextF
	rHalt  // the program's result is register a
	rField // c = field b of the record in a
	rFieldI
	rFieldF
	rFieldB
	// rBranchField is rFieldB and the branch after it: to c unless field b
	// of the record in a is true.
	rBranchField
	// The builders take b registers from a, and put what they build in a;
	// makes[c] has the type and the names.
	rMakeArray
	rMakeDict
	rMakeRecord
	rRecordWith // the record in a with the b-1 registers after it as fields
	// rLoopInit starts loops[c] over the source in a, the seed in b for a
	// fold. An empty source goes to the loop's exit with its result.
	rLoopInit
	rCollect // adds register a, under the key in b unless b is -1: of kind c
	rCollectI
	rCollectF
	rSpread // adds the items of the array in a
	// rLoopBreak ends loops[c] with register a as its answer, and goes to
	// b: the loop's exit, or wherever the stack instruction went on to.
	rLoopBreak
	rBeginFallback // a failure fallback takes goes to a
	rEndFallback
	rFxPush // opens a using of the b quotes from a
	rFxPop
	ropCount
)

var ropNames = [ropCount]string{
	rInvalid: "invalid", rMove: "move", rMoveI: "move_i", rMoveF: "move_f",
	rJump: "jump", rBranch: "branch",
	rAddI: "add_i", rSubI: "sub_i", rMulI: "mul_i", rDivI: "div_i", rModI: "mod_i",
	rAddF: "add_f", rSubF: "sub_f", rMulF: "mul_f", rDivF: "div_f", rConcat: "concat",
	rLtI: "lt_i", rLeI: "le_i", rLtF: "lt_f", rLeF: "le_f", rLtS: "lt_s", rLeS: "le_s",
	rBranchLtI: "branch_lt_i", rBranchLeI: "branch_le_i", rBranchLtF: "branch_lt_f",
	rBranchLeF: "branch_le_f", rBranchLtS: "branch_lt_s", rBranchLeS: "branch_le_s", rBranchEq: "branch_eq",
	rBranchEqI: "branch_eq_i", rBranchEqF: "branch_eq_f", rEq: "eq", rEqI: "eq_i", rEqF: "eq_f",
	rLen: "len", rAt: "at", rAtI: "at_i", rAtF: "at_f", rAtB: "at_b",
	rAtD: "at_d", rAtDI: "at_d_i", rAtDF: "at_d_f", rAtDB: "at_d_b", rIntToF: "int_to_f", rFloatToI: "float_to_i", rAtA: "at_a", rTake: "take", rCall: "call",
	rField: "field", rFieldI: "field_i", rFieldF: "field_f", rFieldB: "field_b", rBranchField: "branch_field", rHasKey: "has_key",
	rMakeArray: "make_array", rMakeDict: "make_dict", rMakeRecord: "make_record", rRecordWith: "record_with",
	rLoopInit: "loop_init", rCollect: "collect", rSpread: "spread", rLoopNext: "loop_next", rLoopBreak: "loop_break", rCollectNext: "collect_next",
	rCollectI: "collect_i", rCollectF: "collect_f", rCollectNextI: "collect_next_i", rCollectNextF: "collect_next_f",
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
	// function; kinds is the kinds of its operands a and b, which say the
	// files they are in.
	origins []int32
	kinds   []opKinds
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
	// scalarArgs is the arguments of an int, a bool or a float the program
	// reads, which a run puts in their files (regvm_banks.go), and result
	// the program's result kind, whose file the answer is in.
	scalarArgs []scalarArg
	result     Kind
	// starts marks, by operation, where a basic block starts.
	starts []bool
}

// rcall is one call: the function, the call's type, its arguments' first
// register and how many, where the result goes, and the stack instruction a
// Batch knows it by.
type rcall struct {
	fn *RegisteredFunction
	// typ is the call's type, and kind its kind, whose file the answer
	// goes in.
	typ  *Type
	kind Kind
	args int32
	argc int32
	dst  int32
	pc   int32
	// banked calls a Go function straight from the registers, outside every
	// fallback's candidate; deadline says it looks at the deadline first,
	// as a host's call that is not pure does.
	banked   pureCall
	deadline bool
	// boxes is the arguments of an int, a bool or a float, which a call
	// that takes values makes values of as it is made.
	boxes []scalarArg
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
	// item is only read field by field (lower_escape.go), and fields is the
	// fields the body reads: each item loads those alone.
	itemsInPlace bool
	fields       []int32
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
	// boxes is the items of an int, a bool or a float, which the builder
	// makes values of as it builds.
	boxes []scalarArg
	built
	// answer marks the record the program answers, built in the frame's
	// own record when the host reads it before the frame goes.
	answer bool
}
