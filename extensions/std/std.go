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
	"cmp"
	"context"
	"errors"
	"fmt"
	"math"
	"slices"

	"github.com/nethinwei/funroute"
)

var (
	errNoAbsolute      = fmt.Errorf("%w: the smallest int has no absolute value", funroute.ErrArithmetic)
	errDivideByZero    = fmt.Errorf("%w: division by zero", funroute.ErrArithmetic)
	errIntegerOverflow = fmt.Errorf("%w: integer overflow in sum", funroute.ErrArithmetic)
	errNotFinite       = fmt.Errorf("%w: non-finite float result in sum", funroute.ErrArithmetic)
)

// maxRangeLength caps one range call. The compiler already requires constant
// arguments (FunctionSpec.ConstantArgs), so this only stops a rule from
// writing an absurd literal; it is not what bounds the language.
const maxRangeLength = 10_000

// Register adds the pack to a registry. The order is the pack's: the order a
// name's overloads are registered in is the order the compiler tries them,
// and the money overloads, registered only when the registry declares money,
// come after all the others.
func Register(registry *funroute.Registry) error {
	if registry == nil {
		return errors.New("registry is required")
	}
	specs := slices.Concat(
		sumSpecs(), extremeSpecs(), quantifierSpecs(), rangeSpecs(),
		caseSpecs(), testSpecs(), partSpecs(), paddingSpecs(),
		shapeSpecs(), sortSpecs(), sequenceSpecs(),
		numberSpecs(), statisticSpecs(),
		selectSpecs(), keyedSpecs(false), whileSpecs(), positionSpecs(),
		groupSpecs(), dictSpecs(),
	)
	if _, declared := registry.Money(); declared {
		specs = append(specs, moneySpecs()...)
	}
	for _, spec := range specs {
		// Everything in the pack depends only on its arguments, so the
		// compiler may fold any call to it; a name's examples go with every
		// overload of it.
		spec.Doc.Constexpr = true
		spec.Doc.Examples = examples[spec.Name]
		if err := registry.Register(spec); err != nil {
			return err
		}
	}
	return nil
}

// sumSpecs fold as add does, item by item from 0: a sum of a comprehension
// adds as it goes, and an overflow is add's.
func sumSpecs() []funroute.FunctionSpec {
	specs := eachType("sum", funroute.Doc{
		Label:       "求和",
		Description: "把数组里的元素依次加起来；空数组是 0。要加的东西先用推导式算出来，再交给它。",
		Category:    "聚合",
		Cost:        4,
		Params:      []string{"数组"},
		Result:      "总和",
	}, sumInts, sumFloats)
	zero, _ := funroute.Float(0) // 0 is finite
	specs[0].Fold = &funroute.Fold{Step: "add", Init: funroute.Int(0)}
	specs[1].Fold = &funroute.Fold{Step: "add", Init: zero}
	return specs
}

func extremeSpecs() []funroute.FunctionSpec {
	specs := make([]funroute.FunctionSpec, 0, 6) // two names, three types each
	for _, extreme := range []struct {
		name, label, result string
		smallest            bool
	}{
		{"min", "最小值", "最小的元素", true},
		{"max", "最大值", "最大的元素", false},
	} {
		doc := funroute.Doc{
			Label:       extreme.label,
			Description: "取数组里" + extreme.label + "；数值按大小、字符串按 UTF-8 字节序；空数组报错，因为没有可取的元素。",
			Category:    "聚合",
			Cost:        4,
			Params:      []string{"数组"},
			Result:      extreme.result,
		}
		// Strings order the same way the comparison operators order them, so
		// the extremes work on them too.
		name, smallest := extreme.name, extreme.smallest
		specs = append(specs, eachType(name, doc,
			extremeOf[int64](name, smallest), extremeOf[float64](name, smallest), extremeOf[string](name, smallest))...)
	}
	return specs
}

// quantifierSpecs stop at the item that decides them: any([p(x) for x in
// xs]) computes no p past the first true, as || computes nothing past it.
func quantifierSpecs() []funroute.FunctionSpec {
	specs := []funroute.FunctionSpec{
		logic("any", funroute.Doc{
			Label:       "任一为真",
			Description: "数组里只要有一个 true 就是 true；空数组是 false。对推导式求值时遇到第一个 true 就停，后面的元素不再计算，和 || 一样。",
			Category:    "聚合",
			Cost:        3,
			Params:      []string{"布尔数组"},
			Result:      "是否存在",
		}, anyTrue),
		logic("all", funroute.Doc{
			Label:       "全部为真",
			Description: "数组里每一个都是 true 才是 true；空数组是 true。对推导式求值时遇到第一个 false 就停，后面的元素不再计算，和 && 一样。",
			Category:    "聚合",
			Cost:        3,
			Params:      []string{"布尔数组"},
			Result:      "是否全部满足",
		}, allTrue),
	}
	specs[0].Fold = &funroute.Fold{Init: funroute.Bool(false), Stops: true, Stop: true}
	specs[1].Fold = &funroute.Fold{Init: funroute.Bool(true), Stops: true, Stop: false}
	return specs
}

