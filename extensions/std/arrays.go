package std

import (
	"context"
	"fmt"
	"sort"

	"github.com/nethinwei/funroute"
)

// The array functions that are about the shape of a list rather than about
// what is in it: take one end, cut it short, turn it around, put two together,
// drop repeats, flatten one level. They are generic, so they are written as
// FunctionSpecs — reflection cannot express "array of any T".
func registerArrays(registry *funroute.Registry) error {
	for _, spec := range arraySpecs() {
		if err := register(registry, spec); err != nil {
			return err
		}
	}
	if err := registerSort(registry); err != nil {
		return err
	}
	return registerSequences(registry)
}

// registerSequences is the sliding-window family. Every one of them keeps the
// "no missing value" rule by shortening the result rather than inventing a
// hole: deltas of n items has n-1 entries, and a window wider than the array
// yields nothing at all.
func registerSequences(registry *funroute.Registry) error {
	item := funroute.TypeVar("T")
	list := funroute.ArrayOf(item)
	for _, spec := range []funroute.FunctionSpec{
		{
			Name: "windows", Params: []funroute.Type{list, funroute.IntType}, Result: funroute.ArrayOf(list), Eval: slidingWindows,
			Doc: funroute.Doc{
				Constexpr: true, Label: "滑动窗口", Category: "数组", Cost: 8,
				Description: "每 size 个相邻元素一组，逐格滑动；窗口比数组还宽就一个都没有。近三笔合计写 [sum(w) for w in windows(amounts, 3)]。",
				Params:      []string{"数组", "窗口大小"}, Result: "各窗口",
			},
		},
		{
			Name: "chunk", Params: []funroute.Type{list, funroute.IntType}, Result: funroute.ArrayOf(list), Eval: chunkItems,
			Doc: funroute.Doc{
				Constexpr: true, Label: "分批", Category: "数组", Cost: 6,
				Description: "按固定大小切成不重叠的几批，最后一批可能不满。批量提交用它。",
				Params:      []string{"数组", "每批大小"}, Result: "各批",
			},
		},
		{
			Name: "intersect", Params: []funroute.Type{list, list}, Result: list, Eval: intersectItems,
			Doc: funroute.Doc{
				Constexpr: true, Label: "交集", Category: "数组", Cost: 7,
				Description: "两个数组里都有的元素，按第一个数组的顺序，重复只留一次。",
				Params:      []string{"前一个", "后一个"}, Result: "交集",
			},
		},
		{
			Name: "except", Params: []funroute.Type{list, list}, Result: list, Eval: exceptItems,
			Doc: funroute.Doc{
				Constexpr: true, Label: "差集", Category: "数组", Cost: 7,
				Description: "在第一个数组里、不在第二个数组里的元素，保持原顺序，重复只留一次。",
				Params:      []string{"前一个", "后一个"}, Result: "差集",
			},
		},
	} {
		if err := register(registry, spec); err != nil {
			return err
		}
	}
	return registerDeltas(registry)
}

func registerDeltas(registry *funroute.Registry) error {
	doc := funroute.Doc{
		Constexpr: true, Label: "相邻差", Category: "数组", Cost: 6,
		Description: "每一项与前一项的差，所以结果比输入少一个；一项或空数组得到空数组。与上一笔比较用它。",
		Params:      []string{"数组"}, Result: "差值序列",
	}
	return eachType(registry, "deltas", doc, deltasOf[int64], deltasOf[float64])
}

func slidingWindows(_ context.Context, args []funroute.Value) (funroute.Value, error) {
	items, size, err := sizedGroups(args, "windows")
	if err != nil {
		return funroute.Value{}, err
	}
	var groups []funroute.Value
	for start := 0; start+size <= len(items); start++ {
		window, err := funroute.Array(elementType(args[0]), items[start:start+size])
		if err != nil {
			return funroute.Value{}, err
		}
		groups = append(groups, window)
	}
	return funroute.Array(args[0].Type(), groups)
}

func chunkItems(_ context.Context, args []funroute.Value) (funroute.Value, error) {
	items, size, err := sizedGroups(args, "chunk")
	if err != nil {
		return funroute.Value{}, err
	}
	var groups []funroute.Value
	for start := 0; start < len(items); start += size {
		end := min(start+size, len(items))
		batch, err := funroute.Array(elementType(args[0]), items[start:end])
		if err != nil {
			return funroute.Value{}, err
		}
		groups = append(groups, batch)
	}
	return funroute.Array(args[0].Type(), groups)
}

