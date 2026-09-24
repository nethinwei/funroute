package machine

import (
	"context"
	"errors"
	"fmt"
	"slices"
)

// convert is amount -> JPY: the one way a rule changes currency, through the
// rate table the host gave the run. It reads the run, so folding never calls
// it, and it rounds once, so round(…) chooses how.

// ratesKey is the context key a run carries its rate table under.
type ratesKey struct{}

// runRates is the version of a rate table one run converts through, and of
// each named table the contract declares, by name in the contract's order.
type runRates struct {
	table  *currencyTable
	graph  *rateGraph
	names  []string
	tables []*rateGraph
}

// named is the version of the named table this run was given, or an empty
// table for one the host left out: a missing rate is ErrNoRate, as it is
// where the host gave no table at all.
func (r runRates) named(name string) *rateGraph {
	if i := slices.Index(r.names, name); i >= 0 && r.tables[i] != nil {
		return r.tables[i]
	}
	return newRateGraph()
}

func (k moneyKernel) registerConvert(registry *Registry) {
	doc := moneyDoc("convert", "按本次运行的汇率表换汇：amount -> JPY。只用这一对货币的报价或反向报价的倒数，不经中转货币；"+
		"要经过别的货币就连写，amount -> CNY -> USD，每一跳舍入一次。没有汇率表或表里没有这一对是 ErrNoRate，fallback 可以兜底。", "金额", "目标币种")
	doc.Cost = 5
	params := []Type{moneyA, CurrencyOf("b")}
	mustRegister(registry, FunctionSpec{Name: "convert", Params: params, Result: moneyB, Doc: doc, readsRun: true,
		Eval: func(ctx context.Context, args []Value) (Value, error) {
			return k.convertIn(ctx, args, k.table.spec.Rounding)
		}})
	explicit := append(append([]Type(nil), params...), RoundingEnumType())
	doc.Params = append(append([]string(nil), doc.Params...), "舍入方式")
	mustRegister(registry, FunctionSpec{Name: "convert", Params: explicit, Result: moneyB, Doc: doc, readsRun: true,
		Eval: func(ctx context.Context, args []Value) (Value, error) {
			mode, err := ParseRounding(args[2].s)
			if err != nil {
				return Value{}, err
			}
			return k.convertIn(ctx, args[:2], mode)
		}})
}

// registerRateReading registers fx(base, quote): the exchange rate the run's
// rate table converts at, as a value — to mark up, to compare, to hand to a
// using. It reads the run, as convert does, so folding never calls it.
func (k moneyKernel) registerRateReading(registry *Registry) {
	doc := moneyDoc("fx", "本次运行的汇率表里从 base 到 quote 的汇率，与 -> 换汇用的是同一个汇率：可以加点、比较或交给 using。"+
		"只看这一对货币的报价或反向报价的倒数；没有汇率表或表里没有这一对是 ErrNoRate。", "基准币种", "报价币种")
	doc.Cost = 5
	mustRegister(registry, FunctionSpec{Name: "fx", Params: []Type{CurrencyOf("a"), CurrencyOf("b")}, Result: fxAB, Doc: doc, readsRun: true,
		Eval: k.rateIn})
}

// rateIn reads the run's rate table as convertIn converts through it.
func (k moneyKernel) rateIn(ctx context.Context, args []Value) (Value, error) {
	rates, ok := ctx.Value(ratesKey{}).(runRates)
	if !ok || rates.graph == nil {
		fx, err := newRateGraph().fxRate(args[0].s, args[1].s)
		if errors.Is(err, ErrNoRate) {
			return Value{}, fmt.Errorf("%w: the run was given no rate table", ErrNoRate)
		}
		return FxRateValue(fx), err
	}
	fx, err := rates.graph.fxRate(args[0].s, args[1].s)
	return FxRateValue(fx), err
}

// convertIn converts through the run's rate table. Money already in the
// currency needs none, so a run without a table still converts it.
func (k moneyKernel) convertIn(ctx context.Context, args []Value, mode Rounding) (Value, error) {
	rates, ok := ctx.Value(ratesKey{}).(runRates)
	if !ok || rates.graph == nil {
		converted, err := newRateGraph().convert(k.table, moneyOf(args[0]), args[1].s, mode)
		if errors.Is(err, ErrNoRate) {
			return Value{}, fmt.Errorf("%w: the run was given no rate table", ErrNoRate)
		}
		return moneyResult(converted, err)
	}
	return moneyResult(rates.graph.convert(rates.table, moneyOf(args[0]), args[1].s, mode))
}
