package machine

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/nethinwei/funroute/internal/money"
)

// Picking one out of a list of candidates is what a routing rule does, and
// doing it needs two lists side by side: the candidates and the key each is
// judged by. There are no lambdas, so that is the shape everything takes —
// sort_by(channels, fees) rather than sort_by(channels, c => c.fee).
//
// The lists must be the same length. Nothing here invents an answer for a
// candidate it has no key for.
func selectSpecs() []FunctionSpec {
	item := TypeVar("T")
	list := ArrayOf(item)
	return []FunctionSpec{
		{
			Name: "indices", Params: []Type{list}, Result: ArrayOf(IntType), Eval: indicesOf,
			Doc: Doc{
				Label: "下标序列", Category: "选择",
				Description: "这个数组的下标，0 到长度减一。配推导式就能按位置把两个数组对起来：[names[i] for i in indices(fees) if fees[i] < cap]。",
				Params:      []string{"数组"}, Result: "下标数组",
			},
		},
		{
			Name: "index_of", Params: []Type{list, item}, Result: IntType, Eval: indexOfItem,
			Doc: Doc{
				Label: "元素位置", Category: "选择",
				Description: "元素第一次出现的下标；不在里面是错误，先用 x in xs 判断。",
				Params:      []string{"数组", "元素"}, Result: "下标",
			},
		},
	}
}

func indicesOf(_ context.Context, args []Value) (Value, error) {
	out := make([]int64, args[0].length())
	for i := range out {
		out[i] = int64(i)
	}
	return nativeArray(out), nil
}

// errNotInArray is index_of of an item the array does not hold.
var errNotInArray = fmt.Errorf("%w: the array does not contain that item", ErrDomain)

func indexOfItem(_ context.Context, args []Value) (Value, error) {
	wanted := args[1]
	var at int
	switch items := args[0].box.(type) {
	case []int64:
		at = slices.Index(items, wanted.i)
	case []float64:
		at = slices.Index(items, wanted.f)
	case []string:
		at = slices.Index(items, wanted.s)
	case []bool:
		at = slices.Index(items, wanted.b)
	default:
		at = slices.IndexFunc(valuesOf(args[0]), wanted.Equal)
	}
	if at < 0 {
		return Value{}, errNotInArray
	}
	return Int(int64(at)), nil
}

// valuesOf is an array's items as values: a nested array's own.
func valuesOf(array Value) []Value {
	items, _ := array.Array()
	return items
}

// keyedSpecs are the keyed sorts, both ways, and the keyed cuts: sort_by
// with a direction and a count, because "the three cheapest" and "the three
// best" are what a rule asks for. A key is a fee, a rate or a name; the
// money keys are money's (the standard pack's).
func keyedSpecs() []FunctionSpec {
	keys := []Type{IntType, FloatType, StringType}
	specs := make([]FunctionSpec, 0, 4*len(keys))
	for _, keyed := range []keyedFunction{
		{"sort_by", "按键排序", "按第二个数组的键把第一个数组升序排好，两者长度必须相同；相等的保持原顺序。取最便宜的三个写 take(sort_by(channels, fees), 3)。", true, false},
		{"sort_by_desc", "按键降序", "sort_by 的反向：按键从大到小排。相等的保持原顺序。", false, false},
		{"bottom_k", "取最小的 k 个", "按键升序取前 k 个候选：最便宜的几个渠道。k 大于长度就取完。", true, true},
		{"top_k", "取最大的 k 个", "按键降序取前 k 个候选：成功率最高的几个渠道。k 大于长度就取完。", false, true},
	} {
		for _, key := range keys {
			specs = append(specs, keyed.spec(key))
		}
	}
	return specs
}

type keyedFunction struct {
	name, label, description string
	ascending, ranked        bool
}

