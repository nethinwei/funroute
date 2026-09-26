package machine

import (
	"errors"
	"fmt"
)

// cold runs the operations the hot loop does not hold, and returns where the
// run goes on, and whether the fast path answered. It is small enough to be
// inlined, so a kernel operation costs one call.
func (f *frame) cold(pc int, in *rinstr) (int, bool, error) {
	if in.op < rCall {
		pc, ok := coldKernel(f.regs, pc, in)
		return pc, ok, nil
	}
	return f.structure(pc, in)
}

// coldKernel runs a kernel operation the hot loop does not hold, and is where
// the run goes on — past it, or where the branch it makes goes — and whether
// the fast path answered.
func coldKernel(regs []Value, pc int, in *rinstr) (int, bool) {
	switch in.op {
	case rBranchLtF:
		return branchUnless(regs[in.a].f < regs[in.b].f, pc, in.c), true
	case rBranchLeF:
		return branchUnless(regs[in.a].f <= regs[in.b].f, pc, in.c), true
	case rBranchLtS:
		return branchUnless(regs[in.a].s < regs[in.b].s, pc, in.c), true
	case rBranchLeS:
		return branchUnless(regs[in.a].s <= regs[in.b].s, pc, in.c), true
	case rBranchEq:
		return branchEq(regs, pc, in)
	case rModI:
		return pc, modI(regs, in)
	case rSubF:
		return pc, subF(regs, in)
	case rMulF:
		return pc, mulF(regs, in)
	case rDivF:
		return pc, divF(regs, in)
	case rEq:
		return pc, eq(regs, in)
	case rAt:
		return pc, at(regs, in)
	case rAtA:
		return pc, arenaAt(regs, in)
	case rIntToF:
		return pc, intToFloat(regs, in)
	}
	return pc, compare(regs, in)
}

// compare runs the kernel operations that cannot fail: the orderings, a
// concatenation, a length.
func compare(regs []Value, in *rinstr) bool {
	a, b := &regs[in.a], &regs[in.b]
	switch in.op {
	case rConcat:
		regs[in.c] = String(a.s + b.s)
	case rLtI:
		regs[in.c] = Bool(a.i < b.i)
	case rLeI:
		regs[in.c] = Bool(a.i <= b.i)
	case rLtF:
		regs[in.c] = Bool(a.f < b.f)
	case rLeF:
		regs[in.c] = Bool(a.f <= b.f)
	case rLtS:
		regs[in.c] = Bool(a.s < b.s)
	case rLeS:
		regs[in.c] = Bool(a.s <= b.s)
	case rLen:
		regs[in.c] = Int(int64(a.length()))
	case rLenA:
		regs[in.c] = Int(int64(arenaLength(*a)))
	}
	return true
}

// structure runs what builds values and what opens and closes a loop, a
// fallback or a using.
func (f *frame) structure(pc int, in *rinstr) (int, bool, error) {
	switch in.op {
	case rField:
		record, ok := f.regs[in.a].box.(*recordValue)
		if !ok {
			return pc, true, errNotARecord
		}
		f.regs[in.c] = record.fields[in.b]
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
