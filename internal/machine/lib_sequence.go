package machine

import (
	"cmp"
	"context"
	"fmt"
	"math"
	"slices"
	"strconv"

	"github.com/nethinwei/funroute/internal/money"
)

// The array functions about a list's shape rather than what is in it — take
// one end, cut it short, turn it around, put two together, drop repeats,
// flatten one level — and the ones that order it or find in it. They work on
// an array's own backing: a native slice, or the Values of a nested array,
// never rebuilt item by item through the public constructors.

// shapeSpecs are generic, so they are written as FunctionSpecs: reflection
// cannot express "array of any T".
func shapeSpecs() []FunctionSpec {
	item := TypeVar("T")
	list := ArrayOf(item)
	shaped := func(name, label, description, result string, params []Type, labels []string, eval EvalFunc) FunctionSpec {
		return FunctionSpec{
			Name: name, Params: params, Result: list, Eval: eval,
			Doc: Doc{Label: label, Category: "数组", Description: description, Params: labels, Result: result},
		}
	}
	end := func(name, label, description, result string, eval EvalFunc) FunctionSpec {
		return FunctionSpec{
			Name: name, Params: []Type{list}, Result: item, Eval: eval,
			Doc: Doc{Label: label, Category: "数组", Description: description, Params: []string{"数组"}, Result: result},
		}
	}
	first := end("first", "首个元素", "取数组的第一个元素；空数组报错，因为没有元素可取。", "首个元素", firstItem)
	// first([… for x in xs if …]) stops at the first item yielded, the rest
	// neither computed nor built.
	first.Fold = &Fold{First: true}
	return []FunctionSpec{
		first,
		end("last", "末个元素", "取数组的最后一个元素；空数组报错。", "末个元素", lastItem),
		shaped("slice", "取一段", "按下标取 [start, end) 这一段，下标从 0 开始；越界报错，不静默截断。", "这一段",
			[]Type{list, IntType, IntType}, []string{"数组", "起点", "终点"}, sliceItems),
		shaped("take", "取前 n 个", "取数组的前 n 个元素；n 大于长度就取完，n 为负是错误。", "前 n 个",
			[]Type{list, IntType}, []string{"数组", "个数"}, takeItems),
		shaped("reverse", "反转", "把数组倒过来。", "反转后的数组", []Type{list}, []string{"数组"}, reverseItems),
		shaped("concat", "拼接数组", "把两个同型数组接成一个。", "拼接结果", []Type{list, list}, []string{"前一个", "后一个"}, concatItems),
		shaped("unique", "去重", "按相等判断去掉重复元素，保留第一次出现的顺序。", "去重后的数组", []Type{list}, []string{"数组"}, uniqueItems),
		shaped("flatten", "拉平一层", "把数组的数组拉平成一层；嵌套推导式配它就是多层遍历。", "拉平后的数组",
			[]Type{ArrayOf(list)}, []string{"嵌套数组"}, flattenItems),
	}
}

func firstItem(_ context.Context, args []Value) (Value, error) {
	if args[0].length() == 0 {
		return Value{}, fmt.Errorf("%w: first of an empty array", ErrDomain)
	}
	return args[0].at(0), nil
}

func lastItem(_ context.Context, args []Value) (Value, error) {
	n := args[0].length()
	if n == 0 {
		return Value{}, fmt.Errorf("%w: last of an empty array", ErrDomain)
	}
	return args[0].at(n - 1), nil
}

func sliceItems(_ context.Context, args []Value) (Value, error) {
	start, end := args[1].i, args[2].i
	n := int64(args[0].length())
	if start < 0 || end < start || end > n {
		return Value{}, fmt.Errorf("%w: slice [%d, %d) is outside an array of %d items", ErrDomain, start, end, n)
	}
	part, _ := args[0].Slice(int(start), int(end))
	return part, nil
}

func takeItems(_ context.Context, args []Value) (Value, error) {
	return takeFirst(args[0], args[1].i, "take needs a count of zero or more, got %d")
}

