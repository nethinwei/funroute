package machine

import (
	"math"
	"math/bits"
)

// The kernel's arithmetic on its fast path. Each answers what builtins.go's
// function answers, or refuses — false — where that function fails, and the
// frame then asks the function. TestKernelOpsAnswerAsTheirFunctions holds
// every operation to its function on the edges.

func addI(regs []Value, in *rinstr) bool {
	a, b := regs[in.a].i, regs[in.b].i
	sum := a + b
	if (a^sum)&(b^sum) < 0 {
		return false
	}
	regs[in.c] = Value{kind: IntKind, i: sum}
	return true
}

func subI(regs []Value, in *rinstr) bool {
	a, b := regs[in.a].i, regs[in.b].i
	difference := a - b
	if (a^b)&(a^difference) < 0 {
		return false
	}
	regs[in.c] = Value{kind: IntKind, i: difference}
	return true
}

func mulI(regs []Value, in *rinstr) bool {
	a, b := regs[in.a].i, regs[in.b].i
	product, ok := mulInt(a, b)
	if !ok {
		return false
	}
	regs[in.c] = Value{kind: IntKind, i: product}
	return true
}

// mulInt is a*b when it fits: the product's high word, signed, is only the
// low word's sign.
func mulInt(a, b int64) (int64, bool) {
	hi, lo := bits.Mul64(uint64(a), uint64(b))
	high := int64(hi)
	if a < 0 {
		high -= b
	}
	if b < 0 {
		high -= a
	}
	return int64(lo), high == int64(lo)>>63
}

func divI(regs []Value, in *rinstr) bool {
	a, b := regs[in.a].i, regs[in.b].i
	if b == 0 || a == math.MinInt64 && b == -1 {
		return false
	}
	regs[in.c] = Value{kind: IntKind, i: a / b}
	return true
}

func modI(regs []Value, in *rinstr) bool {
	a, b := regs[in.a].i, regs[in.b].i
	switch b {
	case 0:
		return false
	case -1:
		regs[in.c] = Value{kind: IntKind}
	default:
		regs[in.c] = Value{kind: IntKind, i: a % b}
	}
	return true
}

// setFloat writes a float result, refusing one that is not finite. The test
// does no arithmetic with the value: x-x would do, but the compiler may fuse
// the product before it into one instruction that does not round, and x*y -
// x*y is then the rounding error, not zero.
func setFloat(regs []Value, c int32, value float64) bool {
	if math.Abs(value) > math.MaxFloat64 || value != value {
		return false
	}
	regs[c] = Value{kind: FloatKind, f: value}
	return true
}

func addF(regs []Value, in *rinstr) bool {
	return setFloat(regs, in.c, regs[in.a].f+regs[in.b].f)
}

func subF(regs []Value, in *rinstr) bool {
	return setFloat(regs, in.c, regs[in.a].f-regs[in.b].f)
}

func mulF(regs []Value, in *rinstr) bool {
	return setFloat(regs, in.c, regs[in.a].f*regs[in.b].f)
}

func divF(regs []Value, in *rinstr) bool {
	if regs[in.b].f == 0 {
		return false
	}
	return setFloat(regs, in.c, regs[in.a].f/regs[in.b].f)
}

// eq compares two values of one type: scalars here, the rest as eq does.
func eq(regs []Value, in *rinstr) bool {
	left, right := &regs[in.a], &regs[in.b]
	if left.kind == right.kind && left.kind >= BoolKind && left.kind <= StringKind {
		regs[in.c] = Value{kind: BoolKind, b: left.b == right.b && left.i == right.i && left.f == right.f && left.s == right.s}
		return true
	}
	result, err := compareEqual(*left, *right)
	if err != nil {
		return false
	}
	regs[in.c] = result
	return true
}

// intToFloat is float(int): exact within 2^53, refused past it.
func intToFloat(regs []Value, in *rinstr) bool {
	value, ok := intFloat(regs[in.a].i)
	if ok {
		regs[in.c] = Value{kind: FloatKind, f: value}
	}
	return ok
}

func intFloat(i int64) (float64, bool) {
	return float64(i), -maxExactFloatInt <= i && i <= maxExactFloatInt
}

// branchEq is rBranchEq: on when the two are equal, to c when they are not.
// A comparison eq refuses — of handles, of money in two currencies — is
// refused here too, and the run stays at the operation.
func branchEq(regs []Value, pc int, in *rinstr) (int, bool) {
	left, right := &regs[in.a], &regs[in.b]
	if left.kind == right.kind && left.kind >= BoolKind && left.kind <= StringKind {
		equal := left.b == right.b && left.i == right.i && left.f == right.f && left.s == right.s
		return branchUnless(equal, pc, in.c), true
	}
	result, err := compareEqual(*left, *right)
	if err != nil {
		return pc, false
	}
	return branchUnless(result.b, pc, in.c), true
}

// at is an array's item, refusing an index outside it.
func at(regs []Value, in *rinstr) bool {
	array, index := regs[in.a], regs[in.b].i
	if index < 0 || index >= int64(array.length()) {
		return false
	}
	regs[in.c] = array.at(int(index))
	return true
}
