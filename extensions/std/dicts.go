package std

import (
	"context"
	"errors"
	"maps"

	"github.com/nethinwei/funroute"
)

// Two things a dictionary needs that a comprehension cannot say. Listing the
// keys or the values is already `[k for k, v in d]`, so it is not here; a
// missing key and a layered override are not, because the language has no null
// to stand for "absent" and no way to build one dictionary out of two.
//
// `d["k"]` on a missing key stays an error — inventing a zero there would put
// a silently wrong amount in a routing decision — and `get` is where a rule
// says what the absence means.
func dictSpecs() []funroute.FunctionSpec {
	item := funroute.TypeVar("T")
	dict := funroute.DictOf(item)
	return []funroute.FunctionSpec{
		{
			Name: "get", Params: []funroute.Type{dict, funroute.StringType, item}, Result: item, Eval: evalGet,
			Doc: funroute.Doc{
				Label: "取值或默认", Category: "容器",
				Description: `按键取值，键不在就返回第三个参数。d["k"] 缺键时报错，要兜底就写 get(d, "k", 0)。`,
				Params:      []string{"字典", "键", "默认值"}, Result: "值或默认值",
			},
		},
		{
			Name: "merge", Params: []funroute.Type{dict, dict}, Result: dict, Eval: evalMerge,
			Doc: funroute.Doc{
				Label: "合并字典", Category: "容器",
				Description: "把两个字典叠在一起，键相同时取后一个的值：默认费率叠上本次覆盖就是 merge(defaults, overrides)。",
				Params:      []string{"底层", "覆盖层"}, Result: "合并结果",
			},
		},
	}
}

func evalGet(_ context.Context, args []funroute.Value) (funroute.Value, error) {
	key, _ := args[1].String()
	if value, ok := args[0].Lookup(key); ok {
		return value, nil
	}
	return args[2], nil
}

func evalMerge(_ context.Context, args []funroute.Value) (funroute.Value, error) {
	base, ok := args[0].Dict()
	overrides, hasOverrides := args[1].Dict()
	if !ok || !hasOverrides {
		return funroute.Value{}, errors.New("merge needs two dictionaries")
	}
	out := make(map[string]funroute.Value, len(base)+len(overrides))
	maps.Copy(out, base)
	maps.Copy(out, overrides)
	return funroute.Dict(elementType(args[0]), out)
}
