// Package paymentdemo demonstrates how an application owns domain functions
// while FunRoute keeps only a minimal computational core.
package paymentdemo

import (
	"fmt"
	"strings"

	"funroute/lang"
)

// NewRegistry is the demo console: the minimal kernel, every lazy form, and the
// payment extensions. Every form iterates a finite input, so any program this
// registry accepts terminates.
func NewRegistry() (*lang.Registry, error) {
	registry := lang.CoreRegistry()
	if err := registry.EnableForm(lang.SwitchForm, lang.ForForm, lang.ReduceForm); err != nil {
		return nil, err
	}
	if err := Register(registry); err != nil {
		return nil, err
	}
	return registry, nil
}

// Register adds the payment functions. Each one is a single Fn call: the
// signature comes from the Go types, so the compiler checks the implementation
// against the ABI the artifact will freeze.
func Register(registry *lang.Registry) error {
	if registry == nil {
		return fmt.Errorf("registry is required")
	}
	if err := registerHealth(registry); err != nil {
		return err
	}
	return registerScore(registry)
}

func registerHealth(registry *lang.Registry) error {
	return lang.Fn1(registry, "route.is_healthy_v1", lang.Doc{
		Label:       "渠道是否健康",
		Description: "把渠道健康快照中的 UP 映射为 true。示例函数只做纯计算，真实健康度应作为参数传入。",
		Category:    "支付路由",
		Color:       "#059669",
		Icon:        "♥",
		Cost:        3,
		Params:      []string{"健康状态"},
		Result:      "是否可用",
		Keywords:    []string{"health", "channel", "路由"},
		Examples:    []lang.FunctionExample{{Title: "健康判断", Expression: `route.is_healthy_v1(health)`}},
		Order:       10,
	}, func(status string) (bool, error) {
		return strings.EqualFold(status, "UP"), nil
	})
}

func registerScore(registry *lang.Registry) error {
	return lang.Fn2(registry, "route.score_v1", lang.Doc{
		Label:       "渠道评分",
		Description: "演示评分：成功率 × 100 − 成本。生产公式应由业务扩展包自行实现和版本化。",
		Category:    "支付路由",
		Color:       "#059669",
		Icon:        "★",
		Cost:        5,
		Params:      []string{"成功率", "成本"},
		Result:      "评分",
		Keywords:    []string{"score", "cost", "auth rate"},
		Examples:    []lang.FunctionExample{{Title: "渠道打分", Expression: `route.score_v1(auth_rate, cost)`}},
		Order:       20,
	}, func(authRate, cost float64) (float64, error) {
		return authRate*100 - cost, nil
	})
}
