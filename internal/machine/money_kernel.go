package machine

import (
	"context"

	"github.com/nethinwei/funroute/internal/money"
)

// The money operators. Their signatures are the dimension rules: money and a
// ratio combine only into money or a ratio, two amounts in one currency
// divide into a ratio, and the exchange rate two amounts in different
// currencies imply is implied(over, under); money × money is simply not
// registered, and changing currency is convert, at the quotes of its using.
// The currencies an operation is handed meet where it computes, and a step
// that falls between two minor units is exact inside a round(…) and rounded
// once by the mode it names, or rounded where it stands by a mode written
// last.

// moneyKernel is the declared table the evaluators consult: a currency's
// decimal places.
type moneyKernel struct{ table *money.Currencies }

// registerMoneyKernel adds the money operators and functions DeclareMoney
// turns on.
func registerMoneyKernel(registry *Registry, table *money.Currencies) error {
	k := moneyKernel{table: table}
	k.registerAdditive(registry)
	k.registerProducts(registry)
	k.registerQuotients(registry)
	k.registerOrdering(registry)
	k.registerFunctions(registry)
	k.registerConvert(registry)
	k.registerRateReading(registry)
	registerRoundingScope(registry)
	return nil
}

var opLabel = map[string]string{"add": "加法", "sub": "减法", "mul": "乘法", "div": "除法"}

func moneyDoc(name, description string, params ...string) Doc {
	label := opLabel[name]
	if label == "" {
		label = name
	}
	return Doc{Cost: 2, Label: label, Description: description, Category: "金额", Params: params, Result: "结果"}
}

func registerMoneyOp(registry *Registry, name string, params []Type, result Type, doc Doc, eval EvalFunc) {
	mustRegister(registry, FunctionSpec{Name: name, Params: params, Result: result, Eval: eval, Doc: doc})
}

// registerExactOp is registerMoneyOp for an operation exact money may be
// handed to, which computes with it as it does with whole minor units.
func registerExactOp(registry *Registry, name string, params []Type, result Type, doc Doc, eval EvalFunc) {
	mustRegister(registry, FunctionSpec{Name: name, Params: params, Result: result, Eval: eval, Doc: doc, takesExact: true})
}

func (k moneyKernel) registerAdditive(registry *Registry) {
	for _, name := range []string{"add", "sub"} {
		sign := int64(1)
		if name == "sub" {
			sign = -1
		}
		registerExactOp(registry, name, []Type{MoneyType, MoneyType}, MoneyType,
			moneyDoc(name, "同币种金额相加减；币种不同是错误，不会悄悄换算。", "左值", "右值"), addMoney(sign))
		registerMoneyOp(registry, name, []Type{RatioType, RatioType}, RatioType,
			moneyDoc(name, "两个比例相加减。", "左值", "右值"), addRatios(sign))
	}
}

// addMoney adds or subtracts two amounts, exactly when either is exact.
func addMoney(sign int64) EvalFunc {
	return func(_ context.Context, args []Value) (Value, error) {
		if IsExact(args[0]) || IsExact(args[1]) {
			left, right := exactOf(args[0]), exactOf(args[1])
			if sign < 0 {
				return exactResult(left.Sub(right))
			}
			return exactResult(left.Add(right))
		}
		left, right := moneyOf(args[0]), moneyOf(args[1])
		if sign < 0 {
			return moneyResult(left.Sub(right))
		}
		return moneyResult(left.Add(right))
	}
}

func addRatios(sign int64) EvalFunc {
	return func(_ context.Context, args []Value) (Value, error) {
		left, right := ratioFrom(args[0]), ratioFrom(args[1])
		sum, err := left.Add(right)
		if sign < 0 {
			sum, err = left.Sub(right)
		}
		return RatioValue(sum), err
	}
}

func (k moneyKernel) registerOrdering(registry *Registry) {
	for _, comparison := range []struct {
		name   string
		accept func(int) bool
	}{
		{"lt", func(order int) bool { return order < 0 }},
		{"le", func(order int) bool { return order <= 0 }},
		{"gt", func(order int) bool { return order > 0 }},
		{"ge", func(order int) bool { return order >= 0 }},
	} {
		accept := comparison.accept
		for _, params := range [][]Type{{MoneyType, MoneyType}, {RatioType, RatioType}, {FxRateType, FxRateType}} {
			doc := moneyDoc(comparison.name, "比较两笔同币种金额、两个比例，或同一货币对的两个汇率（哪个换到的更多）；币种或货币对不同是错误。", "左值", "右值")
			registerExactOp(registry, comparison.name, params, BoolType, doc, func(_ context.Context, args []Value) (Value, error) {
				order, err := compareMoneyKinds(args[0], args[1])
				return Bool(accept(order)), err
			})
		}
	}
}

// compareMoneyKinds orders two amounts, two ratios or two exchange rates of
// one pair.
func compareMoneyKinds(left, right Value) (int, error) {
	switch left.kind {
	case MoneyKind:
		if IsExact(left) || IsExact(right) {
			return exactOf(left).Cmp(exactOf(right))
		}
		return moneyOf(left).Cmp(moneyOf(right))
	case FxRateKind:
		a, _ := left.FxRate()
		b, _ := right.FxRate()
		return a.Cmp(b)
	}
	return ratioFrom(left).Cmp(ratioFrom(right)), nil
}
