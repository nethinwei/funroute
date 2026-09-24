package std

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/nethinwei/funroute"
)

// Picking one out of a list of candidates is what a routing rule does, and
// doing it needs two lists side by side: the candidates and the key each is
// judged by. There are no lambdas here, so that is the shape everything takes
// — sort_by(channels, fees) rather than sort_by(channels, c => c.fee).
//
// The lists must be the same length. Nothing here invents an answer for a
// candidate it has no key for.
func selectSpecs() []funroute.FunctionSpec {
	item := funroute.TypeVar("T")
	list := funroute.ArrayOf(item)
	return []funroute.FunctionSpec{
		{
			Name: "indices", Params: []funroute.Type{list}, Result: funroute.ArrayOf(funroute.IntType), Eval: indicesOf,
			Doc: funroute.Doc{
				Label: "下标序列", Category: "选择", Cost: 4,
				Description: "这个数组的下标，0 到长度减一。配推导式就能按位置把两个数组对起来：[names[i] for i in indices(fees) if fees[i] < cap]。",
				Params:      []string{"数组"}, Result: "下标数组",
			},
		},
		{
			Name: "index_of", Params: []funroute.Type{list, item}, Result: funroute.IntType, Eval: indexOfItem,
			Doc: funroute.Doc{
				Label: "元素位置", Category: "选择", Cost: 5,
				Description: "元素第一次出现的下标；不在里面是错误，先用 x in xs 判断。",
				Params:      []string{"数组", "元素"}, Result: "下标",
			},
		},
	}
}

// keyedSpecs are the keyed sorts, both ways, and the keyed cuts: sort_by
// with a direction and a count, because "the three cheapest" and "the three
// best" are what a rule actually asks for — writing take(sort_by(...), 3) or
// take(reverse(sort_by(...)), 3) every time is a puzzle the rule writer
// should not have to solve twice. The key type is what the registrations are
// for: a key can be a fee, a rate or a name, and money where the registry
// declares it — the cheapest channels by fee.
func keyedSpecs(money bool) []funroute.FunctionSpec {
	keys := []funroute.Type{funroute.IntType, funroute.FloatType, funroute.StringType}
	if money {
		keys = []funroute.Type{funroute.MoneyType}
	}
	sortByMoney := moneyDoc("按金额排序", "按金额键排序候选，相等的保持原顺序。")
	rankByMoney := moneyDoc("按金额取前 k 个", "按金额键取最小或最大的 k 个候选。")
	specs := make([]funroute.FunctionSpec, 0, 4*len(keys))
	for _, keyed := range []keyedFunction{
		{"sort_by", "按键排序", "按第二个数组的键把第一个数组升序排好，两者长度必须相同；相等的保持原顺序。取最便宜的三个写 take(sort_by(channels, fees), 3)。", sortByMoney, true, false},
		{"sort_by_desc", "按键降序", "sort_by 的反向：按键从大到小排。相等的保持原顺序。", sortByMoney, false, false},
		{"bottom_k", "取最小的 k 个", "按键升序取前 k 个候选：最便宜的几个渠道。k 大于长度就取完。", rankByMoney, true, true},
		{"top_k", "取最大的 k 个", "按键降序取前 k 个候选：成功率最高的几个渠道。k 大于长度就取完。", rankByMoney, false, true},
	} {
		for _, key := range keys {
			specs = append(specs, keyed.spec(key, money))
		}
	}
	return specs
}

type keyedFunction struct {
	name, label, description string
	byMoney                  funroute.Doc
	ascending, ranked        bool
}

func (k keyedFunction) spec(key funroute.Type, money bool) funroute.FunctionSpec {
	list := funroute.ArrayOf(funroute.TypeVar("T"))
	ascending := k.ascending
	spec := funroute.FunctionSpec{
		Name: k.name, Params: []funroute.Type{list, funroute.ArrayOf(key)}, Result: list,
		Eval: func(_ context.Context, args []funroute.Value) (funroute.Value, error) {
			return sortedBy(args, ascending)
		},
		Doc: funroute.Doc{
			Label: k.label, Category: "选择", Cost: 9, Description: k.description,
			Params: []string{"候选", "键"}, Result: "排序后的候选",
		},
	}
	if k.ranked {
		spec.Params = append(spec.Params, funroute.IntType)
		spec.Eval = func(_ context.Context, args []funroute.Value) (funroute.Value, error) {
			return pickRanked(args, ascending)
		}
		spec.Doc.Params, spec.Doc.Result = []string{"候选", "键", "个数"}, "选出的候选"
	}
	if money {
		spec.Doc, spec.Eval = k.byMoney, uniformKeys(1, spec.Eval)
	}
	return spec
}

