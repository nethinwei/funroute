package machine

// vecOp is one operation of a run: its operands and the column it writes.
type vecOp struct {
	op      rop
	a, b    *vecValue
	dst     *vecValue
	outKind vecKind
}

// vecBlockRun is a block of the body as one run of the vector reads it.
type vecBlockRun struct {
	ops           []vecOp
	fold          *vecValue
	foldOp        rop
	collect, cond *vecValue
}

// vecRun is one loop's run of the vector: the plan's registers as columns
// and values, and the answer folded so far.
type vecRun struct {
	f      *frame
	plan   *vecPlan
	loop   *regLoop
	item   vecValue
	cols   []vecValue
	consts []vecValue
	blocks []vecBlockRun
	acc    vecValue
	out    *arrayBuilder
}

// startVector reads the plan against what the loop has: the kind of each
// register, the answer and what the loop builds. It declines a plan whose
// kinds are not what the vector computes with, and a run held to a stack
// limit, whose blocks check it.
func (f *frame) startVector(loop *regLoop, plan *vecPlan) (*vecRun, bool) {
	if loop.ints == nil && loop.floats == nil {
		return nil, false
	}
	if f.vector == nil {
		f.vector = new(vectorState)
	}
	run := &f.vector.run
	*run = vecRun{f: f, plan: plan, loop: loop, cols: run.cols[:0], consts: run.consts[:0], blocks: run.blocks[:0], out: loop.out}
	run.item.kind = vecFloat
	if loop.ints != nil {
		run.item.kind = vecInt
	}
	// Operands point into cols and consts: both are made as long as they
	// will get, first.
	run.cols = append(run.cols, make([]vecValue, len(plan.columns))...)
	operands := 0
	for _, block := range plan.blocks {
		operands += 2*len(block.ops) + 3
	}
	if cap(run.consts) < operands {
		run.consts = make([]vecValue, 0, operands)
	}
	if !run.resolveAnswer() {
		return nil, false
	}
	if cap(run.blocks) < len(plan.blocks) {
		run.blocks = make([]vecBlockRun, len(plan.blocks))
	}
	run.blocks = run.blocks[:len(plan.blocks)]
	for k, block := range plan.blocks {
		if !run.resolveBlock(&run.blocks[k], block) {
			return nil, false
		}
	}
	f.vector.columns.aliveFor(len(plan.blocks))
	return run, true
}

// resolveAnswer reads the answer a fold starts from.
func (r *vecRun) resolveAnswer() bool {
	if r.plan.acc < 0 {
		return true
	}
	value, ok := scalarOf(r.f.valueAt(r.plan.acc, r.plan.accKind))
	r.acc = value
	return ok && value.kind != vecBool
}

// scalarOf is a register's value as one value for every item.
func scalarOf(v Value) (vecValue, bool) {
	switch v.kind {
	case IntKind:
		return vecValue{kind: vecInt, i: v.i}, true
	case FloatKind:
		return vecValue{kind: vecFloat, f: v.f}, true
	case BoolKind:
		return vecValue{kind: vecBool, b: v.b}, true
	}
	return vecValue{}, false
}

// operand is register reg, holding a value of kind, as the run reads it: the
// items, a column the body wrote, or the value the register holds
// throughout the loop.
func (r *vecRun) operand(reg int32, kind Kind) (*vecValue, bool) {
	if reg == r.plan.item {
		return &r.item, true
	}
	if c, ok := r.plan.columns[reg]; ok {
		return &r.cols[c], true
	}
	value, ok := scalarOf(r.f.valueAt(reg, kind))
	if !ok {
		return nil, false
	}
	r.consts = append(r.consts, value)
	return &r.consts[len(r.consts)-1], true
}

// resolveBlock reads one block's operations, fold, item and branch into
// run, whose operations' memory is kept from run to run.
func (r *vecRun) resolveBlock(run *vecBlockRun, block vecBlock) bool {
	run.ops, run.fold, run.collect, run.cond = run.ops[:0], nil, nil, nil
	for i, in := range block.ops {
		op, ok := r.resolveOp(in, block.kinds[i])
		if !ok {
			return false
		}
		run.ops = append(run.ops, op)
	}
	ok := true
	if block.folds {
		run.foldOp = block.fold.op
		run.fold, ok = r.operand(block.fold.b, block.foldKind)
		ok = ok && run.fold.kind == r.acc.kind && foldKind(block.fold.op) == r.acc.kind
	}
	if ok && block.collect != -1 {
		run.collect, ok = r.operand(block.collect, block.collectKind)
		ok = ok && run.collect.kind == r.out.vecKind()
	}
	if ok && block.cond != -1 {
		run.cond, ok = r.operand(block.cond, BoolKind)
		ok = ok && run.cond.kind == vecBool
	}
	return ok
}

