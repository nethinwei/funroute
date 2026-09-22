package std

import (
	"context"
	"fmt"

	"funroute/lang"
)

// Two things a dictionary needs that a comprehension cannot say. Listing the
// keys or the values is already `[k for k, v in d]`, so it is not here; a
// missing key and a layered override are not, because the language has no null
// to stand for "absent" and no way to build one dictionary out of two.
//
// `d["k"]` on a missing key stays an error — inventing a zero there would put
// a silently wrong amount in a routing decision — and `get` is where a rule
// says what the absence means.
func registerDicts(registry *lang.Registry) error {
	item := lang.TypeVar("T")
	dict := lang.DictOf(item)
	if err := registry.Register(lang.FunctionSpec{
		Name: "get", Params: []lang.Type{dict, lang.StringType, item}, Result: item, Eval: evalGet,
		Doc: lang.Doc{
			Constexpr: true, Label: "取值或默认", Category: "容器", Cost: 3,
			Description: `按键取值，键不在就返回第三个参数。d["k"] 缺键时报错，要兜底就写 get(d, "k", 0)。`,
			Params:      []string{"字典", "键", "默认值"}, Result: "值或默认值",
		},
	}); err != nil {
		return err
	}
	return registry.Register(lang.FunctionSpec{
		Name: "merge", Params: []lang.Type{dict, dict}, Result: dict, Eval: evalMerge,
		Doc: lang.Doc{
			Constexpr: true, Label: "合并字典", Category: "容器", Cost: 8,
			Description: "把两个字典叠在一起，键相同时取后一个的值：默认费率叠上本次覆盖就是 merge(defaults, overrides)。",
			Params:      []string{"底层", "覆盖层"}, Result: "合并结果",
		},
	})
}

func evalGet(_ context.Context, args []lang.Value) (lang.Value, error) {
	key, _ := args[1].String()
	if value, ok := args[0].Lookup(key); ok {
		return value, nil
	}
	return args[2], nil
}

func evalMerge(_ context.Context, args []lang.Value) (lang.Value, error) {
	base, ok := args[0].Dict()
	overrides, hasOverrides := args[1].Dict()
	if !ok || !hasOverrides {
		return lang.Value{}, fmt.Errorf("merge needs two dictionaries")
	}
	out := make(map[string]lang.Value, len(base)+len(overrides))
	for key, value := range base {
		out[key] = value
	}
	for key, value := range overrides {
		out[key] = value
	}
	return lang.Dict(elementType(args[0]), out)
}