func sizedGroups(args []funroute.Value, name string) ([]funroute.Value, int, error) {
	size, _ := args[1].Int()
	if size <= 0 {
		return nil, 0, fmt.Errorf("%s needs a size of at least one, got %d", name, size)
	}
	return itemsOf(args[0]), int(size), nil
}

func intersectItems(_ context.Context, args []funroute.Value) (funroute.Value, error) {
	return setOperation(args, true)
}

func exceptItems(_ context.Context, args []funroute.Value) (funroute.Value, error) {
	return setOperation(args, false)
}

func setOperation(args []funroute.Value, keepShared bool) (funroute.Value, error) {
	other := itemsOf(args[1])
	var out []funroute.Value
	for _, item := range itemsOf(args[0]) {
		if containsValue(other, item) == keepShared && !containsValue(out, item) {
			out = append(out, item)
		}
	}
	return funroute.Array(elementType(args[0]), out)
}

func deltasOf[T int64 | float64](items []T) ([]T, error) {
	if len(items) < 2 {
		return []T{}, nil
	}
	out := make([]T, len(items)-1)
	for i := 1; i < len(items); i++ {
		out[i-1] = items[i] - items[i-1]
	}
	return out, nil
}

// arraySpecs is the shape-of-a-list family: take one end, cut it short, turn
// it around, put two together, drop repeats, flatten one level.
func arraySpecs() []funroute.FunctionSpec {
	item := funroute.TypeVar("T")
	list := funroute.ArrayOf(item)
	shaped := func(name, label, description, result string, params []funroute.Type, labels []string, cost uint64, eval funroute.EvalFunc) funroute.FunctionSpec {
		return funroute.FunctionSpec{
			Name: name, Params: params, Result: list, Eval: eval,
			Doc: funroute.Doc{
				Constexpr: true, Label: label, Category: "数组", Cost: cost,
				Description: description, Params: labels, Result: result,
			},
		}
	}
	end := func(name, label, description, result string, eval funroute.EvalFunc) funroute.FunctionSpec {
		return funroute.FunctionSpec{
			Name: name, Params: []funroute.Type{list}, Result: item, Eval: eval,
			Doc: funroute.Doc{
				Constexpr: true, Label: label, Category: "数组", Cost: 2,
				Description: description, Params: []string{"数组"}, Result: result,
			},
		}
	}
	return []funroute.FunctionSpec{
		end("first", "首个元素", "取数组的第一个元素；空数组报错，因为没有元素可取。", "首个元素", firstItem),
		end("last", "末个元素", "取数组的最后一个元素；空数组报错。", "末个元素", lastItem),
		shaped("slice", "取一段", "按下标取 [start, end) 这一段，下标从 0 开始；越界报错，不静默截断。", "这一段",
			[]funroute.Type{list, funroute.IntType, funroute.IntType}, []string{"数组", "起点", "终点"}, 4, sliceItems),
		shaped("take", "取前 n 个", "取数组的前 n 个元素；n 大于长度就取完，n 为负是错误。", "前 n 个",
			[]funroute.Type{list, funroute.IntType}, []string{"数组", "个数"}, 4, takeItems),
		shaped("reverse", "反转", "把数组倒过来。", "反转后的数组",
			[]funroute.Type{list}, []string{"数组"}, 4, reverseItems),
		shaped("concat", "拼接数组", "把两个同型数组接成一个。", "拼接结果",
			[]funroute.Type{list, list}, []string{"前一个", "后一个"}, 5, concatItems),
		shaped("unique", "去重", "按相等判断去掉重复元素，保留第一次出现的顺序。", "去重后的数组",
			[]funroute.Type{list}, []string{"数组"}, 6, uniqueItems),
		shaped("flatten", "拉平一层", "把数组的数组拉平成一层；嵌套推导式配它就是多层遍历。", "拉平后的数组",
			[]funroute.Type{funroute.ArrayOf(list)}, []string{"嵌套数组"}, 6, flattenItems),
	}
}

