package machine

import (
	"context"
	"fmt"
	"slices"

	"github.com/nethinwei/funroute/internal/money"
)

// Products and quotients of the money kinds. The ones that land between two
// minor units come twice. Bare, they are steps of a round(…): exact, left for
// the round around them to round once — and only there, which the compiler
// holds them to. With a trailing rounding mode they round at once, anywhere.
// No rounding happens that the rule does not write.

// exactStep is an operation that lands between minor units, computed
// exactly; rounded is the same operation rounding at once, for operands that
// are whole minor units.
type (
	exactStep func(ctx context.Context, args []Value) (money.ExactMoney, error)
	rounded   func(ctx context.Context, args []Value, mode money.Rounding) (Value, error)
)

// registerRounded registers an operation that lands between two minor units:
// the bare step, and its variant with the mode spelled out. An exact operand
// — a step inside a round — goes the exact way and is rounded after. spec is
// the bare step's name, signature and doc, and whether it reads the run.
func registerRounded(registry *Registry, spec FunctionSpec, exactly exactStep, fast rounded) {
	bare := spec
	bare.exactStep = true
	bare.Eval = func(ctx context.Context, args []Value) (Value, error) { return exactResult(exactly(ctx, args)) }
	mustRegister(registry, bare)
	spec.Params = append(slices.Clone(spec.Params), RoundingEnumType())
	spec.Doc.Params = append(slices.Clone(spec.Doc.Params), "舍入方式")
	spec.takesExact = true
	spec.Eval = func(ctx context.Context, args []Value) (Value, error) {
		mode, err := money.ParseRounding(args[len(args)-1].s)
		if err != nil {
			return Value{}, err
		}
		return roundedAt(ctx, args[:len(args)-1], mode, exactly, fast)
	}
	mustRegister(registry, spec)
}

// roundedAt is an operation rounding once by mode: at once for whole minor
// units, after the exact step for an exact operand.
func roundedAt(ctx context.Context, args []Value, mode money.Rounding, exactly exactStep, fast rounded) (Value, error) {
	if !anyExact(args) {
		return fast(ctx, args, mode)
	}
	e, err := exactly(ctx, args)
	if err != nil {
		return Value{}, err
	}
	return moneyResult(e.Round(mode))
}

func anyExact(args []Value) bool { return slices.ContainsFunc(args, IsExact) }

func (k moneyKernel) registerProducts(registry *Registry) {
	byRatio := moneyDoc("mul", "金额乘比例得到同币种金额；落在两个最小单位之间时按舍入方式取整。", "金额", "比例")
	registerRounded(registry, FunctionSpec{Name: "mul", Params: []Type{MoneyType, RatioType}, Result: MoneyType, Doc: byRatio},
		exactTimesRatio(0, 1), moneyTimesRatio(0, 1))
	registerRounded(registry, FunctionSpec{Name: "mul", Params: []Type{RatioType, MoneyType}, Result: MoneyType, Doc: byRatio},
		exactTimesRatio(1, 0), moneyTimesRatio(1, 0))
	byInt := moneyDoc("mul", "金额乘整数（数量），精确。", "金额", "数量")
	registerExactOp(registry, "mul", []Type{MoneyType, IntType}, MoneyType, byInt, moneyTimesInt(0, 1))
	registerExactOp(registry, "mul", []Type{IntType, MoneyType}, MoneyType, byInt, moneyTimesInt(1, 0))
	registerMoneyOp(registry, "mul", []Type{RatioType, RatioType}, RatioType,
		moneyDoc("mul", "两个比例相乘，比如费上加费。", "左值", "右值"), ratioProduct)
	markup := moneyDoc("mul", "汇率乘比例是加点或折让后的同一货币对汇率，精确：用它换汇只舍入一次，比先换汇再乘比例少一次舍入。", "汇率", "比例")
	registerMoneyOp(registry, "mul", []Type{FxRateType, RatioType}, FxRateType, markup, fxRateTimesRate(0, 1))
	markup.Params = []string{"比例", "汇率"}
	registerMoneyOp(registry, "mul", []Type{RatioType, FxRateType}, FxRateType, markup, fxRateTimesRate(1, 0))
}

