package std

import (
	"context"
	"fmt"
	"slices"

	"funroute/lang"
)

// The pack's aggregates over money, registered only when the registry
// declares money — so a host declares its currencies before it registers
// the pack. They are FunctionSpecs rather than Logic because a signature has
// to say money<c> to keep the currency through: reflection sees only money.
// Every one checks that the amounts it is handed share a currency; the
// currency-less zero goes with any.

func registerMoney(registry *lang.Registry) error {
	spec, declared := registry.Money()
	if !declared {
		return nil
	}
	c := lang.MoneyOf("c")
	amounts := lang.ArrayOf(c)
	specs := []lang.FunctionSpec{
		moneySpec("sum", []lang.Type{amounts}, c, "求和", "同币种金额相加；空数组是不带币种的 0。", sumMoney),
		moneySpec("min", []lang.Type{amounts}, c, "最小值", "最小的金额；空数组报错。", extremeMoney(true)),
		moneySpec("max", []lang.Type{amounts}, c, "最大值", "最大的金额；空数组报错。", extremeMoney(false)),
		moneySpec("min", []lang.Type{c, c}, c, "较小者", "两笔同币种金额里较小的。", pairMoney(true)),
		moneySpec("max", []lang.Type{c, c}, c, "较大者", "两笔同币种金额里较大的。", pairMoney(false)),
		moneySpec("abs", []lang.Type{c}, c, "绝对值", "金额的绝对值。", absMoney),
		moneySpec("sort", []lang.Type{amounts}, amounts, "排序", "金额从小到大。", sortMoney(true)),
		moneySpec("sort_desc", []lang.Type{amounts}, amounts, "降序", "金额从大到小。", sortMoney(false)),
		moneySpec("cumsum", []lang.Type{amounts}, amounts, "累加", "逐项累加出的金额序列。", cumulativeMoney),
		moneySpec("deltas", []lang.Type{amounts}, amounts, "差分", "相邻两笔金额之差。", deltasMoney),
		moneySpec("arg_min", []lang.Type{amounts}, lang.IntType, "最小值下标", "最小金额的下标；空数组报错。", argExtremeMoney(true)),
		moneySpec("arg_max", []lang.Type{amounts}, lang.IntType, "最大值下标", "最大金额的下标；空数组报错。", argExtremeMoney(false)),
	}
	specs = append(specs, roundedMoneySpecs(spec.Rounding, amounts, c)...)
	specs = append(specs, keyedByMoney(amounts)...)
	for _, spec := range specs {
		if err := register(registry, spec); err != nil {
			return err
		}
	}
	return nil
}

func moneySpec(name string, params []lang.Type, result lang.Type, label, description string, eval lang.EvalFunc) lang.FunctionSpec {
	return lang.FunctionSpec{
		Name: name, Params: params, Result: result, Eval: eval,
		Doc: lang.Doc{Constexpr: true, Label: label, Category: "金额", Cost: 4, Description: description},
	}
}

// roundedMoneySpecs are avg and median, whose results fall between minor
// units: once with the registry's rounding, once with the mode as a last
// argument for round(…).
func roundedMoneySpecs(rounding lang.Rounding, amounts, c lang.Type) []lang.FunctionSpec {
	var specs []lang.FunctionSpec
	for _, aggregate := range []struct {
		name, label, description string
		of                       func([]lang.Money, lang.Rounding) (lang.Money, error)
	}{
		{"avg", "平均值", "同币种金额的平均，落在两个最小单位之间时按舍入方式取整；空数组报错。", lang.AverageMoney},
		{"median", "中位数", "同币种金额的中位数，偶数个时取中间两笔的平均并按舍入方式取整；空数组报错。", lang.MedianMoney},
	} {
		of := aggregate.of
		specs = append(specs,
			moneySpec(aggregate.name, []lang.Type{amounts}, c, aggregate.label, aggregate.description,
				func(_ context.Context, args []lang.Value) (lang.Value, error) {
					return aggregateMoney(args[0], rounding, of)
				}),
			moneySpec(aggregate.name, []lang.Type{amounts, lang.RoundingEnumType()}, c, aggregate.label, aggregate.description,
				func(_ context.Context, args []lang.Value) (lang.Value, error) {
					text, _ := args[1].String()
					mode, err := lang.ParseRounding(text)
					if err != nil {
						return lang.Value{}, err
					}
					return aggregateMoney(args[0], mode, of)
				}))
	}
	return specs
}

