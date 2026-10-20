package machine

import (
	"github.com/nethinwei/funroute/internal/kit"
	"github.com/nethinwei/funroute/internal/money"
)

// regLoop is one active loop of the register machine.
type regLoop struct {
	source Value
	// keys is a dictionary walk's keys, sorted; ints, floats, strings and
	// bools an array walk's source when it is one of those — its own, or in
	// an arena slot — bound without asking the Value.
	keys          []string
	ints          []int64
	floats        []float64
	strings       []string
	bools         []bool
	length, index int
	// vecAt is the item a loop that may stop hands to the vector, after the
	// body ran the ones before it (scalarFirst); 0 when it does not.
	vecAt int
	site  *rloop
	// view is an array of plain records in the host's memory.
	view *recordsView
	// out is where the comprehension's items go: its own output, or, for
	// the inner clause of a nested one, the outer clause's. A frame's loops
	// never move — the slice is made as deep as the program nests — so the
	// pointer holds for the loop's life.
	out    *arrayBuilder
	output arrayBuilder
	dict   dictBuilder
}

// regLoopInit starts a loop. An empty source goes straight to the exit with
// the loop's result — the seed, or an empty container — which is only built
// then. A body the vector runs is run by it as far as it settles items, and
// the body goes on from there.
func (f *frame) regLoopInit(pc int, in *rinstr) (int, error) {
	site := &f.runtime.reg.loops[in.c]
	f.loops = append(f.loops, regLoop{source: f.regs[in.a], site: site})
	last := len(f.loops) - 1
	loop := &f.loops[last]
	if loop.walk(site.key >= 0) == 0 {
		f.loops[last] = regLoop{}
		f.loops = f.loops[:last]
		if site.acc >= 0 {
			f.copyReg(site.dst, in.b, site.typ.kind)
		} else {
			f.regs[site.dst] = f.emptyResult(site)
		}
		return int(site.exit), nil
	}
	f.startOutput(loop, in)
	switch {
	case site.vec == nil:
	case site.vec.stops:
		// A loop of scalarFirst items or fewer never gets there.
		loop.vecAt = scalarFirst
	case loop.length < vecShortest:
		// Starting the vector costs more than the body saves on so few.
	default:
		return f.vectorFrom(loop, pc)
	}
	f.bindRegs(loop, loop.index)
	return pc, nil
}

// vectorFrom hands the loop to the vector from the item it is at, and goes
// on at body with the item the vector left, or past the loop.
func (f *frame) vectorFrom(loop *regLoop, body int) (int, error) {
	next, resume, finished, err := f.vectorLoop(loop, loop.site.vec, body)
	if err != nil {
		return body, err
	}
	if finished {
		return f.endLoop(int(loop.site.exit))
	}
	loop.index = next
	f.bindRegs(loop, loop.index)
	return resume, nil
}

// walk reads the loop's source — its keys, or its native backing — and is
// how many items it has.
func (l *regLoop) walk(keyed bool) int {
	if keyed {
		l.keys = l.source.keys()
		l.length = len(l.keys)
		return l.length
	}
	switch box := l.source.box.(type) {
	case *int64:
		l.ints, l.length = itemsAt(box, l.source.i), int(l.source.i)
	case *[]int64:
		l.ints, l.length = *box, len(*box)
	case *float64:
		l.floats, l.length = itemsAt(box, l.source.i), int(l.source.i)
	case *[]float64:
		l.floats, l.length = *box, len(*box)
	case *string:
		l.strings, l.length = itemsAt(box, l.source.i), int(l.source.i)
	case *[]string:
		l.strings, l.length = *box, len(*box)
	case *bool:
		l.bools, l.length = itemsAt(box, l.source.i), int(l.source.i)
	case *[]bool:
		l.bools, l.length = *box, len(*box)
	case *recordsView:
		l.view, l.length = box, box.length
	default:
		l.length = l.source.length()
	}
	return l.length
}

// startOutput readies what the loop builds: a fold's answer, the outer
// clause's array — with room for every item still to come — a dictionary,
// or its own array, in its slot when it has one.
func (f *frame) startOutput(loop *regLoop, in *rinstr) {
	site := loop.site
	switch {
	case site.acc >= 0:
		f.copyReg(site.acc, in.b, site.typ.kind)
	case site.spread:
		outer := &f.loops[len(f.loops)-2]
		loop.out = outer.out
		loop.out.grow(expected(outer.length-outer.index, loop.length))
	case site.typ.kind == DictKind:
		loop.dict = newDictBuilder(*site.typ.elem, loop.length)
	default:
		switch slot := f.slotFor(site.built); {
		case slot != nil:
			loop.output = builderIn(slot, *site.typ.elem, loop.length)
		case site.collectsItem && loop.view != nil:
			// The items stay where they are: what is built is which.
			loop.output = arrayBuilder{elem: *site.typ.elem, of: loop.view, picked: make([]int, 0, loop.length)}
		default:
			loop.output = newArrayBuilder(*site.typ.elem, loop.length)
		}
		loop.out = &loop.output
	}
}

