package machine

import (
	"errors"
	"fmt"
)

// cold runs the operations the hot loop does not hold, and returns where the
// run goes on, and whether the fast path answered. It is small enough to be
// inlined, so a kernel operation costs one call: its row of coldKernels.
func (f *frame) cold(pc int, in *rinstr) (int, bool, error) {
	if in.op < rCall {
		pc, ok := coldKernels[in.op](&f.banks, pc, in)
		return pc, ok, nil
	}
	return f.structure(pc, in)
}

// kernelStep runs a kernel operation, and is where the run goes on — past
// it, or where the branch it makes goes — and whether the fast path
// answered.
type kernelStep func(b *banks, pc int, in *rinstr) (int, bool)

// coldKernels is, by operation, the kernel operations the hot loop does not
// hold: a table, not a switch in a switch, so each is one call away. A row
// the hot loop holds runs as it would there. TestEveryKernelOpHasAColdStep
// holds every operation before rCall to one.
var coldKernels = [rCall]kernelStep{
	rInvalid: func(_ *banks, pc int, _ *rinstr) (int, bool) { return pc, true },
	rMove:    func(b *banks, pc int, in *rinstr) (int, bool) { b.regs[in.c] = b.regs[in.a]; return pc, true },
	rMoveI:   func(b *banks, pc int, in *rinstr) (int, bool) { b.ints[in.c] = b.ints[in.a]; return pc, true },
	rMoveF:   func(b *banks, pc int, in *rinstr) (int, bool) { b.floats[in.c] = b.floats[in.a]; return pc, true },
	rJump:    func(_ *banks, _ int, in *rinstr) (int, bool) { return int(in.a), true },
	rBranch:  func(b *banks, pc int, in *rinstr) (int, bool) { return branchUnless(b.ints[in.a] != 0, pc, in.b), true },
	rAddI:    func(b *banks, pc int, in *rinstr) (int, bool) { return pc, addI(b.ints, in) },
	rSubI:    func(b *banks, pc int, in *rinstr) (int, bool) { return pc, subI(b.ints, in) },
	rMulI:    func(b *banks, pc int, in *rinstr) (int, bool) { return pc, mulI(b.ints, in) },
	rDivI:    func(b *banks, pc int, in *rinstr) (int, bool) { return pc, divI(b.ints, in) },
	rModI:    func(b *banks, pc int, in *rinstr) (int, bool) { return pc, modI(b.ints, in) },
	rAddF:    func(b *banks, pc int, in *rinstr) (int, bool) { addF(b.floats, in); return pc, true },
	rSubF:    func(b *banks, pc int, in *rinstr) (int, bool) { subF(b.floats, in); return pc, true },
	rMulF:    func(b *banks, pc int, in *rinstr) (int, bool) { mulF(b.floats, in); return pc, true },
	rDivF:    func(b *banks, pc int, in *rinstr) (int, bool) { divF(b.floats, in); return pc, true },
	rConcat: func(b *banks, pc int, in *rinstr) (int, bool) {
		b.regs[in.c] = String(b.regs[in.a].s + b.regs[in.b].s)
		return pc, true
	},
	rLtI: func(b *banks, pc int, in *rinstr) (int, bool) {
		b.ints[in.c] = word(b.ints[in.a] < b.ints[in.b])
		return pc, true
	},
	rLeI: func(b *banks, pc int, in *rinstr) (int, bool) {
		b.ints[in.c] = word(b.ints[in.a] <= b.ints[in.b])
		return pc, true
	},
	rLtF: func(b *banks, pc int, in *rinstr) (int, bool) {
		b.ints[in.c] = word(b.floats[in.a] < b.floats[in.b])
		return pc, true
	},
	rLeF: func(b *banks, pc int, in *rinstr) (int, bool) {
		b.ints[in.c] = word(b.floats[in.a] <= b.floats[in.b])
		return pc, true
	},
	rLtS: func(b *banks, pc int, in *rinstr) (int, bool) {
		b.ints[in.c] = word(b.regs[in.a].s < b.regs[in.b].s)
		return pc, true
	},
	rLeS: func(b *banks, pc int, in *rinstr) (int, bool) {
		b.ints[in.c] = word(b.regs[in.a].s <= b.regs[in.b].s)
		return pc, true
	},
	rBranchLtI: func(b *banks, pc int, in *rinstr) (int, bool) {
		return branchUnless(b.ints[in.a] < b.ints[in.b], pc, in.c), true
	},
	rBranchLeI: func(b *banks, pc int, in *rinstr) (int, bool) {
		return branchUnless(b.ints[in.a] <= b.ints[in.b], pc, in.c), true
	},
	rBranchLtF: func(b *banks, pc int, in *rinstr) (int, bool) {
		return branchUnless(b.floats[in.a] < b.floats[in.b], pc, in.c), true
	},
	rBranchLeF: func(b *banks, pc int, in *rinstr) (int, bool) {
		return branchUnless(b.floats[in.a] <= b.floats[in.b], pc, in.c), true
	},
	rBranchLtS: func(b *banks, pc int, in *rinstr) (int, bool) {
		return branchUnless(b.regs[in.a].s < b.regs[in.b].s, pc, in.c), true
	},
	rBranchLeS: func(b *banks, pc int, in *rinstr) (int, bool) {
		return branchUnless(b.regs[in.a].s <= b.regs[in.b].s, pc, in.c), true
	},
	rBranchEq: branchEq,
	rBranchEqI: func(b *banks, pc int, in *rinstr) (int, bool) {
		return branchUnless(b.ints[in.a] == b.ints[in.b], pc, in.c), true
	},
	rBranchEqF: func(b *banks, pc int, in *rinstr) (int, bool) {
		return branchUnless(b.floats[in.a] == b.floats[in.b], pc, in.c), true
	},
	rEq: func(b *banks, pc int, in *rinstr) (int, bool) { return pc, eq(b, in) },
	rEqI: func(b *banks, pc int, in *rinstr) (int, bool) {
		b.ints[in.c] = word(b.ints[in.a] == b.ints[in.b])
		return pc, true
	},
	rEqF: func(b *banks, pc int, in *rinstr) (int, bool) {
		b.ints[in.c] = word(b.floats[in.a] == b.floats[in.b])
		return pc, true
	},
	rLen:      func(b *banks, pc int, in *rinstr) (int, bool) { lengthOp(b, in); return pc, true },
	rAt:       func(b *banks, pc int, in *rinstr) (int, bool) { return pc, at(b, in) },
	rAtI:      func(b *banks, pc int, in *rinstr) (int, bool) { return pc, atI(b, in) },
	rAtF:      func(b *banks, pc int, in *rinstr) (int, bool) { return pc, atF(b, in) },
	rAtB:      func(b *banks, pc int, in *rinstr) (int, bool) { return pc, atB(b, in) },
	rAtD:      func(b *banks, pc int, in *rinstr) (int, bool) { return pc, entry(b, in) },
	rAtDI:     func(b *banks, pc int, in *rinstr) (int, bool) { return pc, entryI(b, in) },
	rAtDF:     func(b *banks, pc int, in *rinstr) (int, bool) { return pc, entryF(b, in) },
	rAtDB:     func(b *banks, pc int, in *rinstr) (int, bool) { return pc, entryB(b, in) },
	rIntToF:   func(b *banks, pc int, in *rinstr) (int, bool) { return pc, intToFloat(b, in) },
	rFloatToI: func(b *banks, pc int, in *rinstr) (int, bool) { return pc, floatToInt(b, in) },
	rAtA:      func(b *banks, pc int, in *rinstr) (int, bool) { return pc, arenaAt(b, in) },
	rTake:     func(b *banks, pc int, in *rinstr) (int, bool) { return pc, takeOp(b, in) },
	rHasKey:   func(b *banks, pc int, in *rinstr) (int, bool) { hasKey(b, in); return pc, true },
}

