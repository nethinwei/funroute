package std

import (
	"context"
	"fmt"
	"slices"
	"sort"

	"github.com/nethinwei/funroute"
)

// Grouping, ranking and running totals: the three things a rule does to a list
// that a single fold cannot express. They keep the same two-lists shape as the
// selection functions — the values and the key each is filed under.
func groupSpecs() []funroute.FunctionSpec {
	list := funroute.ArrayOf(funroute.TypeVar("T"))
	return slices.Concat([]funroute.FunctionSpec{{
		Name:   "group_by",
		Params: []funroute.Type{list, funroute.ArrayOf(funroute.StringType)},
		Result: funroute.DictOf(list),
		Eval:   groupByKeys,
		Doc: funroute.Doc{
			Label: "分组", Category: "选择", Cost: 10,
			Description: `按第二个数组的键把第一个数组分组，键相同的排在一组里、保持原顺序；两者长度必须相同。每组再聚合就是一句推导式：{k: sum(v) for k, v in group_by(amounts, channels)}。`,
			Params:      []string{"值", "键"}, Result: "分组结果",
		},
	}}, eachType("rank", funroute.Doc{
		Label: "名次", Category: "选择", Cost: 9,
		Description: "每个元素的升序名次，从 1 开始；并列同名次，其后跳号（1,1,3），和 SQL 的 RANK 一样。要降序就先 reverse。",
		Params:      []string{"键"}, Result: "名次数组",
	}, rankOf[int64], rankOf[float64], rankOf[string]), eachType("cumsum", funroute.Doc{
		Label: "累计和", Category: "聚合", Cost: 8,
		Description: "逐项累加出的序列：第 i 项是前 i+1 项之和。配下标就能找出累计超限的那一笔：first([i for i in indices(xs) if cumsum(xs)[i] > limit])。",
		Params:      []string{"数组"}, Result: "累计序列",
	}, cumulativeInts, cumulativeFloats))
}

func groupByKeys(_ context.Context, args []funroute.Value) (funroute.Value, error) {
	items, keys := itemsOf(args[0]), itemsOf(args[1])
	if len(items) != len(keys) {
		return funroute.Value{}, fmt.Errorf("group_by has %d values and %d keys", len(items), len(keys))
	}
	grouped := map[string][]funroute.Value{}
	var order []string
	for i, key := range keys {
		name, _ := key.String()
		if _, seen := grouped[name]; !seen {
			order = append(order, name)
		}
		grouped[name] = append(grouped[name], items[i])
	}
	elem := elementType(args[0])
	entries := make(map[string]funroute.Value, len(grouped))
	for _, name := range order {
		group, err := funroute.Array(elem, grouped[name])
		if err != nil {
			return funroute.Value{}, err
		}
		entries[name] = group
	}
	return funroute.Dict(funroute.ArrayOf(elem), entries)
}

// rankOf gives each element its position in the sorted order, counting from 1,
// with ties sharing a rank and the next rank skipping past them.
func rankOf[T int64 | float64 | string](keys []T) ([]int64, error) {
	sorted := append([]T(nil), keys...)
	slices.Sort(sorted)
	out := make([]int64, len(keys))
	for i, key := range keys {
		out[i] = int64(sort.Search(len(sorted), func(j int) bool { return !(sorted[j] < key) }) + 1)
	}
	return out, nil
}

func cumulativeInts(items []int64) ([]int64, error) {
	out := make([]int64, len(items))
	total := int64(0)
	for i, item := range items {
		sum, err := sumInts([]int64{total, item})
		if err != nil {
			return nil, err
		}
		total = sum
		out[i] = total
	}
	return out, nil
}

func cumulativeFloats(items []float64) ([]float64, error) {
	out := make([]float64, len(items))
	total := 0.0
	for i, item := range items {
		total += item
		out[i] = total
	}
	if len(out) > 0 {
		if _, err := sumFloats(out[len(out)-1:]); err != nil {
			return nil, err
		}
	}
	return out, nil
}
