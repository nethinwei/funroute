package compile

// Literals in inference. With money declared a decimal may become a rate and
// 0 may become money or a rate; each literal is a type variable restricted to
// those kinds (kindSet), settled at the end to what it was written as unless
// the context asked for another reading, which costs literalPenalty.

import (
	"fmt"
	"slices"

	"funroute/lang/internal/machine"
)

// kindSet is the kinds a literal may still become.
type kindSet uint32

func kindsOf(kinds ...machine.Kind) kindSet {
	var set kindSet
	for _, kind := range kinds {
		set |= 1 << kind
	}
	return set
}

func (k kindSet) has(kind machine.Kind) bool { return k&(1<<kind) != 0 }

// literalTerm types a literal. With money declared a decimal may become a
// rate and 0 may become money or a rate — so -amount and -2.9%, which are
// sub(0, x), mean what they say — whichever the context asks; nothing asking
// leaves them what they always were.
func (s *inferState) literalTerm(value machine.Value, money bool) typeTerm {
	typ := value.Type()
	if !money {
		return s.concrete(typ)
	}
	var set kindSet
	switch typ.Kind() {
	case machine.FloatKind:
		set = kindsOf(machine.FloatKind, machine.RateKind)
	case machine.IntKind:
		if whole, _ := value.Int(); whole == 0 {
			set = kindsOf(machine.IntKind, machine.MoneyKind, machine.RateKind)
		}
	}
	if set == 0 {
		return s.concrete(typ)
	}
	term := s.fresh()
	if s.allowed == nil {
		s.allowed, s.written = map[int]kindSet{}, map[int]machine.Kind{}
	}
	s.allowed[term.id] = set
	s.written[term.id] = typ.Kind()
	return term
}

// constrain keeps a literal's variable within its kinds as it unifies.
func (s *inferState) constrain(variable, other typeTerm) error {
	set, ok := s.allowed[variable.id]
	if !ok {
		return nil
	}
	if other.kind != machine.VarKind {
		if !set.has(other.kind) {
			return fmt.Errorf("a literal cannot be %s", s.describe(other))
		}
		return nil
	}
	if theirs, constrained := s.allowed[other.id]; constrained {
		set &= theirs
		if set == 0 {
			return fmt.Errorf("the literals cannot share a type")
		}
	}
	s.allowed[other.id] = set
	return nil
}

// settleLiterals gives every literal nothing constrained its ordinary kind,
// and counts the ones that became something else — a decimal a rate, a 0 an
// amount: that reading is allowed, never preferred, so `risk < 0.5` stays
// float arithmetic and `score * 0.5 > 0` means what it meant before money.
// Literals left sharing one open type must all have been written as one kind:
// 0 and 0.5 meeting with nothing asking for a rate are an int and a float,
// which is a type error exactly as it is without money.
func (s *inferState) settleLiterals() error {
	ids := make([]int, 0, len(s.written))
	for id := range s.written {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		root := s.deref(typeTerm{kind: machine.VarKind, id: id})
		if root.kind == machine.VarKind {
			kind, ok := s.writtenKind(root.id)
			if !ok {
				return fmt.Errorf("the literals cannot share a type")
			}
			s.subst[root.id] = scalarTerm(kind)
			continue
		}
		if root.kind != s.written[id] {
			s.convertedLiterals++
		}
	}
	return nil
}

// writtenKind is the kind every literal whose type is the open variable root
// was written as, and false when they were written as different kinds. What
// the literals may still become is the intersection allowed keeps at the
// root; what they were written as is each one's own, and only the context —
// never the meeting itself — turns an int and a float into one rate.
func (s *inferState) writtenKind(root int) (machine.Kind, bool) {
	kind, found := machine.VarKind, false
	for id, written := range s.written {
		if s.deref(typeTerm{kind: machine.VarKind, id: id}).id != root {
			continue
		}
		if found && written != kind {
			return 0, false
		}
		kind, found = written, true
	}
	return kind, found
}

// forkLiteral settles a still-open literal now, once per kind it may be, for
// the places that need a type on the spot: a record literal's field. Literals
// written as different kinds meeting there have no reading: nothing asked yet.
func (s *inferState) forkLiteral(term typeTerm) []*inferState {
	root := s.deref(term)
	set, open := s.allowed[root.id]
	if root.kind != machine.VarKind || !open {
		return []*inferState{s}
	}
	written, ok := s.writtenKind(root.id)
	if !ok {
		return nil
	}
	var out []*inferState
	for _, kind := range []machine.Kind{machine.IntKind, machine.FloatKind, machine.RateKind, machine.MoneyKind} {
		if !set.has(kind) {
			continue
		}
		candidate := s.clone()
		target := scalarTerm(kind)
		if kind == machine.MoneyKind {
			target = candidate.unitTerm(kind, []string{""}, func(string) typeTerm { return candidate.freshUnit(unitInfo{}) })
		}
		candidate.subst[root.id] = target
		if kind != written {
			candidate.convertedLiterals++
		}
		out = append(out, candidate)
	}
	return out
}

// candidatePenalty ranks readings that differ only in how they got there.
func candidatePenalty(state *inferState) int {
	return state.convertedLiterals*literalPenalty + state.mixed*mixedPenalty + state.fx*fxPenalty
}

// literalPenalty puts a reading that turns a literal into a rate or an amount
// behind every reading that does not, whatever else those cost: a registry
// that declares money must type a program without money literals exactly as
// before, and there a decimal next to a 0 was float arithmetic paying the
// mixed penalty. The weight only has to outrun the mixed penalties a program
// can collect, one per operator.
const literalPenalty = 1 << 20

// fxPenalty makes an exchange rate the last reading of money / money: with
// both currencies unknown the rate and the exchange rate both type, and the
// rate — same currency, checked at run time — is what a fee over an amount
// means. A contract or a using that asks for an exchange rate still gets one.
const fxPenalty = 50
