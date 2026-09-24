package std

import (
	"context"
	"fmt"
	"slices"

	"github.com/nethinwei/funroute"
)

// The pack's aggregates over money, registered only when the registry
// declares money — so a host declares its currencies before it registers
// the pack. They are FunctionSpecs rather than Logic because a signature has
// to say money: reflection sees money, and the currency is the value's.
// Every one checks that the amounts it is handed share a currency; the
// currency-less zero goes with any.

func registerMoney(registry *funroute.Registry) error {
	_, declared := registry.Money()
	if !declared {
		return nil
	}
	c := funroute.MoneyType
	amounts := funroute.ArrayOf(c)
	specs := []funroute.FunctionSpec{
		moneySpec("sum", []funroute.Type{amounts}, c, "求和", "同币种金额相加；空数组是不带币种的 0。", sumMoney),
		moneySpec("min", []funroute.Type{amounts}, c, "最小值", "最小的金额；空数组报错。", extremeMoney(true)),
		moneySpec("max", []funroute.Type{amounts}, c, "最大值", "最大的金额；空数组报错。", extremeMoney(false)),
		moneySpec("min", []funroute.Type{c, c}, c, "较小者", "两笔同币种金额里较小的。", pairMoney(true)),
		moneySpec("max", []funroute.Type{c, c}, c, "较大者", "两笔同币种金额里较大的。", pairMoney(false)),
		moneySpec("abs", []funroute.Type{c}, c, "绝对值", "金额的绝对值。", absMoney),
		moneySpec("sort", []funroute.Type{amounts}, amounts, "排序", "金额从小到大。", sortMoney(true)),
		moneySpec("sort_desc", []funroute.Type{amounts}, amounts, "降序", "金额从大到小。", sortMoney(false)),
		moneySpec("cumsum", []funroute.Type{amounts}, amounts, "累加", "逐项累加出的金额序列。", cumulativeMoney),
		moneySpec("deltas", []funroute.Type{amounts}, amounts, "差分", "相邻两笔金额之差。", deltasMoney),
		moneySpec("arg_min", []funroute.Type{amounts}, funroute.IntType, "最小值下标", "最小金额的下标；空数组报错。", argExtremeMoney(true)),
		moneySpec("arg_max", []funroute.Type{amounts}, funroute.IntType, "最大值下标", "最大金额的下标；空数组报错。", argExtremeMoney(false)),
	}
	specs = append(specs, roundedMoneySpecs(amounts, c)...)
	specs = append(specs, keyedByMoney(amounts)...)
	for _, spec := range specs {
		if err := register(registry, spec); err != nil {
			return err
		}
	}
	return nil
}

func moneySpec(name string, params []funroute.Type, result funroute.Type, label, description string, eval funroute.EvalFunc) funroute.FunctionSpec {
	return funroute.FunctionSpec{
		Name: name, Params: params, Result: result, Eval: eval,
		Doc: funroute.Doc{Constexpr: true, Label: label, Category: "金额", Cost: 4, Description: description},
	}
}

// roundedMoneySpecs are avg and median, whose results fall between minor
// units: they take the rounding mode as a last argument, since a rule writes
// every rounding it does.
func roundedMoneySpecs(amounts, c funroute.Type) []funroute.FunctionSpec {
	var specs []funroute.FunctionSpec
	for _, aggregate := range []struct {
		name, label, description string
		of                       func([]funroute.Money, funroute.Rounding) (funroute.Money, error)
	}{
		{"avg", "平均值", "同币种金额的平均，落在两个最小单位之间时按舍入方式取整；空数组报错。", funroute.AverageMoney},
		{"median", "中位数", "同币种金额的中位数，偶数个时取中间两笔的平均并按舍入方式取整；空数组报错。", funroute.MedianMoney},
	} {
		of := aggregate.of
		specs = append(specs,
			moneySpec(aggregate.name, []funroute.Type{amounts, funroute.RoundingEnumType()}, c, aggregate.label, aggregate.description,
				func(_ context.Context, args []funroute.Value) (funroute.Value, error) {
					text, _ := args[1].String()
					mode, err := funroute.ParseRounding(text)
					if err != nil {
						return funroute.Value{}, err
					}
					return aggregateMoney(args[0], mode, of)
				}))
	}
	return specs
}

// keyedByMoney lets money be the key of the keyed selections: the cheapest
// channels by fee.
func keyedByMoney(amounts funroute.Type) []funroute.FunctionSpec {
	list := funroute.ArrayOf(funroute.TypeVar("T"))
	var specs []funroute.FunctionSpec
	for _, name := range []string{"sort_by", "sort_by_desc"} {
		up := name == "sort_by"
		specs = append(specs, moneySpec(name, []funroute.Type{list, amounts}, list, "按金额排序", "按金额键排序候选，相等的保持原顺序。",
			uniformKeys(1, func(args []funroute.Value) (funroute.Value, error) { return sortedBy(args, up) })))
	}
	for _, name := range []string{"bottom_k", "top_k"} {
		ascending := name == "bottom_k"
		specs = append(specs, moneySpec(name, []funroute.Type{list, amounts, funroute.IntType}, list, "按金额取前 k 个", "按金额键取最小或最大的 k 个候选。",
			uniformKeys(1, func(args []funroute.Value) (funroute.Value, error) { return pickRanked(args, ascending) })))
	}
	return specs
}

// uniformKeys checks the money keys at index share a currency before eval.
func uniformKeys(index int, eval func([]funroute.Value) (funroute.Value, error)) funroute.EvalFunc {
	return func(_ context.Context, args []funroute.Value) (funroute.Value, error) {
		if _, _, err := amountsOf(args[index]); err != nil {
			return funroute.Value{}, err
		}
		return eval(args)
	}
}

