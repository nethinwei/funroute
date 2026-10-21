package compile

// Solving what one reading of the program leaves open. A call more than one
// overload fits is a choice; a field read, a record update or a quote whose
// operand is still open is deferred. Propagation drops every candidate that
// no longer fits and takes the last one standing, until nothing changes.
// Then the choices still open are decided one at a time, innermost first,
// each by the candidate that costs least — no literal read as another kind,
// no promotion, the plainest types — and propagation runs again. A candidate
// whose consequences fail is dropped, so a decision never strands the rest
// of the program. What is left open after that is ambiguous.

import (
	"fmt"
	"math"
	"strings"

	"github.com/nethinwei/funroute/internal/kit"
	"github.com/nethinwei/funroute/internal/machine"
	"github.com/nethinwei/funroute/internal/syntax"
)

// choice is a call more than one overload fits: the candidates still open,
// and the argument and result terms they are tried against. open is nil
// once one is taken.
type choice struct {
	node   *syntax.CallExpr
	args   []typeTerm
	result typeTerm
	open   []*machine.RegisteredFunction
	queued bool
}

// deferred is a rule waiting for an operand's type: resolve reports whether
// it could run, and settle runs it once nothing else can decide the operand.
type deferred struct {
	resolve func() (bool, error)
	settle  func() error
	done    bool
}

func (s *inferState) setOpen(c *choice, open []*machine.RegisteredFunction) {
	s.trail, s.oldChoices = append(s.trail, undo{op: undoChoice}), append(s.oldChoices, openChoice{choice: c, open: c.open})
	c.open = open
}

func (s *inferState) finish(d *deferred) {
	s.trail, s.oldDeferred = append(s.trail, undo{op: undoDeferred}), append(s.oldDeferred, d)
	d.done = true
}

// apply unifies the call with one candidate's signature and records it as
// the call's function.
func (s *inferState) apply(c *choice, function *machine.RegisteredFunction) error {
	var vars namedTerms
	for i, param := range function.Params {
		if err := s.unify(c.args[i], s.instantiate(param, &vars)); err != nil {
			return err
		}
	}
	if err := s.unify(c.result, s.instantiate(function.Result, &vars)); err != nil {
		return err
	}
	s.selectKey(c.node.ID, function.Key())
	return nil
}

// fits reports whether the candidate still unifies with the call, leaving
// the state as it was.
func (s *inferState) fits(c *choice, function *machine.RegisteredFunction) bool {
	defer s.endTrial(s.trial())
	return s.apply(c, function) == nil
}

// trial starts a change to be taken back, and is where endTrial takes it
// back to: the trail's mark and how many variables there were.
func (s *inferState) trial() (trail, vars int) {
	s.probing++
	return s.mark(), len(s.vars)
}

// endTrial takes back every change since trail, drops the variables the
// trial made, and ends the trial. Nothing outlives a trial that could name
// them: its bindings are on the trail, and a trial spreads no watchers.
func (s *inferState) endTrial(trail, vars int) {
	s.undoTo(trail)
	s.dropVars(vars)
	s.probing--
}

// dropVars forgets the variables made since there were vars, once what made
// them is taken back: only the trail named them.
func (s *inferState) dropVars(vars int) {
	clear(s.vars[vars:])
	s.vars = s.vars[:vars]
}

func (s *inferState) commit(c *choice, function *machine.RegisteredFunction) error {
	if err := s.apply(c, function); err != nil {
		return err
	}
	s.setOpen(c, nil)
	return nil
}

// offer makes a call a choice among the functions that fit it now; the
// only one is taken at once, and none is the call's type error.
func (s *inferState) offer(c *choice, functions []*machine.RegisteredFunction) error {
	c.open = functions
	if err := s.narrowChoice(c); err != nil {
		return err
	}
	if c.open == nil {
		return nil
	}
	s.choices = append(s.choices, c)
	vars := s.freeVars(c.result, nil)
	for _, arg := range c.args {
		vars = s.freeVars(arg, vars)
	}
	for _, v := range vars {
		s.watch(v, c)
	}
	return nil
}

// narrowChoice drops the candidates that no longer fit, taking the last one
// standing. What the kinds already rule out is dropped without a trial, and
// a last candidate is taken without one: if it does not fit after all, what
// it changed is taken back — its bindings, what they queued and the
// variables it made — and nothing fits.
func (s *inferState) narrowChoice(c *choice) error {
	open := kept(c.open, func(function *machine.RegisteredFunction) bool { return s.mayFit(c, function) })
	if len(open) > 1 {
		open = kept(open, func(function *machine.RegisteredFunction) bool { return s.fits(c, function) })
	}
	switch {
	case len(open) == 0:
		return noOverloadError(c.node, s, c.args)
	case len(open) == 1:
		mark, vars := s.mark(), len(s.vars)
		if s.commit(c, open[0]) != nil {
			s.undoTo(mark)
			s.dropQueue()
			s.dropVars(vars)
			return noOverloadError(c.node, s, c.args)
		}
	case len(open) < len(c.open):
		s.setOpen(c, open)
	}
	return nil
}

