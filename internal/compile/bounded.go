package compile

import (
	"maps"
	"slices"
	"strings"

	"github.com/nethinwei/funroute/internal/kit"
	"github.com/nethinwei/funroute/internal/machine"
	"github.com/nethinwei/funroute/internal/syntax"
)

// requireBoundedArgs holds a BoundedArgs function to its promise: what it
// produces is sized by its arguments, so those arguments must have a size the
// inputs already bound. Three things do: a literal, the length of a container
// (that length *is* part of the input size), and the two combined by
// arithmetic; a let's name for one of them is as good. An argument that could
// be any scalar at run time does not, and that is the one docs/termination.md
// rules out.
func (c *bytecodeCompiler) requireBoundedArgs(node *syntax.CallExpr) error {
	for i, arg := range node.Args {
		// Only an integer can stand for a length; the amount allocate splits
		// or anything else of another type says nothing about the size.
		if typ, ok := c.inferred.nodeType(arg.NodeID()); ok && typ.Kind() != machine.IntKind {
			continue
		}
		if c.boundedExpr(arg) {
			continue
		}
		return syntax.Around(arg,
			"%s needs arguments whose size the inputs already bound: a literal, len(...) of a container, or those combined by arithmetic — argument %d is neither",
			node.Name, i+1)
	}
	return nil
}

func (c *bytecodeCompiler) boundedExpr(expr syntax.Expr) bool {
	switch node := expr.(type) {
	case *syntax.LiteralExpr:
		return true
	case *syntax.VariableExpr:
		// A binding that folded to a constant is as good as a literal, and
		// a let's name as good as its value.
		bound, ok := c.names.top(node.Name)
		return ok && (bound.constant || bound.bounded)
	case *syntax.CallExpr:
		return c.boundedCall(node)
	}
	return false
}

// boundedCall knows two things about the kernel: len hands back a size the
// input already bound, and arithmetic on bounded sizes stays bounded (the
// polynomial in docs/termination.md is a polynomial for exactly this reason).
// Only the kernel's own functions count — a host may register another len.
func (c *bytecodeCompiler) boundedCall(node *syntax.CallExpr) bool {
	key, ok := c.inferred.Selections[node.ID]
	if !ok {
		return false
	}
	function, ok := machine.ResolveOf(c.registry, key)
	if !ok || !function.IsBuiltin() {
		return false
	}
	switch function.Name {
	case "len":
		return true
	case "add", "sub", "mul", "div", "mod":
		return !slices.ContainsFunc(node.Args, func(arg syntax.Expr) bool { return !c.boundedExpr(arg) })
	}
	return false
}

// requireBoundedFold holds a reduce to an accumulator that a turn makes
// larger by no more than what the turn itself brings: the next accumulator
// may hold each part of this one once, passed only through what hands back
// no more than it is given (Doc.Additive), and whatever else does not come
// from the accumulator. Then its size after every turn is the first one's
// plus a turn's worth a turn, which the input bounds — the step the proof in
// docs/termination.md takes for a loop. Without it reduce(x in xs, a = [1],
// concat(a, a)) doubles a every turn.
//
// The size is the count Doc.Additive speaks of: the items of every container
// in the value and the characters of every string. A value of a type that
// holds neither is of a fixed size whatever it was computed from, so only
// the expressions that can grow are followed.
func (c *bytecodeCompiler) requireBoundedFold(node *syntax.ReduceExpr) error {
	if typ, ok := c.inferred.nodeType(node.ID); ok && !grows(typ) {
		return nil
	}
	walk := growth{c: c, accumulator: node.Accumulator, names: map[string]held{node.Accumulator: {parts: []string{""}, exact: true}}}
	_, err := walk.held(node.Body)
	return err
}

// grows reports a type whose values have no fixed size: one that holds a
// string, an array or a dictionary anywhere.
func grows(typ machine.Type) bool {
	return machine.TypeContains(typ, machine.StringKind) || machine.TypeContains(typ, machine.ArrayKind) || machine.TypeContains(typ, machine.DictKind)
}

// held is what an expression's value holds of the accumulator: its parts —
// "" the whole of it, "fees.card" a field of a field — each once. exact
// says the value is its one part and nothing else, so that a field of it is
// a part too. err is why a let's value could not be followed, said where
// the name is read: a value that is never read holds nothing.
type held struct {
	parts []string
	exact bool
	err   error
}

// growth follows the accumulator through a reduce's body.
type growth struct {
	c           *bytecodeCompiler
	accumulator string
	// names is what each name in scope holds; a name bound inside the body
	// to what holds nothing is here too, hiding one outside it.
	names map[string]held
	// loops is how many loops inside the body the walk is in: what is read
	// there is read once an item.
	loops int
}