// keyedByMoney lets money be the key of the keyed selections: the cheapest
// channels by fee.
func keyedByMoney(amounts lang.Type) []lang.FunctionSpec {
	list := lang.ArrayOf(lang.TypeVar("T"))
	var specs []lang.FunctionSpec
	for _, name := range []string{"sort_by", "sort_by_desc"} {
		up := name == "sort_by"
		specs = append(specs, moneySpec(name, []lang.Type{list, amounts}, list, "按金额排序", "按金额键排序候选，相等的保持原顺序。",
			uniformKeys(1, func(args []lang.Value) (lang.Value, error) { return sortedBy(args, up) })))
	}
	for _, name := range []string{"bottom_k", "top_k"} {
		ascending := name == "bottom_k"
		specs = append(specs, moneySpec(name, []lang.Type{list, amounts, lang.IntType}, list, "按金额取前 k 个", "按金额键取最小或最大的 k 个候选。",
			uniformKeys(1, func(args []lang.Value) (lang.Value, error) { return pickRanked(args, ascending) })))
	}
	return specs
}

// uniformKeys checks the money keys at index share a currency before eval.
func uniformKeys(index int, eval func([]lang.Value) (lang.Value, error)) lang.EvalFunc {
	return func(_ context.Context, args []lang.Value) (lang.Value, error) {
		if _, _, err := amountsOf(args[index]); err != nil {
			return lang.Value{}, err
		}
		return eval(args)
	}
}

// amountsOf is an array of money's backing and an amount that has the one
// currency they are in — the zero value when every one is the currency-less
// zero. Results are made in that currency with its Like.
func amountsOf(value lang.Value) ([]lang.Money, lang.Money, error) {
	amounts, err := lang.FromValue[[]lang.Money](value)
	if err != nil {
		return nil, lang.Money{}, err
	}
	var reference lang.Money
	for _, amount := range amounts {
		switch {
		case amount.Currency() == "" || amount.Currency() == reference.Currency():
		case reference.Currency() == "":
			reference = amount
		default:
			return nil, lang.Money{}, fmt.Errorf("%w: %s and %s in one list", lang.ErrCurrency, reference.Currency(), amount.Currency())
		}
	}
	return amounts, reference, nil
}

// inCurrency is an amount given the list's currency: the currency-less zero
// becomes the zero of a list that has one.
func inCurrency(amount, reference lang.Money) (lang.Value, error) {
	if amount.Currency() == "" {
		filled, err := reference.Like(amount.Minor())
		if err != nil {
			return lang.Value{}, err
		}
		amount = filled
	}
	return lang.ToValue(amount)
}

func sumMoney(_ context.Context, args []lang.Value) (lang.Value, error) {
	amounts, reference, err := amountsOf(args[0])
	if err != nil {
		return lang.Value{}, err
	}
	total, err := addAll(amounts)
	if err != nil {
		return lang.Value{}, err
	}
	return inCurrency(total, reference)
}

// addAll is the amounts' total, added the way the language adds money.
func addAll(amounts []lang.Money) (lang.Money, error) {
	var total lang.Money
	for _, amount := range amounts {
		sum, err := total.Add(amount)
		if err != nil {
			return lang.Money{}, err
		}
		total = sum
	}
	return total, nil
}

func extremeMoney(smallest bool) lang.EvalFunc {
	return func(_ context.Context, args []lang.Value) (lang.Value, error) {
		amounts, reference, err := amountsOf(args[0])
		if err != nil {
			return lang.Value{}, err
		}
		index, err := extremeMoneyIndex(amounts, smallest)
		if err != nil {
			return lang.Value{}, err
		}
		return inCurrency(amounts[index], reference)
	}
}

