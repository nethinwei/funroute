package std

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"math"
	"slices"

	"github.com/nethinwei/funroute"
)

// sequenceSpecs are the sliding-window family. Every one of them keeps the
// "no missing value" rule by shortening the result rather than inventing a
// hole: deltas of n items has n-1 entries, and a window wider than the array
// yields nothing at all.
func sequenceSpecs() []funroute.FunctionSpec {
	list := funroute.ArrayOf(funroute.TypeVar("T"))
	return append([]funroute.FunctionSpec{
		{
			Name: "windows", Params: []funroute.Type{list, funroute.IntType}, Result: funroute.ArrayOf(list), Eval: slidingWindows,
			Doc: funroute.Doc{
				Constexpr: true, Label: "滑动窗口", Category: "数组",
				Description: "每 size 个相邻元素一组，逐格滑动；窗口比数组还宽就一个都没有。近三笔合计写 [sum(w) for w in windows(amounts, 3)]。",
				Params:      []string{"数组", "窗口大小"}, Result: "各窗口",
			},
		},
		{
			Name: "chunk", Params: []funroute.Type{list, funroute.IntType}, Result: funroute.ArrayOf(list), Eval: chunkItems,
			Doc: funroute.Doc{
				Constexpr: true, Label: "分批", Category: "数组",
				Description: "按固定大小切成不重叠的几批，最后一批可能不满。批量提交用它。",
				Params:      []string{"数组", "每批大小"}, Result: "各批",
			},
		},
		{
			Name: "intersect", Params: []funroute.Type{list, list}, Result: list, Eval: distinctItems,
			Doc: funroute.Doc{
				Constexpr: true, Label: "交集", Category: "数组",
				Description: "两个数组里都有的元素，按第一个数组的顺序，重复只留一次。",
				Params:      []string{"前一个", "后一个"}, Result: "交集",
			},
		},
		{
			Name: "except", Params: []funroute.Type{list, list}, Result: list, Eval: exceptItems,
			Doc: funroute.Doc{
				Constexpr: true, Label: "差集", Category: "数组",
				Description: "在第一个数组里、不在第二个数组里的元素，保持原顺序，重复只留一次。",
				Params:      []string{"前一个", "后一个"}, Result: "差集",
			},
		},
	}, eachType("deltas", funroute.Doc{
		Label: "相邻差", Category: "数组",
		Description: "每一项与前一项的差，所以结果比输入少一个；一项或空数组得到空数组。与上一笔比较用它。",
		Params:      []string{"数组"}, Result: "差值序列",
	}, deltasOf(subtractInts), deltasOf(subtractFloats))...)
}

func slidingWindows(_ context.Context, args []funroute.Value) (funroute.Value, error) {
	return groupsOf(args, "windows", true)
}

func chunkItems(_ context.Context, args []funroute.Value) (funroute.Value, error) {
	return groupsOf(args, "chunk", false)
}

// groupsOf cuts an array into runs of size items: every window of them one
// item apart, or batches one after the other, the last of which may be short.
func groupsOf(args []funroute.Value, name string, windows bool) (funroute.Value, error) {
	size, _ := args[1].Int()
	if size <= 0 {
		return funroute.Value{}, fmt.Errorf("%w: %s needs a size of at least one, got %d", funroute.ErrArithmetic, name, size)
	}
	items, width := itemsOf(args[0]), int(size)
	stride, starts := width, len(items)
	if windows {
		stride, starts = 1, len(items)-width+1
	}
	var groups []funroute.Value
	for start := 0; start < starts; start += stride {
		group, err := funroute.Array(elementType(args[0]), items[start:min(start+width, len(items))])
		if err != nil {
			return funroute.Value{}, err
		}
		groups = append(groups, group)
	}
	return funroute.Array(args[0].Type(), groups)
}

// distinctItems is unique and intersect: an array's items, in order, each
// once — of them, those a second array has too, when there is one.
func distinctItems(_ context.Context, args []funroute.Value) (funroute.Value, error) {
	return distinct(args, true)
}

