package std

import (
	"context"
	"errors"
	"fmt"
	"math"

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
