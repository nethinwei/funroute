package machine

import (
	"fmt"
	"math"
	"slices"
)

// The numeric helpers a fee calculation reaches for. round rounds half away
// from zero, the way math.Round does and the way most people read "四舍五入";
// it takes a float to an int. Money's rounding is another question — how to
// split an amount across the smallest unit without losing a cent — which
// round(amount, @mode) answers on money's own terms.
func numberSpecs() []FunctionSpec {
	specs := libEach("abs", Doc{
		Label: "绝对值", Category: "数值",
		Description: "取绝对值；整数的最小值没有相反数，所以那一个报错而不是绕回去。",
		Params:      []string{"数值"}, Result: "绝对值",
	}, absInt, func(value float64) (float64, error) {
		return math.Abs(value), nil
	})
	for _, fn := range []struct {
		name, label, description string
		apply                    func(float64) float64
	}{
		{"ceil", "向上取整", "取不小于它的最小整数。结果是 int —— 取整就是为了得到整数，要当 float 用写 float(ceil(x))。", math.Ceil},
		{"floor", "向下取整", "取不大于它的最大整数。结果是 int。", math.Floor},
		{"round", "四舍五入", "四舍五入到整数，半数远离零（0.5 进 1，-0.5 进 -1）。结果是 int。要按小数位舍入金额，那是 money 的事。", math.Round},
	} {
		doc := Doc{
			Label: fn.label, Category: "数值",
			Description: fn.description, Params: []string{"数值"}, Result: "整数",
		}
		specs = append(specs, libGo(fn.name, doc, roundingTo(fn.name, fn.apply)))
	}
	return append(append(specs, powerSpecs()...), libGo("mod", Doc{
		Label: "取余", Category: "数值",
		Description: "浮点取余，符号跟随被除数；按 IEEE 754，除数为零得 NaN。写作 a % b。",
		Params:      []string{"被除数", "除数"}, Result: "余数",
	}, modFloat))
}

// roundingTo makes a rounding function return an int. A float64 reaches
// beyond int64 long before it runs out of exponent, so the conversion is
// checked: an amount that cannot be an integer is an error, not a
// wrapped-around one.
//
// The comparison is written against the exact powers of two that bound int64,
// because float64(math.MaxInt64) rounds up to 2^63 and would let that value
// through; a NaN fails both comparisons, so it is written to fail them.
func roundingTo(name string, apply func(float64) float64) func(float64) (int64, error) {
	return func(value float64) (int64, error) {
		rounded := apply(value)
		if !(rounded >= -9223372036854775808.0 && rounded < 9223372036854775808.0) {
			return 0, fmt.Errorf("%w: %s(%g) is outside the range of an int", ErrArithmetic, name, value)
		}
		return int64(rounded), nil
	}
}

func absInt(value int64) (int64, error) {
	if value == math.MinInt64 {
		return 0, errNoAbsolute
	}
	if value < 0 {
		return -value, nil
	}
	return value, nil
}

// modFloat is math.Mod: the sign is the dividend's, and a zero divisor is a
// NaN, as IEEE 754 has it.
func modFloat(dividend, divisor float64) (float64, error) {
	return math.Mod(dividend, divisor), nil
}

// powerSpecs are what an exponential backoff is written with:
// base * pow(2, attempt). There is no ** operator — one spelling is enough,
// and the operator table stays the size it is.
func powerSpecs() []FunctionSpec {
	doc := Doc{
		Label: "幂", Category: "数值",
		Description: "底数的指数次方。整数版的指数不能为负（那不是整数），结果溢出会报错，按平方求幂计算，指数再大也只算几十步；浮点版按 IEEE 754 计算。退避间隔写 base * pow(2, attempt)。",
		Params:      []string{"底数", "指数"}, Result: "幂",
	}
	return libEach("pow", doc, powInt, func(base, exponent float64) (float64, error) {
		return math.Pow(base, exponent), nil
	})
}

// powInt squares its way up, so its work is the exponent's bit length — at
// most 63 steps — never the exponent itself: pow(1, 9223372036854775807) is
// as quick as pow(2, 10), at run time and when the compiler folds it. A base
// whose square is needed and overflows would overflow the result as well.
func powInt(base, exponent int64) (int64, error) {
	if exponent < 0 {
		return 0, fmt.Errorf("%w: a negative exponent has no integer result; use floats for that", ErrArithmetic)
	}
	result := int64(1)
	for exponent > 0 {
		var err error
		if exponent&1 == 1 {
			if result, err = multiplyInts(result, base); err != nil {
				return 0, err
			}
		}
		exponent >>= 1
		if exponent > 0 {
			if base, err = multiplyInts(base, base); err != nil {
				return 0, err
			}
		}
	}
	return result, nil
}

func multiplyInts(left, right int64) (int64, error) {
	product, ok := mulInt(left, right)
	if !ok {
		return 0, fmt.Errorf("%w: integer overflow in pow", ErrArithmetic)
	}
	return product, nil
}