func exceptItems(_ context.Context, args []funroute.Value) (funroute.Value, error) {
	return distinct(args, false)
}

// distinct is the first array's items, in order, each once: all of them, or,
// held against a second array, those it has or those it has not.
func distinct(args []funroute.Value, keepShared bool) (funroute.Value, error) {
	switch items := backing(args[0]).(type) {
	case []int64:
		return distinctNative(items, args, keepShared)
	case []float64:
		return distinctNative(items, args, keepShared)
	case []string:
		return distinctNative(items, args, keepShared)
	case []bool:
		return distinctNative(items, args, keepShared)
	}
	var other []funroute.Value
	if len(args) > 1 {
		other = itemsOf(args[1])
	}
	var out []funroute.Value
	for _, item := range itemsOf(args[0]) {
		if (len(args) == 1 || containsValue(other, item) == keepShared) && !containsValue(out, item) {
			out = append(out, item)
		}
	}
	return funroute.Array(elementType(args[0]), out)
}

// distinctNative is distinct on a native array, by a set: a float's zero and
// negative zero are one key, as they are one value.
func distinctNative[T comparable](items []T, args []funroute.Value, keepShared bool) (funroute.Value, error) {
	var other map[T]bool
	if len(args) > 1 {
		held, _ := backing(args[1]).([]T)
		other = make(map[T]bool, len(held))
		for _, item := range held {
			other[item] = true
		}
	}
	out, seen := []T{}, make(map[T]bool, len(items))
	for _, item := range items {
		if (other == nil || other[item] == keepShared) && !seen[item] {
			seen[item] = true
			out = append(out, item)
		}
	}
	return funroute.ToValue(out)
}

func deltasOf[T int64 | float64](subtract func(T, T) (T, error)) func([]T) ([]T, error) {
	return func(items []T) ([]T, error) {
		if len(items) < 2 {
			return []T{}, nil
		}
		out := make([]T, len(items)-1)
		for i := 1; i < len(items); i++ {
			difference, err := subtract(items[i], items[i-1])
			if err != nil {
				return nil, err
			}
			out[i-1] = difference
		}
		return out, nil
	}
}

func subtractInts(left, right int64) (int64, error) {
	if right < 0 && left > math.MaxInt64+right || right > 0 && left < math.MinInt64+right {
		return 0, fmt.Errorf("%w: integer overflow in deltas", funroute.ErrArithmetic)
	}
	return left - right, nil
}

func subtractFloats(left, right float64) (float64, error) {
	return left - right, nil
}

// shapeSpecs are the array functions that are about the shape of a list
// rather than about what is in it: take one end, cut it short, turn it
// around, put two together, drop repeats, flatten one level. They are
// generic, so they are written as FunctionSpecs — reflection cannot express
// "array of any T".
func shapeSpecs() []funroute.FunctionSpec {
	item := funroute.TypeVar("T")
	list := funroute.ArrayOf(item)
	shaped := func(name, label, description, result string, params []funroute.Type, labels []string, eval funroute.EvalFunc) funroute.FunctionSpec {
		return funroute.FunctionSpec{
			Name: name, Params: params, Result: list, Eval: eval,
			Doc: funroute.Doc{
				Label: label, Category: "数组",
				Description: description, Params: labels, Result: result,
			},
		}
	}
	end := func(name, label, description, result string, eval funroute.EvalFunc) funroute.FunctionSpec {
		return funroute.FunctionSpec{
			Name: name, Params: []funroute.Type{list}, Result: item, Eval: eval,
			Doc: funroute.Doc{
				Label: label, Category: "数组",
				Description: description, Params: []string{"数组"}, Result: result,
			},
		}
	}
	return []funroute.FunctionSpec{
		first(end("first", "首个元素", "取数组的第一个元素；空数组报错，因为没有元素可取。", "首个元素", firstItem)),
		end("last", "末个元素", "取数组的最后一个元素；空数组报错。", "末个元素", lastItem),
		shaped("slice", "取一段", "按下标取 [start, end) 这一段，下标从 0 开始；越界报错，不静默截断。", "这一段",
			[]funroute.Type{list, funroute.IntType, funroute.IntType}, []string{"数组", "起点", "终点"}, sliceItems),
		shaped("take", "取前 n 个", "取数组的前 n 个元素；n 大于长度就取完，n 为负是错误。", "前 n 个",
			[]funroute.Type{list, funroute.IntType}, []string{"数组", "个数"}, takeItems),
		shaped("reverse", "反转", "把数组倒过来。", "反转后的数组",
			[]funroute.Type{list}, []string{"数组"}, reverseItems),
		shaped("concat", "拼接数组", "把两个同型数组接成一个。", "拼接结果",
			[]funroute.Type{list, list}, []string{"前一个", "后一个"}, concatItems),
		shaped("unique", "去重", "按相等判断去掉重复元素，保留第一次出现的顺序。", "去重后的数组",
			[]funroute.Type{list}, []string{"数组"}, distinctItems),
		shaped("flatten", "拉平一层", "把数组的数组拉平成一层；嵌套推导式配它就是多层遍历。", "拉平后的数组",
			[]funroute.Type{funroute.ArrayOf(list)}, []string{"嵌套数组"}, flattenItems),
	}
}