func (w *growth) held(expr syntax.Expr) (held, error) {
	if typ, ok := w.c.inferred.nodeType(expr.NodeID()); ok && !grows(typ) {
		return held{}, nil
	}
	switch node := expr.(type) {
	case *syntax.VariableExpr:
		return w.read(node)
	case *syntax.FieldExpr:
		return w.field(node)
	case *syntax.ArrayExpr:
		return w.together(held{}, node.Items...)
	case *syntax.DictExpr:
		return w.together(held{}, kit.Map(node.Entries, func(entry syntax.DictEntryExpr) syntax.Expr { return entry.Value })...)
	case *syntax.RecordExpr:
		return w.together(held{}, fieldValues(node.Fields)...)
	case *syntax.RecordUpdateExpr:
		return w.update(node)
	case *syntax.LetExpr:
		return w.let(node)
	case *syntax.UsingExpr:
		return w.held(node.Body)
	case *syntax.SwitchExpr:
		results := kit.Map(node.Cases, func(branch syntax.SwitchCaseExpr) syntax.Expr { return branch.Result })
		return w.either(append(results, node.Default)...)
	case *syntax.CallExpr:
		return w.call(node)
	case *syntax.ForExpr:
		return w.comprehension(node)
	case *syntax.ReduceExpr:
		return w.fold(node)
	}
	return held{}, nil
}

// read is what a name holds. One that holds some of the accumulator is not
// read inside a loop: there it goes into every item.
func (w *growth) read(node *syntax.VariableExpr) (held, error) {
	value := w.names[node.Name]
	if value.err != nil {
		return held{}, value.err
	}
	if len(value.parts) > 0 && w.loops > 0 {
		return held{}, syntax.Around(node,
			"reduce's accumulator %s goes into a loop inside the body, once for every item: it could grow by a multiple every turn, and the rule's cost would not be bounded by its input — read it outside the loop",
			w.accumulator)
	}
	return value, nil
}

// field is a field of a part, a part itself, or of anything else, which
// holds no more than the record it is read from.
func (w *growth) field(node *syntax.FieldExpr) (held, error) {
	record, err := w.held(node.Value)
	if err != nil || !record.exact {
		return held{parts: record.parts}, err
	}
	return held{parts: []string{strings.TrimPrefix(record.parts[0]+"."+node.Field, ".")}, exact: true}, nil
}

// together is what a value made of all of exprs holds, on top of so far:
// each part once, or the value could hold the accumulator twice over.
func (w *growth) together(so held, exprs ...syntax.Expr) (held, error) {
	parts := so.parts
	for _, expr := range exprs {
		value, err := w.held(expr)
		if err != nil {
			return held{}, err
		}
		for _, part := range value.parts {
			if slices.ContainsFunc(parts, func(other string) bool { return overlap(part, other) }) {
				return held{}, syntax.Around(expr,
					"reduce's accumulator %s goes twice into what the next turn's is made of: it could double every turn, and the rule's cost would not be bounded by its input — use each part of it once",
					w.accumulator)
			}
		}
		parts = append(slices.Clone(parts), value.parts...)
	}
	return held{parts: parts}, nil
}

// overlap reports two parts that share something: one is the other, or a
// field inside it.
func overlap(a, b string) bool {
	return a == b || a == "" || b == "" || strings.HasPrefix(a, b+".") || strings.HasPrefix(b, a+".")
}

// either is what a value that is one of exprs holds: the parts of any, for
// only one of them is the value. A nil expr is a branch not written.
func (w *growth) either(exprs ...syntax.Expr) (held, error) {
	var parts []string
	for _, expr := range exprs {
		if expr == nil {
			continue
		}
		value, err := w.held(expr)
		if err != nil {
			return held{}, err
		}
		for _, part := range value.parts {
			if !slices.Contains(parts, part) {
				parts = append(parts, part)
			}
		}
	}
	return held{parts: parts}, nil
}

// update is a record with fields replaced: what the record holds outside
// those fields, and what the new values hold. So a with {fees: append(a.fees,
// x)} holds a.fees once — the field replaced is no longer of the record.
func (w *growth) update(node *syntax.RecordUpdateExpr) (held, error) {
	base, err := w.held(node.Base)
	if err != nil {
		return held{}, err
	}
	typ, ok := w.c.inferred.nodeType(node.Base.NodeID())
	if base.exact && ok {
		kept := []string{}
		for _, field := range typ.Fields() {
			replaced := slices.ContainsFunc(node.Fields, func(update syntax.RecordFieldExpr) bool { return update.Name == field.Name() })
			if !replaced && grows(field.Type()) {
				kept = append(kept, strings.TrimPrefix(base.parts[0]+"."+field.Name(), "."))
			}
		}
		base = held{parts: kept}
	}
	return w.together(held{parts: base.parts}, fieldValues(node.Fields)...)
}