// mayFit reports a candidate each of whose parameters could still meet its
// argument: false only where the unification would fail on a kind or a
// name, so it drops no candidate a trial would keep.
func (s *inferState) mayFit(c *choice, function *machine.RegisteredFunction) bool {
	for i, param := range function.Params {
		if !s.couldBe(c.args[i], param) {
			return false
		}
	}
	return true
}

// couldBe reports whether term could unify with an instance of t.
func (s *inferState) couldBe(term typeTerm, t machine.Type) bool {
	term = s.deref(term)
	kind := t.Kind()
	switch {
	case kind == machine.VarKind || kind == machine.EnumKind && t.Name() == "":
		return true
	case term.kind == machine.VarKind:
		return s.infoOf(term.id).domain.has(kind)
	case term.kind != kind || term.name != t.Name():
		return false
	case term.elem != nil && (kind == machine.ArrayKind || kind == machine.DictKind) && hasElem(t):
		return s.couldBe(*term.elem, elemOf(t))
	}
	return true
}

// kept is the items keep is true of, in order: items itself when that is
// every one, so only a list that shrinks costs one of its own.
func kept[T any](items []T, keep func(T) bool) []T {
	for i, item := range items {
		if keep(item) {
			continue
		}
		out := append(make([]T, 0, len(items)-1), items[:i]...)
		for _, rest := range items[i+1:] {
			if keep(rest) {
				out = append(out, rest)
			}
		}
		return out
	}
	return items
}

// propagate narrows every open choice a binding may have narrowed and runs
// every deferred rule that can run, until nothing changes.
func (s *inferState) propagate() error {
	for {
		// Walked by index, narrowing may queue more behind it; emptied after,
		// so the array is the next walk's and holds no choice it walked.
		for next := 0; next < len(s.queue); {
			c := s.queue[next]
			next++
			c.queued = false
			if c.open == nil {
				continue
			}
			if err := s.narrowChoice(c); err != nil {
				s.dropQueue()
				return err
			}
		}
		s.dropQueue()
		ran, err := s.runDeferred()
		if err != nil || !ran && len(s.queue) == 0 {
			return err
		}
	}
}

// dropQueue empties the queue, unmarking what it held: what a failure left
// there was queued by changes about to be taken back.
func (s *inferState) dropQueue() {
	for _, c := range s.queue {
		c.queued = false
	}
	clear(s.queue)
	s.queue = s.queue[:0]
}

func (s *inferState) runDeferred() (bool, error) {
	ran := false
	for _, d := range s.deferred {
		if d.done {
			continue
		}
		ok, err := d.resolve()
		if err != nil {
			return false, err
		}
		if ok {
			s.finish(d)
			ran = true
		}
	}
	return ran, nil
}

// solve decides everything the program leaves open: the choices, then the
// deferred rules by their defaults, then the literals.
func (s *inferState) solve() error {
	for {
		if err := s.propagate(); err != nil {
			return err
		}
		decided, err := s.decide()
		if err != nil {
			return err
		}
		if !decided {
			break
		}
	}
	for _, d := range s.deferred {
		if d.done {
			continue
		}
		if err := d.settle(); err != nil {
			return err
		}
		if err := s.propagate(); err != nil {
			return err
		}
	}
	return s.settleLiterals()
}

// decide takes the cheapest candidate of the first open choice that has one
// cheapest candidate, and reports whether a choice was open. A candidate
// whose consequences fail is dropped instead; every choice open with ties
// is the program's ambiguity.
func (s *inferState) decide() (bool, error) {
	var first *choice
	for _, c := range s.choices {
		if c.open == nil {
			continue
		}
		if first == nil {
			first = c
		}
		best, unique := s.cheapest(c)
		if !unique {
			continue
		}
		mark, vars := s.mark(), len(s.vars)
		err := s.commit(c, best)
		if err == nil {
			err = s.propagate()
		}
		if err == nil {
			return true, nil
		}
		s.undoTo(mark)
		s.dropQueue()
		s.dropVars(vars)
		rest := kept(c.open, func(f *machine.RegisteredFunction) bool { return f != best })
		if len(rest) == 0 {
			// The last candidate failed: why it did is the program's error,
			// not an ambiguity among none.
			return false, err
		}
		s.setOpen(c, rest)
		return true, nil
	}
	if first == nil {
		return false, nil
	}
	return false, ambiguityError(first)
}

