package machine

// The vector of lower_vector.go: a loop body run a block of items at a
// time. For each block of up to vecChunk items it runs each operation of the
// body over every item that reaches it, a column at a time, and finds the
// first item any would fail on; then it settles the items in order, up to
// that one — each item's fuel, its fold into the answer, the item it adds —
// and hands the loop back to the ordinary body at the first item it cannot
// settle: one that fails, one the fuel left does not pay for, one that stops
// the loop. The body runs that item as it always does.

// vecChunk is how many items a column holds.
const vecChunk = 256

// vecKind is what a column holds.
type vecKind uint8

const (
	vecInt vecKind = iota
	vecFloat
	vecBool
	vecNone
)

// vecValue is one operand: a column of this block's items, or one value
// for all of them.
type vecValue struct {
	kind   vecKind
	ints   []int64
	floats []float64
	bools  []bool
	i      int64
	f      float64
	b      bool
}

func (v *vecValue) int(i int) int64 {
	if v.ints != nil {
		return v.ints[i]
	}
	return v.i
}

func (v *vecValue) float(i int) float64 {
	if v.floats != nil {
		return v.floats[i]
	}
	return v.f
}

func (v *vecValue) bool(i int) bool {
	if v.bools != nil {
		return v.bools[i]
	}
	return v.b
}

// vecScratch is a frame's columns, kept from run to run: one of each kind
// per column the plans take, and each block's items that reach it.
type vecScratch struct {
	ints   [][]int64
	floats [][]float64
	bools  [][]bool
	alive  [][]bool
	kinds  []vecKind
	costs  []uint64
}

// column readies column c to hold kind.
func (s *vecScratch) column(c int, kind vecKind) {
	for len(s.kinds) <= c {
		s.ints, s.floats, s.bools = append(s.ints, nil), append(s.floats, nil), append(s.bools, nil)
		s.kinds = append(s.kinds, kind)
	}
	s.kinds[c] = kind
	switch kind {
	case vecInt:
		if s.ints[c] == nil {
			s.ints[c] = make([]int64, vecChunk)
		}
	case vecFloat:
		if s.floats[c] == nil {
			s.floats[c] = make([]float64, vecChunk)
		}
	default:
		if s.bools[c] == nil {
			s.bools[c] = make([]bool, vecChunk)
		}
	}
}

// aliveFor readies the masks of n blocks, and the items' costs.
func (s *vecScratch) aliveFor(n int) {
	for len(s.alive) < n {
		s.alive = append(s.alive, make([]bool, vecChunk))
	}
	if s.costs == nil {
		s.costs = make([]uint64, vecChunk)
	}
}

// vectorLoop runs as much of the loop as the vector can settle, and is the
// item the body goes on from and where; finished when that is every item.
// The prelude is paid first, and the body then goes on past it; without the
// fuel for it, the body goes on from the start, and runs it.
func (f *frame) vectorLoop(loop *regLoop, plan *vecPlan, start int) (int, int, bool) {
	run, ok := f.startVector(loop, plan)
	if !ok || f.fuelLeft < plan.prelude {
		return 0, start, false
	}
	f.fuelLeft -= plan.prelude
	for base := 0; base < loop.length; base += vecChunk {
		n := min(vecChunk, loop.length-base)
		run.load(loop, base, n)
		limit := run.columns(n)
		if settled := run.settle(limit); settled < n {
			run.finish()
			return base + settled, int(plan.body), false
		}
	}
	run.finish()
	return loop.length, int(plan.body), true
}
