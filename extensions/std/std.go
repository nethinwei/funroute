// Package std is the standard pack: the folds, string operations and array
// operations a payment rule actually writes, as ordinary functions.
//
// The language itself has no fold construct. A comprehension maps over a
// finite input; these functions collapse the result to one value. Counting is
// not here: that is len, which the kernel already has for every container. Which
// aggregations a console offers is therefore a registration decision, like
// every other capability — a registry without this pack can map and filter
// but cannot add anything up.
package std

import (
	"context"
	"fmt"
	"math"

	"github.com/nethinwei/funroute"
)

var (
	errNoAbsolute   = fmt.Errorf("the smallest int has no absolute value")
	errDivideByZero = fmt.Errorf("%w: division by zero", funroute.ErrArithmetic)
)

// maxRangeLength caps one range call. The compiler already requires constant
// arguments (FunctionSpec.ConstantArgs), so this only stops a rule from
// writing an absurd literal; it is not what bounds the language.
const maxRangeLength = 10_000

// Register adds the pack to a registry.
func Register(registry *funroute.Registry) error {
	if registry == nil {
		return fmt.Errorf("registry is required")
	}
	for _, register := range []func(*funroute.Registry) error{
		registerSum, registerExtremes, registerQuantifiers, registerRange,
		registerStrings, registerArrays, registerNumbers, registerStatistics, registerSelect, registerGroups, registerDicts,
		registerMoney,
	} {
		if err := register(registry); err != nil {
			return err
		}
	}
	return nil
}

func registerSum(registry *funroute.Registry) error {
	doc := funroute.Doc{Constexpr: true,
		Label:       "求和",
		Description: "把数组里的元素依次加起来；空数组是 0。要加的东西先用推导式算出来，再交给它。",
		Category:    "聚合",
		Cost:        4,
		Params:      []string{"数组"},
		Result:      "总和",
	}
	return eachType(registry, "sum", doc, sumInts, sumFloats)
}

func registerExtremes(registry *funroute.Registry) error {
	for _, extreme := range []struct {
		name, label, result string
		ints                func([]int64) (int64, error)
		floats              func([]float64) (float64, error)
		texts               func([]string) (string, error)
	}{
		{"min", "最小值", "最小的元素", minOf[int64], minOf[float64], minOf[string]},
		{"max", "最大值", "最大的元素", maxOf[int64], maxOf[float64], maxOf[string]},
	} {
		doc := funroute.Doc{Constexpr: true,
			Label:       extreme.label,
			Description: "取数组里" + extreme.label + "；数值按大小、字符串按 UTF-8 字节序；空数组报错，因为没有可取的元素。",
			Category:    "聚合",
			Cost:        4,
			Params:      []string{"数组"},
			Result:      extreme.result,
		}
		// Strings order the same way the comparison operators order them, so
		// the extremes work on them too.
		if err := eachType(registry, extreme.name, doc, extreme.ints, extreme.floats, extreme.texts); err != nil {
			return err
		}
	}
	return nil
}

func registerQuantifiers(registry *funroute.Registry) error {
	any := funroute.Doc{Constexpr: true,
		Label:       "任一为真",
		Description: "数组里只要有一个 true 就是 true；空数组是 false。",
		Category:    "聚合",
		Cost:        3,
		Params:      []string{"布尔数组"},
		Result:      "是否存在",
	}
	all := funroute.Doc{Constexpr: true,
		Label:       "全部为真",
		Description: "数组里每一个都是 true 才是 true；空数组是 true。",
		Category:    "聚合",
		Cost:        3,
		Params:      []string{"布尔数组"},
		Result:      "是否全部满足",
	}
	if err := logic(registry, "any", any, anyTrue); err != nil {
		return err
	}
	return logic(registry, "all", all, allTrue)
}

// registerRange is the only source of a sequence that does not come from the
// host. Its arguments must be constant, so the length of what it produces is
// known when the rule is compiled and the bounds in docs/termination.md hold
// unchanged.
func registerRange(registry *funroute.Registry) error {
	for _, form := range []struct {
		labels []string
		bounds func([]funroute.Value) (int64, int64, int64)
	}{
		{[]string{"个数"}, func(args []funroute.Value) (int64, int64, int64) { return 0, argInt(args, 0), 1 }},
		{[]string{"起点", "终点"}, func(args []funroute.Value) (int64, int64, int64) {
			return argInt(args, 0), argInt(args, 1), 1
		}},
		{[]string{"起点", "终点", "步长"}, func(args []funroute.Value) (int64, int64, int64) {
			return argInt(args, 0), argInt(args, 1), argInt(args, 2)
		}},
	} {
		params := make([]funroute.Type, len(form.labels))
		for i := range params {
			params[i] = funroute.IntType
		}
		if err := register(registry, rangeSpec(params, form.labels, form.bounds)); err != nil {
			return err
		}
	}
	return nil
}

