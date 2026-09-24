package machine

import (
	"context"
	"fmt"
	"strings"

	"github.com/nethinwei/funroute/internal/money"
)

// The money functions: building money from data, taking it apart, splitting
// it without losing a cent, and the conversions between ratios and the other
// numbers. Money and plain numbers only meet here and in the operators.

func (k moneyKernel) registerFunctions(registry *Registry) {
	k.registerBuilders(registry)
	registerParts(registry)
	registerAllocate(registry)
	k.registerProportions(registry)
	registerRatioConversions(registry)
	registerCurrencyKeys(registry)
}

func (k moneyKernel) registerBuilders(registry *Registry) {
	registerMoneyOp(registry, "money", []Type{IntType, CurrencyType}, MoneyType,
		moneyDoc("money", "由最小单位的整数与币种造出金额：money(170, USD) 是 USD 1.70。输入里的金额与币种分开时用它。", "最小单位", "币种"),
		func(_ context.Context, args []Value) (Value, error) { return MoneyValue(args[0].i, args[1].s), nil })
	registerMoneyOp(registry, "currency_of", []Type{StringType}, CurrencyType,
		moneyDoc("currency_of", "把币种代码字符串读成币种；不是注册表声明的币种就是错误。", "代码"),
		func(_ context.Context, args []Value) (Value, error) {
			if _, err := k.table.Places(args[0].s); err != nil {
				return Value{}, err
			}
			return CurrencyValue(args[0].s), nil
		})
}

func registerParts(registry *Registry) {
	registerMoneyOp(registry, "minor", []Type{MoneyType}, IntType,
		moneyDoc("minor", "金额的最小单位整数：USD 1.70 是 170。", "金额"),
		func(_ context.Context, args []Value) (Value, error) { return Int(args[0].i), nil })
	registerExactOp(registry, "currency", []Type{MoneyType}, CurrencyType,
		moneyDoc("currency", "金额的币种，可以进 switch、作字典的键。", "金额"),
		func(_ context.Context, args []Value) (Value, error) {
			if args[0].s == "" {
				return Value{}, fmt.Errorf("%w: a currency-less zero has no currency", ErrCurrency)
			}
			return CurrencyValue(args[0].s), nil
		})
	registerExactOp(registry, "sign", []Type{MoneyType}, IntType,
		moneyDoc("sign", "金额的符号：负数 -1，零 0，正数 1。", "金额"),
		func(_ context.Context, args []Value) (Value, error) {
			// An exact amount's numerator carries its sign.
			return Int(int64(compareOrdered(args[0].i, 0))), nil
		})
	registerMoneyOp(registry, "string", []Type{CurrencyType}, StringType,
		moneyDoc("string", "币种的代码。", "币种"),
		func(_ context.Context, args []Value) (Value, error) { return String(args[0].s), nil })
}

func registerAllocate(registry *Registry) {
	byWeight := moneyDoc("allocate", "按整数权重分一笔金额，一分不丢：各份向零取整，剩下的最小单位按最大余数法发出（被舍掉最多的份先得）。", "金额", "权重")
	registerMoneyOp(registry, "allocate", []Type{MoneyType, ArrayOf(IntType)}, ArrayOf(MoneyType), byWeight, allocateByWeight)
	byWeight = moneyDoc("allocate", "按整数权重分一笔金额，一分不丢；剩下的最小单位按策略发出："+strategyDoc, "金额", "权重", "分配策略")
	registerMoneyOp(registry, "allocate", []Type{MoneyType, ArrayOf(IntType), AllocationEnumType()}, ArrayOf(MoneyType), byWeight, allocateByWeight)
	doc := moneyDoc("allocate", "把一笔金额平均分成 n 份，一分不丢：余数从第一份开始给。n 的规模必须由输入界定。", "金额", "份数")
	doc.BoundedArgs = true
	registerMoneyOp(registry, "allocate", []Type{MoneyType, IntType}, ArrayOf(MoneyType), doc, allocateEvenly)
	doc = moneyDoc("allocate", "把一笔金额平均分成 n 份，一分不丢；余数按策略发出："+strategyDoc+"n 的规模必须由输入界定。", "金额", "份数", "分配策略")
	doc.BoundedArgs = true
	registerMoneyOp(registry, "allocate", []Type{MoneyType, IntType, AllocationEnumType()}, ArrayOf(MoneyType), doc, allocateEvenly)
}

const strategyDoc = "@largest_remainder 被舍掉最多的份先得（缺省），@largest_weight 权重大的先得，@in_order 从第一份起，" +
	"@reverse_order 从最后一份起，@all_first 全给第一份，@all_last 全给最后一份。"

// allocationStrategy is the strategy a call names in its third argument, or
// the largest remainder when it names none.
func allocationStrategy(args []Value) (money.AllocationStrategy, error) {
	if len(args) < 3 {
		return money.AllocateLargestRemainder, nil
	}
	return money.ParseAllocation(args[2].s)
}

func allocateByWeight(_ context.Context, args []Value) (Value, error) {
	strategy, err := allocationStrategy(args)
	if err != nil {
		return Value{}, err
	}
	weights, _ := args[1].box.([]int64)
	amount, _ := args[0].Money()
	shares, err := amount.AllocateBy(strategy, weights...)
	return Value{kind: ArrayKind, box: shares}, err
}

