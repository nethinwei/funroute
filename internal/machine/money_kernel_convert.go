package machine

import (
	"context"

	"github.com/nethinwei/funroute/internal/money"
)

// convert is amount -> JPY: the one way a rule changes currency, at the
// quotes of the using it is written in. It reads the run, so folding never
// calls it, and it rounds once, so round(…) chooses how.

func (k moneyKernel) registerConvert(registry *Registry) {
	doc := moneyDoc("convert", "按所在 using 的报价换汇：amount -> JPY。只用这一对货币的报价或反向报价的倒数，不经中转货币；"+
		"要经过别的货币就连写，amount -> CNY -> USD，每一跳舍入一次。using 里没有这一对的报价是 ErrNoFxRate，fallback 可以兜底。", "金额", "目标币种")
	doc.Cost = 5
	params := []Type{MoneyType, CurrencyType}
	mustRegister(registry, FunctionSpec{Name: "convert", Params: params, Result: MoneyType, Doc: doc, readsRun: true, exactStep: true,
		Eval: func(ctx context.Context, args []Value) (Value, error) {
			return exactResult(k.convertExactIn(ctx, args))
		}})
	explicit := append(append([]Type(nil), params...), RoundingEnumType())
	doc.Params = append(append([]string(nil), doc.Params...), "舍入方式")
	mustRegister(registry, FunctionSpec{Name: "convert", Params: explicit, Result: MoneyType, Doc: doc, readsRun: true, takesExact: true,
		Eval: func(ctx context.Context, args []Value) (Value, error) {
			mode, err := money.ParseRounding(args[2].s)
			if err != nil {
				return Value{}, err
			}
			if IsExact(args[0]) {
				e, err := k.convertExactIn(ctx, args[:2])
				if err != nil {
					return Value{}, err
				}
				return moneyResult(e.Round(mode))
			}
			return k.convertIn(ctx, args[:2], mode)
		}})
}

// registerRateReading registers fx(base, quote): the exchange rate the
// using converts at, as a value — to mark up, to compare, to hand to an
// inner using. It reads the run, as convert does, so folding never calls it.
func (k moneyKernel) registerRateReading(registry *Registry) {
	doc := moneyDoc("fx", "所在 using 里从 base 到 quote 的汇率，与 -> 换汇用的是同一个汇率：可以加点、比较或交给内层的 using。"+
		"只看这一对货币的报价或反向报价的倒数；没有这一对是 ErrNoFxRate。", "基准币种", "报价币种")
	doc.Cost = 5
	mustRegister(registry, FunctionSpec{Name: "fx", Params: []Type{CurrencyType, CurrencyType}, Result: FxRateType, Doc: doc, readsRun: true,
		Eval: k.ratioIn})
}

// ratioIn reads the using's rate as convertIn converts at it.
func (k moneyKernel) ratioIn(ctx context.Context, args []Value) (Value, error) {
	f, err := scopeFrame(ctx)
	if err != nil {
		return Value{}, err
	}
	fx, err := f.rateBetween(k.table, args[0].s, args[1].s)
	if err != nil {
		return Value{}, err
	}
	return FxRateValue(fx), nil
}

// convertIn converts at the using's rate. Money already in the currency,
// and the currency-less zero, need none.
func (k moneyKernel) convertIn(ctx context.Context, args []Value, mode money.Rounding) (Value, error) {
	m, to := moneyOf(args[0]), args[1].s
	if _, err := k.table.Places(to); err != nil {
		return Value{}, err
	}
	if m.Currency() == to {
		return MoneyValue(m.Minor(), to), nil
	}
	if m.Currency() == "" {
		return MoneyValue(0, to), noCurrencyIsZero(MoneyValue(m.Minor(), ""))
	}
	f, err := scopeFrame(ctx)
	if err != nil {
		return Value{}, err
	}
	fx, err := f.rateBetween(k.table, m.Currency(), to)
	if err != nil {
		return Value{}, err
	}
	return moneyResult(money.ConvertAt(k.table, m, fx, mode))
}

// convertExactIn is convertIn without the rounding: a conversion inside
// round(…), exact until the round.
func (k moneyKernel) convertExactIn(ctx context.Context, args []Value) (money.ExactMoney, error) {
	m, to := exactOf(args[0]), args[1].s
	if _, err := k.table.Places(to); err != nil {
		return money.ExactMoney{}, err
	}
	// Money with no currency is only ever zero, and in its own currency it
	// needs no rate.
	if m.Currency() == to || m.Currency() == "" {
		return money.ExactFrom(to, m.Minor()), nil
	}
	f, err := scopeFrame(ctx)
	if err != nil {
		return money.ExactMoney{}, err
	}
	fx, err := f.rateBetween(k.table, m.Currency(), to)
	if err != nil {
		return money.ExactMoney{}, err
	}
	return money.ConvertExactAt(k.table, m, fx)
}
