package machine

import "slices"

// A loop whose body is a few int, float and bool operations — a filter, a
// map, a fold, a count, a stop — runs a column at a time: each operation over
// a block of items at once, then each item's effects in order: its fold into
// the answer, the item it adds (regvm_vector.go). That is the shape of most
// comprehensions and aggregates, and the one where the interpreter's
// per-operation dispatch cost the most.
//
// It answers as the loop does, to the word: where an item would fail or stop
// the loop, the vector hands the loop back to the ordinary body at that
// item, which runs it — and fails, stops or goes on — as it always did.

// vecEnd is how a block of the body ends: falling into the next, the item
// done, or the loop stopped.
type vecEnd uint8

const (
	vecFall vecEnd = iota
	vecDone
	vecStop
)

// vecBlock is one basic block of the body: the operations it runs on every
// item that reaches it, the fold into the answer or the item it collects,
// and the branch out of it — to target when cond is false.
type vecBlock struct {
	ops     []rinstr
	fold    rinstr
	folds   bool
	collect int32
	cond    int32
	target  int
	end     vecEnd
}

// vecPlan is a loop body the vector runs: its blocks in order — the first is
// the body's start, each falls into the next — the registers it computes
// columns of, and the loop's item and answer.
type vecPlan struct {
	blocks    []vecBlock
	columns   map[int32]int
	item, acc int32
	// body is where the body starts, past a prelude that runs once before
	// the first item and only jumps there — all's, which lays its stop out
	// ahead of the body.
	body int32
	// chain is how many blocks items fall through, from the first; the
	// rest are reached by a branch. foldAt and collectAt are the chain's
	// block that folds and the one that collects, or -1. uniform says no
	// block branches or stops: every item reaches every block.
	chain             int
	foldAt, collectAt int
	uniform           bool
	// stops says some block stops the loop: its first blocks of items are
	// small (regvm_vector.go).
	stops bool
}

// maxVecBlocks bounds the body a vector takes.
const maxVecBlocks = 8

// elementwise are the operations a vector runs a column at a time.
var elementwise = map[rop]bool{
	rMove: true, rAddI: true, rSubI: true, rMulI: true, rDivI: true, rModI: true,
	rAddF: true, rSubF: true, rMulF: true, rDivF: true,
	rLtI: true, rLeI: true, rLtF: true, rLeF: true, rEq: true, rIntToF: true,
}

// branchCompare is the comparison each fused branch makes.
var branchCompare = map[rop]rop{
	rBranchLtI: rLtI, rBranchLeI: rLeI, rBranchLtF: rLtF, rBranchLeF: rLeF, rBranchEq: rEq,
}

// vectorize gives each array loop of the register form whose body a vector
// can run its plan.
func (l *lowerer) vectorize() {
	for init, in := range l.out.code {
		if in.op != rLoopInit || l.out.loops[in.c].key >= 0 {
			continue
		}
		body := bodyOf(l.out.code, init)
		planner := &vecPlanner{code: l.out.code, starts: l.out.starts, body: body, byPC: map[int]int{}, hidden: -2}
		loop := &l.out.loops[in.c]
		planner.plan = &vecPlan{columns: map[int32]int{}, item: loop.item, acc: loop.acc, body: int32(body)}
		if planner.walk() && planner.check() {
			loop.vec = planner.plan
		}
	}
}

// bodyOf is where the body of the loop started at init starts, past a
// prelude that only jumps to it.
func bodyOf(code []rinstr, init int) int {
	if first := code[init+1]; first.op == rJump {
		return int(first.a)
	}
	return init + 1
}

// vecPlanner reads a loop body into a plan.
type vecPlanner struct {
	code   []rinstr
	starts []bool
	body   int
	byPC   map[int]int
	plan   *vecPlan
	hidden int32 // the next register a fused branch's comparison is given
}

// walk reads the blocks from the body's start, falling from one into the
// next, and each branch's target, until each path is done or stopped.
func (p *vecPlanner) walk() bool {
	for pc := p.body; pc >= 0; {
		next, ok := p.block(pc)
		if !ok {
			return false
		}
		pc = next
	}
	// terminal may read another block into the plan, and move the blocks:
	// each is found again by its index, never held across the call. A block
	// it reads ends the item or the loop, so has no branch to resolve.
	for i := range p.plan.blocks {
		if p.plan.blocks[i].cond != -1 {
			target, ok := p.terminal(p.plan.blocks[i].target)
			if !ok {
				return false
			}
			p.plan.blocks[i].target = target
		}
	}
	return len(p.plan.blocks) <= maxVecBlocks
}

// terminal is the block at pc, which a branch goes to: one with nothing to
// do but end the item, or stop the loop.
func (p *vecPlanner) terminal(pc int) (int, bool) {
	if index, ok := p.byPC[pc]; ok {
		block := p.plan.blocks[index]
		return index, len(block.ops) == 0 && !block.folds && block.collect == -1 && block.cond == -1 && block.end != vecFall
	}
	if _, ok := p.block(pc); !ok {
		return 0, false
	}
	return p.terminal(pc)
}

