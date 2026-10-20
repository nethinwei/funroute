package std

import (
	"cmp"
	"context"
	"fmt"
	"slices"

	"github.com/nethinwei/funroute"
)

// keyedSpecs are the money overloads of the kernel's keyed sorts, both
// ways, and keyed cuts: sort_by with a direction and a count, because "the
// three cheapest" and "the three best" are what a rule actually asks for —
// writing take(sort_by(...), 3) or take(reverse(sort_by(...)), 3) every time
// is a puzzle the rule writer should not have to solve twice. The kernel
// keys by fee, rate or name; these key by money, where the registry
// declares it — the cheapest channels by fee.
func keyedSpecs() []funroute.FunctionSpec {
	list := funroute.ArrayOf(funroute.TypeVar("T"))
	keys := funroute.ArrayOf(funroute.MoneyType)
	sortByMoney := moneyDoc("按金额排序", "按金额键排序候选，相等的保持原顺序。")
	rankByMoney := moneyDoc("按金额取前 k 个", "按金额键取最小或最大的 k 个候选。")
	specs := make([]funroute.FunctionSpec, 0, 4)
	for _, keyed := range []struct {
		name              string
		ascending, ranked bool
	}{{"sort_by", true, false}, {"sort_by_desc", false, false}, {"bottom_k", true, true}, {"top_k", false, true}} {
		ascending := keyed.ascending
		spec := funroute.FunctionSpec{
			Name: keyed.name, Params: []funroute.Type{list, keys}, Result: list, Doc: sortByMoney,
			Eval: func(_ context.Context, args []funroute.Value) (funroute.Value, error) {
				return sortedBy(args, ascending)
			},
		}
		if keyed.ranked {
			spec.Params, spec.Doc = append(spec.Params, funroute.IntType), rankByMoney
			spec.Eval = func(_ context.Context, args []funroute.Value) (funroute.Value, error) {
				return pickRanked(args, ascending)
			}
		}
		specs = append(specs, spec)
	}
	return specs
}

// whileSpecs cut a list where a run of trues ends. The test is the second
// array, same as every other pair here — "every item until the running total
// passes the cap" is take_while(amounts, [t <= cap for t in cumsum(amounts)]).
func whileSpecs() []funroute.FunctionSpec {
	list := funroute.ArrayOf(funroute.TypeVar("T"))
	specs := make([]funroute.FunctionSpec, 0, 2)
	for _, side := range []struct {
		name, label, description string
		prefix                   bool
	}{
		{"take_while", "取到不满足为止", "从头取，遇到第一个 false 就停，后面还有 true 也不要。判断数组必须与候选等长。", true},
		{"drop_while", "跳过开头满足的", "从头跳过 true，遇到第一个 false 就把剩下的全部返回。判断数组必须与候选等长。", false},
	} {
		prefix, name := side.prefix, side.name
		specs = append(specs, funroute.FunctionSpec{
			Name: name, Params: []funroute.Type{list, funroute.ArrayOf(funroute.BoolType)}, Result: list,
			Eval: func(ctx context.Context, args []funroute.Value) (funroute.Value, error) {
				return cutWhile(name, args, prefix)
			},
			Doc: funroute.Doc{
				Label: side.label, Category: "选择",
				Description: side.description,
				Params:      []string{"候选", "逐项判断"}, Result: "截取后的候选",
			},
		})
	}
	return specs
}

func cutWhile(name string, args []funroute.Value, prefix bool) (funroute.Value, error) {
	count, _ := args[0].Length()
	tests, _ := args[1].Length()
	if count != tests {
		return funroute.Value{}, fmt.Errorf("%w: %s has %d candidates and %d tests", funroute.ErrDomain, name, count, tests)
	}
	cut := count
	for i := range tests {
		test, _ := args[1].At(i)
		if keep, _ := test.Bool(); !keep {
			cut = i
			break
		}
	}
	if prefix {
		return part(args[0], 0, cut)
	}
	return part(args[0], cut, count)
}