// sortSpecs cover both directions of a plain sort. Sorting by a separate
// array of keys is a different question with a different shape — that is
// sort_by, and it lives with the other two-list functions in select.go.
func sortSpecs() []funroute.FunctionSpec {
	return slices.Concat(eachType("sort", funroute.Doc{
		Label: "排序", Category: "数组",
		Description: "按自然顺序升序排列（数值按大小，字符串按 UTF-8 字节序）。要按别的键排，先用推导式算出键。",
		Params:      []string{"数组"}, Result: "升序数组",
	}, sorter[int64](false, nil), sorter(false, hasNegativeZero), sorter[string](false, nil)), eachType("sort_desc", funroute.Doc{
		Label: "降序排序", Category: "数组",
		Description: "按自然顺序降序排列，省得写 reverse(sort(xs))。",
		Params:      []string{"数组"}, Result: "降序数组",
	}, sorter[int64](true, nil), sorter(true, hasNegativeZero), sorter[string](true, nil)))
}

// sorter sorts a copy: a Value's backing is read-only, so sorting in place
// would edit the caller's array. The sort is stable. Where no two equal
// items can be told apart — ints, strings, floats without a -0 among them,
// which distinct, when it is given, looks for — the order among equal ones
// cannot show, and the faster sort gives the same array.
func sorter[T cmp.Ordered](descending bool, distinct func([]T) bool) func([]T) []T {
	return func(items []T) []T {
		out := append([]T(nil), items...)
		switch {
		case distinct != nil && distinct(out):
			sortStable(out, descending)
		case descending:
			slices.Sort(out)
			slices.Reverse(out)
		default:
			slices.Sort(out)
		}
		return out
	}
}

func sortStable[T cmp.Ordered](items []T, descending bool) {
	if descending {
		slices.SortStableFunc(items, func(a, b T) int { return cmp.Compare(b, a) })
	} else {
		slices.SortStableFunc(items, cmp.Compare[T])
	}
}

// hasNegativeZero reports a -0 among floats: it equals 0 and still differs
// from it, the one such pair a float array can hold.
func hasNegativeZero(items []float64) bool {
	return slices.ContainsFunc(items, func(f float64) bool { return f == 0 && math.Signbit(f) })
}

// first makes spec a first-item fold: first([… for x in xs if …]) stops at
// the first item yielded, the rest neither computed nor built.
func first(spec funroute.FunctionSpec) funroute.FunctionSpec {
	spec.Fold = &funroute.Fold{First: true}
	return spec
}

func firstItem(_ context.Context, args []funroute.Value) (funroute.Value, error) {
	value, ok := args[0].At(0)
	if !ok {
		return funroute.Value{}, fmt.Errorf("%w: first of an empty array", funroute.ErrDomain)
	}
	return value, nil
}

