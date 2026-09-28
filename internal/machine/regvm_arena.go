package machine

import "slices"

// The arrays a run builds in memory the frame keeps (lower_escape.go says
// which). An arena array, one that never leaves the run, is built in its own
// slot and read there: its Value holds a pointer to the slot's slice, which
// a Value's box holds without an allocation, and only the operations the
// lowering chose for it — a loop, len, an index — ever see one. An answer's
// array is built in the slot the host lent (Program.RunInto) and handed
// back into the host's struct before the frame goes.

// arenaSlot is the memory of one array: of whichever native kind it is.
type arenaSlot struct {
	bools   []bool
	ints    []int64
	floats  []float64
	strings []string
}

// maxKept is the most items a slot keeps between runs: a run that built a
// huge array does not leave the frame holding it.
const maxKept = 1 << 16

// builderIn starts an array of elem in the slot, with room for capacity.
func builderIn(slot *arenaSlot, elem Type, capacity int) arrayBuilder {
	builder := arrayBuilder{elem: elem}
	switch elem.kind {
	case BoolKind:
		builder.bools = slices.Grow(slot.bools[:0], capacity)
	case IntKind:
		builder.ints = slices.Grow(slot.ints[:0], capacity)
	case FloatKind:
		builder.floats = slices.Grow(slot.floats[:0], capacity)
	default:
		builder.strings = slices.Grow(slot.strings[:0], capacity)
	}
	return builder
}

// finishIn keeps what the builder built in the slot, and is the array: a
// Value that points at the slot.
func finishIn(slot *arenaSlot, builder *arrayBuilder) Value {
	switch builder.elem.kind {
	case BoolKind:
		slot.bools = builder.bools
		return Value{kind: ArrayKind, i: int64(len(slot.bools)), box: &slot.bools}
	case IntKind:
		slot.ints = builder.ints
		return Value{kind: ArrayKind, i: int64(len(slot.ints)), box: &slot.ints}
	case FloatKind:
		slot.floats = builder.floats
		return Value{kind: ArrayKind, i: int64(len(slot.floats)), box: &slot.floats}
	}
	slot.strings = builder.strings
	return Value{kind: ArrayKind, i: int64(len(slot.strings)), box: &slot.strings}
}

// release drops the strings a slot holds, and a slot grown past maxKept.
func (s *arenaSlot) release() {
	clear(s.strings)
	if cap(s.bools) > maxKept || cap(s.ints) > maxKept || cap(s.floats) > maxKept || cap(s.strings) > maxKept {
		*s = arenaSlot{}
	}
}

// unarena is an array in a slot as an ordinary Value, for a failure's
// sake: the function that says why is handed what it always is.
func unarena(v Value) Value {
	switch box := v.box.(type) {
	case *[]bool:
		return arrayOf(*box)
	case *[]int64:
		return arrayOf(*box)
	case *[]float64:
		return arrayOf(*box)
	case *[]string:
		return arrayOf(*box)
	}
	return v
}

// arenaAt is item b of the array in a slot, refusing an index outside it.
func arenaAt(b *banks, in *rinstr) bool {
	regs, index := b.regs, b.ints[in.b]
	if index < 0 || index >= regs[in.a].i {
		return false
	}
	switch box := regs[in.a].box.(type) {
	default:
		regs[in.c] = regs[in.a].at(int(index))
	case *[]bool:
		regs[in.c] = Bool((*box)[index])
	case *[]int64:
		regs[in.c] = Int((*box)[index])
	case *[]float64:
		regs[in.c] = Float((*box)[index])
	case *[]string:
		regs[in.c] = String((*box)[index])
	}
	return true
}

// grow makes room in the builder for n more items.
func (b *arrayBuilder) grow(n int) {
	switch b.elem.kind {
	case BoolKind:
		b.bools = slices.Grow(b.bools, n)
	case IntKind:
		b.ints = slices.Grow(b.ints, n)
	case FloatKind:
		b.floats = slices.Grow(b.floats, n)
	case StringKind:
		b.strings = slices.Grow(b.strings, n)
	case MoneyKind:
		b.monies = slices.Grow(b.monies, n)
	case FxRateKind:
		b.fxRates = slices.Grow(b.fxRates, n)
	default:
		b.values = slices.Grow(b.values, n)
	}
}