// takeFirst is an array's first count items, all of them when it has fewer;
// a negative count is an error, in the caller's words.
func takeFirst(array Value, count int64, negative string) (Value, error) {
	if count < 0 {
		return Value{}, fmt.Errorf("%w: "+negative, ErrArithmetic, count)
	}
	first, _ := array.Slice(0, int(min(count, int64(array.length()))))
	return first, nil
}

func reverseItems(_ context.Context, args []Value) (Value, error) {
	n := args[0].length()
	return rearranged(args[0], n, func(i int) int { return n - 1 - i }), nil
}

// rearranged is n items of array, item i of it being item at(i) of array:
// in the backing array has, which is the array's type.
func rearranged(array Value, n int, at func(int) int) Value {
	switch box := array.box.(type) {
	case *bool:
		return arrayOf(picked(itemsAt(box, array.i), n, at))
	case *int64:
		return arrayOf(picked(itemsAt(box, array.i), n, at))
	case *float64:
		return arrayOf(picked(itemsAt(box, array.i), n, at))
	case *string:
		return arrayOf(picked(itemsAt(box, array.i), n, at))
	case *money.Money:
		return arrayOf(picked(itemsAt(box, array.i), n, at))
	case *money.FxRate:
		return arrayOf(picked(itemsAt(box, array.i), n, at))
	case *nestedArray:
		return nestedOf(box.elem, picked(box.items, n, at))
	case *recordsView:
		return viewed(box.arranged(n, at))
	}
	// A backing not listed: item by item, as any array is read, never the
	// array as it was.
	builder := newArrayBuilder(array.elemType(), n)
	for i := range n {
		builder.add(array.at(at(i)))
	}
	return builder.finish()
}

func picked[T any](items []T, n int, at func(int) int) []T {
	out := make([]T, n)
	for i := range out {
		out[i] = items[at(i)]
	}
	return out
}

// concatItems joins two arrays of one type: their backings are of one
// kind, as their type is one.
func concatItems(_ context.Context, args []Value) (Value, error) {
	switch left := args[0].box.(type) {
	case *bool:
		return joined(itemsAt(left, args[0].i), args[1]), nil
	case *int64:
		return joined(itemsAt(left, args[0].i), args[1]), nil
	case *float64:
		return joined(itemsAt(left, args[0].i), args[1]), nil
	case *string:
		return joined(itemsAt(left, args[0].i), args[1]), nil
	case *money.Money:
		return joined(itemsAt(left, args[0].i), args[1]), nil
	case *money.FxRate:
		return joined(itemsAt(left, args[0].i), args[1]), nil
	}
	// Arrays of anything else — nested, or records in the host's memory —
	// join as their items.
	items := appended(valuesOf(args[0]), valuesOf(args[1]))
	return nestedOf(args[0].elemType(), items), nil
}

// appended is left and then right in a slice of its own, never nil: two
// empty arrays make an empty one, which a host reads as [] and not null.
func appended[T any](left, right []T) []T {
	return append(append(make([]T, 0, len(left)+len(right)), left...), right...)
}

func joined[T any](left []T, right Value) Value {
	tail, _ := nativeItems[T](right)
	return arrayOf(appended(left, tail))
}

// uniqueItems is an array's items, in order, each once: a native array's by
// a set — a float's zero and negative zero are one key, as they are one
// value, and a NaN, equal to nothing, is kept every time — an amount's and
// a record of scalars' by a set of keys that are equal when they are, and
// any other's by Equal. Money meets first (money_meet.go): an array of two
// currencies has no answer.
func uniqueItems(_ context.Context, args []Value) (Value, error) {
	if err := meetValue(args[0]); err != nil {
		return Value{}, err
	}
	switch box := args[0].box.(type) {
	case *bool:
		return arrayOf(distinctOf(itemsAt(box, args[0].i))), nil
	case *int64:
		return arrayOf(distinctOf(itemsAt(box, args[0].i))), nil
	case *float64:
		return arrayOf(distinctOf(itemsAt(box, args[0].i))), nil
	case *string:
		return arrayOf(distinctOf(itemsAt(box, args[0].i))), nil
	case *money.Money:
		return arrayOf(distinctAmounts(itemsAt(box, args[0].i))), nil
	}
	items, _ := args[0].Array()
	if out, keyed := distinctKeyed(items); keyed {
		return packArray(args[0].elemType(), out), nil
	}
	out := make([]Value, 0, len(items))
	for _, item := range items {
		if !slices.ContainsFunc(out, item.Equal) {
			out = append(out, item)
		}
	}
	return packArray(args[0].elemType(), out), nil
}