// whileSpecs cut a list where a run of trues ends. The test is the second
// array, same as every other pair here — "every item until the running total
// passes the cap" is take_while(amounts, [t <= cap for t in cumsum(amounts)]).
func whileSpecs() []funroute.FunctionSpec {
	list := funroute.ArrayOf(funroute.TypeVar("T"))
	specs := make([]funroute.FunctionSpec, 0, 2)
	for _, side := range []struct {
		name, label, description string
		prefix                   bool
	}{
		{"take_while", "取到不满足为止", "从头取，遇到第一个 false 就停，后面还有 true 也不要。判断数组必须与候选等长。", true},
		{"drop_while", "跳过开头满足的", "从头跳过 true，遇到第一个 false 就把剩下的全部返回。判断数组必须与候选等长。", false},
	} {
		prefix, name := side.prefix, side.name
		specs = append(specs, funroute.FunctionSpec{
			Name: name, Params: []funroute.Type{list, funroute.ArrayOf(funroute.BoolType)}, Result: list,
			Eval: func(ctx context.Context, args []funroute.Value) (funroute.Value, error) {
				return cutWhile(name, args, prefix)
			},
			Doc: funroute.Doc{
				Label: side.label, Category: "选择", Cost: 6,
				Description: side.description,
				Params:      []string{"候选", "逐项判断"}, Result: "截取后的候选",
			},
		})
	}
	return specs
}

func cutWhile(name string, args []funroute.Value, prefix bool) (funroute.Value, error) {
	items, flags := itemsOf(args[0]), itemsOf(args[1])
	if len(items) != len(flags) {
		return funroute.Value{}, fmt.Errorf("%s has %d candidates and %d tests", name, len(items), len(flags))
	}
	cut := len(items)
	for i, flag := range flags {
		if keep, _ := flag.Bool(); !keep {
			cut = i
			break
		}
	}
	if prefix {
		return funroute.Array(elementType(args[0]), items[:cut])
	}
	return funroute.Array(elementType(args[0]), items[cut:])
}

func pickRanked(args []funroute.Value, ascending bool) (funroute.Value, error) {
	sorted, err := sortedBy(args, ascending)
	if err != nil {
		return funroute.Value{}, err
	}
	count, _ := args[2].Int()
	return takeFirst(sorted, count, "a count of candidates cannot be negative, got %d")
}

// positionSpecs are the ones a routing rule reaches for most: which
// candidate, not which value. channels[arg_min(fees)] is the cheapest channel.
func positionSpecs() []funroute.FunctionSpec {
	specs := make([]funroute.FunctionSpec, 0, 6) // two names, three types each
	for _, extreme := range []struct {
		name, label, description string
		smallest                 bool
	}{
		{"arg_min", "最小值的位置", "最小元素的下标；并列取第一个，空数组报错。channels[arg_min(fees)] 就是最便宜的那个。", true},
		{"arg_max", "最大值的位置", "最大元素的下标；并列取第一个，空数组报错。", false},
	} {
		doc := funroute.Doc{
			Label: extreme.label, Category: "选择", Cost: 5,
			Description: extreme.description, Params: []string{"键"}, Result: "下标",
		}
		smallest := extreme.smallest
		specs = append(specs, eachType(extreme.name, doc,
			extremeIndex[int64](smallest), extremeIndex[float64](smallest), extremeIndex[string](smallest))...)
	}
	return specs
}

func indicesOf(_ context.Context, args []funroute.Value) (funroute.Value, error) {
	length, ok := args[0].Length()
	if !ok {
		return funroute.Value{}, errors.New("indices needs an array")
	}
	out := make([]int64, length)
	for i := range out {
		out[i] = int64(i)
	}
	return funroute.ToValue(out)
}

func indexOfItem(_ context.Context, args []funroute.Value) (funroute.Value, error) {
	for i, item := range itemsOf(args[0]) {
		if item.Equal(args[1]) {
			return funroute.Int(int64(i)), nil
		}
	}
	return funroute.Value{}, errors.New("the array does not contain that item")
}

func sortedBy(args []funroute.Value, ascending bool) (funroute.Value, error) {
	items, keys := itemsOf(args[0]), itemsOf(args[1])
	if len(items) != len(keys) {
		return funroute.Value{}, fmt.Errorf("sort_by has %d candidates and %d keys", len(items), len(keys))
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
	out := make([]funroute.Value, len(items))
	for position, index := range order {
		out[position] = items[index]
	}
	return funroute.Array(elementType(args[0]), out)
}

// valueLess orders the key types the same way the comparison operators do.
func valueLess(left, right funroute.Value) bool {
	if a, ok := left.Int(); ok {
		b, _ := right.Int()
		return a < b
	}
	if a, ok := left.Money(); ok {
		b, _ := right.Money()
		return a.Minor() < b.Minor()
	}
	if a, ok := left.Float(); ok {
		b, _ := right.Float()
		return a < b
	}
	a, _ := left.String()
	b, _ := right.String()
	return a < b
}

// errNoExtreme is an arg_min or arg_max, or a money min or max, of nothing.
var errNoExtreme = errors.New("an empty array has no extreme")

// extremeIndex builds the arg_min / arg_max body for one key type.
func extremeIndex[T cmp.Ordered](smallest bool) func([]T) (int64, error) {
	return func(keys []T) (int64, error) {
		at, ok := best(keys, smallest)
		if !ok {
			return 0, errNoExtreme
		}
		return int64(at), nil
	}
}
