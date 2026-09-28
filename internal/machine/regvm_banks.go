package machine

// The registers are three files over one index space, as the JVM and Wasm
// keep ints and floats apart from references: register r holds an int or a
// bool — 0 or 1 — in ints, a float in floats, and anything else in regs, by
// the kind the verifier proved of the value it holds there. An operation
// reads and writes the files its operands' kinds say; lowering chose it
// knowing them, so a run asks no value its kind, and an int written is eight
// bytes with no pointer in them, not a whole Value. A Value is made of an
// int, a bool or a float only where something takes values — a call that
// is not straight from the files (rcall.boxes), a builder (rmake.boxes), a
// dictionary's entry, the answer — as it takes them, by the kind lowering
// wrote down, never by an operation of its own.

// banks is a frame's register files, each as long as the program has
// registers.
type banks struct {
	regs   []Value
	ints   []int64
	floats []float64
}

// bank is the file a value of a kind is in.
type bank uint8

const (
	valueBank bank = iota
	intBank
	floatBank
)

func bankOf(kind Kind) bank {
	switch kind {
	case IntKind, BoolKind:
		return intBank
	case FloatKind:
		return floatBank
	}
	return valueBank
}

// opKinds is the kinds of an operation's operands a and b: InvalidKind for
// one it does not read.
type opKinds struct{ a, b Kind }

// scalarArg is an argument of an int, a bool or a float: the register it is
// in, and its kind.
type scalarArg struct {
	reg  int32
	kind Kind
}

// word is a bool as an int register holds it.
func word(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

// valueAt is register reg, holding a value of kind, as a Value.
func (b *banks) valueAt(reg int32, kind Kind) Value {
	switch kind {
	case IntKind:
		return Value{kind: IntKind, i: b.ints[reg]}
	case BoolKind:
		return Value{kind: BoolKind, b: b.ints[reg] != 0}
	case FloatKind:
		return Value{kind: FloatKind, f: b.floats[reg]}
	}
	return b.regs[reg]
}

// setValue puts v in register reg, in the file of its kind.
func (b *banks) setValue(reg int32, v Value) {
	switch v.kind {
	case IntKind:
		b.ints[reg] = v.i
	case BoolKind:
		b.ints[reg] = word(v.b)
	case FloatKind:
		b.floats[reg] = v.f
	default:
		b.regs[reg] = v
	}
}

// put puts v, of kind, in register reg: its int, bool or float in their
// file, anything else as it is.
func (b *banks) put(reg int32, kind Kind, v Value) {
	switch kind {
	case IntKind:
		b.ints[reg] = v.i
	case BoolKind:
		b.ints[reg] = word(v.b)
	case FloatKind:
		b.floats[reg] = v.f
	default:
		b.regs[reg] = v
	}
}

// copyReg copies register from to register to, both holding a value of kind.
func (b *banks) copyReg(to, from int32, kind Kind) {
	switch bankOf(kind) {
	case intBank:
		b.ints[to] = b.ints[from]
	case floatBank:
		b.floats[to] = b.floats[from]
	default:
		b.regs[to] = b.regs[from]
	}
}

// boxArgs makes values of the scalar arguments of a call that takes
// values, in their registers.
func (b *banks) boxArgs(args []scalarArg) {
	for _, arg := range args {
		b.regs[arg.reg] = b.valueAt(arg.reg, arg.kind)
	}
}

// bankArgs puts the scalar arguments, which a run loads as values, in their
// files.
func (b *banks) bankArgs(args []scalarArg) {
	for _, arg := range args {
		b.put(arg.reg, arg.kind, b.regs[arg.reg])
	}
}

// newBanks is the register files of a program of size registers, the
// constants in theirs.
func newBanks(size int32, constants []Value) banks {
	b := banks{regs: make([]Value, size), ints: make([]int64, size), floats: make([]float64, size)}
	copy(b.regs, constants)
	for i, constant := range constants {
		b.setValue(int32(i), constant)
	}
	return b
}
