package machine

// Settling a block of items, a column at a time too: the stop, the fuel,
// the fold and the items added. Each finds the first item it cannot settle,
// and the items before it are exactly those the body would have run.

// settle settles the first limit items, and is how many it settled: the
// first it cannot — one that stops the loop, one the fuel does not pay for,
// one whose fold fails — it leaves to the body.
func (r *vecRun) settle(limit int) int {
	limit = r.stopAt(limit)
	paid := r.pay(limit)
	folded := r.foldItems(paid)
	if folded < paid {
		r.refund(folded, paid)
	}
	r.collectItems(folded)
	return folded
}

// alive is which items reach block k: nil when every item does.
func (r *vecRun) alive(k int) []bool {
	if r.plan.uniform {
		return nil
	}
	return r.f.vector.columns.alive[k]
}

// stopAt is the first item that stops the loop, or limit.
func (r *vecRun) stopAt(limit int) int {
	if r.plan.uniform {
		return limit
	}
	for k := range r.plan.chain {
		block, alive := &r.plan.blocks[k], r.alive(k)
		stops := block.end == vecStop
		branchStops := block.cond != -1 && r.plan.blocks[block.target].end == vecStop
		if !stops && !branchStops {
			continue
		}
		cond := r.blocks[k].cond
		for i := range limit {
			if alive[i] && (stops || !cond.bool(i)) {
				limit = i
				break
			}
		}
	}
	return limit
}

// pay charges the fuel of as many items from the first as it pays for, up
// to limit, and is how many. When the fuel pays for them all, the total is
// counted a block at a time; only when it does not is each item's cost
// needed, to find the first it does not pay for.
func (r *vecRun) pay(limit int) int {
	if r.plan.uniform {
		paid := limit
		if cost := r.plan.cost; cost > 0 {
			paid = int(min(uint64(limit), r.f.fuelLeft/cost))
			r.f.fuelLeft -= uint64(paid) * cost
		}
		return paid
	}
	if total := r.total(limit); total <= r.f.fuelLeft {
		r.f.fuelLeft -= total
		return limit
	}
	for i, cost := range r.costs(limit) {
		if r.f.fuelLeft < cost {
			return i
		}
		r.f.fuelLeft -= cost
	}
	return limit
}

// refund gives back the fuel of items from to to, which were paid for and
// not settled.
func (r *vecRun) refund(from, to int) {
	if r.plan.uniform {
		r.f.fuelLeft += uint64(to-from) * r.plan.cost
		return
	}
	for _, cost := range r.costs(to)[from:] {
		r.f.fuelLeft += cost
	}
}

// total is the fuel of the first limit items: each block's cost for the
// items that reach it, and a branch's target's for the items it sends there.
func (r *vecRun) total(limit int) uint64 {
	var total uint64
	for k := range r.plan.chain {
		block, alive := &r.plan.blocks[k], r.alive(k)[:limit]
		reached, away := uint64(0), uint64(0)
		cond := r.blocks[k].cond
		for i, in := range alive {
			reached += bit(in)
			if cond != nil {
				away += bit(in && !cond.bool(i))
			}
		}
		total = addFuel(total, reached*block.cost)
		if cond != nil {
			total = addFuel(total, away*r.plan.blocks[block.target].cost)
		}
	}
	return total
}

// bit is 1 for true and 0 for false, with no branch.
func bit(b bool) uint64 {
	if b {
		return 1
	}
	return 0
}

// costs is each of the first limit items' fuel: the blocks it runs, and
// where it branches, the block it goes to.
func (r *vecRun) costs(limit int) []uint64 {
	costs := r.f.vector.columns.costs[:limit]
	clear(costs)
	for k := range r.plan.chain {
		block, alive, cond := &r.plan.blocks[k], r.alive(k), r.blocks[k].cond
		var away uint64
		if cond != nil {
			away = r.plan.blocks[block.target].cost
		}
		for i := range costs {
			costs[i] += bit(alive[i]) * block.cost
			if cond != nil {
				costs[i] += bit(alive[i] && !cond.bool(i)) * away
			}
		}
	}
	return costs
}

