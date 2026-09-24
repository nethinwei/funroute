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
	"slices"
	"strings"

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
	s.trail = append(s.trail, undo{op: undoChoice, choice: c, open: c.open})
	c.open = open
}

func (s *inferState) finish(d *deferred) {
	s.trail = append(s.trail, undo{op: undoDeferred, deferred: d})
	d.done = true
}

// apply unifies the call with one candidate's signature and records it as
// the call's function.
func (s *inferState) apply(c *choice, function *machine.RegisteredFunction) error {
	vars := map[string]typeTerm{}
	for i, param := range function.Params {
		if err := s.unify(c.args[i], s.instantiate(param, vars)); err != nil {
			return err
		}
	}
	if err := s.unify(c.result, s.instantiate(function.Result, vars)); err != nil {
		return err
	}
	s.selectKey(c.node.ID, function.Key())
	return nil
}

// fits reports whether the candidate still unifies with the call, leaving
// the state as it was.
func (s *inferState) fits(c *choice, function *machine.RegisteredFunction) bool {
	defer s.trial()()
	return s.apply(c, function) == nil
}

// trial starts a change to be taken back, and returns what takes it back.
func (s *inferState) trial() func() {
	mark := s.mark()
	s.probing++
	return func() {
		s.undoTo(mark)
		s.probing--
	}
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
		s.watchers[v] = append(s.watchers[v], c)
	}
	return nil
}

// narrowChoice drops the candidates that no longer fit, taking the last one
// standing.
func (s *inferState) narrowChoice(c *choice) error {
	kept := slices.DeleteFunc(slices.Clone(c.open), func(function *machine.RegisteredFunction) bool { return !s.fits(c, function) })
	switch {
	case len(kept) == 0:
		return noOverloadError(c.node, s, c.args)
	case len(kept) == 1:
		return s.commit(c, kept[0])
	case len(kept) < len(c.open):
		s.setOpen(c, kept)
	}
	return nil
}

// propagate narrows every open choice a binding may have narrowed and runs
// every deferred rule that can run, until nothing changes.
func (s *inferState) propagate() error {
	for {
		for len(s.queue) > 0 {
			c := s.queue[0]
			s.queue = s.queue[1:]
			c.queued = false
			if c.open == nil {
				continue
			}
			if err := s.narrowChoice(c); err != nil {
				return err
			}
		}
		ran, err := s.runDeferred()
		if err != nil || !ran && len(s.queue) == 0 {
			return err
		}
	}
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
		mark := s.mark()
		if err := s.commit(c, best); err == nil && s.propagate() == nil {
			return true, nil
		}
		s.undoTo(mark)
		s.setOpen(c, slices.DeleteFunc(slices.Clone(c.open), func(f *machine.RegisteredFunction) bool { return f == best }))
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
	bestCost, unique := 0, false
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
// the call.
func (s *inferState) cost(c *choice, function *machine.RegisteredFunction) int {
	defer s.trial()()
	before := s.converted
	if s.apply(c, function) != nil {
		return int(^uint(0) >> 1)
	}
	cost := (s.converted-before)*literalPenalty + s.scoreTerm(c.result)
	if isMixedNumeric(function.Params) {
		cost += mixedPenalty
	}
	for _, arg := range c.args {
		cost += s.scoreTerm(arg)
	}
	return cost
}

func ambiguityError(c *choice) error {
	keys := make([]string, len(c.open))
	for i, function := range c.open {
		keys[i] = function.Key()
	}
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
var implicitScores = map[machine.Kind]int{
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
}

func (s *inferState) scoreTerm(term typeTerm) int {
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
	actual := make([]string, len(args))
	for i, arg := range args {
		actual[i] = s.describe(arg)
	}
	message := fmt.Sprintf("type error: no overload %s(%s)", node.Name, strings.Join(actual, ", "))
	if hint := overloadHint(node.Name, actual); hint != "" {
		message += "；" + hint
	}
	return syntax.Around(node, "%s", message)
}
