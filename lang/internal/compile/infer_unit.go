package compile

// Currency units in inference.
//
// Every place a type carries a unit — money's currency, an exchange rate's
// two sides — holds a unit variable, and what is known about it lives at the
// root of its union-find class (unitInfo). Unifying two units merges their
// classes and never fails: two different known currencies meeting only makes
// the class dynamic, which is right where branches meet (if, switch, array
// elements) and is caught where values are combined — a call whose signature
// names one unit variable in several places — by unitConflict, which looks at
// the operands before they were merged. So `amount_c + USD 1` is a compile
// error, `if(x, usd, eur)` is money whose currency the run decides, and every
// value whose class is dynamic is checked at run time wherever a signature
// demands one currency.
//
// A known unit is proven: it came from a literal, a constant, the contract
// (whose arguments the frame checks as they enter), or only such values were
// ever merged into it. A value's own type is therefore trustworthy wherever it
// publishes a unit, and dynamic wherever it does not.

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"funroute/lang/internal/machine"
	"funroute/lang/internal/syntax"
)

// unitInfo is what inference knows about one unit class.
type unitInfo struct {
	known string // a currency code or a contract variable; "" when none
	dyn   bool   // a value in the class has a currency only the run knows
}

func unitFor(name string) unitInfo {
	if name == "" {
		return unitInfo{dyn: true}
	}
	return unitInfo{known: name}
}

func (a unitInfo) join(b unitInfo) unitInfo {
	out := unitInfo{known: a.known, dyn: a.dyn || b.dyn}
	if out.known == "" {
		out.known = b.known
	}
	if a.known != "" && b.known != "" && a.known != b.known {
		out.dyn = true
	}
	return out
}

// published is the unit a public type states: the known one when it is
// proven, and "" — decided at run time — otherwise.
func (a unitInfo) published() string {
	if a.dyn {
		return ""
	}
	return a.known
}

func (s *inferState) freshUnit(info unitInfo) typeTerm {
	term := s.fresh()
	if s.units == nil {
		s.units = map[int]unitInfo{}
	}
	s.units[term.id] = info
	return term
}

func (s *inferState) isUnit(term typeTerm) bool {
	_, ok := s.units[term.id]
	return term.kind == machine.VarKind && ok
}

func (s *inferState) unitOf(term typeTerm) unitInfo {
	return s.units[s.deref(term).id]
}

func (s *inferState) unifyUnit(left, right typeTerm) {
	left, right = s.deref(left), s.deref(right)
	if left.id == right.id {
		return
	}
	merged := s.units[left.id].join(s.units[right.id])
	s.subst[left.id] = right
	s.units[right.id] = merged
}

// unitTerm builds a unit-carrying term. units are the type's units; name
// says what an empty one means: unknown in a value, anything in a signature.
func (s *inferState) unitTerm(kind machine.Kind, units []string, unit func(string) typeTerm) typeTerm {
	term := typeTerm{kind: kind}
	first := unit(units[0])
	term.elem = &first
	if kind == machine.FxRateKind {
		second := unit(units[1])
		term.quote = &second
	}
	return term
}

// concrete is the term of a known type. Each unit becomes a fresh class, so
// two reads of one argument do not tie each other's fate.
func (s *inferState) concrete(t machine.Type) typeTerm {
	switch t.Kind() {
	case machine.ArrayKind, machine.DictKind:
		return containerTerm(t.Kind(), s.concrete(elemOf(t)))
	case machine.RecordKind:
		return s.recordTerm(t)
	case machine.MoneyKind, machine.CurrencyKind, machine.FxRateKind:
		return s.unitTerm(t.Kind(), t.Units(), func(name string) typeTerm { return s.freshUnit(unitFor(name)) })
	default:
		return typeTerm{kind: t.Kind(), name: t.Name(), values: append([]string(nil), t.Values()...)}
	}
}

// signatureUnit instantiates one unit of a signature: a code is known, a
// variable is shared by every place it appears, and an empty one accepts any
// currency in a parameter and is unknown in a result.
func (s *inferState) signatureUnit(name string, vars map[string]typeTerm, result bool) typeTerm {
	switch {
	case machine.IsUnitVariable(name):
		key := "unit:" + name
		if existing, ok := vars[key]; ok {
			return existing
		}
		fresh := s.freshUnit(unitInfo{})
		vars[key] = fresh
		return fresh
	case name == "" && result:
		return s.freshUnit(unitInfo{dyn: true})
	default:
		return s.freshUnit(unitInfo{known: name})
	}
}

