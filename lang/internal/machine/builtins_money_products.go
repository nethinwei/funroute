package machine

import (
	"context"
	"fmt"
)

// Products and quotients of the money kinds. The ones that land between two
// minor units come twice: once rounding by the registry's default, and once
// with a trailing rounding argument, which is what round(expr, @mode)
// selects for every step inside it.

// rounded is an evaluator that takes the rounding mode as its last argument
// when it has one more argument than the operation.
type rounded func(args []Value, mode Rounding) (Value, error)

// registerRounded registers an operation that rounds, and its variant with
// the mode spelled out.
func (k moneyKernel) registerRounded(registry *Registry, name string, params []Type, result Type, doc Doc, eval rounded) {
	registerMoneyOp(registry, name, params, result, doc, func(_ context.Context, args []Value) (Value, error) {
		return eval(args, k.table.spec.Rounding)
	})
	explicit := append(append([]Type(nil), params...), RoundingEnumType())
	doc.Params = append(append([]string(nil), doc.Params...), "舍入方式")
	registerMoneyOp(registry, name, explicit, result, doc, func(_ context.Context, args []Value) (Value, error) {
		mode, err := ParseRounding(args[len(args)-1].s)
		if err != nil {
			return Value{}, err
		}
		return eval(args[:len(args)-1], mode)
	})
}

func (k moneyKernel) registerProducts(registry *Registry) {
	byRate := moneyDoc("mul", "金额乘比例得到同币种金额；落在两个最小单位之间时按舍入方式取整。", "金额", "比例")
	k.registerRounded(registry, "mul", []Type{moneyU, RateType}, moneyU, byRate, moneyTimesRate(0, 1))
	k.registerRounded(registry, "mul", []Type{RateType, moneyU}, moneyU, byRate, moneyTimesRate(1, 0))
	byInt := moneyDoc("mul", "金额乘整数（数量），精确。", "金额", "数量")
	registerMoneyOp(registry, "mul", []Type{moneyU, IntType}, moneyU, byInt, moneyTimesInt(0, 1))
	registerMoneyOp(registry, "mul", []Type{IntType, moneyU}, moneyU, byInt, moneyTimesInt(1, 0))
	registerMoneyOp(registry, "mul", []Type{RateType, RateType}, RateType,
		moneyDoc("mul", "两个比例相乘，比如费上加费。", "左值", "右值"), rateProduct)
	markup := moneyDoc("mul", "汇率乘比例是加点或折让后的同一货币对汇率，精确：用它换汇只舍入一次，比先换汇再乘比例少一次舍入。", "汇率", "比例")
	registerMoneyOp(registry, "mul", []Type{fxAB, RateType}, fxAB, markup, fxRateTimesRate(0, 1))
	markup.Params = []string{"比例", "汇率"}
	registerMoneyOp(registry, "mul", []Type{RateType, fxAB}, fxAB, markup, fxRateTimesRate(1, 0))
}

// fxRateTimesRate is FxRate.MulRate: a markup on an exchange rate.
func fxRateTimesRate(fx, rate int) EvalFunc {
	return func(_ context.Context, args []Value) (Value, error) {
		quote, _ := args[fx].FxRate()
		marked, err := quote.MulRate(newRate(args[rate].i))
		return FxRateValue(marked), err
	}
}

// The evaluators below are the Go methods on Money, Rate and Currencies (money_ops.go, currencies.go), so a rule and a host computing
// beside it get one answer.

func moneyOf(value Value) Money { money, _ := value.Money(); return money }

func moneyResult(money Money, err error) (Value, error) {
	return MoneyValue(money.minor, money.currency), err
}

func moneyTimesRate(money, rate int) rounded {
	return func(args []Value, mode Rounding) (Value, error) {
		return moneyResult(moneyOf(args[money]).MulRate(newRate(args[rate].i), mode))
	}
}

func moneyTimesInt(money, count int) EvalFunc {
	return func(_ context.Context, args []Value) (Value, error) {
		return moneyResult(moneyOf(args[money]).MulInt(args[count].i))
	}
}

func rateProduct(_ context.Context, args []Value) (Value, error) {
	product, err := newRate(args[0].i).Mul(newRate(args[1].i))
	return RateValue(product), err
}

func (k moneyKernel) registerQuotients(registry *Registry) {
	gross := moneyDoc("div", "金额除以比例，比如由净额反推含费总额 net / (100% - fee)。", "金额", "比例")
	k.registerRounded(registry, "div", []Type{moneyU, RateType}, moneyU, gross, moneyOverRate)
	registerMoneyOp(registry, "div", []Type{moneyU, moneyU}, RateType,
		moneyDoc("div", "同币种金额相除得到比例，比如实际费率 fee / amount。", "金额", "金额"), moneyRatio)
	registerMoneyOp(registry, "div", []Type{moneyB, moneyA}, FxRateOf("a", "b"),
		moneyDoc("div", "不同币种的金额相除得到汇率：到账额 ÷ 支付额是这笔的成交汇率，交给 using 换汇。精确，不舍入。", "金额", "金额"), k.impliedRate)
	registerMoneyOp(registry, "div", []Type{RateType, RateType}, RateType,
		moneyDoc("div", "两个比例相除。", "左值", "右值"), rateQuotient)
}

func moneyOverRate(args []Value, mode Rounding) (Value, error) {
	return moneyResult(moneyOf(args[0]).DivRate(newRate(args[1].i), mode))
}

// impliedRate is over / under as an exchange rate: how much of over's
// currency one unit of under's bought.
func (k moneyKernel) impliedRate(_ context.Context, args []Value) (Value, error) {
	over, under := moneyOf(args[0]), moneyOf(args[1])
	if over.currency == "" || under.currency == "" {
		return Value{}, fmt.Errorf("%w: a currency-less zero implies no exchange rate", ErrCurrency)
	}
	fx, err := (&Currencies{table: k.table}).Implied(over, under)
	if err != nil {
		return Value{}, err
	}
	return FxRateValue(fx), nil
}

func moneyRatio(_ context.Context, args []Value) (Value, error) {
	ratio, err := moneyOf(args[0]).Ratio(moneyOf(args[1]))
	return RateValue(ratio), err
}

func rateQuotient(_ context.Context, args []Value) (Value, error) {
	quotient, err := newRate(args[0].i).Div(newRate(args[1].i))
	return RateValue(quotient), err
}
