package machine

import "fmt"

// Fold declares a function of one array a fold of the array's items: what
// it answers for no items, how it takes in the next, and an item that
// decides the answer at once. A call of such a function on a comprehension,
// sum([fee(x) for x in xs if x.ok]), is compiled into one pass over xs that
// folds each yielded item as it is made: no array is built, and none is
// handed to the function. The compiler reads nothing else — not the name —
// to do it; a function without a Fold is called on the array as always.
//
// The fold must answer as the function does: the same value for the same
// items, and a failure where the function fails, of the same class. Step's
// failure is its own — sum's overflow is add's — and a stop ends the pass at
// the item that decides it, so the items after it are not computed and do
// not fail: any and all stop as && and || do.
type Fold struct {
	// Step is the kernel function that takes the answer so far and the next
	// item and gives the answer with it: "add" for a sum, called as
	// add(answer, item). A fold steps, stops or counts.
	Step string
	// Init is the answer for no items: 0 for a sum, false for any.
	Init Value
	// Stops ends a fold of bools at the first item that is Stop, which is
	// then the answer: true for any, false for all.
	Stops, Stop bool
	// Counts makes the fold count the items: len. A count computes an item
	// only when it is more than a name or a literal, and then does not fuse.
	Counts bool
	// First makes the first item the answer, and ends the fold there: first.
	// With no item the function itself is called on an empty array, and
	// fails as it does. The function answers its array's element type; the
	// fold fuses where that is a bool, an int, a float or a string.
	First bool
}

// validateFold holds a declared fold to the function it folds for: one
// array parameter; for a count, an int result and nothing else; otherwise
// an Init of the result type, and a Step that is a kernel function of the
// answer and the item, or a stop of bools. The fold is copied, so the host's
// value cannot change it afterwards.
func (r *Registry) validateFold(spec *FunctionSpec) error {
	if spec.Fold == nil {
		return nil
	}
	fold := *spec.Fold
	spec.Fold = &fold
	fail := func(format string, args ...any) error {
		return fmt.Errorf("function %s fold: %s", spec.Name, fmt.Sprintf(format, args...))
	}
	if len(spec.Params) != 1 || spec.Params[0].kind != ArrayKind || spec.Params[0].elem == nil {
		return fail("the function must take one array")
	}
	if fold.Counts || fold.First {
		plain := fold.Step == "" && !fold.Stops && !fold.Stop && fold.Init.kind == InvalidKind && !(fold.Counts && fold.First)
		switch {
		case fold.Counts && (spec.Result.kind != IntKind || !plain):
			return fail("a count answers an int, and has no step, stop or init")
		case fold.First && (!spec.Result.Equal(*spec.Params[0].elem) || !plain):
			return fail("a first answers the array's element type, and has no step, stop, init or count")
		}
		return nil
	}
	return r.validateFolding(spec, fold, fail)
}

func (r *Registry) validateFolding(spec *FunctionSpec, fold Fold, fail failFunc) error {
	answer, item := spec.Result, *spec.Params[0].elem
	if !answer.IsConcrete() || !item.IsConcrete() {
		return fail("a fold of %s into %s needs concrete types", item, answer)
	}
	if !fold.Init.hasType(answer) {
		return fail("init %s is not a %s", fold.Init.Type(), answer)
	}
	if fold.Stops {
		if fold.Step != "" || answer.kind != BoolKind || item.kind != BoolKind {
			return fail("a stop ends a fold of bools into a bool, which has no step")
		}
		return nil
	}
	if fold.Step == "" {
		return fail("it needs a step, a stop or a count")
	}
	if _, ok := FoldStep(r, fold.Step, answer, item); !ok {
		return fail("there is no kernel function %s(%s,%s)->%s", fold.Step, answer, item, answer)
	}
	return nil
}

// FoldStep is the kernel function name(answer, item)->answer a fold steps
// with, if the registry has one: for the compiler, which lays the fold out.
func FoldStep(r *Registry, name string, answer, item Type) (*RegisteredFunction, bool) {
	key := FunctionSpec{Name: name, Params: []Type{answer, item}, Result: answer}.Signature()
	function, ok := r.Resolve(key)
	return function, ok && function.builtin
}
