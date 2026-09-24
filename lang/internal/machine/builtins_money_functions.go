package machine

import (
	"context"
	"fmt"
	"strings"
)

// The money functions: building money from data, taking it apart, splitting
// it without losing a cent, and the conversions between rates and the other
// numbers. Money and plain numbers only meet here and in the operators.

func (k moneyKernel) registerFunctions(registry *Registry) {
	k.registerBuilders(registry)
	registerParts(registry)
	registerAllocate(registry)
	k.registerProportions(registry)
	registerRateConversions(registry)
	registerCurrencyKeys(registry)
}

func (k moneyKernel) registerBuilders(registry *Registry) {
	currencyU := CurrencyOf("u")
	registerMoneyOp(registry, "money", []Type{IntType, currencyU}, moneyU,
		moneyDoc("money", "由最小单位的整数与币种造出金额：money(170, USD) 是 USD 1.70。输入里的金额与币种分开时用它。", "最小单位", "币种"),
		func(_ context.Context, args []Value) (Value, error) { return MoneyValue(args[0].i, args[1].s), nil })
	registerMoneyOp(registry, "currency_of", []Type{StringType}, CurrencyOf(""),
		moneyDoc("currency_of", "把币种代码字符串读成币种；不是注册表声明的币种就是错误。", "代码"),
		func(_ context.Context, args []Value) (Value, error) {
			if _, err := k.table.places(args[0].s); err != nil {
				return Value{}, err
			}
			return CurrencyValue(args[0].s), nil
		})
	registerMoneyOp(registry, "like", []Type{moneyU, IntType}, moneyU,
		moneyDoc("like", "在一笔金额的币种里造出 n 个最小单位，配合按币种查表的阈值用。", "同币种金额", "最小单位"),
		func(_ context.Context, args []Value) (Value, error) {
			return moneyResult(moneyOf(args[0]).Like(args[1].i))
		})
}

func registerParts(registry *Registry) {
	registerMoneyOp(registry, "minor", []Type{moneyU}, IntType,
		moneyDoc("minor", "金额的最小单位整数：USD 1.70 是 170。", "金额"),
		func(_ context.Context, args []Value) (Value, error) { return Int(args[0].i), nil })
	registerMoneyOp(registry, "currency", []Type{moneyU}, CurrencyOf("u"),
		moneyDoc("currency", "金额的币种，可以进 switch、作字典的键。", "金额"),
		func(_ context.Context, args []Value) (Value, error) {
			if args[0].s == "" {
				return Value{}, fmt.Errorf("%w: a currency-less zero has no currency", ErrCurrency)
			}
			return CurrencyValue(args[0].s), nil
		})
	registerMoneyOp(registry, "sign", []Type{moneyU}, IntType,
		moneyDoc("sign", "金额的符号：负数 -1，零 0，正数 1。", "金额"),
		func(_ context.Context, args []Value) (Value, error) {
			return Int(int64(compareOrdered(args[0].i, 0))), nil
		})
	registerMoneyOp(registry, "string", []Type{CurrencyOf("u")}, StringType,
		moneyDoc("string", "币种的代码。", "币种"),
		func(_ context.Context, args []Value) (Value, error) { return String(args[0].s), nil })
}

func registerAllocate(registry *Registry) {
	byWeight := moneyDoc("allocate", "按整数权重分一笔金额，一分不丢：各份向零取整，剩下的最小单位按最大余数法发出（被舍掉最多的份先得）。", "金额", "权重")
	registerMoneyOp(registry, "allocate", []Type{moneyU, ArrayOf(IntType)}, ArrayOf(moneyU), byWeight, allocateByWeight)
	byWeight = moneyDoc("allocate", "按整数权重分一笔金额，一分不丢；剩下的最小单位按策略发出："+strategyDoc, "金额", "权重", "分配策略")
	registerMoneyOp(registry, "allocate", []Type{moneyU, ArrayOf(IntType), AllocationEnumType()}, ArrayOf(moneyU), byWeight, allocateByWeight)
	doc := moneyDoc("allocate", "把一笔金额平均分成 n 份，一分不丢：余数从第一份开始给。n 的规模必须由输入界定。", "金额", "份数")
	doc.BoundedArgs = true
	registerMoneyOp(registry, "allocate", []Type{moneyU, IntType}, ArrayOf(moneyU), doc, allocateEvenly)
	doc = moneyDoc("allocate", "把一笔金额平均分成 n 份，一分不丢；余数按策略发出："+strategyDoc+"n 的规模必须由输入界定。", "金额", "份数", "分配策略")
	doc.BoundedArgs = true
	registerMoneyOp(registry, "allocate", []Type{moneyU, IntType, AllocationEnumType()}, ArrayOf(moneyU), doc, allocateEvenly)
}

