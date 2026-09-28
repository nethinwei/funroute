package machine

import (
	"math"
	"math/bits"
)

// The kernel's arithmetic on its fast path. Each answers what builtins.go's
// function answers, or refuses — false — where that function fails, and the
// frame then asks the function. TestKernelOpsAnswerAsTheirFunctions holds
// every operation to its function on the edges.

func addI(ints []int64, in *rinstr) bool {
	x, y := ints[in.a], ints[in.b]
	sum := x + y
	if (x^sum)&(y^sum) < 0 {
		return false
	}
	ints[in.c] = sum
	return true
}

func subI(ints []int64, in *rinstr) bool {
	x, y := ints[in.a], ints[in.b]
	difference := x - y
	if (x^y)&(x^difference) < 0 {
		return false
	}
	ints[in.c] = difference
	return true
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
	x, y := ints[in.a], ints[in.b]
	if y == 0 || x == math.MinInt64 && y == -1 {
		return false
	}
	ints[in.c] = x / y
	return true
}

func modI(ints []int64, in *rinstr) bool {
	x, y := ints[in.a], ints[in.b]
	switch y {
	case 0:
		return false
	case -1:
		ints[in.c] = 0
	default:
		ints[in.c] = x % y
	}
	return true
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