// registerSort covers both directions of a plain sort. Sorting by a separate
// array of keys is a different question with a different shape — that is
// sort_by, and it lives with the other two-list functions in select.go.
func registerSort(registry *funroute.Registry) error {
	up := funroute.Doc{Constexpr: true,
		Label: "排序", Category: "数组", Cost: 8,
		Description: "按自然顺序升序排列（数值按大小，字符串按 UTF-8 字节序）。要按别的键排，先用推导式算出键。",
		Params:      []string{"数组"}, Result: "升序数组",
	}
	if err := eachType(registry, "sort", up, ascending[int64], ascending[float64], ascending[string]); err != nil {
		return err
	}
	down := funroute.Doc{Constexpr: true,
		Label: "降序排序", Category: "数组", Cost: 8,
		Description: "按自然顺序降序排列，省得写 reverse(sort(xs))。",
		Params:      []string{"数组"}, Result: "降序数组",
	}
	return eachType(registry, "sort_desc", down, descending[int64], descending[float64], descending[string])
}

// Both sort a copy: a Value's backing is read-only, so sorting in place would
// edit the caller's array.
func ascending[T int64 | float64 | string](items []T) ([]T, error) {
	out := append([]T(nil), items...)
	sort.SliceStable(out, func(i, j int) bool { return out[i] < out[j] })
	return out, nil
}

func descending[T int64 | float64 | string](items []T) ([]T, error) {
	out := append([]T(nil), items...)
	sort.SliceStable(out, func(i, j int) bool { return out[j] < out[i] })
	return out, nil
}

func firstItem(_ context.Context, args []funroute.Value) (funroute.Value, error) {
	value, ok := args[0].At(0)
	if !ok {
		return funroute.Value{}, fmt.Errorf("first of an empty array")
	}
	return value, nil
}

func lastItem(_ context.Context, args []funroute.Value) (funroute.Value, error) {
	length, _ := args[0].Length()
	value, ok := args[0].At(length - 1)
	if !ok {
		return funroute.Value{}, fmt.Errorf("last of an empty array")
	}
	return value, nil
}

func sliceItems(_ context.Context, args []funroute.Value) (funroute.Value, error) {
	items := itemsOf(args[0])
	start, _ := args[1].Int()
	end, _ := args[2].Int()
	if start < 0 || end < start || end > int64(len(items)) {
		return funroute.Value{}, fmt.Errorf("slice [%d, %d) is outside an array of %d items", start, end, len(items))
	}
	return funroute.Array(elementType(args[0]), items[start:end])
}

func takeItems(_ context.Context, args []funroute.Value) (funroute.Value, error) {
	count, _ := args[1].Int()
	if count < 0 {
		return funroute.Value{}, fmt.Errorf("take needs a count of zero or more, got %d", count)
	}
	items := itemsOf(args[0])
	if count > int64(len(items)) {
		count = int64(len(items))
	}
	return funroute.Array(elementType(args[0]), items[:count])
}

func reverseItems(_ context.Context, args []funroute.Value) (funroute.Value, error) {
	items := itemsOf(args[0])
	out := make([]funroute.Value, len(items))
	for i, item := range items {
		out[len(items)-1-i] = item
	}
	return funroute.Array(elementType(args[0]), out)
}

func concatItems(_ context.Context, args []funroute.Value) (funroute.Value, error) {
	left, right := itemsOf(args[0]), itemsOf(args[1])
	out := make([]funroute.Value, 0, len(left)+len(right))
	out = append(out, left...)
	out = append(out, right...)
	return funroute.Array(elementType(args[0]), out)
}

func uniqueItems(_ context.Context, args []funroute.Value) (funroute.Value, error) {
	var out []funroute.Value
	for _, item := range itemsOf(args[0]) {
		if !containsValue(out, item) {
			out = append(out, item)
		}
	}
	return funroute.Array(elementType(args[0]), out)
}

func flattenItems(_ context.Context, args []funroute.Value) (funroute.Value, error) {
	var out []funroute.Value
	for _, inner := range itemsOf(args[0]) {
		out = append(out, itemsOf(inner)...)
	}
	inner, ok := elementType(args[0]).Elem()
	if !ok {
		return funroute.Value{}, fmt.Errorf("flatten needs an array of arrays")
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
// handed over read-only like everywhere else at this boundary.
func itemsOf(value funroute.Value) []funroute.Value {
	items, _ := value.Array()
	return items
}

func elementType(value funroute.Value) funroute.Type {
	elem, _ := value.Type().Elem()
	return elem
}