// expected is how many items to make room for when outer items are left,
// each with inner items: no more than maxGrow at once.
func expected(outer, inner int) int {
	if inner > 0 && outer > maxGrow/inner {
		return maxGrow
	}
	return min(outer*inner, maxGrow)
}

// maxGrow is the most items one estimate makes room for; past it the array
// grows as it fills.
const maxGrow = 1 << 20

// emptyResult is what a mapping loop over nothing gives: an empty array or
// dictionary — nothing for a clause whose items go to the clause outside it.
func (f *frame) emptyResult(site *rloop) Value {
	if site.spread {
		return Value{}
	}
	if slot := f.slotFor(site.built); slot != nil {
		empty := builderIn(slot, *site.typ.elem, 0)
		return finishIn(slot, &empty)
	}
	// Neither can fail: loading proved the element type concrete.
	if site.typ.kind == DictKind {
		empty, _ := Dict(*site.typ.elem, nil)
		return empty
	}
	empty, _ := Array(*site.typ.elem, nil)
	return empty
}

// bindRegs binds the loop's names to the item at index, each in the file
// of its kind.
func (f *frame) bindRegs(loop *regLoop, index int) {
	switch item := loop.site.item; {
	case loop.site.itemsInPlace:
		f.loadItem(loop, index)
	case loop.ints != nil:
		f.ints[item] = loop.ints[index]
	case loop.floats != nil:
		f.floats[item] = loop.floats[index]
	case loop.strings != nil:
		f.regs[item] = Value{kind: StringKind, s: loop.strings[index]}
	case loop.bools != nil:
		f.ints[item] = word(loop.bools[index])
	case loop.view != nil:
		f.regs[item] = loop.view.record(index)
	case loop.keys != nil:
		value, _ := loop.source.lookup(loop.keys[index])
		f.setValue(item, value)
		f.regs[loop.site.key] = String(loop.keys[index])
	default:
		f.setValue(item, loop.source.at(index))
	}
}

// loadItem loads the fields the body reads of the item at index into their
// registers: out of the host's memory for a view, and out of the record
// otherwise. The value flow proved the item is read for those alone, so no
// record is made of it.
func (f *frame) loadItem(loop *regLoop, index int) {
	site := loop.site
	if loop.view != nil {
		loop.view.loadInto(index, site.fields, site.promoted, &f.banks)
		return
	}
	record, _ := loop.source.at(index).box.(*recordValue)
	for i, field := range site.fields {
		f.setValue(site.promoted[i], record.fields[field])
	}
}

// collect adds register value to what the comprehension builds, under the
// key in register key unless it is -1: a dictionary's entry, of kind, out
// of its file.
func (f *frame) collect(value, key int32, kind Kind) {
	loop := &f.loops[len(f.loops)-1]
	if key >= 0 {
		loop.dict.add(f.regs[key].s, f.valueAt(value, kind))
		return
	}
	// An int, a bool or a float is collected out of its file (collectInt,
	// collectFloat): what reaches here is in the values.
	loop.out.add(f.regs[value])
}

// collectItem adds the innermost loop's item as it is: where it is in the
// host's slice, for a loop over a view, and otherwise the record itself.
func (f *frame) collectItem() {
	loop := &f.loops[len(f.loops)-1]
	if loop.view == nil {
		loop.out.add(loop.source.at(loop.index))
		return
	}
	at := loop.index
	if loop.view.perm != nil {
		at = loop.view.perm[at]
	}
	loop.out.picked = append(loop.out.picked, at)
}

// collectInt adds the int or the bool in register value to the array the
// comprehension builds.
func (f *frame) collectInt(value int32) {
	out := f.loops[len(f.loops)-1].out
	if out.elem.kind == BoolKind {
		out.bools = append(out.bools, f.ints[value] != 0)
		return
	}
	out.ints = append(out.ints, f.ints[value])
}

// collectFloat adds the float in register value.
func (f *frame) collectFloat(value int32) {
	out := f.loops[len(f.loops)-1].out
	out.floats = append(out.floats, f.floats[value])
}

// collectNext is rCollectNext: the value, and the next iteration.
func (f *frame) collectNext(in *rinstr) (int, error) {
	f.collect(in.a, -1, InvalidKind)
	return f.regLoopNext(int(in.c), in.b)
}

