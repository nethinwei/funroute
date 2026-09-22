package std

import (
	"context"
	"fmt"
	"sort"

	"funroute/lang"
)

// The array functions that are about the shape of a list rather than about
// what is in it: take one end, cut it short, turn it around, put two together,
// drop repeats, flatten one level. They are generic, so they are written as
// FunctionSpecs — reflection cannot express "array of any T".
func registerArrays(registry *lang.Registry) error {
	for _, spec := range arraySpecs() {
		if err := registry.Register(spec); err != nil {
			return err
		}
	}
	return registerSort(registry)
}

// arraySpecs is the shape-of-a-list family: take one end, cut it short, turn
// it around, put two together, drop repeats, flatten one level.
func arraySpecs() []lang.FunctionSpec {
	item := lang.TypeVar("T")
	list := lang.ArrayOf(item)
	shaped := func(name, label, description, result string, params []lang.Type, labels []string, cost uint64, eval lang.EvalFunc) lang.FunctionSpec {
		return lang.FunctionSpec{
			Name: name, Params: params, Result: list, Eval: eval,
			Doc: lang.Doc{
				Constexpr: true, Label: label, Category: "数组", Cost: cost,
				Description: description, Params: labels, Result: result,
			},
		}
	}
	end := func(name, label, description, result string, eval lang.EvalFunc) lang.FunctionSpec {
		return lang.FunctionSpec{
			Name: name, Params: []lang.Type{list}, Result: item, Eval: eval,
			Doc: lang.Doc{
				Constexpr: true, Label: label, Category: "数组", Cost: 2,
				Description: description, Params: []string{"数组"}, Result: result,
			},
		}
	}
	return []lang.FunctionSpec{
		end("first", "首个元素", "取数组的第一个元素；空数组报错，因为没有元素可取。", "首个元素", firstItem),
		end("last", "末个元素", "取数组的最后一个元素；空数组报错。", "末个元素", lastItem),
		shaped("take", "取前 n 个", "取数组的前 n 个元素；n 大于长度就取完，n 为负是错误。", "前 n 个",
			[]lang.Type{list, lang.IntType}, []string{"数组", "个数"}, 4, takeItems),
		shaped("reverse", "反转", "把数组倒过来。", "反转后的数组",
			[]lang.Type{list}, []string{"数组"}, 4, reverseItems),
		shaped("concat", "拼接数组", "把两个同型数组接成一个。", "拼接结果",
			[]lang.Type{list, list}, []string{"前一个", "后一个"}, 5, concatItems),
		shaped("unique", "去重", "按相等判断去掉重复元素，保留第一次出现的顺序。", "去重后的数组",
			[]lang.Type{list}, []string{"数组"}, 6, uniqueItems),
		shaped("flatten", "拉平一层", "把数组的数组拉平成一层；嵌套推导式配它就是多层遍历。", "拉平后的数组",
			[]lang.Type{lang.ArrayOf(list)}, []string{"嵌套数组"}, 6, flattenItems),
	}
}

func registerSort(registry *lang.Registry) error {
	doc := lang.Doc{Constexpr: true,
		Label: "排序", Category: "数组", Cost: 8,
		Description: "按自然顺序升序排列（数值按大小，字符串按 UTF-8 字节序）。要按别的键排，先用推导式算出键。",
		Params:      []string{"数组"}, Result: "升序数组",
	}
	if err := lang.Logic(registry, "sort", doc, func(items []int64) ([]int64, error) {
		out := append([]int64(nil), items...)
		sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
		return out, nil
	}); err != nil {
		return err
	}
	if err := lang.Logic(registry, "sort", doc, func(items []float64) ([]float64, error) {
		out := append([]float64(nil), items...)
		sort.Float64s(out)
		return out, nil
	}); err != nil {
		return err
	}
	return lang.Logic(registry, "sort", doc, func(items []string) ([]string, error) {
		out := append([]string(nil), items...)
		sort.Strings(out)
		return out, nil
	})
}

func firstItem(_ context.Context, args []lang.Value) (lang.Value, error) {
	value, ok := args[0].At(0)
	if !ok {
		return lang.Value{}, fmt.Errorf("first of an empty array")
	}
	return value, nil
}

func lastItem(_ context.Context, args []lang.Value) (lang.Value, error) {
	length, _ := args[0].Length()
	value, ok := args[0].At(length - 1)
	if !ok {
		return lang.Value{}, fmt.Errorf("last of an empty array")
	}
	return value, nil
}

func takeItems(_ context.Context, args []lang.Value) (lang.Value, error) {
	count, _ := args[1].Int()
	if count < 0 {
		return lang.Value{}, fmt.Errorf("take needs a count of zero or more, got %d", count)
	}
	items := itemsOf(args[0])
	if count > int64(len(items)) {
		count = int64(len(items))
	}
	return lang.Array(elementType(args[0]), items[:count])
}

func reverseItems(_ context.Context, args []lang.Value) (lang.Value, error) {
	items := itemsOf(args[0])
	out := make([]lang.Value, len(items))
	for i, item := range items {
		out[len(items)-1-i] = item
	}
	return lang.Array(elementType(args[0]), out)
}

func concatItems(_ context.Context, args []lang.Value) (lang.Value, error) {
	left, right := itemsOf(args[0]), itemsOf(args[1])
	out := make([]lang.Value, 0, len(left)+len(right))
	out = append(out, left...)
	out = append(out, right...)
	return lang.Array(elementType(args[0]), out)
}

func uniqueItems(_ context.Context, args []lang.Value) (lang.Value, error) {
	var out []lang.Value
	for _, item := range itemsOf(args[0]) {
		if !containsValue(out, item) {
			out = append(out, item)
		}
	}
	return lang.Array(elementType(args[0]), out)
}

func flattenItems(_ context.Context, args []lang.Value) (lang.Value, error) {
	var out []lang.Value
	for _, inner := range itemsOf(args[0]) {
		out = append(out, itemsOf(inner)...)
	}
	elem := elementType(args[0])
	if elem.Elem == nil {
		return lang.Value{}, fmt.Errorf("flatten needs an array of arrays")
	}
	return lang.Array(*elem.Elem, out)
}

func containsValue(values []lang.Value, wanted lang.Value) bool {
	for _, value := range values {
		if value.Equal(wanted) {
			return true
		}
	}
	return false
}

// itemsOf reads a container's elements. The values are the container's own,
// handed over read-only like everywhere else at this boundary.
func itemsOf(value lang.Value) []lang.Value {
	items, _ := value.Array()
	return items
}

func elementType(value lang.Value) lang.Type {
	typ := value.Type()
	if typ.Elem == nil {
		return lang.Type{}
	}
	return *typ.Elem
}