const strategyDoc = "@largest_remainder 被舍掉最多的份先得（缺省），@largest_weight 权重大的先得，@in_order 从第一份起，" +
	"@reverse_order 从最后一份起，@all_first 全给第一份，@all_last 全给最后一份。"

// allocationStrategy is the strategy a call names in its third argument, or
// the largest remainder when it names none.
func allocationStrategy(args []Value) (AllocationStrategy, error) {
	if len(args) < 3 {
		return AllocateLargestRemainder, nil
	}
	return ParseAllocation(args[2].s)
}

func allocateByWeight(_ context.Context, args []Value) (Value, error) {
	strategy, err := allocationStrategy(args)
	if err != nil {
		return Value{}, err
	}
	weights, _ := args[1].box.([]int64)
	money, _ := args[0].Money()
	shares, err := money.AllocateBy(strategy, weights...)
	return Value{kind: ArrayKind, box: shares}, err
}

func allocateEvenly(_ context.Context, args []Value) (Value, error) {
	strategy, err := allocationStrategy(args)
	if err != nil {
		return Value{}, err
	}
	money, _ := args[0].Money()
	shares, err := money.SplitBy(strategy, int(args[1].i))
	return Value{kind: ArrayKind, box: shares}, err
}

// registerProportions registers prorate and round_to, which land between two
// minor units and so come with a variant taking the rounding mode.
func (k moneyKernel) registerProportions(registry *Registry) {
	byInts := moneyDoc("prorate", "按比例取金额：prorate(m, part, whole) 是 m × part / whole，中间精确、只舍入一次。", "金额", "分子", "分母")
	k.registerRounded(registry, "prorate", []Type{moneyU, IntType, IntType}, moneyU, byInts, func(args []Value, mode Rounding) (Value, error) {
		return moneyResult(moneyOf(args[0]).Prorate(args[1].i, args[2].i, mode))
	})
	byAmounts := moneyDoc("prorate", "按两笔同币种金额之比取金额：prorate(fee, refund, paid) 按退款占比退手续费，中间精确、只舍入一次。", "金额", "部分", "整体")
	moneyV := MoneyOf("v")
	k.registerRounded(registry, "prorate", []Type{moneyU, moneyV, moneyV}, moneyU, byAmounts, func(args []Value, mode Rounding) (Value, error) {
		return moneyResult(moneyOf(args[0]).ProrateBy(moneyOf(args[1]), moneyOf(args[2]), mode))
	})
	step := moneyDoc("round_to", "把金额取到粒度的整数倍：round_to(m, CHF 0.05) 是现金舍入，粒度是同币种的正金额。", "金额", "粒度")
	k.registerRounded(registry, "round_to", []Type{moneyU, moneyU}, moneyU, step, func(args []Value, mode Rounding) (Value, error) {
		return moneyResult(moneyOf(args[0]).RoundTo(moneyOf(args[1]), mode))
	})
}

func registerRateConversions(registry *Registry) {
	registerMoneyOp(registry, "rate", []Type{StringType}, RateType,
		moneyDoc("rate", "把十进制字符串精确地读成比例，超过 10 位小数是错误。", "文本"),
		func(_ context.Context, args []Value) (Value, error) {
			rate, err := ParseRate(strings.TrimSpace(args[0].s))
			return RateValue(rate), err
		})
}

// registerCurrencyKeys lets a currency index a dictionary: rates[currency(m)].
func registerCurrencyKeys(registry *Registry) {
	t := TypeVar("T")
	registerMoneyOp(registry, "at", []Type{DictOf(t), CurrencyOf("u")}, t,
		moneyDoc("at", "按币种取字典的值；键是币种代码，不存在是错误。写作 d[cur]。", "字典", "币种"), evalDictAt)
	registerMoneyOp(registry, "member", []Type{CurrencyOf("u"), DictOf(t)}, BoolType,
		moneyDoc("member", "字典里有没有这个币种的键。写作 cur in d。", "币种", "字典"), evalDictMember)
}

// registerRoundingScope adds round(expr, @mode). It is not called at run
// time: the compiler compiles expr with every rounding step inside it taking
// mode, and emits no call for round itself.
func registerRoundingScope(registry *Registry) {
	t := TypeVar("T")
	mustRegister(registry, FunctionSpec{
		Name: "round", Params: []Type{t, RoundingEnumType()}, Result: t, roundingScope: true,
		Eval: func(_ context.Context, args []Value) (Value, error) { return args[0], nil },
		Doc: Doc{
			Cost: 1, Label: "指定舍入", Category: "金额",
			Description: "表达式里每一步落到最小单位的舍入都按这里写的方式，而不是注册表的缺省；里面没有舍入步骤是错误。",
			Params:      []string{"表达式", "舍入方式"}, Result: "结果",
		},
	})
}
