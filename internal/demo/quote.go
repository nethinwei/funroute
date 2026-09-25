package demo

import (
	"context"
	"fmt"

	"github.com/nethinwei/funroute"
)

// channelPricing is a channel's price list: a percentage and a fixed fee in
// minor units of whatever currency the payment is in. A real host reads it
// from its pricing service; the point here is the Go side of money.
var channelPricing = map[string]struct {
	percent string
	fixed   int64
}{
	"adyen":    {percent: "2.6", fixed: 25},
	"stripe":   {percent: "2.9", fixed: 30},
	"checkout": {percent: "2.4", fixed: 40},
}

// registerMoneyQuote adds route.money_quote_v1(channel, amount): the fee a
// channel charges on an amount, in the amount's currency. It is written with
// funroute.Money's own arithmetic, so it computes exactly what a rule would, and
// it names its rounding as a rule must: channels round their fees half up.
func registerMoneyQuote(registry *funroute.Registry) error {
	currencies, declared := registry.Currencies()
	if !declared {
		return nil
	}
	amount := funroute.MoneyType
	return registry.Register(funroute.FunctionSpec{
		Name: "route.money_quote_v1", Params: []funroute.Type{funroute.StringType, amount}, Result: amount,
		Eval: func(_ context.Context, args []funroute.Value) (funroute.Value, error) {
			channel, _ := args[0].String()
			money, _ := args[1].Money()
			fee, err := quoteFee(currencies, channel, money)
			if err != nil {
				return funroute.Value{}, err
			}
			return funroute.ToValue(fee)
		},
		Doc: funroute.Doc{
			Label:       "渠道报价（金额）",
			Description: "某渠道对这笔金额收多少手续费：百分比加固定费，结果与金额同币种。宿主在 Go 里用 funroute.Money 的方法算，舍入与规则里完全一致。",
			Category:    "支付路由",
			Params:      []string{"渠道", "金额"},
			Result:      "手续费",
		},
	})
}

func quoteFee(currencies *funroute.Currencies, channel string, amount funroute.Money) (funroute.Money, error) {
	pricing, ok := channelPricing[channel]
	if !ok {
		return funroute.Money{}, fmt.Errorf("no price list for channel %q", channel)
	}
	ratio, err := funroute.Percent(pricing.percent)
	if err != nil {
		return funroute.Money{}, err
	}
	fee, err := amount.MulRatio(ratio, funroute.RoundHalfUp)
	if err != nil {
		return funroute.Money{}, err
	}
	fixed, err := currencies.Minor(amount.Currency(), pricing.fixed)
	if err != nil {
		return funroute.Money{}, err
	}
	return fee.Add(fixed)
}