// cheapest is the candidate that costs least, and whether no other costs
// the same.
func (s *inferState) cheapest(c *choice) (*machine.RegisteredFunction, bool) {
	var best *machine.RegisteredFunction
	bestCost, unique := int64(0), false
	for _, function := range c.open {
		cost := s.cost(c, function)
		switch {
		case best == nil || cost < bestCost:
			best, bestCost, unique = function, cost, true
		case cost == bestCost:
			unique = false
		}
	}
	return best, unique
}

// cost is what taking a candidate costs: every type it leaves open, every
// literal it reads as another kind, a promotion, then the types it leaves
// the call. It is an int64 whatever the platform's int: two open types are
// past 2^31, and one source must type alike on every machine.
func (s *inferState) cost(c *choice, function *machine.RegisteredFunction) int64 {
	defer s.endTrial(s.trial())
	before := s.converted
	if s.apply(c, function) != nil {
		return math.MaxInt64
	}
	cost := int64(s.converted-before)*literalPenalty + s.scoreTerm(c.result)
	if isMixedNumeric(function.Params) {
		cost += mixedPenalty
	}
	for _, arg := range c.args {
		cost += s.scoreTerm(arg)
	}
	return cost
}

func ambiguityError(c *choice) error {
	keys := kit.Map(c.open, (*machine.RegisteredFunction).Key)
	return syntax.Around(c.node, "type error: %s is ambiguous here (%s); use int(...), float(...), string(...), bool(...), or provide a type hint",
		c.node.Name, strings.Join(keys, " | "))
}

// waitFor runs resolve now if it can, and otherwise waits for it to be able
// to, settling it by settle once nothing else decides its operand.
func (s *inferState) waitFor(resolve func() (bool, error), settle func() error) error {
	ok, err := resolve()
	if err != nil || ok {
		return err
	}
	s.deferred = append(s.deferred, &deferred{resolve: resolve, settle: settle})
	return nil
}

// mixedPenalty makes a mixed-numeric overload the last resort: for `risk < 0.5`
// the cheap-by-type choice would be risk:int promoted to float, which is almost
// never what the author meant. Writing the mixed form explicitly still works —
// it only loses when a same-type reading exists.
const mixedPenalty = 100

// openPenalty puts a candidate that leaves a type open behind every one that
// does not: len(note) is the string's length, not an array's whose element
// nothing names.
const openPenalty = 1 << 30

// literalPenalty puts a reading that turns a literal into a ratio or an amount
// behind every reading that does not, whatever else those cost: a registry
// that declares money must type a program without money literals exactly as
// before. The weight only has to outrun the rest of a candidate's cost.
const literalPenalty = 1 << 20

// isMixedNumeric reports a signature like (int, float) — one that only exists
// to allow safe promotion.
func isMixedNumeric(params []machine.Type) bool {
	if len(params) != 2 || params[0].Equal(params[1]) {
		return false
	}
	numeric := func(kind machine.Kind) bool { return kind == machine.IntKind || kind == machine.FloatKind }
	return numeric(params[0].Kind()) && numeric(params[1].Kind())
}

// implicitScores is what a type of each kind costs when candidates are
// ranked; a kind not listed costs 100, a container its element's plus 2 and
// a type still open openPenalty.
var implicitScores = map[machine.Kind]int64{
	machine.IntKind:   0,
	machine.BoolKind:  1,
	machine.FloatKind: 10,
	// A ratio is exact: allowed wherever it types, never the reading of an
	// unconstrained number.
	machine.RatioKind:    12,
	machine.StringKind:   20,
	machine.EnumKind:     20,
	machine.CurrencyKind: 20,
	machine.HandleKind:   30,
	machine.RecordKind:   30,
	machine.MoneyKind:    40,
	machine.FxRateKind:   50,
	machine.TimeKind:     20,
	machine.DurationKind: 20,
}

func (s *inferState) scoreTerm(term typeTerm) int64 {
	term = s.deref(term)
	switch {
	case term.kind == machine.VarKind:
		return openPenalty
	case term.elem != nil:
		return 2 + s.scoreTerm(*term.elem)
	}
	if score, ok := implicitScores[term.kind]; ok {
		return score
	}
	return 100
}

// noOverloadError says which call no signature fits, with the arguments as
// inference sees them and a pointer where one is known.
func noOverloadError(node *syntax.CallExpr, s *inferState, args []typeTerm) error {
	actual := kit.Map(args, s.describe)
	message := fmt.Sprintf("type error: no overload %s(%s)", node.Name, strings.Join(actual, ", "))
	if hint := overloadHint(node.Name, actual); hint != "" {
		message += "；" + hint
	}
	return syntax.Around(node, "%s", message)
}
