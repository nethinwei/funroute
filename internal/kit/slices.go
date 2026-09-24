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

// SortedKeys is a map's keys in order, in one allocation. It is never nil.
func SortedKeys[T any](entries map[string]T) []string {
	keys := make([]string, 0, len(entries))
	for key := range entries {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}