func (k keyedFunction) spec(key Type) FunctionSpec {
	list := ArrayOf(TypeVar("T"))
	ascending := k.ascending
	spec := FunctionSpec{
		Name: k.name, Params: []Type{list, ArrayOf(key)}, Result: list,
		Eval: func(_ context.Context, args []Value) (Value, error) {
			return sortedBy(args, ascending)
		},
		Doc: Doc{
			Label: k.label, Category: "选择", Description: k.description,
			Params: []string{"候选", "键"}, Result: "排序后的候选",
		},
	}
	if k.ranked {
		spec.Params = append(spec.Params, IntType)
		spec.Eval = func(_ context.Context, args []Value) (Value, error) {
			return rankedBy(args, ascending)
		}
		spec.Doc.Params, spec.Doc.Result = []string{"候选", "键", "个数"}, "选出的候选"
	}
	return spec
}

// sortedBy is the candidates in the order of their keys, ties in place: an
// order worked out on the keys' own backing, the candidates rearranged by it.
func sortedBy(args []Value, ascending bool) (Value, error) {
	items, keys := args[0], args[1]
	n := items.length()
	if count := keys.length(); count != n {
		return Value{}, fmt.Errorf("%w: sort_by has %d candidates and %d keys", ErrDomain, n, count)
	}
	var order []int
	switch box := keys.box.(type) {
	case []int64:
		order = orderBy(box, ascending)
	case []float64:
		order = orderBy(box, ascending)
	case []string:
		order = orderBy(box, ascending)
	default:
		return Value{}, fmt.Errorf("internal error: keys of %s", keys.Type())
	}
	return rearranged(items, n, func(i int) int { return order[i] }), nil
}

// rankedBy is the count first candidates in the order of their keys, ties
// in place: sortedBy's first count, found without sorting the rest.
func rankedBy(args []Value, ascending bool) (Value, error) {
	items, keys, count := args[0], args[1], args[2].i
	n := items.length()
	if m := keys.length(); m != n {
		return Value{}, fmt.Errorf("%w: sort_by has %d candidates and %d keys", ErrDomain, n, m)
	}
	if count < 0 {
		return Value{}, fmt.Errorf("%w: a count of candidates cannot be negative, got %d", ErrArithmetic, count)
	}
	k := int(min(count, int64(n)))
	var order []int
	switch box := keys.box.(type) {
	case []int64:
		order = firstOf(box, k, ascending)
	case []float64:
		order = firstOf(box, k, ascending)
	case []string:
		order = firstOf(box, k, ascending)
	default:
		return Value{}, fmt.Errorf("internal error: keys of %s", keys.Type())
	}
	return rearranged(items, k, func(i int) int { return order[i] }), nil
}

// firstOf is the positions of the k first keys in order, ties by position —
// a stable sort's first k — kept in a heap whose root is the last of them,
// so the work is n log k. When k is most of the keys, sorting them all is
// quicker.
func firstOf[T cmp.Ordered](keys []T, k int, ascending bool) []int {
	if 4*k >= len(keys) {
		return orderBy(keys, ascending)[:k]
	}
	before := func(a, b int) bool {
		order := cmp.Compare(keys[a], keys[b])
		if !ascending {
			order = -order
		}
		return order < 0 || order == 0 && a < b
	}
	kept := make([]int, 0, k)
	for i := range keys {
		switch {
		case len(kept) < k:
			kept = append(kept, i)
			siftUp(kept, len(kept)-1, before)
		case k > 0 && before(i, kept[0]):
			kept[0] = i
			siftDown(kept, before)
		}
	}
	slices.SortFunc(kept, func(a, b int) int { return cmp.Compare(boolRank(before(b, a)), boolRank(before(a, b))) })
	return kept
}

func boolRank(b bool) int {
	if b {
		return 1
	}
	return 0
}

// siftUp moves the position at i up the heap past every one that comes
// before it: the heap's root is the one that comes last.
func siftUp(heap []int, i int, before func(a, b int) bool) {
	for i > 0 {
		parent := (i - 1) / 2
		if !before(heap[parent], heap[i]) {
			return
		}
		heap[parent], heap[i] = heap[i], heap[parent]
		i = parent
	}
}

// siftDown moves a new root down past every one that comes after it.
func siftDown(heap []int, before func(a, b int) bool) {
	for i := 0; ; {
		later, left := i, 2*i+1
		if left < len(heap) && before(heap[later], heap[left]) {
			later = left
		}
		if right := left + 1; right < len(heap) && before(heap[later], heap[right]) {
			later = right
		}
		if later == i {
			return
		}
		heap[i], heap[later] = heap[later], heap[i]
		i = later
	}
}