// structure runs what builds values and what opens and closes a loop, a
// fallback or a using.
func (f *frame) structure(pc int, in *rinstr) (int, bool, error) {
	// What a loop's body does most after a field read — add an item — is
	// here, a call from the hot loop, and the rest one more away.
	switch in.op {
	case rCollectI:
		f.collectInt(in.a)
	case rCollectF:
		f.collectFloat(in.a)
	case rCollectNextF:
		f.collectFloat(in.a)
		pc, err := f.regLoopNext(int(in.c), in.b)
		return pc, true, err
	default:
		return f.scoped(pc, in)
	}
	return pc, true, nil
}

// readField reads field b of the record in a into register c, in the file
// of its kind, or branches on it, and is where the run goes on.
func (f *frame) readField(pc int, in *rinstr) (int, error) {
	record, ok := f.regs[in.a].box.(*recordValue)
	if !ok {
		return pc, errNotARecord
	}
	switch field := &record.fields[in.b]; in.op {
	case rFieldI:
		f.ints[in.c] = field.i
	case rFieldF:
		f.floats[in.c] = field.f
	case rFieldB:
		f.ints[in.c] = word(field.b)
	case rBranchField:
		return branchUnless(field.b, pc, in.c), nil
	default:
		f.regs[in.c] = *field
	}
	return pc, nil
}