// let binds each name to what its value holds, for the body to read: once,
// or it holds it twice.
func (w *growth) let(node *syntax.LetExpr) (held, error) {
	defer w.scope()()
	for _, binding := range node.Bindings {
		value, err := w.held(binding.Value)
		if err != nil {
			value = held{err: err}
		}
		w.names[binding.Name] = value
	}
	return w.held(node.Body)
}

// scope opens a scope for names, and returns what closes it.
func (w *growth) scope() func() {
	outer := w.names
	w.names = maps.Clone(outer)
	return func() { w.names = outer }
}

// call is a call's value: one branch of an if or a fallback, and of any
// other function what its arguments hold between them — if the function
// hands back no more than it is given.
func (w *growth) call(node *syntax.CallExpr) (held, error) {
	function, ok := machine.ResolveOf(w.c.registry, w.c.inferred.Selections[node.ID])
	switch {
	case !ok:
		return held{}, nil
	case function.IsLazyIf():
		return w.either(node.Args[1:]...)
	case function.IsLazyFallback():
		return w.either(node.Args...)
	}
	value, err := w.together(held{}, node.Args...)
	if err != nil || len(value.parts) == 0 || function.Doc.Additive {
		return value, err
	}
	return held{}, syntax.Around(node,
		"%s may hand back more than it is given, so reduce's accumulator %s cannot go through it: it could grow by a multiple every turn, and the rule's cost would not be bounded by its input — keep to what adds to it, picks from it or reorders it (concat, merge, slice, sort, a filter)",
		node.Name, w.accumulator)
}

// comprehension is a comprehension's value. Over some of the accumulator it
// is known not to hold more only when it keeps items as they are or makes a
// value of a fixed size of each; over anything else it holds nothing of the
// accumulator, which its items must not read.
func (w *growth) comprehension(node *syntax.ForExpr) (held, error) {
	source, err := w.held(node.Source)
	if err != nil {
		return held{}, err
	}
	if len(source.parts) == 0 {
		return held{}, w.inside([]string{node.Variable, node.KeyVariable}, node.YieldKey, node.Yield)
	}
	if w.keeps(node.Yield, node.Variable) && (node.YieldKey == nil || w.keeps(node.YieldKey, node.KeyVariable)) {
		return held{parts: source.parts}, nil
	}
	return held{}, w.rebuilt(node)
}

// keeps reports a yield that is the loop's own name, or of a fixed size.
func (w *growth) keeps(yield syntax.Expr, name string) bool {
	if variable, ok := yield.(*syntax.VariableExpr); ok && variable.Name == name {
		return true
	}
	typ, ok := w.c.inferred.nodeType(yield.NodeID())
	return ok && !grows(typ)
}

func (w *growth) rebuilt(node syntax.Expr) error {
	return syntax.Around(node,
		"the items of reduce's accumulator %s are made anew here: it could grow by a multiple every turn, and the rule's cost would not be bounded by its input — only a filter of them ([x for x in %s if …]) or a value of a fixed size for each is known not to grow",
		w.accumulator, w.accumulator)
}

// fold is a reduce inside the body: what its first accumulator holds, when
// its turns add nothing of the outer one. Its own accumulator is held to the
// same rule where it is compiled.
func (w *growth) fold(node *syntax.ReduceExpr) (held, error) {
	source, err := w.held(node.Source)
	if err != nil {
		return held{}, err
	}
	if len(source.parts) > 0 {
		return held{}, w.rebuilt(node)
	}
	first, err := w.held(node.Init)
	if err != nil {
		return held{}, err
	}
	return held{parts: first.parts}, w.inside([]string{node.Variable, node.KeyVariable, node.Accumulator}, node.Body)
}

// inside follows exprs as what a loop computes for each item, the loop's
// names bound: nothing they compute may hold the accumulator.
func (w *growth) inside(names []string, exprs ...syntax.Expr) error {
	defer w.scope()()
	for _, name := range names {
		w.names[name] = held{}
	}
	w.loops++
	defer func() { w.loops-- }()
	for _, expr := range exprs {
		if expr == nil {
			continue
		}
		if _, err := w.held(expr); err != nil {
			return err
		}
	}
	return nil
}