func argExtremeMoney(smallest bool) lang.EvalFunc {
	return func(_ context.Context, args []lang.Value) (lang.Value, error) {
		amounts, _, err := amountsOf(args[0])
		if err != nil {
			return lang.Value{}, err
		}
		index, err := extremeMoneyIndex(amounts, smallest)
		return lang.Int(int64(index)), err
	}
}

func extremeMoneyIndex(amounts []lang.Money, smallest bool) (int, error) {
	if len(amounts) == 0 {
		return 0, fmt.Errorf("an empty array has no extreme")
	}
	best := 0
	for i, amount := range amounts {
		if (smallest && amount.Minor() < amounts[best].Minor()) || (!smallest && amount.Minor() > amounts[best].Minor()) {
			best = i
		}
	}
	return best, nil
}

func pairMoney(smallest bool) lang.EvalFunc {
	return func(_ context.Context, args []lang.Value) (lang.Value, error) {
		pair, err := lang.Array(lang.MoneyOf(""), args)
		if err != nil {
			return lang.Value{}, err
		}
		return extremeMoney(smallest)(context.Background(), []lang.Value{pair})
	}
}

func absMoney(_ context.Context, args []lang.Value) (lang.Value, error) {
	amount, _ := args[0].Money()
	absolute, err := amount.Abs()
	if err != nil {
		return lang.Value{}, err
	}
	return lang.ToValue(absolute)
}

func sortMoney(ascending bool) lang.EvalFunc {
	return func(_ context.Context, args []lang.Value) (lang.Value, error) {
		amounts, _, err := amountsOf(args[0])
		if err != nil {
			return lang.Value{}, err
		}
		sorted := slices.Clone(amounts)
		slices.SortStableFunc(sorted, func(a, b lang.Money) int {
			if ascending {
				return compareInts(a.Minor(), b.Minor())
			}
			return compareInts(b.Minor(), a.Minor())
		})
		return lang.ToValue(sorted)
	}
}

func compareInts(a, b int64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

func cumulativeMoney(_ context.Context, args []lang.Value) (lang.Value, error) {
	amounts, reference, err := amountsOf(args[0])
	if err != nil {
		return lang.Value{}, err
	}
	out := make([]lang.Money, len(amounts))
	var total lang.Money
	for i, amount := range amounts {
		if total, err = total.Add(amount); err != nil {
			return lang.Value{}, err
		}
		if out[i], err = filled(total, reference); err != nil {
			return lang.Value{}, err
		}
	}
	return lang.ToValue(out)
}

func deltasMoney(_ context.Context, args []lang.Value) (lang.Value, error) {
	amounts, reference, err := amountsOf(args[0])
	if err != nil {
		return lang.Value{}, err
	}
	out := make([]lang.Money, 0, max(len(amounts)-1, 0))
	for i := 1; i < len(amounts); i++ {
		step, err := amounts[i].Sub(amounts[i-1])
		if err != nil {
			return lang.Value{}, err
		}
		if step, err = filled(step, reference); err != nil {
			return lang.Value{}, err
		}
		out = append(out, step)
	}
	return lang.ToValue(out)
}

// filled is inCurrency for an amount that stays Go.
func filled(amount, reference lang.Money) (lang.Money, error) {
	if amount.Currency() != "" {
		return amount, nil
	}
	return reference.Like(amount.Minor())
}

func aggregateMoney(value lang.Value, mode lang.Rounding, of func([]lang.Money, lang.Rounding) (lang.Money, error)) (lang.Value, error) {
	amounts, _, err := amountsOf(value)
	if err != nil {
		return lang.Value{}, err
	}
	result, err := of(amounts, mode)
	if err != nil {
		return lang.Value{}, err
	}
	return lang.ToValue(result)
}
