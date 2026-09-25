package machine

import "github.com/nethinwei/funroute/internal/kit"

// regLoop is one active loop of the register machine.
type regLoop struct {
	source Value
	// keys is a dictionary walk's keys, sorted; ints and floats an array
	// walk's source when it is one of those, bound without asking the Value.
	keys          []string
	ints          []int64
	floats        []float64
	length, index int
	site          *rloop
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
// then.
func (f *frame) regLoopInit(pc int, in *rinstr) int {
	site := &f.runtime.reg.loops[in.c]
	source := f.regs[in.a]
	length := source.length()
	if length == 0 {
		f.regs[site.dst] = f.emptyResult(site, in)
		return int(site.exit)
	}
	f.loops = append(f.loops, regLoop{source: source, length: length, site: site})
	loop := &f.loops[len(f.loops)-1]
	if site.key >= 0 {
		loop.keys = source.keys()
	} else {
		loop.ints, _ = source.box.([]int64)
		loop.floats, _ = source.box.([]float64)
	}
	switch {
	case site.acc >= 0:
		f.regs[site.acc] = f.regs[in.b]
	case site.spread:
		loop.out = f.loops[len(f.loops)-2].out
	case site.typ.kind == DictKind:
		loop.dict = newDictBuilder(*site.typ.elem, length)
	default:
		loop.output = newArrayBuilder(*site.typ.elem, length)
		loop.out = &loop.output
	}
	f.bindRegs(loop, 0)
	return pc
}

// emptyResult is what a loop over nothing gives: the seed of a fold, and
// otherwise an empty array or dictionary — nothing for a clause whose items
// go to the clause outside it.
func (f *frame) emptyResult(site *rloop, in *rinstr) Value {
	switch {
	case site.acc >= 0:
		return f.regs[in.b]
	case site.spread:
		return Value{}
	}
	// Neither can fail: loading proved the element type concrete.
	if site.typ.kind == DictKind {
		empty, _ := Dict(*site.typ.elem, nil)
		return empty
	}
	empty, _ := Array(*site.typ.elem, nil)
	return empty
}

// bindRegs binds the loop's names to the item at index.
func (f *frame) bindRegs(loop *regLoop, index int) {
	switch {
	case loop.ints != nil:
		f.regs[loop.site.item] = Value{kind: IntKind, i: loop.ints[index]}
	case loop.floats != nil:
		f.regs[loop.site.item] = Value{kind: FloatKind, f: loop.floats[index]}
	case loop.keys != nil:
		value, _ := loop.source.lookup(loop.keys[index])
		f.regs[loop.site.item] = value
		f.regs[loop.site.key] = String(loop.keys[index])
	default:
		f.regs[loop.site.item] = loop.source.at(index)
	}
}

// collect adds register value to what the comprehension builds, under the
// key in register key unless it is -1.
func (f *frame) collect(value, key int32) {
	loop := &f.loops[len(f.loops)-1]
	if key >= 0 {
		loop.dict.add(f.regs[key].s, f.regs[value])
		return
	}
	// An array of ints or floats is appended to as it is, not through add.
	switch out := loop.out; {
	case out.ints != nil:
		out.ints = append(out.ints, f.regs[value].i)
	case out.floats != nil:
		out.floats = append(out.floats, f.regs[value].f)
	default:
		out.add(f.regs[value])
	}
}

// collectNext is rCollectNext: the value, and the next iteration.
func (f *frame) collectNext(pc int, in *rinstr) (int, error) {
	f.collect(in.a, -1)
	return f.regLoopNext(pc, in.b)
}

// regLoopNext goes back to the body while items are left; after the last, the
// loop's result goes in its register. Going back, it pays for the body's
// block itself and goes on past the block's fuel operation, which is left to
// run — and fail — only when the fuel is short.
func (f *frame) regLoopNext(pc int, body int32) (int, error) {
	last := len(f.loops) - 1
	loop := &f.loops[last]
	loop.index++
	if loop.index < loop.length {
		f.bindRegs(loop, loop.index)
		if start := &f.runtime.reg.code[body]; start.op == rFuel && f.charge(start) {
			return int(body) + 1, nil
		}
		return int(body), nil
	}
	result, err := f.loopValue(loop)
	if err != nil {
		return pc, err
	}
	f.regs[loop.site.dst] = result
	f.loops[last] = regLoop{}
	f.loops = f.loops[:last]
	return pc, nil
}

// loopBreak ends the innermost loop with register a as its answer, and is
// where the run goes on: the loop's exit.
func (f *frame) loopBreak(in *rinstr) int {
	last := len(f.loops) - 1
	site := f.loops[last].site
	f.regs[site.dst] = f.regs[in.a]
	f.loops[last] = regLoop{}
	f.loops = f.loops[:last]
	return int(site.exit)
}

// loopValue is a finished loop's value: the accumulator, the array or the
// dictionary — nothing for a clause whose items went to the clause outside.
func (f *frame) loopValue(loop *regLoop) (Value, error) {
	switch {
	case loop.site.acc >= 0:
		return f.regs[loop.site.acc], nil
	case loop.site.spread:
		return Value{}, nil
	case loop.site.typ.kind == DictKind:
		return loop.dict.finish()
	}
	return loop.output.finish(), nil
}

// dictBuilder builds a comprehension's dictionary as it goes: an int's or a
// float's straight into its native map, anything else through a map of
// values packed at the end. A key made twice is refused when the loop ends,
// as a literal's is refused: one key, one value, whichever door the
// dictionary came in by — and after every item, as the loop would have run.
type dictBuilder struct {
	elem   Type
	ints   map[string]int64
	floats map[string]float64
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
	default:
		builder.values = make(map[string]Value, capacity)
	}
	return builder
}

func (d *dictBuilder) add(key string, value Value) {
	var before, after int
	switch {
	case d.ints != nil:
		before = len(d.ints)
		d.ints[key] = value.i
		after = len(d.ints)
	case d.floats != nil:
		before = len(d.floats)
		d.floats[key] = value.f
		after = len(d.floats)
	default:
		before = len(d.values)
		d.values[key] = value
		after = len(d.values)
	}
	if after == before && !d.twiced {
		d.twice, d.twiced = key, true
	}
}

func (d *dictBuilder) finish() (Value, error) {
	switch {
	case d.twiced:
		return Value{}, kit.Errorf(ErrDomain, "the comprehension produced the key %q twice", d.twice)
	case d.ints != nil:
		return Value{kind: DictKind, box: d.ints}, nil
	case d.floats != nil:
		return Value{kind: DictKind, box: d.floats}, nil
	}
	return packDict(d.elem, d.values), nil
}