// statisticSpecs are the "what does this list look like" family. The
// answers are floats: the average of whole numbers rarely is one, and the
// median of an even count is the midpoint of the middle two.
func statisticSpecs() []FunctionSpec {
	return slices.Concat(
		libEach("avg", Doc{
			Label: "平均值", Category: "聚合",
			Description: "算术平均；空数组报错，因为没有可平均的东西。",
			Params:      []string{"数组"}, Result: "平均值",
		}, averageOf[int64], averageOf[float64]),
		libEach("median", Doc{
			Label: "中位数", Category: "聚合",
			Description: "排序后的中间值；个数为偶时取中间两个的平均。空数组报错。",
			Params:      []string{"数组"}, Result: "中位数",
		}, medianOf[int64], medianOf[float64]),
		libEach("stddev", Doc{
			Label: "标准差", Category: "聚合",
			Description: "总体标准差（除以个数，不是个数减一）：数据就是全部时用它，衡量成功率或费率的抖动。空数组报错。",
			Params:      []string{"数组"}, Result: "标准差",
		}, deviationOf[int64], deviationOf[float64]),
		libEach("percentile", Doc{
			Label: "分位数", Category: "聚合",
			Description: "升序排列后的分位值，比例写 0 到 1（p95 就是 0.95），落在两个样本之间时线性插值。空数组报错。",
			Params:      []string{"数组", "比例"}, Result: "分位值",
		}, percentileOf[int64], percentileOf[float64]),
		pairwiseSpecs(),
	)
}

func averageOf[T int64 | float64](items []T) (float64, error) {
	if len(items) == 0 {
		return 0, fmt.Errorf("%w: avg of an empty array", ErrDomain)
	}
	total := 0.0
	for _, item := range items {
		total += float64(item)
	}
	return total / float64(len(items)), nil
}

func medianOf[T int64 | float64](items []T) (float64, error) {
	if len(items) == 0 {
		return 0, fmt.Errorf("%w: median of an empty array", ErrDomain)
	}
	sorted := append([]T(nil), items...)
	slices.Sort(sorted)
	middle := len(sorted) / 2
	if len(sorted)%2 == 1 {
		return float64(sorted[middle]), nil
	}
	return (float64(sorted[middle-1]) + float64(sorted[middle])) / 2, nil
}

func deviationOf[T int64 | float64](items []T) (float64, error) {
	mean, err := averageOf(items)
	if err != nil {
		return 0, fmt.Errorf("%w: stddev of an empty array", ErrDomain)
	}
	total := 0.0
	for _, item := range items {
		diff := float64(item) - mean
		total += diff * diff
	}
	return math.Sqrt(total / float64(len(items))), nil
}

func percentileOf[T int64 | float64](items []T, ratio float64) (float64, error) {
	if len(items) == 0 {
		return 0, fmt.Errorf("%w: percentile of an empty array", ErrDomain)
	}
	// Written so that a NaN ratio is refused too.
	if !(0 <= ratio && ratio <= 1) {
		return 0, fmt.Errorf("%w: a percentile is a ratio between 0 and 1, got %v", ErrArithmetic, ratio)
	}
	sorted := append([]T(nil), items...)
	slices.Sort(sorted)
	position := ratio * float64(len(sorted)-1)
	lower := int(math.Floor(position))
	upper := int(math.Ceil(position))
	if lower == upper {
		return float64(sorted[lower]), nil
	}
	weight := position - float64(lower)
	return float64(sorted[lower])*(1-weight) + float64(sorted[upper])*weight, nil
}

// pairwiseSpecs are min and max on two values rather than on a list: a fee
// cap reads min(fee, cap), and making someone build an array for that is the
// kind of friction a rule writer notices every day.
func pairwiseSpecs() []FunctionSpec {
	specs := make([]FunctionSpec, 0, 6) // two names, three types each
	for _, extreme := range []struct {
		name, label, which string
		smallest           bool
	}{
		{"min", "两者取小", "较小", true},
		{"max", "两者取大", "较大", false},
	} {
		doc := Doc{
			Label: extreme.label, Category: "数值",
			Description: "两个同型数值或字符串里" + extreme.which + "的那个。封顶写 min(fee, cap)。",
			Params:      []string{"左值", "右值"}, Result: "结果",
		}
		smallest := extreme.smallest
		specs = append(specs, libEach(extreme.name, doc, pairwise[int64](smallest), pairwise[float64](smallest), pairwise[string](smallest))...)
	}
	return specs
}

// pairwise is Go's min or max: a NaN of either is the answer.
func pairwise[T int64 | float64 | string](smallest bool) func(T, T) (T, error) {
	return func(left, right T) (T, error) {
		if smallest {
			return min(left, right), nil
		}
		return max(left, right), nil
	}
}

// numericSpecs are the numeric helpers in the order the pack registered
// them: the ones on a number, then the statistics and min and max on two
// values.
func numericSpecs() []FunctionSpec {
	return slices.Concat(numberSpecs(), statisticSpecs())
}