// foldItems folds the first n items that reach the fold into the answer,
// and is how many items it got through: the first whose fold fails stops it.
func (r *vecRun) foldItems(n int) int {
	k := r.plan.foldAt
	if k < 0 {
		return n
	}
	run, alive := &r.blocks[k], r.alive(k)
	switch {
	case run.foldOp == rAddI && alive == nil:
		return r.addInts(run.fold, n)
	case run.foldOp == rAddI && run.fold.ints != nil:
		return r.addAlive(run.fold.ints, alive, n)
	}
	for i := range n {
		if alive != nil && !alive[i] {
			continue
		}
		var ok bool
		if r.acc.kind == vecInt {
			r.acc.i, ok = foldInt(run.foldOp, r.acc.i, run.fold.int(i))
		} else {
			r.acc.f, ok = foldFloat(run.foldOp, r.acc.f, run.fold.float(i))
		}
		if !ok {
			return i
		}
	}
	return n
}

// foldInt and foldFloat are the fold's step: the answer as it was when the
// step fails.
func foldInt(op rop, answer, value int64) (int64, bool) {
	next, ok := intOp(op, answer, value)
	if !ok {
		return answer, false
	}
	return next, true
}

func foldFloat(op rop, answer, value float64) (float64, bool) {
	next, ok := floatOp(op, answer, value)
	if !ok {
		return answer, false
	}
	return next, true
}

// addInts is a sum of ints, the fold most loops are.
func (r *vecRun) addInts(values *vecValue, n int) int {
	answer := r.acc.i
	defer func() { r.acc.i = answer }()
	if values.ints == nil {
		for i := range n {
			sum := answer + values.i
			if (answer^sum)&(values.i^sum) < 0 {
				return i
			}
			answer = sum
		}
		return n
	}
	for i, value := range values.ints[:n] {
		sum := answer + value
		if (answer^sum)&(value^sum) < 0 {
			return i
		}
		answer = sum
	}
	return n
}

// addAlive is a sum of the ints of the items that reach the fold: an item
// that does not adds 0, with no branch.
func (r *vecRun) addAlive(values []int64, alive []bool, n int) int {
	answer := r.acc.i
	defer func() { r.acc.i = answer }()
	for i, value := range values[:n] {
		value *= int64(bit(alive[i]))
		sum := answer + value
		if (answer^sum)&(value^sum) < 0 {
			return i
		}
		answer = sum
	}
	return n
}

// collectItems adds the first n items that reach the collect to what the
// loop builds: all at once when every item does.
func (r *vecRun) collectItems(n int) {
	k := r.plan.collectAt
	if k < 0 {
		return
	}
	value, alive := r.blocks[k].collect, r.alive(k)
	if alive == nil {
		r.addAll(value, n)
		return
	}
	for i := range n {
		if alive[i] {
			r.add(value, i)
		}
	}
}

// addAll adds the value of each of the first n items.
func (r *vecRun) addAll(value *vecValue, n int) {
	switch {
	case value.ints != nil:
		r.out.ints = append(r.out.ints, value.ints[:n]...)
	case value.floats != nil:
		r.out.floats = append(r.out.floats, value.floats[:n]...)
	case value.bools != nil:
		r.out.bools = append(r.out.bools, value.bools[:n]...)
	default:
		for i := range n {
			r.add(value, i)
		}
	}
}

// add adds item i's value to what the loop builds.
func (r *vecRun) add(value *vecValue, i int) {
	switch value.kind {
	case vecInt:
		r.out.ints = append(r.out.ints, value.int(i))
	case vecFloat:
		r.out.floats = append(r.out.floats, value.float(i))
	default:
		r.out.bools = append(r.out.bools, value.bool(i))
	}
}