// distinctAmounts is distinctOf for amounts that meet: in one currency, or
// the currency-less zero, which equals that currency's zero — so two are
// one amount when their minor units are.
func distinctAmounts(amounts []money.Money) []money.Money {
	out, seen := []money.Money{}, make(map[int64]bool, len(amounts))
	for _, amount := range amounts {
		if !seen[amount.Minor()] {
			seen[amount.Minor()] = true
			out = append(out, amount)
		}
	}
	return out
}

// distinctKeyed is the items each once, by itemKey, and true; false for
// items it cannot key, which are left to Equal. An item equal to nothing is
// kept each time it comes, as Equal would keep it.
func distinctKeyed(items []Value) ([]Value, bool) {
	out, seen := []Value{}, make(map[string]bool, len(items))
	var key []byte
	for _, item := range items {
		var state keyState
		switch key, state = itemKey(item, key[:0]); state {
		case keyNone:
			return nil, false
		case keyAlone:
			out = append(out, item)
		case keyFound:
			if !seen[string(key)] {
				seen[string(key)] = true
				out = append(out, item)
			}
		}
	}
	return out, true
}

// keyState is what itemKey makes of an item.
type keyState uint8

const (
	// keyFound is a key two items of one type share exactly when they are
	// Equal.
	keyFound keyState = iota
	// keyAlone is an item Equal to nothing, itself neither: a NaN or a
	// handle, or a record, array or dictionary holding one. It has no key
	// to share, and needs none: a set keeps it each time, and finds it
	// nowhere.
	keyAlone
	// keyNone is an item with no key, exact money or a ratio: Equal decides
	// it.
	keyNone
)

// itemKey appends item to key as bytes two items of one type share exactly
// when they are Equal: a float's zeros as one, an amount by its minor units —
// its currency is the place's one, or none for a zero — a string by its
// length then itself, a rate by its pair and its ratio, a time, a duration
// and a currency by what Equal reads of them, a record field by field, an
// array by its length then item by item and a dictionary by its size then
// its entries in key order, each written whole before the next. A ratio has
// no key yet: its zero is written two ways.
func itemKey(item Value, key []byte) ([]byte, keyState) {
	key = append(key, byte(item.kind))
	switch {
	case item.kind == HandleKind, item.kind == FloatKind && math.IsNaN(item.f):
		return key, keyAlone
	case item.kind == BoolKind && item.b:
		key = append(key, 1)
	case item.kind == BoolKind:
		key = append(key, 0)
	case item.kind == IntKind, item.kind == TimeKind, item.kind == DurationKind:
		key = strconv.AppendInt(key, item.i, 10)
	case item.kind == FloatKind:
		key = strconv.AppendUint(key, math.Float64bits(item.f+0), 16) // -0 + 0 is 0
	case item.kind == StringKind, item.kind == CurrencyKind:
		key = append(strconv.AppendInt(key, int64(len(item.s)), 10), ':')
		key = append(key, item.s...)
	case item.kind == MoneyKind && !IsExact(item):
		key = strconv.AppendInt(key, item.i, 10)
	case item.kind == FxRateKind:
		// A rate is its pair and its ratio, as == has two equal.
		rate, _ := item.FxRate()
		num, den := money.RatioParts(ratioFrom(item))
		key = append(append(append(key, rate.Base()...), '/'), rate.Quote()...)
		key = strconv.AppendInt(append(strconv.AppendInt(append(key, ':'), num, 10), '/'), den, 10)
	case item.kind == RecordKind || item.kind == ArrayKind:
		return partsKey(item, key)
	case item.kind == DictKind:
		return entriesKey(item, key)
	default:
		return key, keyNone
	}
	return append(key, '|'), keyFound
}

