package std

import (
	"cmp"
	"context"
	"fmt"
	"slices"

	"github.com/nethinwei/funroute"
)

// The pack's aggregates over money, registered only when the registry
// declares money — so a host declares its currencies before it registers
// the pack. They write Params, Result and Eval rather than Go because a signature has
// to say money: reflection sees money, and the currency is the value's.
// Every one checks that the amounts it is handed share a currency; the
// currency-less zero goes with any.

func moneySpecs() []funroute.FunctionSpec {
	c := funroute.MoneyType
	amounts := funroute.ArrayOf(c)
	item := funroute.TypeVar("T")
	list := funroute.ArrayOf(item)
	return slices.Concat([]funroute.FunctionSpec{
		moneySpec("sum", []funroute.Type{amounts}, c, moneyDoc("求和", "同币种金额相加；空数组是不带币种的 0。", "总和", "数组"), sumMoney),
		moneySpec("min", []funroute.Type{amounts}, c, moneyDoc("最小值", "最小的金额；空数组报错。", "最小的元素", "数组"), extremeMoney(true)),
		moneySpec("max", []funroute.Type{amounts}, c, moneyDoc("最大值", "最大的金额；空数组报错。", "最大的元素", "数组"), extremeMoney(false)),
		moneySpec("min", []funroute.Type{c, c}, c, moneyDoc("较小者", "两笔同币种金额里较小的。", "结果", "左值", "右值"), pairMoney(true)),
		moneySpec("max", []funroute.Type{c, c}, c, moneyDoc("较大者", "两笔同币种金额里较大的。", "结果", "左值", "右值"), pairMoney(false)),
		moneySpec("abs", []funroute.Type{c}, c, moneyDoc("绝对值", "金额的绝对值。", "绝对值", "金额"), absMoney),
		moneySpec("sort", []funroute.Type{amounts}, amounts, moneyDoc("排序", "金额从小到大。", "升序数组", "数组"), sortMoney(true)),
		moneySpec("sort_desc", []funroute.Type{amounts}, amounts, moneyDoc("降序", "金额从大到小。", "降序数组", "数组"), sortMoney(false)),
		moneySpec("cumsum", []funroute.Type{amounts}, amounts, moneyDoc("累加", "逐项累加出的金额序列。", "累计序列", "数组"), cumulativeMoney),
		moneySpec("deltas", []funroute.Type{amounts}, amounts, moneyDoc("差分", "相邻两笔金额之差。", "差值序列", "数组"), deltasMoney),
		moneySpec("arg_min", []funroute.Type{amounts}, funroute.IntType, moneyDoc("最小值下标", "最小金额的下标；空数组报错。", "下标", "键"), argExtremeMoney(true)),
		moneySpec("arg_max", []funroute.Type{amounts}, funroute.IntType, moneyDoc("最大值下标", "最大金额的下标；空数组报错。", "下标", "键"), argExtremeMoney(false)),
		moneySpec("min_by", []funroute.Type{list, amounts}, item, moneyDoc("金额最小的候选", "金额键最小的那个候选；并列取第一个，空数组报错。", "候选", "候选", "键"), extremeByMoney(true)),
		moneySpec("max_by", []funroute.Type{list, amounts}, item, moneyDoc("金额最大的候选", "金额键最大的那个候选；并列取第一个，空数组报错。", "候选", "候选", "键"), extremeByMoney(false)),
	}, roundedMoneySpecs(amounts, c))
}

func moneySpec(name string, params []funroute.Type, result funroute.Type, doc funroute.Doc, eval funroute.EvalFunc) funroute.FunctionSpec {
	return funroute.FunctionSpec{Name: name, Params: params, Result: result, Eval: eval, Doc: doc}
}

// moneyDoc names the result and each parameter as the overloads of the same
// name over other elements do, so a signature reads alike whichever is shown.
func moneyDoc(label, description, result string, params ...string) funroute.Doc {
	return funroute.Doc{Label: label, Category: "金额", Description: description, Params: params, Result: result}
}

// roundedMoneySpecs are avg and median, whose results fall between minor
// units: they take the rounding mode as a last argument, since a rule writes
// every rounding it does.
func roundedMoneySpecs(amounts, c funroute.Type) []funroute.FunctionSpec {
	specs := make([]funroute.FunctionSpec, 0, 2)
	for _, aggregate := range []struct {
		name, label, description, result string
		of                               func([]funroute.Money, funroute.Rounding) (funroute.Money, error)
	}{
		{"avg", "平均值", "同币种金额的平均，落在两个最小单位之间时按舍入方式取整；空数组报错。", "平均值", funroute.AverageMoney},
		{"median", "中位数", "同币种金额的中位数，偶数个时取中间两笔的平均并按舍入方式取整；空数组报错。", "中位数", funroute.MedianMoney},
	} {
		of := aggregate.of
		specs = append(specs,
			moneySpec(aggregate.name, []funroute.Type{amounts, funroute.RoundingEnumType()}, c, moneyDoc(aggregate.label, aggregate.description, aggregate.result, "数组", "舍入方式"),
				func(_ context.Context, args []funroute.Value) (funroute.Value, error) {
					text, _ := args[1].String()
					mode, err := funroute.ParseRounding(text)
					if err != nil {
						return funroute.Value{}, err
					}
					amounts, _, err := amountsOf(args[0])
					if err != nil {
						return funroute.Value{}, err
					}
					result, err := of(amounts, mode)
					if err != nil {
						return funroute.Value{}, err
					}
					return funroute.ToValue(result)
				}))
	}
	return specs
}