func rangeSpec(params []funroute.Type, labels []string, bounds func([]funroute.Value) (int64, int64, int64)) funroute.FunctionSpec {
	return funroute.FunctionSpec{
		Name:   "range",
		Params: params,
		Result: funroute.ArrayOf(funroute.IntType),
		Eval: func(_ context.Context, args []funroute.Value) (funroute.Value, error) {
			start, stop, step := bounds(args)
			return sequence(start, stop, step)
		},
		Doc: funroute.Doc{Constexpr: true,
			Label:       "整数序列",
			Description: "生成一段整数：range(3) 是 [0,1,2]，range(1,4) 是 [1,2,3]，第三个参数是步长。参数的规模必须由输入界定 —— 字面量、len(容器) 或两者的算术组合，所以 range(len(fees)) 可以，range(某个入参) 不行。",
			BoundedArgs: true,
			Category:    "聚合",
			Cost:        8,
			Params:      labels,
			Result:      "整数数组",
		},
	}
}

func argInt(args []funroute.Value, index int) int64 {
	value, _ := args[index].Int()
	return value
}

func sequence(start, stop, step int64) (funroute.Value, error) {
	if step == 0 {
		return funroute.Value{}, fmt.Errorf("range step must not be zero")
	}
	items := []int64{}
	for value := start; step > 0 && value < stop || step < 0 && value > stop; value += step {
		if len(items) == maxRangeLength {
			return funroute.Value{}, fmt.Errorf("range is longer than %d items", maxRangeLength)
		}
		items = append(items, value)
	}
	return funroute.ToValue(items)
}

func sumInts(items []int64) (int64, error) {
	total := int64(0)
	for _, item := range items {
		if item > 0 && total > math.MaxInt64-item || item < 0 && total < math.MinInt64-item {
			return 0, fmt.Errorf("%w: integer overflow in sum", funroute.ErrArithmetic)
		}
		total += item
	}
	return total, nil
}

func sumFloats(items []float64) (float64, error) {
	total := 0.0
	for _, item := range items {
		total += item
	}
	if math.IsNaN(total) || math.IsInf(total, 0) {
		return 0, fmt.Errorf("%w: non-finite float result in sum", funroute.ErrArithmetic)
	}
	return total, nil
}

func minOf[T int64 | float64 | string](items []T) (T, error) {
	return extremeOf(items, "min", func(candidate, best T) bool { return candidate < best })
}

func maxOf[T int64 | float64 | string](items []T) (T, error) {
	return extremeOf(items, "max", func(candidate, best T) bool { return candidate > best })
}

func extremeOf[T int64 | float64 | string](items []T, name string, better func(T, T) bool) (T, error) {
	var best T
	if len(items) == 0 {
		return best, fmt.Errorf("%s of an empty array", name)
	}
	best = items[0]
	for _, item := range items[1:] {
		if better(item, best) {
			best = item
		}
	}
	return best, nil
}

func anyTrue(items []bool) (bool, error) {
	for _, item := range items {
		if item {
			return true, nil
		}
	}
	return false, nil
}

func allTrue(items []bool) (bool, error) {
	for _, item := range items {
		if !item {
			return false, nil
		}
	}
	return true, nil
}

// logic and register are how the pack registers: funroute.Logic and
// Registry.Register with the name's examples (examples.go) in the doc, so
// every overload of a name shows the same uses.
func logic(registry *funroute.Registry, name string, doc funroute.Doc, fn any) error {
	doc.Examples = examples[name]
	return funroute.Logic(registry, name, doc, fn)
}

func register(registry *funroute.Registry, spec funroute.FunctionSpec) error {
	spec.Doc.Examples = examples[spec.Name]
	return registry.Register(spec)
}

// eachType registers one name for every element type it serves. The language
// has no type classes, so a function that works on int, float and string is
// three registrations — that is the signature, not repetition. What this takes
// out is the error check that used to be written once per type, and what it
// buys back is a single place to read how many types a name covers.
func eachType(registry *funroute.Registry, name string, doc funroute.Doc, implementations ...any) error {
	for _, implementation := range implementations {
		if err := logic(registry, name, doc, implementation); err != nil {
			return err
		}
	}
	return nil
}