func (s *inferState) publicUnits(term typeTerm) (machine.Type, bool) {
	if term.elem == nil {
		return machine.Type{}, false
	}
	first := s.unitOf(*term.elem).published()
	if term.kind == machine.FxRateKind {
		if term.quote == nil {
			return machine.Type{}, false
		}
		return machine.FxRateOf(first, s.unitOf(*term.quote).published()), true
	}
	return machine.ScalarType(term.kind, first, nil), true
}

// unitConflict is where combining values demands one currency: a signature
// naming a unit variable in several parameters, or a currency code. It looks
// at the operands as they were before this call merged them, so two proven
// currencies that differ are a compile error rather than a dynamic class.
func unitConflict(state *inferState, args []typeTerm, params []machine.Type) error {
	seen := map[string][]unitInfo{}
	for i, param := range params {
		state.collectUnits(param, args[i], seen)
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if known := provenUnits(name, seen[name]); conflicting(known) {
			return currencyMismatch(known)
		}
	}
	return nil
}

func currencyMismatch(known []string) error {
	return fmt.Errorf("currency mismatch: %s cannot meet where one currency is required", strings.Join(known, " and "))
}

// sharedVariableConflict is unitConflict for a comparison — eq(T, T),
// member(T, array<T>): the operands meet as one type, so their currencies
// meet position by position, wherever in a parameter the variable sits. Only
// for a comparison — where if(bool, T, T) names one variable twice its
// branches merely meet, and a meeting of two currencies is a currency the run
// knows.
func (s *inferState) sharedVariableConflict(args []typeTerm, params []machine.Type) error {
	seen := map[string][]typeTerm{}
	for i, param := range params {
		s.variableTerms(param, args[i], seen)
	}
	for _, terms := range seen {
		for _, later := range terms[1:] {
			if err := s.termsConflict(terms[0], later); err != nil {
				return err
			}
		}
	}
	return nil
}

// comparesOperands names the functions whose type variable is one value's
// type compared with another's, not the meeting of two branches.
var comparesOperands = map[string]bool{"eq": true, "member": true}

// variableTerms pairs each type variable in a parameter with what the
// operand holds there, through arrays and dictionaries.
func (s *inferState) variableTerms(param machine.Type, arg typeTerm, into map[string][]typeTerm) {
	arg = s.deref(arg)
	switch param.Kind() {
	case machine.VarKind:
		into[param.Name()] = append(into[param.Name()], arg)
	case machine.ArrayKind, machine.DictKind:
		if elem, ok := param.Elem(); ok && arg.kind == param.Kind() && arg.elem != nil {
			s.variableTerms(elem, *arg.elem, into)
		}
	}
}

// termsConflict reports two types that would be one type but for proven
// currencies that differ somewhere in them.
func (s *inferState) termsConflict(a, b typeTerm) error {
	a, b = s.deref(a), s.deref(b)
	if a.kind != b.kind {
		return nil
	}
	// Two currencies are compared to find out whether they are one: that
	// question always has an answer, so only amounts and rates conflict.
	switch a.kind {
	case machine.MoneyKind:
		return unitsDiffer(s.unitOf(*a.elem).published(), s.unitOf(*b.elem).published())
	case machine.FxRateKind:
		if err := unitsDiffer(s.unitOf(*a.elem).published(), s.unitOf(*b.elem).published()); err != nil {
			return err
		}
		return unitsDiffer(s.unitOf(*a.quote).published(), s.unitOf(*b.quote).published())
	case machine.ArrayKind, machine.DictKind:
		if a.elem != nil && b.elem != nil {
			return s.termsConflict(*a.elem, *b.elem)
		}
	case machine.RecordKind:
		if a.record != nil && b.record != nil {
			return typesConflict(s.publicRecord(a), s.publicRecord(b))
		}
	}
	return nil
}

// typesConflict is termsConflict for concrete types, a record's fields.
func typesConflict(a, b machine.Type) error {
	if a.Kind() != b.Kind() {
		return nil
	}
	switch a.Kind() {
	case machine.MoneyKind:
		return unitsDiffer(a.Name(), b.Name())
	case machine.FxRateKind:
		if err := unitsDiffer(a.Values()[0], b.Values()[0]); err != nil {
			return err
		}
		return unitsDiffer(a.Values()[1], b.Values()[1])
	case machine.ArrayKind, machine.DictKind:
		return typesConflict(elemOf(a), elemOf(b))
	case machine.RecordKind:
		for i := range min(len(a.Fields()), len(b.Fields())) {
			if err := typesConflict(a.Fields()[i].Type(), b.Fields()[i].Type()); err != nil {
				return err
			}
		}
	}
	return nil
}

