package kit

import "slices"

// Map is f of each of xs, in their order. It is never nil, even for no xs.
func Map[T, U any](xs []T, f func(T) U) []U {
	out := make([]U, len(xs))
	for i, x := range xs {
		out[i] = f(x)
	}
	return out
}

// Identity is x itself, for a projection or a key that is the item.
func Identity[T any](x T) T { return x }

// Repeated is the first key two of items share, in their order.
func Repeated[T any](items []T, key func(T) string) (string, bool) {
	seen := make(map[string]bool, len(items))
	for _, item := range items {
		k := key(item)
		if seen[k] {
			return k, true
		}
		seen[k] = true
	}
	return "", false
}

// Reach is items long enough to hold index i, the items it did not hold
// zero: its capacity grows by doubling, so filling a table by increasing
// index costs a logarithmic number of allocations, not one a step.
func Reach[T any](items []T, i int) []T {
	if i < len(items) {
		return items
	}
	n := len(items)
	items = slices.Grow(items, i+1-n)[:i+1]
	clear(items[n:])
	return items
}

// SortedKeys is a map's keys in order, in one allocation. It is never nil.
func SortedKeys[T any](entries map[string]T) []string {
	keys := make([]string, 0, len(entries))
	for key := range entries {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}