// block reads the block at pc into the plan, and is where it falls — -1
// when it ends the item or the loop.
func (p *vecPlanner) block(pc int) (int, bool) {
	if pc >= len(p.code) || !p.startsAt(pc) {
		return -1, false
	}
	block := vecBlock{collect: -1, cond: -1}
	p.byPC[pc] = len(p.plan.blocks)
	for start := pc; pc < len(p.code); pc++ {
		if pc != start && p.startsAt(pc) {
			return p.add(block, pc)
		}
		if next, ended, ok := p.read(&block, p.code[pc], pc); ended || !ok {
			if !ok {
				return -1, false
			}
			return p.add(block, next)
		}
	}
	return -1, false
}

// startsAt reports an operation a block starts at.
func (p *vecPlanner) startsAt(pc int) bool { return pc < len(p.starts) && p.starts[pc] }

func (p *vecPlanner) add(block vecBlock, next int) (int, bool) {
	p.plan.blocks = append(p.plan.blocks, block)
	return next, true
}

// read takes one operation into the block, and says whether it ends it and
// where the block goes on.
func (p *vecPlanner) read(block *vecBlock, in rinstr, pc int) (int, bool, bool) {
	switch {
	case p.folding(in):
		block.fold, block.folds = in, true
	case elementwise[in.op]:
		block.ops = append(block.ops, in)
	case in.op == rCollect && in.b < 0:
		block.collect = in.a
	case in.op == rCollectNext && int(in.b) == p.body:
		block.collect, block.end = in.a, vecDone
		return -1, true, true
	case in.op == rLoopNext && int(in.a) == p.body:
		block.end = vecDone
		return -1, true, true
	case in.op == rLoopBreak:
		block.end = vecStop
		return -1, true, true
	case in.op == rBranch:
		block.cond, block.target = in.a, int(in.b)
		return pc + 1, true, true
	case branchCompare[in.op] != rInvalid:
		block.ops = append(block.ops, rinstr{op: branchCompare[in.op], a: in.a, b: in.b, c: p.hidden})
		block.cond, block.target = p.hidden, int(in.c)
		p.hidden--
		return pc + 1, true, true
	default:
		return 0, false, false
	}
	return 0, false, true
}

// folding reports the loop's fold of a value into its answer.
func (p *vecPlanner) folding(in rinstr) bool {
	switch in.op {
	case rAddI, rSubI, rMulI, rDivI, rModI, rAddF, rSubF, rMulF, rDivF:
		return p.plan.acc >= 0 && in.a == p.plan.acc && in.c == p.plan.acc && in.b != p.plan.acc
	}
	return false
}

// check holds the plan to what a column can compute: an operation writes
// neither the item nor the answer, reads the answer only as the fold, and
// reads a register the body writes only after the body wrote it this item.
// Each register the body writes is a column.
func (p *vecPlanner) check() bool {
	plan := p.plan
	written := map[int32]bool{}
	writes := map[int32]bool{}
	for _, block := range plan.blocks {
		for _, in := range block.ops {
			writes[in.c] = true
		}
	}
	readable := func(r int32) bool { return r != plan.acc && (!writes[r] || written[r]) }
	if !plan.shape() {
		return false
	}
	for _, block := range plan.blocks {
		for _, in := range block.ops {
			if !readable(in.a) || in.op != rMove && in.op != rIntToF && !readable(in.b) || in.c == plan.item || in.c == plan.acc {
				return false
			}
			written[in.c] = true
			if _, ok := plan.columns[in.c]; !ok {
				plan.columns[in.c] = len(plan.columns)
			}
		}
		if block.folds && !readable(block.fold.b) || block.collect != -1 && !readable(block.collect) || block.cond >= 0 && !readable(block.cond) {
			return false
		}
	}
	return true
}

// shape finds the chain, the blocks that fold and collect, and whether the
// body is uniform; a body that folds or collects twice is not one the
// vector runs.
func (plan *vecPlan) shape() bool {
	plan.foldAt, plan.collectAt, plan.uniform = -1, -1, true
	plan.stops = slices.ContainsFunc(plan.blocks, func(block vecBlock) bool { return block.end == vecStop })
	for k := range plan.blocks {
		block := &plan.blocks[k]
		plan.chain = k + 1
		if block.folds {
			if plan.foldAt >= 0 {
				return false
			}
			plan.foldAt = k
		}
		if block.collect != -1 {
			if plan.collectAt >= 0 {
				return false
			}
			plan.collectAt = k
		}
		if block.cond != -1 || block.end == vecStop {
			plan.uniform = false
		}
		if block.end != vecFall {
			return true
		}
	}
	return true
}