// regLoopNext goes back to the body while items are left, counting the turn
// (limit.go); after the last, the loop's result goes in its register.
func (f *frame) regLoopNext(pc int, body int32) (int, error) {
	last := len(f.loops) - 1
	loop := &f.loops[last]
	loop.index++
	if loop.index < loop.length {
		if f.watching() {
			if err := f.turn(1); err != nil {
				return pc, err
			}
		}
		if loop.index == loop.vecAt {
			return f.vectorFrom(loop, int(body))
		}
		f.bindRegs(loop, loop.index)
		return int(body), nil
	}
	return f.endLoop(pc)
}

// endLoop ends the innermost loop, its result in its register, and goes on
// at pc.
func (f *frame) endLoop(pc int) (int, error) {
	last := len(f.loops) - 1
	if site := f.loops[last].site; site.acc >= 0 {
		f.copyReg(site.dst, site.acc, site.typ.kind)
	} else {
		result, err := f.loopValue(&f.loops[last])
		if err != nil {
			return pc, err
		}
		f.regs[site.dst] = result
	}
	f.loops[last] = regLoop{}
	f.loops = f.loops[:last]
	return pc, nil
}

// loopBreak ends the innermost loop with register a as its answer, and is
// where the run goes on: the loop's exit.
func (f *frame) loopBreak(in *rinstr) int {
	last := len(f.loops) - 1
	site := f.loops[last].site
	f.copyReg(site.dst, in.a, site.typ.kind)
	f.loops[last] = regLoop{}
	f.loops = f.loops[:last]
	return int(in.b)
}

// loopValue is a finished mapping loop's value: the array or the
// dictionary — nothing for a clause whose items went to the clause outside.
func (f *frame) loopValue(loop *regLoop) (Value, error) {
	switch {
	case loop.site.spread:
		return Value{}, nil
	case loop.site.typ.kind == DictKind:
		return loop.dict.finish()
	}
	if slot := f.slotFor(loop.site.built); slot != nil {
		return finishIn(slot, &loop.output), nil
	}
	return loop.output.finish(), nil
}

// dictBuilder builds a dictionary as it goes, a comprehension's or a
// literal's: a native kind's values straight into the native map the
// dictionary is, anything else into a map of values it owns. A key made twice
// is refused when the loop ends, as a literal's is refused: one key, one
// value, whichever door the dictionary came in by — and after every item, as
// the loop would have run.
type dictBuilder struct {
	elem   Type
	ints   map[string]int64
	floats map[string]float64
	bools  map[string]bool
	texts  map[string]string
	monies map[string]money.Money
	values map[string]Value
	twice  string
	twiced bool
}

func newDictBuilder(elem Type, capacity int) dictBuilder {
	builder := dictBuilder{elem: elem}
	switch elem.kind {
	case IntKind:
		builder.ints = make(map[string]int64, capacity)
	case FloatKind:
		builder.floats = make(map[string]float64, capacity)
	case BoolKind:
		builder.bools = make(map[string]bool, capacity)
	case StringKind:
		builder.texts = make(map[string]string, capacity)
	case MoneyKind:
		builder.monies = make(map[string]money.Money, capacity)
	default:
		builder.values = make(map[string]Value, capacity)
	}
	return builder
}

func (d *dictBuilder) add(key string, value Value) {
	var fresh bool
	switch {
	case d.ints != nil:
		fresh = added(d.ints, key, value.i)
	case d.floats != nil:
		fresh = added(d.floats, key, value.f)
	case d.bools != nil:
		fresh = added(d.bools, key, value.b)
	case d.texts != nil:
		fresh = added(d.texts, key, value.s)
	case d.monies != nil:
		fresh = added(d.monies, key, money.Make(value.s, value.i))
	default:
		fresh = added(d.values, key, value)
	}
	if !fresh && !d.twiced {
		d.twice, d.twiced = key, true
	}
}

// added sets key in entries, and reports whether it was not there before.
func added[T any](entries map[string]T, key string, value T) bool {
	n := len(entries)
	entries[key] = value
	return len(entries) > n
}

func (d *dictBuilder) finish() (Value, error) {
	var box any
	switch {
	case d.twiced:
		return Value{}, kit.Errorf(ErrDomain, "the comprehension produced the key %q twice", d.twice)
	case d.ints != nil:
		box = d.ints
	case d.floats != nil:
		box = d.floats
	case d.bools != nil:
		box = d.bools
	case d.texts != nil:
		box = d.texts
	case d.monies != nil:
		box = d.monies
	default:
		// The builder's own map: nothing else holds it, so it is not copied.
		box = &nestedDict{elem: d.elem, entries: d.values}
	}
	return Value{kind: DictKind, box: box}, nil
}
