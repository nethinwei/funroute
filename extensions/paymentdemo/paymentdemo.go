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
	if err := registerScore(registry); err != nil {
		return err
	}
	return registerModel(registry)
}

// Embedding stands in for an inference engine's tensor. The expression passes
// it from one model function to the next as handle<demo.embedding> and never
// looks inside; a real host would define its engine's tensor type the same way.
type Embedding struct{ Features []float64 }

// registerModel is the deep-learning shape: a model that turns features into
// an engine value, and a model that scores it. Both carry a batch
// implementation, so a Batch calls them once per batch of requests.
func registerModel(registry *lang.Registry) error {
	if err := lang.DefineHandle[*Embedding](registry, "demo.embedding"); err != nil {
		return err
	}
	err := lang.Model(registry, "model.embed_v1", lang.Doc{
		Label: "特征向量化", Description: "演示模型：把特征数组交给引擎，得到一个不透明的向量句柄。", Category: "模型",
		Color: "#7C3AED", Icon: "⊙", Cost: 20, Params: []string{"特征"}, Result: "向量句柄",
		Examples: []lang.FunctionExample{{Title: "向量化", Expression: `model.embed_v1(features)`}}, Order: 10,
	}, func(features []float64) (*Embedding, error) {
		return &Embedding{Features: features}, nil
	}, func(features [][]float64) ([]*Embedding, error) {
		out := make([]*Embedding, len(features))
		for i, row := range features {
			out[i] = &Embedding{Features: row}
		}
		return out, nil
	})
	if err != nil {
		return err
	}
	return lang.Model(registry, "model.fraud_v1", lang.Doc{
		Label: "欺诈评分", Description: "演示模型：对向量句柄打分，返回 0 到 1 的欺诈概率（这里取特征均值）。", Category: "模型",
		Color: "#7C3AED", Icon: "⚠", Cost: 20, Params: []string{"向量句柄"}, Result: "欺诈概率",
		Examples: []lang.FunctionExample{{Title: "评分", Expression: `model.fraud_v1(model.embed_v1(features)) > 0.8`}}, Order: 20,
	}, fraudScore, func(embeddings []*Embedding) ([]float64, error) {
		out := make([]float64, len(embeddings))
		for i, embedding := range embeddings {
			out[i], _ = fraudScore(embedding)
		}
		return out, nil
	})
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

func registerHealth(registry *lang.Registry) error {
	return lang.Logic(registry, "route.is_healthy_v1", lang.Doc{
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
	return lang.Logic(registry, "route.score_v1", lang.Doc{
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