// amountsOf is an array of money's backing and the zero of the one currency
// they are in — the currency-less zero when every one of them is that zero.
// A result made from them is in their currency: a total that starts at that
// zero, or inCurrencyOf.
func amountsOf(value funroute.Value) ([]funroute.Money, funroute.Money, error) {
	amounts, err := funroute.FromValue[[]funroute.Money](value)
	if err != nil {
		return nil, funroute.Money{}, err
	}
	var zero funroute.Money
	for _, amount := range amounts {
		switch {
		case amount.Currency() == "" || amount.Currency() == zero.Currency():
		case zero.Currency() == "":
			if zero, err = amount.MulInt(0); err != nil {
				return nil, funroute.Money{}, err
			}
		default:
			return nil, funroute.Money{}, fmt.Errorf("%w: %s and %s in one list", funroute.ErrCurrency, zero.Currency(), amount.Currency())
		}
	}
	return amounts, zero, nil
}

// inCurrencyOf is an amount made from a list, in the list's currency: the
// currency-less zero is the list's zero.
func inCurrencyOf(amount, zero funroute.Money) funroute.Money {
	if amount.Currency() == "" {
		return zero
	}
	return amount
}

// sumMoney adds the amounts the way the language adds money.
func sumMoney(_ context.Context, args []funroute.Value) (funroute.Value, error) {
	amounts, total, err := amountsOf(args[0])
	if err != nil {
		return funroute.Value{}, err
	}
	for _, amount := range amounts {
		if total, err = total.Add(amount); err != nil {
			return funroute.Value{}, err
		}
	}
	return funroute.ToValue(total)
}

func extremeMoney(smallest bool) funroute.EvalFunc {
	return func(_ context.Context, args []funroute.Value) (funroute.Value, error) {
		amounts, zero, err := amountsOf(args[0])
		if err != nil {
			return funroute.Value{}, err
		}
		at, err := extremeMoneyIndex(amounts, smallest)
		if err != nil {
			return funroute.Value{}, err
		}
		return funroute.ToValue(inCurrencyOf(amounts[at], zero))
	}
}

func argExtremeMoney(smallest bool) funroute.EvalFunc {
	return func(_ context.Context, args []funroute.Value) (funroute.Value, error) {
		amounts, _, err := amountsOf(args[0])
		if err != nil {
			return funroute.Value{}, err
		}
		at, err := extremeMoneyIndex(amounts, smallest)
		return funroute.Int(int64(at)), err
	}
}

// extremeByMoney is the candidate at argExtremeMoney's position among the
// keys.
func extremeByMoney(smallest bool) funroute.EvalFunc {
	return func(_ context.Context, args []funroute.Value) (funroute.Value, error) {
		amounts, _, err := amountsOf(args[1])
		if err != nil {
			return funroute.Value{}, err
		}
		if n, _ := args[0].Length(); n != len(amounts) {
			return funroute.Value{}, fmt.Errorf("%w: the list has %d candidates and %d keys", funroute.ErrDomain, n, len(amounts))
		}
		at, err := extremeMoneyIndex(amounts, smallest)
		if err != nil {
			return funroute.Value{}, err
		}
		item, _ := args[0].At(at)
		return item, nil
	}
}

// extremeMoneyIndex is best for money, which orders by its minor units. It is
// its own loop because a key function would be a call per item: best cannot
// take one without making every extreme several times slower.
func extremeMoneyIndex(amounts []funroute.Money, smallest bool) (int, error) {
	if len(amounts) == 0 {
		return 0, errNoExtreme
	}
	at := 0
	for i, amount := range amounts {
		if smallest && amount.Minor() < amounts[at].Minor() || !smallest && amount.Minor() > amounts[at].Minor() {
			at = i
		}
	}
	return at, nil
}

// pairMoney is extremeMoney over two amounts, compared where they are: the
// currency-less zero takes the other's currency, as it would in a list.
func pairMoney(smallest bool) funroute.EvalFunc {
	return func(_ context.Context, args []funroute.Value) (funroute.Value, error) {
		first, _ := args[0].Money()
		second, _ := args[1].Money()
		if first.Currency() != "" && second.Currency() != "" && first.Currency() != second.Currency() {
			return funroute.Value{}, fmt.Errorf("%w: %s and %s in one list", funroute.ErrCurrency, first.Currency(), second.Currency())
		}
		best, other := first, second
		if smallest && second.Minor() < first.Minor() || !smallest && second.Minor() > first.Minor() {
			best, other = second, first
		}
		if best.Currency() == "" && other.Currency() != "" {
			zero, err := other.MulInt(0)
			if err != nil {
				return funroute.Value{}, err
			}
			best = zero
		}
		return funroute.ToValue(best)
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
				return cmp.Compare(a.Minor(), b.Minor())
			}
			return cmp.Compare(b.Minor(), a.Minor())
		})
		return funroute.ToValue(sorted)
	}
}

func cumulativeMoney(_ context.Context, args []funroute.Value) (funroute.Value, error) {
	amounts, total, err := amountsOf(args[0])
	if err != nil {
		return funroute.Value{}, err
	}
	out := make([]funroute.Money, len(amounts))
	for i, amount := range amounts {
		if total, err = total.Add(amount); err != nil {
			return funroute.Value{}, err
		}
		out[i] = total
	}
	return funroute.ToValue(out)
}

func deltasMoney(_ context.Context, args []funroute.Value) (funroute.Value, error) {
	amounts, zero, err := amountsOf(args[0])
	if err != nil {
		return funroute.Value{}, err
	}
	out := make([]funroute.Money, 0, max(len(amounts)-1, 0))
	for i := 1; i < len(amounts); i++ {
		step, err := amounts[i].Sub(amounts[i-1])
		if err != nil {
			return funroute.Value{}, err
		}
		out = append(out, inCurrencyOf(step, zero))
	}
	return funroute.ToValue(out)
}
