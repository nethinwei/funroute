package machine

import (
	"cmp"
	"context"
	"fmt"
	"slices"
)

// The aggregates and the numeric helpers a rule writes most: sum, min, max,
// any, all, avg, range, and abs, ceil, floor, round, pow and a float's mod.

var (
	errNoAbsolute      = fmt.Errorf("%w: the smallest int has no absolute value", ErrArithmetic)
	errIntegerOverflow = fmt.Errorf("%w: integer overflow in sum", ErrArithmetic)
	errNoExtreme       = fmt.Errorf("%w: an empty array has no extreme", ErrDomain)
)

// maxRangeLength caps one range call. The compiler already requires that its
// arguments be bounded by the input (Doc.BoundedArgs), so this only stops a
// rule from writing an absurd literal; it is not what bounds the language.
const maxRangeLength = 10_000

// sumSpecs fold as add does, item by item from 0: a sum of a comprehension
// adds as it goes, and an overflow is add's.
func sumSpecs() []FunctionSpec {
	specs := libEach("sum", Doc{
		Label:       "求和",
		Description: "把数组里的元素依次加起来；空数组是 0。要加的东西先用推导式算出来，再交给它。",
		Category:    "聚合",
		Params:      []string{"数组"},
		Result:      "总和",
	}, sumInts, sumFloats)
	specs[0].Fold = &Fold{Step: "add", Init: Int(0)}
	specs[1].Fold = &Fold{Step: "add", Init: Float(0)}
	return specs
}

func sumInts(items []int64) (int64, error) {
	total := int64(0)
	for _, item := range items {
		var ok bool
		if total, ok = addInt(total, item); !ok {
			return 0, errIntegerOverflow
		}
	}
	return total, nil
}

// sumFloats adds as IEEE 754 does: past the range of a float is an
// infinity, and a NaN makes the sum NaN.
func sumFloats(items []float64) (float64, error) {
	total := 0.0
	for _, item := range items {
		total += item
	}
	return total, nil
}

func extremeSpecs() []FunctionSpec {
	specs := make([]FunctionSpec, 0, 6) // two names, three types each
	for _, extreme := range []struct {
		name, label, result string
		smallest            bool
	}{
		{"min", "最小值", "最小的元素", true},
		{"max", "最大值", "最大的元素", false},
	} {
		doc := Doc{
			Label:       extreme.label,
			Description: "取数组里" + extreme.label + "；数值按大小、字符串按 UTF-8 字节序；空数组报错，因为没有可取的元素。",
			Category:    "聚合",
			Params:      []string{"数组"},
			Result:      extreme.result,
		}
		// Strings order the same way the comparison operators order them, so
		// the extremes work on them too.
		name, smallest := extreme.name, extreme.smallest
		specs = append(specs, libEach(name, doc,
			extremeOf[int64](name, smallest), extremeOf[float64](name, smallest), extremeOf[string](name, smallest))...)
	}
	return specs
}

// extremeOf builds the min or max body for one element type.
func extremeOf[T cmp.Ordered](name string, smallest bool) func([]T) (T, error) {
	return func(items []T) (T, error) {
		at, ok := best(items, smallest)
		if !ok {
			var zero T
			return zero, fmt.Errorf("%w: %s of an empty array", ErrDomain, name)
		}
		return items[at], nil
	}
}

// extremeIndex builds the arg_min / arg_max body for one key type.
func extremeIndex[T cmp.Ordered](smallest bool) func([]T) (int64, error) {
	return func(keys []T) (int64, error) {
		at, ok := best(keys, smallest)
		if !ok {
			return 0, errNoExtreme
		}
		return int64(at), nil
	}
}

// best is the position of the smallest item, or the largest, the first of
// equal ones; false when there are none. min, max, arg_min and arg_max are
// all this one comparison.
func best[T cmp.Ordered](items []T, smallest bool) (int, bool) {
	if len(items) == 0 {
		return 0, false
	}
	// One pass: a NaN is the answer when there is one, as Go's min and max
	// have it, and the first NaN is where it is; otherwise only an item
	// strictly past the best so far moves it, which keeps the first of equal
	// ones — of 0 and -0 as well, which compare equal.
	at := 0
	for i, item := range items {
		switch {
		case isNaN(item):
			return i, true
		case smallest && item < items[at], !smallest && item > items[at]:
			at = i
		}
	}
	return at, true
}