// rangeSpecs are the only source of a sequence that does not come from the
// host. Their arguments must be constant, so the length of what they produce
// is known when the rule is compiled and the bounds in docs/termination.md
// hold unchanged.
func rangeSpecs() []funroute.FunctionSpec {
	specs := make([]funroute.FunctionSpec, 0, 3)
	for _, labels := range [][]string{{"个数"}, {"起点", "终点"}, {"起点", "终点", "步长"}} {
		specs = append(specs, funroute.FunctionSpec{
			Name:   "range",
			Params: slices.Repeat([]funroute.Type{funroute.IntType}, len(labels)),
			Result: funroute.ArrayOf(funroute.IntType),
			Eval:   evalRange,
			Doc: funroute.Doc{
				Label:       "整数序列",
				Description: "生成一段整数：range(3) 是 [0,1,2]，range(1,4) 是 [1,2,3]，第三个参数是步长。参数的规模必须由输入界定 —— 字面量、len(容器) 或两者的算术组合，所以 range(len(fees)) 可以，range(某个入参) 不行。",
				BoundedArgs: true,
				Category:    "聚合",
				Cost:        8,
				Params:      labels,
				Result:      "整数数组",
			},
		})
	}
	return specs
}

// evalRange reads its bounds from how many arguments it has: range(stop),
// range(start, stop) or range(start, stop, step).
func evalRange(_ context.Context, args []funroute.Value) (funroute.Value, error) {
	start, step := int64(0), int64(1)
	if len(args) > 1 {
		start = argInt(args, 0)
	}
	if len(args) > 2 {
		step = argInt(args, 2)
	}
	return sequence(start, argInt(args, min(len(args)-1, 1)), step)
}

func argInt(args []funroute.Value, index int) int64 {
	value, _ := args[index].Int()
	return value
}

func sequence(start, stop, step int64) (funroute.Value, error) {
	if step == 0 {
		return funroute.Value{}, fmt.Errorf("%w: range step must not be zero", funroute.ErrArithmetic)
	}
	items := []int64{}
	for value := start; step > 0 && value < stop || step < 0 && value > stop; value += step {
		if len(items) == maxRangeLength {
			return funroute.Value{}, fmt.Errorf("%w: range is longer than %d items", funroute.ErrArithmetic, maxRangeLength)
		}
		items = append(items, value)
		// The next value would be past int64, so past stop as well.
		if step > 0 && value > math.MaxInt64-step || step < 0 && value < math.MinInt64-step {
			break
		}
	}
	return funroute.ToValue(items)
}

func sumInts(items []int64) (int64, error) {
	total := int64(0)
	for _, item := range items {
		if item > 0 && total > math.MaxInt64-item || item < 0 && total < math.MinInt64-item {
			return 0, errIntegerOverflow
		}
		total += item
	}
	return total, nil
}

// finiteIn is a float result of the named function, or ErrArithmetic when
// it is not a finite number: an overflow the float could not hold.
func finiteIn(name string, value float64) (float64, error) {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, fmt.Errorf("%w: non-finite float result in %s", funroute.ErrArithmetic, name)
	}
	return value, nil
}

func sumFloats(items []float64) (float64, error) {
	total := 0.0
	for _, item := range items {
		total += item
	}
	if math.IsNaN(total) || math.IsInf(total, 0) {
		return 0, errNotFinite
	}
	return total, nil
}

// extremeOf builds the min or max body for one element type.
func extremeOf[T cmp.Ordered](name string, smallest bool) func([]T) (T, error) {
	return func(items []T) (T, error) {
		at, ok := best(items, smallest)
		if !ok {
			var zero T
			return zero, fmt.Errorf("%w: %s of an empty array", funroute.ErrDomain, name)
		}
		return items[at], nil
	}
}

// best is the position of the smallest item, or the largest, the first of
// equal ones; false when there are none. min, max, arg_min, arg_max and the
// pairs are all this one comparison.
func best[T cmp.Ordered](items []T, smallest bool) (int, bool) {
	at := 0
	for i, item := range items {
		if smallest && item < items[at] || !smallest && item > items[at] {
			at = i
		}
	}
	return at, len(items) > 0
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

// logic is a Go function as a spec.
func logic(name string, doc funroute.Doc, fn any) funroute.FunctionSpec {
	return funroute.FunctionSpec{Name: name, Doc: doc, Go: fn}
}

// eachType registers one name for every element type it serves. The language
// has no type classes, so a function that works on int, float and string is
// three registrations — that is the signature, not repetition. What it buys
// is a single place to read how many types a name covers.
func eachType(name string, doc funroute.Doc, implementations ...any) []funroute.FunctionSpec {
	specs := make([]funroute.FunctionSpec, len(implementations))
	for i, implementation := range implementations {
		specs[i] = logic(name, doc, implementation)
	}
	return specs
}
