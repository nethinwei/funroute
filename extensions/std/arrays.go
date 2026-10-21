package std

import (
	"context"
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
	count, _ := args[0].Length()
	// A size past the items is one past them, which an int holds wherever
	// int is 32 bits: no window fits, and one batch takes them all.
	width := int(min(size, int64(count)+1))
	stride, starts := width, count
	if windows {
		stride, starts = 1, count-width+1
	}
	groups := make([]funroute.Value, 0, max(0, (starts+stride-1)/stride))
	for start := 0; start < starts; start += stride {
		group, err := part(args[0], start, min(start+width, count))
		if err != nil {
			return funroute.Value{}, err
		}
		groups = append(groups, group)
	}
	return funroute.Array(args[0].Type(), groups)
}

// part is items [from, to) of array, over the array's own memory where it
// has a form to share — a window is then no copy — and copied where not.
func part(array funroute.Value, from, to int) (funroute.Value, error) {
	if shared, ok := array.Slice(from, to); ok {
		return shared, nil
	}
	return funroute.Array(elementType(array), itemsOf(array)[from:to])
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

// itemsOf reads a container's elements. The values are the container's own,
// handed over read-only like everywhere else at this boundary. A native
// array is built into values here, so the functions that run often work on
// its backing instead, and wrap theirs the same way.
func itemsOf(value funroute.Value) []funroute.Value {
	items, _ := value.Array()
	return items
}

func elementType(value funroute.Value) funroute.Type {
	elem, _ := value.Type().Elem()
	return elem
}
