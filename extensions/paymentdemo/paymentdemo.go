// Package paymentdemo demonstrates how an application owns domain functions
// while FunRoute keeps only a minimal computational core.
package paymentdemo

import (
	"fmt"
	"strings"

	"funroute/lang"
)

// NewRegistry is the demo console: the minimal kernel, every lazy form, and the
// payment extensions. A real operator console would leave RecurForm out — that
// single line is the whole difference between a total and a Turing-complete
// language.
func NewRegistry() (*lang.Registry, error) {
	registry := lang.CoreRegistry()
	if err := registry.EnableForm(lang.SwitchForm, lang.ForForm, lang.ReduceForm, lang.RecurForm); err != nil {
		return nil, err
	}
	if err := Register(registry); err != nil {
		return nil, err
	}
	return registry, nil
}

func Register(registry *lang.Registry) error {
	if registry == nil {
		return fmt.Errorf("registry is required")
	}
	for _, spec := range []lang.FunctionSpec{isHealthySpec(), scoreSpec()} {
		if err := registry.Register(spec); err != nil {
			return err
		}
	}
	return nil
}

func isHealthySpec() lang.FunctionSpec {
	return lang.FunctionSpec{
		Name:   "route.is_healthy@1",
		Params: []lang.Type{lang.StringType},
		Result: lang.BoolType,
		Cost:   3,
		Eval: func(args []lang.Value) (lang.Value, error) {
			status, ok := args[0].String()
			if !ok {
				return lang.Value{}, fmt.Errorf("status must be string")
			}
			return lang.Bool(strings.EqualFold(status, "UP")), nil
		},
		Display: lang.FunctionDisplay{
			Label:       "渠道是否健康",
			Description: "把渠道健康快照中的 UP 映射为 true。示例函数只做纯计算，真实健康度应作为参数传入。",
			Category:    "支付路由",
			Color:       "#059669",
			Icon:        "♥",
			Keywords:    []string{"health", "channel", "路由"},
			Parameters: []lang.ParameterDisplay{{
				Name: "status", Label: "健康状态", Description: "例如 UP、DOWN", Placeholder: "UP",
			}},
			Result:   lang.ResultDisplay{Label: "是否可用", Description: "UP 返回 true"},
			Examples: []lang.FunctionExample{{Title: "健康判断", Expression: `route.is_healthy@1(health)`}},
			Order:    10,
		},
	}
}

func scoreSpec() lang.FunctionSpec {
	return lang.FunctionSpec{
		Name:   "route.score@1",
		Params: []lang.Type{lang.FloatType, lang.FloatType},
		Result: lang.FloatType,
		Cost:   5,
		Eval: func(args []lang.Value) (lang.Value, error) {
			authRate, ok := args[0].Float()
			if !ok {
				return lang.Value{}, fmt.Errorf("auth_rate must be float")
			}
			cost, ok := args[1].Float()
			if !ok {
				return lang.Value{}, fmt.Errorf("cost must be float")
			}
			return lang.Float(authRate*100 - cost), nil
		},
		Display: lang.FunctionDisplay{
			Label:       "渠道评分",
			Description: "演示评分：成功率 × 100 − 成本。生产公式应由业务扩展包自行实现和版本化。",
			Category:    "支付路由",
			Color:       "#059669",
			Icon:        "★",
			Keywords:    []string{"score", "cost", "auth rate"},
			Parameters: []lang.ParameterDisplay{
				{Name: "authRate", Label: "成功率", Description: "0 到 1 的浮点数", Placeholder: "0.95"},
				{Name: "cost", Label: "成本", Description: "演示用浮点成本", Placeholder: "1.2"},
			},
			Result:   lang.ResultDisplay{Label: "评分", Description: "越大越优"},
			Examples: []lang.FunctionExample{{Title: "渠道打分", Expression: `route.score@1(auth_rate,cost)`}},
			Order:    20,
		},
	}
}
