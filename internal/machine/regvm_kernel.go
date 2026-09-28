package machine

import (
	"math"
	"math/bits"
)

// The kernel's arithmetic on its fast path. Each answers what builtins.go's
// function answers, or refuses — false — where that function fails, and the
// frame then asks the function. TestKernelOpsAnswerAsTheirFunctions holds
// every operation to its function on the edges.

// The int arithmetic, each answering false where int64 has no answer: the
// one statement of it, which the kernel's functions, their operations, the
// vector and the library all use.

// addInt is a+b when it fits: an overflow gives the sum the sign neither
// operand has.
func addInt(a, b int64) (int64, bool) {
	sum := a + b
	return sum, (a^sum)&(b^sum) >= 0
}

// subInt is a-b when it fits.
func subInt(a, b int64) (int64, bool) {
	difference := a - b
	return difference, (a^b)&(a^difference) >= 0
}

// divInt is a/b, truncated, when b is not zero and the quotient fits.
func divInt(a, b int64) (int64, bool) {
	if b == 0 || a == math.MinInt64 && b == -1 {
		return 0, false
	}
	return a / b, true
}

// modInt is a%b, its sign the dividend's, when b is not zero; by -1 it is 0,
// which a%b would trap on for the smallest int.
func modInt(a, b int64) (int64, bool) {
	switch b {
	case 0:
		return 0, false
	case -1:
		return 0, true
	}
	return a % b, true
}

func addI(ints []int64, in *rinstr) bool {
	sum, ok := addInt(ints[in.a], ints[in.b])
	if ok {
		ints[in.c] = sum
	}
	return ok
}

func subI(ints []int64, in *rinstr) bool {
	difference, ok := subInt(ints[in.a], ints[in.b])
	if ok {
		ints[in.c] = difference
	}
	return ok
}

