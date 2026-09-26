package compile

// Literals in inference. With money declared a decimal may become a ratio and
// 0 may become money or a ratio; such a literal's type is a variable held to
// those kinds, settled at the end to what it was written as unless the
// context asked for another reading, which costs literalPenalty.

import (
	"errors"
	"strings"

	"github.com/nethinwei/funroute/internal/machine"
)

// namedKinds is every kind a variable may be held to, in the order they are
// named: what a literal may become, and the enum a signature's wildcard is.
var namedKinds = []machine.Kind{machine.IntKind, machine.FloatKind, machine.RatioKind, machine.MoneyKind, machine.EnumKind}

// kindSet is the kinds a variable may still become.
type kindSet uint32

// allKinds is a variable nothing restricts.
const allKinds = ^kindSet(0)

func kindsOf(kinds ...machine.Kind) kindSet {
	var set kindSet
	for _, kind := range kinds {
		set |= 1 << kind
	}
	return set
}

func (k kindSet) has(kind machine.Kind) bool { return k&(1<<kind) != 0 }

// literalTerm types a literal. With money declared a decimal may become a
// ratio and 0 may become money or a ratio — so -amount and -2.9%, which are
// sub(0, x), mean what they say — whichever the context asks; nothing asking
// leaves them what they always were.
func (s *inferState) literalTerm(value machine.Value, money bool) typeTerm {
	typ := value.Type()
	if !money {
		return s.concrete(typ)
	}
	info := varInfo{}
	switch typ.Kind() {
	case machine.FloatKind:
		info = varInfo{domain: kindsOf(machine.FloatKind, machine.RatioKind), floats: 1}
	case machine.IntKind:
		if whole, _ := value.Int(); whole == 0 {
			info = varInfo{domain: kindsOf(machine.IntKind, machine.MoneyKind, machine.RatioKind), ints: 1}
		}
	}
	if info.domain == 0 {
		return s.concrete(typ)
	}
	term := s.fresh()
	s.vars[term.id].info, s.vars[term.id].restricted = info, true
	s.literals = append(s.literals, term.id)
	return term
}

// describeOpen names a type inference has not settled the way a reader can
// use: a variable held to kinds by the kinds it may still be, anything else
// as unknown. The variable's number is inference's bookkeeping, not the
// program's.
func (s *inferState) describeOpen(term typeTerm) string {
	info := s.infoOf(term.id)
	if info.domain == allKinds {
		return "?"
	}
	var kinds []string
	for _, kind := range namedKinds {
		if info.domain.has(kind) {
			kinds = append(kinds, kind.String())
		}
	}
	return strings.Join(kinds, "|")
}

// settleLiterals gives every literal nothing settled what it was written as:
// reading a decimal as a ratio or a 0 as an amount is allowed, never
// preferred, so `risk < 0.5` stays float arithmetic. Literals left sharing
// one open type must all have been written as one kind: 0 and 0.5 meeting
// with nothing asking for a ratio are an int and a float, which is a type
// error exactly as it is without money.
func (s *inferState) settleLiterals() error {
	for _, id := range s.literals {
		root := s.deref(varTerm(id))
		if root.kind != machine.VarKind {
			continue
		}
		info := s.infoOf(root.id)
		kind := machine.IntKind
		if info.floats > 0 {
			kind = machine.FloatKind
		}
		if info.ints > 0 && info.floats > 0 || !info.domain.has(kind) {
			return errors.New("the literals cannot share a type")
		}
		s.bind(root.id, scalarTerm(kind))
	}
	return nil
}
