package payment

import (
	"context"
	"fmt"

	"funroute/lang"
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
// lang.Money's own arithmetic, so it rounds exactly as a rule would, and its
// signature says money<u> twice so the currency comes back unchanged.
func registerMoneyQuote(registry *lang.Registry) error {
	currencies, declared := registry.Currencies()
	if !declared {
		return nil
	}
	amount := lang.MoneyOf("u")
	return registry.Register(lang.FunctionSpec{
		Name: "route.money_quote_v1", Params: []lang.Type{lang.StringType, amount}, Result: amount,
		Eval: func(_ context.Context, args []lang.Value) (lang.Value, error) {
			channel, _ := args[0].String()
			money, _ := args[1].Money()
			fee, err := quoteFee(channel, money, currencies.Rounding())
			if err != nil {
				return lang.Value{}, err
			}
			return lang.ToValue(fee)
		},
		Doc: lang.Doc{
			Label:       "渠道报价（金额）",
			Description: "某渠道对这笔金额收多少手续费：百分比加固定费，结果与金额同币种。宿主在 Go 里用 lang.Money 的方法算，舍入与规则里完全一致。",
			Category:    "支付路由",
			Cost:        5,
			Params:      []string{"渠道", "金额"},
			Result:      "手续费",
		},
	})
}

func quoteFee(channel string, amount lang.Money, rounding lang.Rounding) (lang.Money, error) {
	pricing, ok := channelPricing[channel]
	if !ok {
		return lang.Money{}, fmt.Errorf("no price list for channel %q", channel)
	}
	rate, err := lang.Percent(pricing.percent)
	if err != nil {
		return lang.Money{}, err
	}
	fee, err := amount.MulRate(rate, rounding)
	if err != nil {
		return lang.Money{}, err
	}
	fixed, err := amount.Like(pricing.fixed)
	if err != nil {
		return lang.Money{}, err
	}
	return fee.Add(fixed)
}
