package machine

import (
	"context"
	"fmt"
)

// The money operators. Their signatures are the dimension rules: money in a
// unit and a unitless rate combine only into one of those two, and two
// amounts in different currencies divide into an exchange rate, which a rule
// hands to using; money × money is simply not registered, and changing
// currency is convert, against the run's rate table. Every evaluator checks the
// currencies it is handed as well — inference proved them where it could,
// and this is the one-nanosecond second opinion — and every product that
// falls between two minor units is rounded once, by the registry's default or
// by the mode a round(…) around it names.

// moneyKernel is the declared table the evaluators consult: a currency's
// decimal places and the default rounding.
type moneyKernel struct{ table *currencyTable }

// registerMoneyKernel adds the money operators and functions DeclareMoney
// turns on.
func registerMoneyKernel(registry *Registry, table *currencyTable) error {
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

var (
	moneyU = MoneyOf("u")
	moneyA = MoneyOf("a")
	moneyB = MoneyOf("b")
	// fxAB is an exchange rate from a to b, the pair its operations keep.
	fxAB    = FxRateOf("a", "b")
	opLabel = map[string]string{"add": "加法", "sub": "减法", "mul": "乘法", "div": "除法"}
)

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

func (k moneyKernel) registerAdditive(registry *Registry) {
	for _, name := range []string{"add", "sub"} {
		sign := int64(1)
		if name == "sub" {
			sign = -1
		}
		registerMoneyOp(registry, name, []Type{moneyU, moneyU}, moneyU,
			moneyDoc(name, "同币种金额相加减；币种不同是错误，不会悄悄换算。", "左值", "右值"), addMoney(sign))
		registerMoneyOp(registry, name, []Type{RateType, RateType}, RateType,
			moneyDoc(name, "两个比例相加减。", "左值", "右值"), addRates(sign))
	}
}

func addMoney(sign int64) EvalFunc {
	return func(_ context.Context, args []Value) (Value, error) {
		left, right := moneyOf(args[0]), moneyOf(args[1])
		if sign < 0 {
			return moneyResult(left.Sub(right))
		}
		return moneyResult(left.Add(right))
	}
}

func addRates(sign int64) EvalFunc {
	return func(_ context.Context, args []Value) (Value, error) {
		left, right := newRate(args[0].i), newRate(args[1].i)
		sum, err := left.Add(right)
		if sign < 0 {
			sum, err = left.Sub(right)
		}
		return RateValue(sum), err
	}
}

// meet is the currency two amounts combine in: theirs when they agree, the
// other's when one is the currency-less zero.
func meet(a, b string) (string, error) {
	switch {
	case a == b || b == "":
		return a, nil
	case a == "":
		return b, nil
	default:
		return "", fmt.Errorf("%w: %s and %s", ErrCurrency, a, b)
	}
}

// addInt64 is a + sign·b, refusing to wrap.
func addInt64(a, b, sign int64) (int64, error) {
	if sign < 0 {
		if (b < 0 && a > maxInt64+b) || (b > 0 && a < minInt64+b) {
			return 0, errFixedOverflow
		}
		return a - b, nil
	}
	if (b > 0 && a > maxInt64-b) || (b < 0 && a < minInt64-b) {
		return 0, errFixedOverflow
	}
	return a + b, nil
}

const (
	maxInt64 = 1<<63 - 1
	minInt64 = -1 << 63
)

// mulInt64 is a·b, refusing to wrap.
func mulInt64(a, b int64) (int64, error) {
	return mulDivRound(a, b, 1, RoundDown)
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
		for _, params := range [][]Type{{moneyU, moneyU}, {RateType, RateType}, {fxAB, fxAB}} {
			doc := moneyDoc(comparison.name, "比较两笔同币种金额、两个比例，或同一货币对的两个汇率（哪个换到的更多）；币种或货币对不同是错误。", "左值", "右值")
			registerMoneyOp(registry, comparison.name, params, BoolType, doc, func(_ context.Context, args []Value) (Value, error) {
				order, err := compareMoneyKinds(args[0], args[1])
				return Bool(accept(order)), err
			})
		}
	}
}

// compareMoneyKinds orders two amounts or two rates.
func compareMoneyKinds(left, right Value) (int, error) {
	switch left.kind {
	case MoneyKind:
		return moneyOf(left).Cmp(moneyOf(right))
	case FxRateKind:
		a, _ := left.FxRate()
		b, _ := right.FxRate()
		return a.Cmp(b)
	}
	return compareOrdered(left.i, right.i), nil
}
