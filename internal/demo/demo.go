// Package demo is the workbench's demo console, and the reference for how a
// host assembles one: it owns its payment functions and registers them beside
// the standard pack, while FunRoute itself keeps only a minimal core. The
// workbench's language server (web/wasm) and the language server's example
// tests run on it. It uses only the public package, as a host does; a host
// copies it rather than importing it.
package demo

import (
	"errors"
	"fmt"
	"strings"

	"github.com/nethinwei/funroute"
	"github.com/nethinwei/funroute/extensions/std"
)

// NewRegistry is the demo console: the minimal kernel, every lazy form, money
// in the ISO 4217 currencies and a few crypto assets, the standard pack and
// the payment functions. Every form iterates a finite input, so any program this
// registry accepts terminates.
func NewRegistry() (*funroute.Registry, error) {
	registry := funroute.CoreRegistry()
	// Money is declared before the pack, which registers its aggregates over
	// money only when there is money to aggregate.
	currencies := append(std.ISO4217(), cryptoCurrencies...)
	for _, step := range []func() error{
		func() error { return registry.EnableForm(funroute.SwitchForm, funroute.ForForm, funroute.ReduceForm) },
		func() error { return registry.DeclareMoney(funroute.MoneySpec{Currencies: currencies}) },
		func() error { return std.Register(registry) },
		func() error { return Register(registry) },
	} {
		if err := step(); err != nil {
			return nil, err
		}
	}
	return registry, nil
}

// Register adds the payment functions. Each is registered from its Go
// function: the signature comes from the Go types, so the compiler checks the
// implementation against the ABI the artifact will freeze.
func Register(registry *funroute.Registry) error {
	if registry == nil {
		return errors.New("registry is required")
	}
	if err := registerAll(registry, routeFunctions()); err != nil {
		return err
	}
	if err := registerMoneyQuote(registry); err != nil {
		return err
	}
	if err := funroute.DefineHandle[*Embedding](registry, "demo.embedding"); err != nil {
		return err
	}
	return registerAll(registry, modelFunctions())
}

func registerAll(registry *funroute.Registry, specs []funroute.FunctionSpec) error {
	for _, spec := range specs {
		if err := registry.Register(spec); err != nil {
			return err
		}
	}
	return nil
}

// batchOf is the batch implementation of a function of one request: it calls
// it once per request, in order, and stops at the first error.
func batchOf[In, Out any](one func(In) (Out, error)) func([]In) ([]Out, error) {
	return func(inputs []In) ([]Out, error) {
		out := make([]Out, len(inputs))
		for i, input := range inputs {
			result, err := one(input)
			if err != nil {
				return nil, err
			}
			out[i] = result
		}
		return out, nil
	}
}

// Embedding stands in for an inference engine's tensor. The expression passes
// it from one model function to the next as handle<demo.embedding> and never
// looks inside; a real host would define its engine's tensor type the same way.
type Embedding struct{ Features []float64 }

// modelFunctions are the deep-learning shape: a model that turns features into
// an engine value, and a model that scores it. Both carry a batch
// implementation, so a Batch calls them once per batch of requests.
func modelFunctions() []funroute.FunctionSpec {
	return []funroute.FunctionSpec{{
		Name: "model.embed_v1",
		Doc: funroute.Doc{
			Label: "特征向量化", Description: "演示模型：把特征数组交给引擎，得到一个不透明的向量句柄。", Category: "模型", Cost: 20, Params: []string{"特征"}, Result: "向量句柄",
		},
		Go:      embed,
		GoBatch: batchOf(embed),
	}, {
		Name: "model.fraud_v1",
		Doc: funroute.Doc{
			Label: "欺诈评分", Description: "演示模型：对向量句柄打分，返回 0 到 1 的欺诈概率（这里取特征均值）。", Category: "模型", Cost: 20, Params: []string{"向量句柄"}, Result: "欺诈概率",
		},
		Go:      fraudScore,
		GoBatch: batchOf(fraudScore),
	}}
}

func embed(features []float64) (*Embedding, error) {
	return &Embedding{Features: features}, nil
}

func fraudScore(embedding *Embedding) (float64, error) {
	if len(embedding.Features) == 0 {
		return 0, nil
	}
	total := 0.0
	for _, feature := range embedding.Features {
		total += feature
	}
	return total / float64(len(embedding.Features)), nil
}

// routeFunctions are the payment routing functions of plain Go types, made
// afresh for each registry as a host would.
func routeFunctions() []funroute.FunctionSpec {
	return []funroute.FunctionSpec{{
		Name: "route.is_healthy_v1",
		Doc: funroute.Doc{
			Label:       "渠道是否健康",
			Description: "把渠道健康快照中的 UP 映射为 true。示例函数只做纯计算，真实健康度应作为参数传入。",
			Category:    "支付路由",
			Cost:        3,
			Params:      []string{"健康状态"},
			Result:      "是否可用",
		},
		Go: func(status string) (bool, error) {
			return strings.EqualFold(status, "UP"), nil
		},
	}, {
		Name: "route.score_v1",
		Doc: funroute.Doc{
			Label:       "渠道评分",
			Description: "演示评分：成功率 × 100 − 成本。生产公式应由业务扩展包自行实现和版本化。",
			Category:    "支付路由",
			Cost:        5,
			Params:      []string{"成功率", "成本"},
			Result:      "评分",
		},
		Go: func(authRate, cost float64) (float64, error) {
			return authRate*100 - cost, nil
		},
	}, {
		Name: "route.fee_quote_v1",
		Doc: funroute.Doc{
			Label:       "获取渠道费率",
			Description: "演示一个可能失败的渠道调用：健康状态不是 UP 时返回扩展错误，可由 fallback 切到备用报价。",
			Category:    "支付路由",
			Cost:        5,
			Params:      []string{"健康状态", "渠道报价"},
			Result:      "有效费率",
		},
		Go: func(status string, fee float64) (float64, error) {
			if !strings.EqualFold(status, "UP") {
				return 0, fmt.Errorf("fee quote provider is %s", status)
			}
			return fee, nil
		},
	}}
}

// cryptoCurrencies are the tokens a payment desk settles in, declared at the
// precision the business keeps rather than the chain's: a stablecoin's six
// places, eight for the rest. ETH's eighteen on chain do not fit an int64 of
// wei past 9.2 ETH, so exact on-chain amounts are the ledger's to convert at
// its edge; a rule decides and prices at eight places.
var cryptoCurrencies = []funroute.CurrencySpec{
	{Code: "USDT", Digits: 6}, {Code: "USDC", Digits: 6}, {Code: "BTC", Digits: 8}, {Code: "ETH", Digits: 8},
}