// resolveOp reads one operation, and gives the column it writes its kind.
func (r *vecRun) resolveOp(in rinstr, kinds opKinds) (vecOp, bool) {
	a, ok := r.operand(in.a, kinds.a)
	if !ok {
		return vecOp{}, false
	}
	b := a
	if in.op != rMove && in.op != rIntToF {
		if b, ok = r.operand(in.b, kinds.b); !ok {
			return vecOp{}, false
		}
	}
	kind, ok := resultKind(in.op, a.kind, b.kind)
	if !ok {
		return vecOp{}, false
	}
	c := r.plan.columns[in.c]
	s := &r.f.vector.columns
	s.column(c, kind)
	r.cols[c] = vecValue{kind: kind}
	switch kind {
	case vecInt:
		r.cols[c].ints = s.ints[c]
	case vecFloat:
		r.cols[c].floats = s.floats[c]
	default:
		r.cols[c].bools = s.bools[c]
	}
	return vecOp{op: in.op, a: a, b: b, dst: &r.cols[c], outKind: kind}, true
}

// resultKind is what an operation of operands of kinds a and b makes, and
// false for operands it does not take.
func resultKind(op rop, a, b vecKind) (vecKind, bool) {
	switch op {
	case rMove:
		return a, true
	case rAddI, rSubI, rMulI, rDivI, rModI:
		return vecInt, a == vecInt && b == vecInt
	case rAddF, rSubF, rMulF, rDivF:
		return vecFloat, a == vecFloat && b == vecFloat
	case rIntToF:
		return vecFloat, a == vecInt
	case rLtI, rLeI:
		return vecBool, a == vecInt && b == vecInt
	case rLtF, rLeF:
		return vecBool, a == vecFloat && b == vecFloat
	case rEq:
		return vecBool, a == b
	}
	return 0, false
}

// foldKind is the kind a fold's operation computes with.
func foldKind(op rop) vecKind {
	switch op {
	case rAddF, rSubF, rMulF, rDivF:
		return vecFloat
	}
	return vecInt
}

// vecKind is what the builder holds, as a column's kind: vecNone for an
// element no column holds.
func (b *arrayBuilder) vecKind() vecKind {
	switch b.elem.kind {
	case IntKind:
		return vecInt
	case FloatKind:
		return vecFloat
	case BoolKind:
		return vecBool
	}
	return vecNone
}

// load makes the item operand this block's n items from base.
func (r *vecRun) load(loop *regLoop, base, n int) {
	if loop.ints != nil {
		r.item.ints = loop.ints[base : base+n]
	} else {
		r.item.floats = loop.floats[base : base+n]
	}
}

// columns runs each block's operations over the items that reach it, and
// is how many items none of them fails on: the first that fails, and those
// after it, are left to the body.
func (r *vecRun) columns(n int) int {
	limit := n
	alive := r.f.vector.columns.alive
	for k := range r.plan.blocks {
		switch {
		case k == 0:
			fill(alive[0][:n], true)
		case r.plan.blocks[k-1].end == vecFall:
			r.reach(alive[k-1], alive[k], k-1, n)
		default:
			continue // reached by a branch alone, and does nothing
		}
		for i := range r.blocks[k].ops {
			limit = r.blocks[k].ops[i].eval(limit, alive[k])
		}
	}
	return limit
}

// reach is which items fall from block k into the next: those that reach
// it and do not branch away.
func (r *vecRun) reach(from, to []bool, k, n int) {
	cond := r.blocks[k].cond
	switch {
	case cond == nil:
		copy(to[:n], from[:n])
	case cond.bools != nil:
		for i, in := range cond.bools[:n] {
			to[i] = from[i] && in
		}
	default:
		for i := range n {
			to[i] = from[i] && cond.b
		}
	}
}

func fill(mask []bool, value bool) {
	for i := range mask {
		mask[i] = value
	}
}

// finish writes the answer folded so far back into its register.
func (r *vecRun) finish() {
	switch {
	case r.plan.acc < 0:
	case r.acc.kind == vecInt:
		r.f.ints[r.plan.acc] = r.acc.i
	default:
		r.f.floats[r.plan.acc] = r.acc.f
	}
}