func allocateEvenly(_ context.Context, args []Value) (Value, error) {
	strategy, err := allocationStrategy(args)
	if err != nil {
		return Value{}, err
	}
	amount, _ := args[0].Money()
	shares, err := amount.SplitBy(strategy, int(args[1].i))
	return Value{kind: ArrayKind, box: shares}, err
}

// registerProportions registers prorate, which lands between two minor
// units and so is a step of a round or takes its mode, and round_to, which
// is a rounding itself and always takes its mode.
func (k moneyKernel) registerProportions(registry *Registry) {
	byInts := moneyDoc("prorate", "按比例取金额：prorate(m, part, whole) 是 m × part / whole，中间精确、只舍入一次。", "金额", "分子", "分母")
	registerRounded(registry, FunctionSpec{Name: "prorate", Params: []Type{MoneyType, IntType, IntType}, Result: MoneyType, Doc: byInts},
		func(_ context.Context, args []Value) (money.ExactMoney, error) {
			part, err := proportion(args[1].i, args[2].i)
			if err != nil {
				return money.ExactMoney{}, err
			}
			return exactOf(args[0]).MulRatio(part)
		},
		func(_ context.Context, args []Value, mode money.Rounding) (Value, error) {
			return moneyResult(moneyOf(args[0]).Prorate(args[1].i, args[2].i, mode))
		})
	byAmounts := moneyDoc("prorate", "按两笔同币种金额之比取金额：prorate(fee, refund, paid) 按退款占比退手续费，中间精确、只舍入一次。", "金额", "部分", "整体")
	registerRounded(registry, FunctionSpec{Name: "prorate", Params: []Type{MoneyType, MoneyType, MoneyType}, Result: MoneyType, Doc: byAmounts},
		func(_ context.Context, args []Value) (money.ExactMoney, error) {
			part, err := moneyOf(args[1]).Ratio(moneyOf(args[2]))
			if err != nil {
				return money.ExactMoney{}, err
			}
			return exactOf(args[0]).MulRatio(part)
		},
		func(_ context.Context, args []Value, mode money.Rounding) (Value, error) {
			return moneyResult(moneyOf(args[0]).ProrateBy(moneyOf(args[1]), moneyOf(args[2]), mode))
		})
	step := moneyDoc("round_to", "把金额取到粒度的整数倍：round_to(m, CHF 0.05, @half_up) 是现金舍入，粒度是同币种的正金额。", "金额", "粒度", "舍入方式")
	registerMoneyOp(registry, "round_to", []Type{MoneyType, MoneyType, RoundingEnumType()}, MoneyType, step, func(_ context.Context, args []Value) (Value, error) {
		mode, err := money.ParseRounding(args[2].s)
		if err != nil {
			return Value{}, err
		}
		return moneyResult(moneyOf(args[0]).RoundTo(moneyOf(args[1]), mode))
	})
}

// proportion is part / whole as a ratio; a whole of zero is no proportion.
func proportion(part, whole int64) (money.Ratio, error) {
	if whole == 0 {
		return money.Ratio{}, errDivisionByZero
	}
	return money.RatioOf(part, whole)
}

func registerRatioConversions(registry *Registry) {
	registerMoneyOp(registry, "ratio", []Type{StringType}, RatioType,
		moneyDoc("ratio", "把文本精确地读成比例：十进制小数或整数之比（1/3）；读不成比例是错误。", "文本"),
		func(_ context.Context, args []Value) (Value, error) {
			r, err := money.ParseRatio(strings.TrimSpace(args[0].s))
			return RatioValue(r), err
		})
}

// registerCurrencyKeys lets a currency index a dictionary: rates[currency(m)].
func registerCurrencyKeys(registry *Registry) {
	t := TypeVar("T")
	registerMoneyOp(registry, "at", []Type{DictOf(t), CurrencyType}, t,
		moneyDoc("at", "按币种取字典的值；键是币种代码，不存在是错误。写作 d[cur]。", "字典", "币种"), evalDictAt)
	registerMoneyOp(registry, "member", []Type{CurrencyType, DictOf(t)}, BoolType,
		moneyDoc("member", "字典里有没有这个币种的键。写作 cur in d。", "币种", "字典"), evalDictMember)
}

// registerRoundingScope adds round(expr, @mode): the steps inside expr that
// land between minor units are exact, and round rounds what they make once,
// by mode. The compiler holds those steps to a round and the exact values
// to its inside; at run time round is this call.
func registerRoundingScope(registry *Registry) {
	mustRegister(registry, FunctionSpec{
		Name: "round", Params: []Type{MoneyType, RoundingEnumType()}, Result: MoneyType, roundingScope: true,
		Eval: func(_ context.Context, args []Value) (Value, error) {
			mode, err := money.ParseRounding(args[1].s)
			if err != nil || !IsExact(args[0]) {
				return args[0], err
			}
			return moneyResult(exactOf(args[0]).Round(mode))
		},
		Doc: Doc{
			Cost: 1, Label: "舍入", Category: "金额",
			Description: "里面乘除比例、换汇、按比例取金额这些落在两个最小单位之间的步骤都精确计算，round 在最后按写出的方式只舍入一次；里面没有这样的步骤是错误。",
			Params:      []string{"金额", "舍入方式"}, Result: "舍入后的金额",
		},
	})
}