func orderBy[T cmp.Ordered](keys []T, ascending bool) []int {
	order := make([]int, len(keys))
	for i := range order {
		order[i] = i
	}
	slices.SortStableFunc(order, func(a, b int) int {
		if ascending {
			return cmp.Compare(keys[a], keys[b])
		}
		return cmp.Compare(keys[b], keys[a])
	})
	return order
}

// positionSpecs are the ones a routing rule reaches for most: which
// candidate, not which value. channels[arg_min(fees)] is the cheapest channel.
func positionSpecs() []FunctionSpec {
	specs := make([]FunctionSpec, 0, 6) // two names, three types each
	for _, extreme := range []struct {
		name, label, description string
		smallest                 bool
	}{
		{"arg_min", "最小值的位置", "最小元素的下标；并列取第一个，空数组报错。channels[arg_min(fees)] 就是最便宜的那个。", true},
		{"arg_max", "最大值的位置", "最大元素的下标；并列取第一个，空数组报错。", false},
	} {
		doc := Doc{
			Label: extreme.label, Category: "选择",
			Description: extreme.description, Params: []string{"键"}, Result: "下标",
		}
		smallest := extreme.smallest
		specs = append(specs, libEach(extreme.name, doc,
			extremeIndex[int64](smallest), extremeIndex[float64](smallest), extremeIndex[string](smallest))...)
	}
	return specs
}

// dictSpecs are two things a dictionary needs that a comprehension cannot
// say. `d["k"]` on a missing key stays an error — inventing a zero there
// would put a silently wrong amount in a routing decision — and `get` is
// where a rule says what the absence means.
func dictSpecs() []FunctionSpec {
	item := TypeVar("T")
	dict := DictOf(item)
	return []FunctionSpec{
		{
			Name: "get", Params: []Type{dict, StringType, item}, Result: item, Eval: evalGet,
			Doc: Doc{
				Label: "取值或默认", Category: "容器",
				Description: `按键取值，键不在就返回第三个参数。d["k"] 缺键时报错，要兜底就写 get(d, "k", 0)。`,
				Params:      []string{"字典", "键", "默认值"}, Result: "值或默认值",
			},
		},
		{
			Name: "merge", Params: []Type{dict, dict}, Result: dict, Eval: evalMerge,
			Doc: Doc{
				Label: "合并字典", Category: "容器",
				Description: "把两个字典叠在一起，键相同时取后一个的值：默认费率叠上本次覆盖就是 merge(defaults, overrides)。",
				Params:      []string{"底层", "覆盖层"}, Result: "合并结果",
			},
		},
	}
}

func evalGet(_ context.Context, args []Value) (Value, error) {
	if value, ok := args[0].lookup(args[1].s); ok {
		return value, nil
	}
	return args[2], nil
}

// evalMerge lays the second dictionary over the first, on their own
// backings: of one kind, as their type is one.
func evalMerge(_ context.Context, args []Value) (Value, error) {
	switch base := args[0].box.(type) {
	case map[string]bool:
		return merged(base, args[1].box), nil
	case map[string]int64:
		return merged(base, args[1].box), nil
	case map[string]float64:
		return merged(base, args[1].box), nil
	case map[string]string:
		return merged(base, args[1].box), nil
	case map[string]money.Money:
		return merged(base, args[1].box), nil
	case *nestedDict:
		over, _ := args[1].box.(*nestedDict)
		return Value{kind: DictKind, box: &nestedDict{elem: base.elem, entries: overlaid(base.entries, over.entries)}}, nil
	}
	return Value{}, errors.New("merge needs two dictionaries")
}

func merged[T any](base map[string]T, over any) Value {
	top, _ := over.(map[string]T)
	return Value{kind: DictKind, box: overlaid(base, top)}
}

// overlaid is base with top's entries over it, in a map of its own.
func overlaid[T any](base, top map[string]T) map[string]T {
	out := make(map[string]T, len(base)+len(top))
	maps.Copy(out, base)
	maps.Copy(out, top)
	return out
}