func mulI(ints []int64, in *rinstr) bool {
	product, ok := mulInt(ints[in.a], ints[in.b])
	if !ok {
		return false
	}
	ints[in.c] = product
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

func divI(ints []int64, in *rinstr) bool {
	quotient, ok := divInt(ints[in.a], ints[in.b])
	if ok {
		ints[in.c] = quotient
	}
	return ok
}

func modI(ints []int64, in *rinstr) bool {
	remainder, ok := modInt(ints[in.a], ints[in.b])
	if ok {
		ints[in.c] = remainder
	}
	return ok
}

// A float operation always answers, as IEEE 754 does: an overflow is an
// infinity, 0/0 is NaN.

func addF(floats []float64, in *rinstr) { floats[in.c] = floats[in.a] + floats[in.b] }

func subF(floats []float64, in *rinstr) { floats[in.c] = floats[in.a] - floats[in.b] }

func mulF(floats []float64, in *rinstr) { floats[in.c] = floats[in.a] * floats[in.b] }

func divF(floats []float64, in *rinstr) { floats[in.c] = floats[in.a] / floats[in.b] }

// eq compares two values of one type: strings, and the rest as eq does. The
// answer is a bool, in the ints.
func eq(b *banks, in *rinstr) bool {
	left, right := &b.regs[in.a], &b.regs[in.b]
	if left.kind == right.kind && left.kind >= BoolKind && left.kind <= StringKind {
		b.ints[in.c] = word(left.b == right.b && left.i == right.i && left.f == right.f && left.s == right.s)
		return true
	}
	result, err := compareEqual(*left, *right)
	if err != nil {
		return false
	}
	b.ints[in.c] = word(result.b)
	return true
}

// takeOp is take: the array's first b items, all of them when it has
// fewer, sharing its backing. A negative count is the function's to refuse.
func takeOp(b *banks, in *rinstr) bool {
	count := b.ints[in.b]
	if count < 0 {
		return false
	}
	b.regs[in.c], _ = b.regs[in.a].Slice(0, int(min(count, b.regs[in.a].i)))
	return true
}

// lengthOp is len of the array or dictionary in a, which never fails.
func lengthOp(b *banks, in *rinstr) { b.ints[in.c] = int64(b.regs[in.a].length()) }

// intToFloat is float(int): exact within 2^53, refused past it.
func intToFloat(b *banks, in *rinstr) bool {
	value, ok := intFloat(b.ints[in.a])
	if ok {
		b.floats[in.c] = value
	}
	return ok
}

// floatToInt is int(float): a float that is a whole int, refused otherwise.
func floatToInt(b *banks, in *rinstr) bool {
	whole, ok := floatInt(b.floats[in.a])
	if ok {
		b.ints[in.c] = whole
	}
	return ok
}

func intFloat(i int64) (float64, bool) {
	return float64(i), -maxExactFloatInt <= i && i <= maxExactFloatInt
}

// branchEq is rBranchEq: on when the two are equal, to c when they are not.
// A comparison eq refuses — of handles, of money in two currencies — is
// refused here too, and the run stays at the operation.
func branchEq(b *banks, pc int, in *rinstr) (int, bool) {
	left, right := &b.regs[in.a], &b.regs[in.b]
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

// at is an array's item as a value, refusing an index outside it.
func at(b *banks, in *rinstr) bool {
	array, index := &b.regs[in.a], b.ints[in.b]
	if index < 0 || index >= array.i {
		return false
	}
	b.regs[in.c] = array.at(int(index))
	return true
}

// atI is an int array's item, out of its backing — its own, or an arena
// slot's — into the ints.
func atI(b *banks, in *rinstr) bool {
	array, index := &b.regs[in.a], b.ints[in.b]
	if index < 0 || index >= array.i {
		return false
	}
	switch items := array.box.(type) {
	case *int64:
		b.ints[in.c] = itemsAt(items, array.i)[index]
	case *[]int64:
		b.ints[in.c] = (*items)[index]
	}
	return true
}

// atB is atI of a bool array, the bool as an int.
func atB(b *banks, in *rinstr) bool {
	array, index := &b.regs[in.a], b.ints[in.b]
	if index < 0 || index >= array.i {
		return false
	}
	switch items := array.box.(type) {
	case *bool:
		b.ints[in.c] = word(itemsAt(items, array.i)[index])
	case *[]bool:
		b.ints[in.c] = word((*items)[index])
	}
	return true
}

// entry is a dictionary's entry as a value, refusing a key it lacks.
func entry(b *banks, in *rinstr) bool {
	value, ok := b.regs[in.a].lookup(b.regs[in.b].s)
	if ok {
		b.regs[in.c] = value
	}
	return ok
}

// hasKey is member(string, dict<T>): whether the dictionary has the key.
func hasKey(b *banks, in *rinstr) {
	_, ok := b.regs[in.b].lookup(b.regs[in.a].s)
	b.ints[in.c] = word(ok)
}

// entryI is an int dictionary's entry, out of its map into the ints.
func entryI(b *banks, in *rinstr) bool {
	entries, _ := b.regs[in.a].box.(map[string]int64)
	value, ok := entries[b.regs[in.b].s]
	if ok {
		b.ints[in.c] = value
	}
	return ok
}

// entryF is entryI of a float dictionary.
func entryF(b *banks, in *rinstr) bool {
	entries, _ := b.regs[in.a].box.(map[string]float64)
	value, ok := entries[b.regs[in.b].s]
	if ok {
		b.floats[in.c] = value
	}
	return ok
}

// entryB is entryI of a bool dictionary, the bool as an int.
func entryB(b *banks, in *rinstr) bool {
	entries, _ := b.regs[in.a].box.(map[string]bool)
	value, ok := entries[b.regs[in.b].s]
	if ok {
		b.ints[in.c] = word(value)
	}
	return ok
}

// atF is atI of a float array.
func atF(b *banks, in *rinstr) bool {
	array, index := &b.regs[in.a], b.ints[in.b]
	if index < 0 || index >= array.i {
		return false
	}
	switch items := array.box.(type) {
	case *float64:
		b.floats[in.c] = itemsAt(items, array.i)[index]
	case *[]float64:
		b.floats[in.c] = (*items)[index]
	}
	return true
}
