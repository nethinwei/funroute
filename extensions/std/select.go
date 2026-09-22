package std

import (
	"context"
	"fmt"
	"sort"

	"funroute/lang"
)

// Picking one out of a list of candidates is what a routing rule does, and
// doing it needs two lists side by side: the candidates and the key each is
// judged by. There are no lambdas here, so that is the shape everything takes
// — sort_by(channels, fees) rather than sort_by(channels, c => c.fee).
//
// The lists must be the same length. Nothing here invents an answer for a
// candidate it has no key for.
func registerSelect(registry *lang.Registry) error {
	item := lang.TypeVar("T")
	list := lang.ArrayOf(item)
	specs := []lang.FunctionSpec{
		{
			Name: "indices", Params: []lang.Type{list}, Result: lang.ArrayOf(lang.IntType), Eval: indicesOf,
			Doc: lang.Doc{
				Constexpr: true, Label: "下标序列", Category: "选择", Cost: 4,
				Description: "这个数组的下标，0 到长度减一。配推导式就能按位置把两个数组对起来：[names[i] for i in indices(fees) if fees[i] < cap]。",
				Params:      []string{"数组"}, Result: "下标数组",
			},
		},
		{
			Name: "index_of", Params: []lang.Type{list, item}, Result: lang.IntType, Eval: indexOfItem,
			Doc: lang.Doc{
				Constexpr: true, Label: "元素位置", Category: "选择", Cost: 5,
				Description: "元素第一次出现的下标；不在里面是错误，先用 x in xs 判断。",
				Params:      []string{"数组", "元素"}, Result: "下标",
			},
		},
	}
	for _, spec := range specs {
		if err := registry.Register(spec); err != nil {
			return err
		}
	}
	if err := registerSortBy(registry, list); err != nil {
		return err
	}
	if err := registerRanked(registry, list); err != nil {
		return err
	}
	if err := registerWhile(registry, list); err != nil {
		return err
	}
	return registerExtremeIndex(registry)
}

// registerSortBy covers both directions of a keyed sort. The key type is what
// the three registrations are for: a key can be a fee, a rate or a name.
func registerSortBy(registry *lang.Registry, list lang.Type) error {
	for _, direction := range []struct {
		name, label, description string
		ascending                bool
	}{
		{"sort_by", "按键排序", "按第二个数组的键把第一个数组升序排好，两者长度必须相同；相等的保持原顺序。取最便宜的三个写 take(sort_by(channels, fees), 3)。", true},
		{"sort_by_desc", "按键降序", "sort_by 的反向：按键从大到小排。相等的保持原顺序。", false},
	} {
		up := direction.ascending
		eval := func(_ context.Context, args []lang.Value) (lang.Value, error) { return sortedBy(args, up) }
		for _, key := range []lang.Type{lang.IntType, lang.FloatType, lang.StringType} {
			if err := registry.Register(lang.FunctionSpec{
				Name: direction.name, Params: []lang.Type{list, lang.ArrayOf(key)}, Result: list, Eval: eval,
				Doc: lang.Doc{
					Constexpr: true, Label: direction.label, Category: "选择", Cost: 9,
					Description: direction.description,
					Params:      []string{"候选", "键"}, Result: "排序后的候选",
				},
			}); err != nil {
				return err
			}
		}
	}
	return nil
}

// registerRanked is sort_by with a direction and a cut, because "the three
// cheapest" and "the three best" are what a rule actually asks for — writing
// take(sort_by(...), 3) or take(reverse(sort_by(...)), 3) every time is a
// puzzle the rule writer should not have to solve twice.
func registerRanked(registry *lang.Registry, list lang.Type) error {
	for _, ranked := range []struct {
		name, label, description string
		ascending                bool
	}{
		{"bottom_k", "取最小的 k 个", "按键升序取前 k 个候选：最便宜的几个渠道。k 大于长度就取完。", true},
		{"top_k", "取最大的 k 个", "按键降序取前 k 个候选：成功率最高的几个渠道。k 大于长度就取完。", false},
	} {
		ascending := ranked.ascending
		for _, key := range []lang.Type{lang.IntType, lang.FloatType, lang.StringType} {
			if err := registry.Register(lang.FunctionSpec{
				Name:   ranked.name,
				Params: []lang.Type{list, lang.ArrayOf(key), lang.IntType},
				Result: list,
				Eval:   func(ctx context.Context, args []lang.Value) (lang.Value, error) { return pickRanked(args, ascending) },
				Doc: lang.Doc{
					Constexpr: true, Label: ranked.label, Category: "选择", Cost: 9,
					Description: ranked.description,
					Params:      []string{"候选", "键", "个数"}, Result: "选出的候选",
				},
			}); err != nil {
				return err
			}
		}
	}
	return nil
}