// fxRateTimesRate is FxRate.MulRatio: a markup on an exchange rate.
func fxRateTimesRate(fx, rate int) EvalFunc {
	return func(_ context.Context, args []Value) (Value, error) {
		quote, _ := args[fx].FxRate()
		marked, err := quote.MulRatio(ratioFrom(args[rate]))
		return FxRateValue(marked), err
	}
}

// The evaluators below are the Go methods on Money, Ratio and Currencies (money_ops.go, currencies.go), so a rule and a host computing
// beside it get one answer.

func moneyOf(value Value) money.Money { amount, _ := value.Money(); return amount }

func moneyResult(amount money.Money, err error) (Value, error) {
	return MoneyValue(amount.Minor(), amount.Currency()), err
}

func moneyTimesRatio(amount, ratio int) rounded {
	return func(_ context.Context, args []Value, mode money.Rounding) (Value, error) {
		return moneyResult(moneyOf(args[amount]).MulRatio(ratioFrom(args[ratio]), mode))
	}
}

func exactTimesRatio(amount, ratio int) exactStep {
	return func(_ context.Context, args []Value) (money.ExactMoney, error) {
		return exactOf(args[amount]).MulRatio(ratioFrom(args[ratio]))
	}
}

// moneyTimesInt is a count of an amount, exact either way.
func moneyTimesInt(amount, count int) EvalFunc {
	return func(_ context.Context, args []Value) (Value, error) {
		if IsExact(args[amount]) {
			return exactResult(exactOf(args[amount]).MulInt(args[count].i))
		}
		return moneyResult(moneyOf(args[amount]).MulInt(args[count].i))
	}
}

func ratioProduct(_ context.Context, args []Value) (Value, error) {
	product, err := ratioFrom(args[0]).Mul(ratioFrom(args[1]))
	return RatioValue(product), err
}

func (k moneyKernel) registerQuotients(registry *Registry) {
	gross := moneyDoc("div", "金额除以比例，比如由净额反推含费总额 net / (100% - fee)。", "金额", "比例")
	registerRounded(registry, FunctionSpec{Name: "div", Params: []Type{MoneyType, RatioType}, Result: MoneyType, Doc: gross},
		exactOverRatio, moneyOverRatio)
	registerMoneyOp(registry, "div", []Type{MoneyType, MoneyType}, RatioType,
		moneyDoc("div", "同币种金额相除得到比例，比如实际费率 fee / amount。", "金额", "金额"), moneyRatio)
	registerMoneyOp(registry, "implied", []Type{MoneyType, MoneyType}, FxRateType,
		moneyDoc("implied", "两笔不同币种金额隐含的汇率：implied(到账额, 支付额) 是这笔的成交汇率，交给 using 换汇。精确，不舍入；同币种没有汇率，比值用 a / b。", "到账额", "支付额"), k.impliedRate)
	registerMoneyOp(registry, "div", []Type{RatioType, RatioType}, RatioType,
		moneyDoc("div", "两个比例相除。", "左值", "右值"), ratioQuotient)
}

func ratioQuotient(_ context.Context, args []Value) (Value, error) {
	quotient, err := ratioFrom(args[0]).Div(ratioFrom(args[1]))
	return RatioValue(quotient), err
}

func moneyOverRatio(_ context.Context, args []Value, mode money.Rounding) (Value, error) {
	return moneyResult(moneyOf(args[0]).DivRatio(ratioFrom(args[1]), mode))
}

func exactOverRatio(_ context.Context, args []Value) (money.ExactMoney, error) {
	return exactOf(args[0]).DivRatio(ratioFrom(args[1]))
}

// impliedRate is over / under as an exchange rate: how much of over's
// currency one unit of under's bought.
func (k moneyKernel) impliedRate(_ context.Context, args []Value) (Value, error) {
	over, under := moneyOf(args[0]), moneyOf(args[1])
	if over.Currency() == "" || under.Currency() == "" {
		return Value{}, fmt.Errorf("%w: a currency-less zero implies no exchange rate", ErrCurrency)
	}
	fx, err := k.table.Implied(over, under)
	if err != nil {
		return Value{}, err
	}
	return FxRateValue(fx), nil
}

func moneyRatio(_ context.Context, args []Value) (Value, error) {
	ratio, err := moneyOf(args[0]).Ratio(moneyOf(args[1]))
	return RatioValue(ratio), err
}