// amountsOf is an array of money's backing and an amount that has the one
// currency they are in — the zero value when every one is the currency-less
// zero. Results are made in that currency with its Like.
func amountsOf(value funroute.Value) ([]funroute.Money, funroute.Money, error) {
	amounts, err := funroute.FromValue[[]funroute.Money](value)
	if err != nil {
		return nil, funroute.Money{}, err
	}
	var reference funroute.Money
	for _, amount := range amounts {
		switch {
		case amount.Currency() == "" || amount.Currency() == reference.Currency():
		case reference.Currency() == "":
			reference = amount
		default:
			return nil, funroute.Money{}, fmt.Errorf("%w: %s and %s in one list", funroute.ErrCurrency, reference.Currency(), amount.Currency())
		}
	}
	return amounts, reference, nil
}

// inCurrency is an amount given the list's currency: the currency-less zero
// becomes the zero of a list that has one.
func inCurrency(amount, reference funroute.Money) (funroute.Value, error) {
	if amount.Currency() == "" {
		// The currency-less zero is only ever zero: the list's zero.
		filled, err := reference.MulInt(0)
		if err != nil {
			return funroute.Value{}, err
		}
		amount = filled
	}
	return funroute.ToValue(amount)
}

func sumMoney(_ context.Context, args []funroute.Value) (funroute.Value, error) {
	amounts, reference, err := amountsOf(args[0])
	if err != nil {
		return funroute.Value{}, err
	}
	total, err := addAll(amounts)
	if err != nil {
		return funroute.Value{}, err
	}
	return inCurrency(total, reference)
}

// addAll is the amounts' total, added the way the language adds money.
func addAll(amounts []funroute.Money) (funroute.Money, error) {
	var total funroute.Money
	for _, amount := range amounts {
		sum, err := total.Add(amount)
		if err != nil {
			return funroute.Money{}, err
		}
		total = sum
	}
	return total, nil
}

func extremeMoney(smallest bool) funroute.EvalFunc {
	return func(_ context.Context, args []funroute.Value) (funroute.Value, error) {
		amounts, reference, err := amountsOf(args[0])
		if err != nil {
			return funroute.Value{}, err
		}
		index, err := extremeMoneyIndex(amounts, smallest)
		if err != nil {
			return funroute.Value{}, err
		}
		return inCurrency(amounts[index], reference)
	}
}

func argExtremeMoney(smallest bool) funroute.EvalFunc {
	return func(_ context.Context, args []funroute.Value) (funroute.Value, error) {
		amounts, _, err := amountsOf(args[0])
		if err != nil {
			return funroute.Value{}, err
		}
		index, err := extremeMoneyIndex(amounts, smallest)
		return funroute.Int(int64(index)), err
	}
}

func extremeMoneyIndex(amounts []funroute.Money, smallest bool) (int, error) {
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

func pairMoney(smallest bool) funroute.EvalFunc {
	return func(_ context.Context, args []funroute.Value) (funroute.Value, error) {
		pair, err := funroute.Array(funroute.MoneyType, args)
		if err != nil {
			return funroute.Value{}, err
		}
		return extremeMoney(smallest)(context.Background(), []funroute.Value{pair})
	}
}

func absMoney(_ context.Context, args []funroute.Value) (funroute.Value, error) {
	amount, _ := args[0].Money()
	absolute, err := amount.Abs()
	if err != nil {
		return funroute.Value{}, err
	}
	return funroute.ToValue(absolute)
}

func sortMoney(ascending bool) funroute.EvalFunc {
	return func(_ context.Context, args []funroute.Value) (funroute.Value, error) {
		amounts, _, err := amountsOf(args[0])
		if err != nil {
			return funroute.Value{}, err
		}
		sorted := slices.Clone(amounts)
		slices.SortStableFunc(sorted, func(a, b funroute.Money) int {
			if ascending {
				return compareInts(a.Minor(), b.Minor())
			}
			return compareInts(b.Minor(), a.Minor())
		})
		return funroute.ToValue(sorted)
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

func cumulativeMoney(_ context.Context, args []funroute.Value) (funroute.Value, error) {
	amounts, reference, err := amountsOf(args[0])
	if err != nil {
		return funroute.Value{}, err
	}
	out := make([]funroute.Money, len(amounts))
	var total funroute.Money
	for i, amount := range amounts {
		if total, err = total.Add(amount); err != nil {
			return funroute.Value{}, err
		}
		if out[i], err = filled(total, reference); err != nil {
			return funroute.Value{}, err
		}
	}
	return funroute.ToValue(out)
}

func deltasMoney(_ context.Context, args []funroute.Value) (funroute.Value, error) {
	amounts, reference, err := amountsOf(args[0])
	if err != nil {
		return funroute.Value{}, err
	}
	out := make([]funroute.Money, 0, max(len(amounts)-1, 0))
	for i := 1; i < len(amounts); i++ {
		step, err := amounts[i].Sub(amounts[i-1])
		if err != nil {
			return funroute.Value{}, err
		}
		if step, err = filled(step, reference); err != nil {
			return funroute.Value{}, err
		}
		out = append(out, step)
	}
	return funroute.ToValue(out)
}

// filled is inCurrency for an amount that stays Go.
func filled(amount, reference funroute.Money) (funroute.Money, error) {
	if amount.Currency() != "" {
		return amount, nil
	}
	return reference.MulInt(0)
}

func aggregateMoney(value funroute.Value, mode funroute.Rounding, of func([]funroute.Money, funroute.Rounding) (funroute.Money, error)) (funroute.Value, error) {
	amounts, _, err := amountsOf(value)
	if err != nil {
		return funroute.Value{}, err
	}
	result, err := of(amounts, mode)
	if err != nil {
		return funroute.Value{}, err
	}
	return funroute.ToValue(result)
}
