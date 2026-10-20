package machine

import (
	"context"
	"slices"

	"github.com/nethinwei/funroute/internal/money"
)

// An allow-list and a block-list are what a rule holds candidates against:
// intersect keeps the ones a second list has, except the ones it has not.
// Each keeps the first list's order, an item once, by the equality == asks
// — money meets first (money_meet.go) — found by a set where the items have
// keys: a native array's own items, an amount's minor units, a record's
// recordKey. Anything else is compared item by item.
func setSpecs() []FunctionSpec {
	list := ArrayOf(TypeVar("T"))
	held := func(name, label, description, result string, eval EvalFunc) FunctionSpec {
		return FunctionSpec{
			Name: name, Params: []Type{list, list}, Result: list, Eval: eval,
			Doc: Doc{Label: label, Category: "数组", Description: description, Params: []string{"前一个", "后一个"}, Result: result},
		}
	}
	return []FunctionSpec{
		held("intersect", "交集", "两个数组里都有的元素，按第一个数组的顺序，重复只留一次。", "交集", intersectItems),
		held("except", "差集", "在第一个数组里、不在第二个数组里的元素，保持原顺序，重复只留一次。", "差集", exceptItems),
	}
}

func intersectItems(_ context.Context, args []Value) (Value, error) {
	return heldAgainst(args[0], args[1], true)
}

func exceptItems(_ context.Context, args []Value) (Value, error) {
	return heldAgainst(args[0], args[1], false)
}

// heldAgainst is array's items, in order, each once, that other has —
// shared — or has not.
func heldAgainst(array, other Value, shared bool) (Value, error) {
	if err := meetPair(array, other); err != nil {
		return Value{}, err
	}
	keep := keptAgainst(array, other, shared)
	return rearranged(array, len(keep), func(i int) int { return keep[i] }), nil
}

// keptAgainst is where the items heldAgainst keeps are in array.
func keptAgainst(array, other Value, shared bool) []int {
	switch items := array.box.(type) {
	case *bool:
		return keptBy(itemsAt(items, array.i), mustItems[bool](other), shared)
	case *int64:
		return keptBy(itemsAt(items, array.i), mustItems[int64](other), shared)
	case *float64:
		return keptBy(itemsAt(items, array.i), mustItems[float64](other), shared)
	case *string:
		return keptBy(itemsAt(items, array.i), mustItems[string](other), shared)
	case *money.Money:
		// Met, two amounts are one when their minor units are.
		return keptBy(minorsOf(itemsAt(items, array.i)), minorsOf(mustItems[money.Money](other)), shared)
	}
	if keys, keyed := recordKeys(array); keyed {
		if others, keyed := recordKeys(other); keyed {
			return keptBy(keys, others, shared)
		}
	}
	return keptByEqual(valuesOf(array), valuesOf(other), shared)
}

// mustItems is the native items of an array of T: one array's type is one
// backing, so the second list's is the first's.
func mustItems[T any](array Value) []T {
	items, _ := nativeItems[T](array)
	return items
}

func minorsOf(amounts []money.Money) []int64 {
	minors := make([]int64, len(amounts))
	for i, amount := range amounts {
		minors[i] = amount.Minor()
	}
	return minors
}

// recordKeys is the recordKey of each item of an array of records, and
// false when one has none.
func recordKeys(array Value) ([]string, bool) {
	items := valuesOf(array)
	keys := make([]string, len(items))
	var key []byte
	for i, item := range items {
		var keyed bool
		if key, keyed = recordKey(item, key[:0]); !keyed {
			return nil, false
		}
		keys[i] = string(key)
	}
	return keys, true
}

// keptBy is where in items the ones others has — or has not — first are,
// by key: a NaN, which no key equals, is never had, and kept each time.
func keptBy[K comparable](items, others []K, shared bool) []int {
	had := make(map[K]bool, len(others))
	for _, key := range others {
		had[key] = true
	}
	seen := make(map[K]bool, len(items))
	var keep []int
	for i, key := range items {
		if had[key] == shared && !seen[key] {
			seen[key] = true
			keep = append(keep, i)
		}
	}
	return keep
}

// keptByEqual is keptBy for items with no key, compared by Equal.
func keptByEqual(items, others []Value, shared bool) []int {
	var keep []int
	for i, item := range items {
		if slices.ContainsFunc(others, item.Equal) == shared && !slices.ContainsFunc(keep, func(at int) bool { return items[at].Equal(item) }) {
			keep = append(keep, i)
		}
	}
	return keep
}