// registerWhile cuts a list where a run of trues ends. The test is the second
// array, same as every other pair here — "every item until the running total
// passes the cap" is take_while(amounts, [t <= cap for t in cumsum(amounts)]).
func registerWhile(registry *lang.Registry, list lang.Type) error {
	for _, side := range []struct {
		name, label, description string
		prefix                   bool
	}{
		{"take_while", "取到不满足为止", "从头取，遇到第一个 false 就停，后面还有 true 也不要。判断数组必须与候选等长。", true},
		{"drop_while", "跳过开头满足的", "从头跳过 true，遇到第一个 false 就把剩下的全部返回。判断数组必须与候选等长。", false},
	} {
		prefix, name := side.prefix, side.name
		if err := registry.Register(lang.FunctionSpec{
			Name: name, Params: []lang.Type{list, lang.ArrayOf(lang.BoolType)}, Result: list,
			Eval: func(ctx context.Context, args []lang.Value) (lang.Value, error) {
				return cutWhile(name, args, prefix)
			},
			Doc: lang.Doc{
				Constexpr: true, Label: side.label, Category: "选择", Cost: 6,
				Description: side.description,
				Params:      []string{"候选", "逐项判断"}, Result: "截取后的候选",
			},
		}); err != nil {
			return err
		}
	}
	return nil
}

func cutWhile(name string, args []lang.Value, prefix bool) (lang.Value, error) {
	items, flags := itemsOf(args[0]), itemsOf(args[1])
	if len(items) != len(flags) {
		return lang.Value{}, fmt.Errorf("%s has %d candidates and %d tests", name, len(items), len(flags))
	}
	cut := len(items)
	for i, flag := range flags {
		if keep, _ := flag.Bool(); !keep {
			cut = i
			break
		}
	}
	if prefix {
		return lang.Array(elementType(args[0]), items[:cut])
	}
	return lang.Array(elementType(args[0]), items[cut:])
}

func pickRanked(args []lang.Value, ascending bool) (lang.Value, error) {
	sorted, err := sortedBy(args, ascending)
	if err != nil {
		return lang.Value{}, err
	}
	count, _ := args[2].Int()
	if count < 0 {
		return lang.Value{}, fmt.Errorf("a count of candidates cannot be negative, got %d", count)
	}
	items := itemsOf(sorted)
	if count > int64(len(items)) {
		count = int64(len(items))
	}
	return lang.Array(elementType(sorted), items[:count])
}

// registerExtremeIndex is the one a routing rule reaches for most: which
// candidate, not which value. channels[arg_min(fees)] is the cheapest channel.
func registerExtremeIndex(registry *lang.Registry) error {
	for _, extreme := range []struct {
		name, label, description string
		ints                     func([]int64) (int64, error)
		floats                   func([]float64) (int64, error)
		texts                    func([]string) (int64, error)
	}{
		{"arg_min", "最小值的位置", "最小元素的下标；并列取第一个，空数组报错。channels[arg_min(fees)] 就是最便宜的那个。",
			extremeIndex[int64](true), extremeIndex[float64](true), extremeIndex[string](true)},
		{"arg_max", "最大值的位置", "最大元素的下标；并列取第一个，空数组报错。",
			extremeIndex[int64](false), extremeIndex[float64](false), extremeIndex[string](false)},
	} {
		doc := lang.Doc{
			Constexpr: true, Label: extreme.label, Category: "选择", Cost: 5,
			Description: extreme.description, Params: []string{"键"}, Result: "下标",
		}
		if err := lang.Logic(registry, extreme.name, doc, extreme.ints); err != nil {
			return err
		}
		if err := lang.Logic(registry, extreme.name, doc, extreme.floats); err != nil {
			return err
		}
		if err := lang.Logic(registry, extreme.name, doc, extreme.texts); err != nil {
			return err
		}
	}
	return nil
}

func indicesOf(_ context.Context, args []lang.Value) (lang.Value, error) {
	length, ok := args[0].Length()
	if !ok {
		return lang.Value{}, fmt.Errorf("indices needs an array")
	}
	out := make([]int64, length)
	for i := range out {
		out[i] = int64(i)
	}
	return lang.ToValue(out)
}

func indexOfItem(_ context.Context, args []lang.Value) (lang.Value, error) {
	for i, item := range itemsOf(args[0]) {
		if item.Equal(args[1]) {
			return lang.Int(int64(i)), nil
		}
	}
	return lang.Value{}, fmt.Errorf("the array does not contain that item")
}

func sortedBy(args []lang.Value, ascending bool) (lang.Value, error) {
	items, keys := itemsOf(args[0]), itemsOf(args[1])
	if len(items) != len(keys) {
		return lang.Value{}, fmt.Errorf("sort_by has %d candidates and %d keys", len(items), len(keys))
	}
	order := make([]int, len(items))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool {
		left, right := keys[order[a]], keys[order[b]]
		if ascending {
			return valueLess(left, right)
		}
		return valueLess(right, left)
	})
	out := make([]lang.Value, len(items))
	for position, index := range order {
		out[position] = items[index]
	}
	return lang.Array(elementType(args[0]), out)
}

// valueLess orders the key types the same way the comparison operators do.
func valueLess(left, right lang.Value) bool {
	if a, ok := left.Int(); ok {
		b, _ := right.Int()
		return a < b
	}
	if a, ok := left.Float(); ok {
		b, _ := right.Float()
		return a < b
	}
	a, _ := left.String()
	b, _ := right.String()
	return a < b
}

// extremeIndex builds the arg_min / arg_max body for one key type.
func extremeIndex[T int64 | float64 | string](smallest bool) func([]T) (int64, error) {
	return func(keys []T) (int64, error) {
		if len(keys) == 0 {
			return 0, fmt.Errorf("an empty array has no extreme")
		}
		best := 0
		for i, key := range keys {
			if (smallest && key < keys[best]) || (!smallest && key > keys[best]) {
				best = i
			}
		}
		return int64(best), nil
	}
}