func unitsDiffer(a, b string) error {
	if a != "" && b != "" && a != b && conflicting([]string{a, b}) {
		return currencyMismatch([]string{a, b})
	}
	return nil
}

// conflicting reports proven units that cannot be one currency: two codes,
// or a code and a contract variable — a rule that holds only when c is USD
// has a contract that should say money<USD>, and the compiler can say so.
// Two variables are two currencies the run may well bind alike, a payment
// refunded in its own currency, so where only variables meet the run checks.
func conflicting(known []string) bool {
	codes := 0
	for _, unit := range known {
		if machine.IsCurrencyCode(unit) {
			codes++
		}
	}
	return codes > 1 || (codes == 1 && len(known) > 1)
}

// presumedDistinct reports two amounts in two different contract variables:
// money<c> / money<d> is read as an exchange rate, as it was written to be,
// though c and d may be one currency at run time.
func (s *inferState) presumedDistinct(args []typeTerm) bool {
	if len(args) != 2 {
		return false
	}
	units := make([]string, 2)
	for i, arg := range args {
		arg = s.deref(arg)
		if arg.kind != machine.MoneyKind || arg.elem == nil {
			return false
		}
		units[i] = s.unitOf(*arg.elem).published()
	}
	return machine.IsUnitVariable(units[0]) && machine.IsUnitVariable(units[1]) && units[0] != units[1]
}

// collectUnits pairs each unit a parameter names with what the operand
// holds there.
func (s *inferState) collectUnits(param machine.Type, arg typeTerm, into map[string][]unitInfo) {
	arg = s.deref(arg)
	if arg.kind != param.Kind() {
		return
	}
	switch param.Kind() {
	case machine.MoneyKind, machine.CurrencyKind:
		into[param.Name()] = append(into[param.Name()], s.unitOf(*arg.elem))
	case machine.FxRateKind:
		into[param.Values()[0]] = append(into[param.Values()[0]], s.unitOf(*arg.elem))
		into[param.Values()[1]] = append(into[param.Values()[1]], s.unitOf(*arg.quote))
	case machine.ArrayKind, machine.DictKind:
		if arg.elem != nil && hasElem(param) {
			s.collectUnits(elemOf(param), *arg.elem, into)
		}
	case machine.RecordKind:
		names := recordUnitNames(param)
		if len(names) != len(arg.units) {
			return
		}
		for i, name := range names {
			into[name] = append(into[name], s.unitOf(arg.units[i]))
		}
	}
}

// provenUnits lists the distinct proven currencies meeting at one signature
// unit, the unit itself included when it is a code.
func provenUnits(name string, infos []unitInfo) []string {
	if name == "" {
		return nil
	}
	var known []string
	if machine.IsCurrencyCode(name) {
		known = append(known, name)
	}
	if !machine.IsUnitVariable(name) && !machine.IsCurrencyCode(name) {
		return nil
	}
	for _, info := range infos {
		if proven := info.published(); proven != "" && !slices.Contains(known, proven) {
			known = append(known, proven)
		}
	}
	return known
}

// sameUnitProven reports two units that are certainly one currency.
func (s *inferState) sameUnitProven(a, b typeTerm) bool {
	if s.deref(a).id == s.deref(b).id {
		return true
	}
	first, second := s.unitOf(a).published(), s.unitOf(b).published()
	return first != "" && first == second
}

// convertsIntoItself is an exchange-rate result whose two sides are certainly
// one currency: two amounts proven to be in one currency divide into a rate,
// and that is the reading.
func (s *inferState) convertsIntoItself(result typeTerm) bool {
	result = s.deref(result)
	return result.kind == machine.FxRateKind && s.sameUnitProven(*result.elem, *result.quote)
}

// carriesUnits reports a type with a currency unit anywhere in it.
func carriesUnits(t machine.Type) bool {
	found := false
	_ = machine.WalkTypes(t, func(inner machine.Type) error {
		found = found || machine.IsUnitKind(inner.Kind())
		return nil
	})
	return found
}

// checkResultUnits refuses a result whose proven currency is not the one the
// contract declares.
func checkResultUnits(expr syntax.Expr, state *inferState, term typeTerm, declared machine.Type) error {
	seen := map[string][]unitInfo{}
	state.collectUnits(declared, term, seen)
	for name, infos := range seen {
		known := provenUnits(name, infos)
		if machine.IsUnitVariable(name) && !slices.Contains(known, name) {
			known = append([]string{name}, known...)
		}
		if conflicting(known) {
			return syntax.Around(expr, "type error: the contract returns %s but the expression is in %s", declared, strings.Join(known[1:], ", "))
		}
	}
	return nil
}