// entriesKey is itemKey of a dictionary: its size, then each key and its
// value in key order — two are equal when they hold the same entries,
// whatever order their maps keep them in.
func entriesKey(item Value, key []byte) ([]byte, keyState) {
	keys := item.keys()
	key = append(strconv.AppendInt(key, int64(len(keys)), 10), ':')
	for _, name := range keys {
		key = append(append(strconv.AppendInt(key, int64(len(name)), 10), ':'), name...)
		entry, _ := item.lookup(name)
		var state keyState
		if key, state = itemKey(entry, key); state != keyFound {
			return key, state
		}
	}
	return append(key, '|'), keyFound
}

// partsKey is itemKey of a record or an array: its fields, or its length and
// its items. A part equal to nothing makes the whole so, as Equal finds it
// unequal there whatever the other parts are.
func partsKey(item Value, key []byte) ([]byte, keyState) {
	record, isRecord := item.box.(*recordValue)
	n := item.length()
	if isRecord {
		n = len(record.fields)
	} else {
		key = append(strconv.AppendInt(key, int64(n), 10), ':')
	}
	for i := range n {
		var part Value
		if isRecord {
			part = record.fields[i]
		} else {
			part = item.at(i)
		}
		var state keyState
		if key, state = itemKey(part, key); state != keyFound {
			return key, state
		}
	}
	return append(key, '|'), keyFound
}

func distinctOf[T comparable](items []T) []T {
	out, seen := []T{}, make(map[T]bool, len(items))
	for _, item := range items {
		if !seen[item] {
			seen[item] = true
			out = append(out, item)
		}
	}
	return out
}

// flattenItems splices each inner array into one of their items' type.
func flattenItems(_ context.Context, args []Value) (Value, error) {
	inners, _ := args[0].Array()
	total := 0
	for _, inner := range inners {
		total += inner.length()
	}
	elem, _ := args[0].elemType().Elem()
	builder := newArrayBuilder(elem, total)
	for _, inner := range inners {
		builder.addAll(inner)
	}
	return builder.finish(), nil
}

// sortSpecs cover both directions of a plain sort; sorting by a separate
// array of keys is sort_by (keyedSpecs).
func sortSpecs() []FunctionSpec {
	return slices.Concat(libEach("sort", Doc{
		Label: "排序", Category: "数组",
		Description: "按自然顺序升序排列（数值按大小，字符串按 UTF-8 字节序）。要按别的键排，先用推导式算出键。",
		Params:      []string{"数组"}, Result: "升序数组",
	}, sorter[int64](false, nil), sorter(false, hasNegativeZero), sorter[string](false, nil)), libEach("sort_desc", Doc{
		Label: "降序排序", Category: "数组",
		Description: "按自然顺序降序排列，省得写 reverse(sort(xs))。",
		Params:      []string{"数组"}, Result: "降序数组",
	}, sorter[int64](true, nil), sorter(true, hasNegativeZero), sorter[string](true, nil)))
}

// sorter sorts a copy: a Value's backing is read-only, so sorting in place
// would edit the caller's array. The sort is stable. Where no two equal
// items can be told apart — ints, strings, floats without a -0 among them,
// which distinct, when it is given, looks for — the order among equal ones
// cannot show, and the faster sort gives the same array.
func sorter[T cmp.Ordered](descending bool, distinct func([]T) bool) func([]T) []T {
	return func(items []T) []T {
		out := append([]T(nil), items...)
		switch {
		case distinct != nil && distinct(out):
			sortStable(out, descending)
		case descending:
			slices.Sort(out)
			slices.Reverse(out)
		default:
			slices.Sort(out)
		}
		return out
	}
}

func sortStable[T cmp.Ordered](items []T, descending bool) {
	if descending {
		slices.SortStableFunc(items, func(a, b T) int { return cmp.Compare(b, a) })
	} else {
		slices.SortStableFunc(items, cmp.Compare[T])
	}
}

// hasNegativeZero reports a -0 among floats: it equals 0 and still differs
// from it, the one such pair a float array can hold.
func hasNegativeZero(items []float64) bool {
	return slices.ContainsFunc(items, func(f float64) bool { return f == 0 && math.Signbit(f) })
}