func pickRanked(args []funroute.Value, ascending bool) (funroute.Value, error) {
	keys, err := keysOf(args)
	if err != nil {
		return funroute.Value{}, err
	}
	count, _ := args[2].Int()
	if count < 0 {
		return funroute.Value{}, fmt.Errorf("%w: a count of candidates cannot be negative, got %d", funroute.ErrArithmetic, count)
	}
	return arranged(args[0], best(keys, int(min(count, int64(len(keys)))), ascending))
}

func sortedBy(args []funroute.Value, ascending bool) (funroute.Value, error) {
	keys, err := keysOf(args)
	if err != nil {
		return funroute.Value{}, err
	}
	return arranged(args[0], best(keys, len(keys), ascending))
}

// keysOf is the amounts the candidates are keyed by, one currency and as
// many as the candidates.
func keysOf(args []funroute.Value) ([]funroute.Money, error) {
	keys, _, err := amountsOf(args[1])
	if err != nil {
		return nil, err
	}
	if items, _ := args[0].Length(); items != len(keys) {
		return nil, fmt.Errorf("%w: sort_by has %d candidates and %d keys", funroute.ErrDomain, items, len(keys))
	}
	return keys, nil
}

// best is the indexes of the count first keys in order, ties in their own
// order: every one sorted, or for fewer the count first kept in a heap whose
// top is the last of them, n log count rather than n log n.
func best(keys []funroute.Money, count int, ascending bool) []int {
	before := func(a, b int) int {
		if order := cmp.Compare(keys[a].Minor(), keys[b].Minor()); order != 0 {
			if ascending {
				return order
			}
			return -order
		}
		return cmp.Compare(a, b)
	}
	if count == len(keys) {
		order := make([]int, len(keys))
		for i := range order {
			order[i] = i
		}
		slices.SortFunc(order, before)
		return order
	}
	kept := make([]int, 0, count)
	for i := range keys {
		switch {
		case len(kept) < count:
			kept = append(kept, i)
			siftUp(kept, before)
		case count > 0 && before(i, kept[0]) < 0:
			kept[0] = i
			siftDown(kept, before)
		}
	}
	slices.SortFunc(kept, before)
	return kept
}

// siftUp and siftDown keep heap's last-ranked index on top as one is added
// at the end or the top replaced.
func siftUp(heap []int, before func(a, b int) int) {
	for at := len(heap) - 1; at > 0; {
		parent := (at - 1) / 2
		if before(heap[parent], heap[at]) >= 0 {
			return
		}
		heap[parent], heap[at] = heap[at], heap[parent]
		at = parent
	}
}

func siftDown(heap []int, before func(a, b int) int) {
	for at := 0; ; {
		last := at
		for _, child := range []int{2*at + 1, 2*at + 2} {
			if child < len(heap) && before(heap[child], heap[last]) > 0 {
				last = child
			}
		}
		if last == at {
			return
		}
		heap[at], heap[last] = heap[last], heap[at]
		at = last
	}
}

// arranged is the candidates in order: a native array's as its own slice.
func arranged(items funroute.Value, order []int) (funroute.Value, error) {
	switch items := backing(items).(type) {
	case []int64:
		return funroute.ToValue(picked(items, order))
	case []float64:
		return funroute.ToValue(picked(items, order))
	case []string:
		return funroute.ToValue(picked(items, order))
	case []bool:
		return funroute.ToValue(picked(items, order))
	}
	values := itemsOf(items)
	return funroute.Array(elementType(items), picked(values, order))
}

func picked[T any](items []T, order []int) []T {
	out := make([]T, len(order))
	for i, at := range order {
		out[i] = items[at]
	}
	return out
}

// errNoExtreme is an arg_min or arg_max, or a money min or max, of nothing.
var errNoExtreme = fmt.Errorf("%w: an empty array has no extreme", funroute.ErrDomain)