// isNaN reports a NaN, the one value unequal to itself; of an int or a
// string, never.
func isNaN[T cmp.Ordered](x T) bool {
	same := x
	return x != same
}

// quantifierSpecs stop at the item that decides them: any([p(x) for x in
// xs]) computes no p past the first true, as || computes nothing past it.
func quantifierSpecs() []FunctionSpec {
	specs := []FunctionSpec{
		libGo("any", Doc{
			Label:       "任一为真",
			Description: "数组里只要有一个 true 就是 true；空数组是 false。对推导式求值时遇到第一个 true 就停，后面的元素不再计算，和 || 一样。",
			Category:    "聚合",
			Params:      []string{"布尔数组"},
			Result:      "是否存在",
		}, anyTrue),
		libGo("all", Doc{
			Label:       "全部为真",
			Description: "数组里每一个都是 true 才是 true；空数组是 true。对推导式求值时遇到第一个 false 就停，后面的元素不再计算，和 && 一样。",
			Category:    "聚合",
			Params:      []string{"布尔数组"},
			Result:      "是否全部满足",
		}, allTrue),
	}
	specs[0].Fold = &Fold{Init: Bool(false), Stops: true, Stop: true}
	specs[1].Fold = &Fold{Init: Bool(true), Stops: true, Stop: false}
	return specs
}

func anyTrue(items []bool) (bool, error) { return slices.Contains(items, true), nil }

func allTrue(items []bool) (bool, error) { return !slices.Contains(items, false), nil }

// rangeSpecs are the only source of a sequence that does not come from the
// host. Their arguments must be bounded by the input, so the length of what
// they produce is too, and the bounds in docs/termination.md hold unchanged.
func rangeSpecs() []FunctionSpec {
	specs := make([]FunctionSpec, 0, 3)
	for _, labels := range [][]string{{"个数"}, {"起点", "终点"}, {"起点", "终点", "步长"}} {
		specs = append(specs, FunctionSpec{
			Name:   "range",
			Params: slices.Repeat([]Type{IntType}, len(labels)),
			Result: ArrayOf(IntType),
			Eval:   evalRange,
			Doc: Doc{
				Label:       "整数序列",
				Description: "生成一段整数：range(3) 是 [0,1,2]，range(1,4) 是 [1,2,3]，第三个参数是步长。参数的规模必须由输入界定 —— 字面量、len(容器) 或两者的算术组合，所以 range(len(fees)) 可以，range(某个入参) 不行。",
				BoundedArgs: true,
				Category:    "聚合",
				Params:      labels,
				Result:      "整数数组",
			},
		})
	}
	return specs
}

// evalRange reads its bounds from how many arguments it has: range(stop),
// range(start, stop) or range(start, stop, step).
func evalRange(_ context.Context, args []Value) (Value, error) {
	start, step := int64(0), int64(1)
	if len(args) > 1 {
		start = args[0].i
	}
	if len(args) > 2 {
		step = args[2].i
	}
	return sequence(start, args[min(len(args)-1, 1)].i, step)
}

func sequence(start, stop, step int64) (Value, error) {
	if step == 0 {
		return Value{}, fmt.Errorf("%w: range step must not be zero", ErrArithmetic)
	}
	n := rangeLength(start, stop, step)
	if n > maxRangeLength {
		return Value{}, fmt.Errorf("%w: range is longer than %d items", ErrArithmetic, maxRangeLength)
	}
	items := make([]int64, n)
	// Every item is between start and stop, so an int64; the product may
	// wrap on the way, and the sum wraps back.
	for i := range items {
		items[i] = start + int64(i)*step
	}
	return arrayOf(items), nil
}

// rangeLength is how many items there are from start to stop by step, or
// one past sequence's limit when there are more.
func rangeLength(start, stop, step int64) int {
	var span, stride uint64
	switch {
	case step > 0 && start < stop:
		span, stride = uint64(stop)-uint64(start), uint64(step)
	case step < 0 && start > stop:
		span, stride = uint64(start)-uint64(stop), -uint64(step)
	default:
		return 0
	}
	return int(min((span-1)/stride+1, maxRangeLength+1))
}