func lastItem(_ context.Context, args []funroute.Value) (funroute.Value, error) {
	length, _ := args[0].Length()
	value, ok := args[0].At(length - 1)
	if !ok {
		return funroute.Value{}, fmt.Errorf("%w: last of an empty array", funroute.ErrDomain)
	}
	return value, nil
}

func sliceItems(_ context.Context, args []funroute.Value) (funroute.Value, error) {
	start, _ := args[1].Int()
	end, _ := args[2].Int()
	if part, ok := args[0].Slice(int(start), int(end)); ok {
		return part, nil
	}
	length, _ := args[0].Length()
	return funroute.Value{}, fmt.Errorf("%w: slice [%d, %d) is outside an array of %d items", funroute.ErrDomain, start, end, length)
}

func takeItems(_ context.Context, args []funroute.Value) (funroute.Value, error) {
	count, _ := args[1].Int()
	return takeFirst(args[0], count, "take needs a count of zero or more, got %d")
}

// takeFirst is an array's first count items, all of them when it has fewer;
// a negative count is an error, in the caller's words.
func takeFirst(array funroute.Value, count int64, negative string) (funroute.Value, error) {
	if count < 0 {
		return funroute.Value{}, fmt.Errorf("%w: "+negative, funroute.ErrArithmetic, count)
	}
	length, _ := array.Length()
	if first, ok := array.Slice(0, int(min(count, int64(length)))); ok {
		return first, nil
	}
	return funroute.Value{}, errors.New("take needs an array")
}

func reverseItems(_ context.Context, args []funroute.Value) (funroute.Value, error) {
	switch items := backing(args[0]).(type) {
	case []int64:
		return funroute.ToValue(reversed(items))
	case []float64:
		return funroute.ToValue(reversed(items))
	case []string:
		return funroute.ToValue(reversed(items))
	case []bool:
		return funroute.ToValue(reversed(items))
	}
	items := itemsOf(args[0])
	out := make([]funroute.Value, len(items))
	for i, item := range items {
		out[len(items)-1-i] = item
	}
	return funroute.Array(elementType(args[0]), out)
}

func reversed[T any](items []T) []T {
	out := make([]T, len(items))
	for i, item := range items {
		out[len(items)-1-i] = item
	}
	return out
}

func concatItems(_ context.Context, args []funroute.Value) (funroute.Value, error) {
	return funroute.Array(elementType(args[0]), slices.Concat(itemsOf(args[0]), itemsOf(args[1])))
}

func flattenItems(_ context.Context, args []funroute.Value) (funroute.Value, error) {
	inners := itemsOf(args[0])
	total := 0
	for _, inner := range inners {
		length, _ := inner.Length()
		total += length
	}
	out := make([]funroute.Value, 0, total)
	for _, inner := range inners {
		out = append(out, itemsOf(inner)...)
	}
	inner, ok := elementType(args[0]).Elem()
	if !ok {
		return funroute.Value{}, errors.New("flatten needs an array of arrays")
	}
	return funroute.Array(inner, out)
}

func containsValue(values []funroute.Value, wanted funroute.Value) bool {
	for _, value := range values {
		if value.Equal(wanted) {
			return true
		}
	}
	return false
}

// itemsOf reads a container's elements. The values are the container's own,
// handed over read-only like everywhere else at this boundary. A native
// array is built into values here, so the functions that run often work on
// its backing instead, and wrap theirs the same way.
func itemsOf(value funroute.Value) []funroute.Value {
	items, _ := value.Array()
	return items
}

// backing is a native array's own slice — the []bool, []int64, []float64 or
// []string an array of those is held in, which Any hands over with no pass —
// or nil: for an empty array, which the general path serves as well, and for
// any other, whose Any would build a Go value of every item.
func backing(value funroute.Value) any {
	first, ok := value.At(0)
	if !ok {
		return nil
	}
	_, isBool := first.Bool()
	_, isInt := first.Int()
	_, isFloat := first.Float()
	_, isString := first.String()
	if isBool || isInt || isFloat || isString {
		return value.Any()
	}
	return nil
}

func elementType(value funroute.Value) funroute.Type {
	elem, _ := value.Type().Elem()
	return elem
}