// scoped runs what builds values and what opens and closes a loop, a
// fallback or a using.
func (f *frame) scoped(pc int, in *rinstr) (int, bool, error) {
	switch in.op {
	case rMakeArray, rMakeDict, rMakeRecord, rRecordWith:
		built, err := f.build(in)
		f.regs[in.a] = built
		return pc, true, err
	case rLoopInit:
		pc, err := f.regLoopInit(pc, in)
		return pc, true, err
	case rLoopBreak:
		return f.loopBreak(in), true, nil
	case rSpread:
		f.loops[len(f.loops)-1].out.addAll(f.regs[in.a])
	case rBeginFallback:
		f.fallbacks = append(f.fallbacks, fallbackFrame{target: int(in.a), loops: len(f.loops), scopes: len(f.fxMarks)})
	case rEndFallback:
		f.fallbacks = f.fallbacks[:len(f.fallbacks)-1]
	case rFxPush:
		f.fxMarks = append(f.fxMarks, len(f.fxQuotes))
		f.fxQuotes = append(f.fxQuotes, f.regs[in.a:in.a+in.b]...)
	case rFxPop:
		f.popScope()
	default:
		return pc, true, fmt.Errorf("internal error: unknown register operation %s", in.op)
	}
	return pc, true, nil
}

// errNotARecord is a field read or an update of what is not a record, which
// loading proved no program does.
var errNotARecord = errors.New("internal error: not a record")

// build makes an array, a dictionary or a record from the operation's
// registers. Loading proved each of the type it goes in as.
func (f *frame) build(in *rinstr) (Value, error) {
	made := &f.runtime.reg.makes[in.c]
	if len(made.boxes) > 0 {
		f.boxArgs(made.boxes)
	}
	items := f.regs[in.a : in.a+in.b]
	switch in.op {
	case rMakeArray:
		slot := f.slotFor(made.built)
		if slot == nil {
			builder := newArrayBuilder(*made.typ.elem, len(items))
			for _, item := range items {
				builder.add(item)
			}
			return builder.finish(), nil
		}
		builder := builderIn(slot, *made.typ.elem, len(items))
		for _, item := range items {
			builder.add(item)
		}
		return finishIn(slot, &builder), nil
	case rMakeDict:
		return packDict(*made.typ.elem, zipEntries(made.keys, items)), nil
	case rMakeRecord:
		if made.answer && f.lending {
			f.answer.typ, f.answer.fields = made.typ, append(f.answer.fields[:0], items...)
			return Value{kind: RecordKind, box: &f.answer}, nil
		}
		record := newRecord(made.typ, len(items))
		copy(record.fields, items)
		return Value{kind: RecordKind, box: record}, nil
	}
	base, ok := items[0].box.(*recordValue)
	if !ok {
		return Value{}, errNotARecord
	}
	// The copy shares the base's type: an update does not change it. The
	// answer is built in the frame's own record, as make_record's is.
	record := &f.answer
	if made.answer && f.lending {
		f.answer.typ, f.answer.fields = base.typ, append(f.answer.fields[:0], base.fields...)
	} else {
		record = newRecord(base.typ, len(base.fields))
		copy(record.fields, base.fields)
	}
	for i, index := range made.fields {
		record.fields[index] = items[1+i]
	}
	return Value{kind: RecordKind, box: record}, nil
}

// zipEntries pairs keys with values, one for one.
func zipEntries(keys []string, values []Value) map[string]Value {
	entries := make(map[string]Value, len(values))
	for i, value := range values {
		entries[keys[i]] = value
	}
	return entries
}
