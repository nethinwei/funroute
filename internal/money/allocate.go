package money

import (
	"cmp"
	"fmt"
	"slices"
)

// AllocationStrategy says where the units go that rounding every share down
// leaves over: fewer of them than there are shares of positive weight, and
// never to a share of weight zero. The zero value is the largest remainder.
type AllocationStrategy uint8

const (
	// AllocateLargestRemainder gives them to the shares rounding took the
	// most from, the larger weight on a tie, then the earlier.
	AllocateLargestRemainder AllocationStrategy = iota
	// AllocateLargestWeight gives them to the largest weights, the earlier on
	// a tie.
	AllocateLargestWeight
	// AllocateInOrder gives them one each from the first share on.
	AllocateInOrder
	// AllocateReverseOrder gives them one each from the last share back.
	AllocateReverseOrder
	// AllocateAllFirst gives them all to the first share.
	AllocateAllFirst
	// AllocateAllLast gives them all to the last share.
	AllocateAllLast
)

var allocationNames = [...]string{"largest_remainder", "largest_weight", "in_order", "reverse_order", "all_first", "all_last"}

// AllocationStrategies lists every strategy by name, in declaration order:
// the members of the allocation enum a rule writes @last with.
func AllocationStrategies() []string { return append([]string(nil), allocationNames[:]...) }

func (a AllocationStrategy) String() string { return nameOf(allocationNames[:], a) }

// ParseAllocation reads a strategy by name.
func ParseAllocation(name string) (AllocationStrategy, error) {
	if strategy, ok := valueOf[AllocationStrategy](allocationNames[:], name); ok {
		return strategy, nil
	}
	return 0, fmt.Errorf("%w: unknown allocation strategy %q", ErrArithmetic, name)
}

func (a AllocationStrategy) MarshalText() ([]byte, error) {
	if a.String() == "invalid" {
		return nil, fmt.Errorf("invalid allocation strategy %d", a)
	}
	return []byte(a.String()), nil
}

func (a *AllocationStrategy) UnmarshalText(text []byte) error {
	parsed, err := ParseAllocation(string(text))
	if err != nil {
		return err
	}
	*a = parsed
	return nil
}

// Allocate splits m by integer weights without losing a unit, by the largest
// remainder: AllocateBy with AllocateLargestRemainder.
func (m Money) Allocate(weights ...int64) ([]Money, error) {
	return m.AllocateBy(AllocateLargestRemainder, weights...)
}

// AllocateBy splits m by integer weights without losing a unit: each share
// is its weight's part rounded toward zero, and the units left go where the
// strategy says. The split of -m is the split of m negated.
func (m Money) AllocateBy(strategy AllocationStrategy, weights ...int64) ([]Money, error) {
	if strategy.String() == "invalid" {
		return nil, fmt.Errorf("%w: allocation strategy %d is not one", ErrArithmetic, strategy)
	}
	total, err := allocationTotal(weights)
	if err != nil {
		return nil, err
	}
	magnitudes := make([]uint64, len(weights))
	remainders := make([]uint64, len(weights))
	left := magnitude(m.minor)
	for i, weight := range weights {
		part, rest, err := mulDivParts(magnitude(m.minor), uint64(weight), uint64(total))
		if err != nil {
			return nil, err
		}
		magnitudes[i], remainders[i] = part, rest
		left -= part
	}
	for _, i := range receivers(strategy, weights, remainders, int(left)) {
		magnitudes[i]++
	}
	return signedShares(m, magnitudes)
}

// signedShares is the magnitudes as shares of m, with m's sign.
func signedShares(m Money, magnitudes []uint64) ([]Money, error) {
	shares := make([]Money, len(magnitudes))
	for i, part := range magnitudes {
		minor, err := signed(part, m.minor < 0)
		if err != nil {
			return nil, err
		}
		shares[i] = Money{currency: m.currency, minor: minor}
	}
	return shares, nil
}

// receivers lists the share each of left units goes to under strategy,
// among the shares of positive weight.
func receivers(strategy AllocationStrategy, weights []int64, remainders []uint64, left int) []int {
	var positive []int
	for i, weight := range weights {
		if weight > 0 {
			positive = append(positive, i)
		}
	}
	switch strategy {
	case AllocateLargestRemainder:
		return largestRemainders(positive, weights, remainders)[:left]
	case AllocateLargestWeight:
		slices.SortStableFunc(positive, func(a, b int) int { return cmp.Compare(weights[b], weights[a]) })
	case AllocateReverseOrder:
		slices.Reverse(positive)
	case AllocateAllFirst:
		return slices.Repeat(positive[:1], left)
	case AllocateAllLast:
		return slices.Repeat(positive[len(positive)-1:], left)
	}
	return positive[:left]
}

// allocationTotal is the sum of weights that are none of them negative and
// not all zero.
func allocationTotal(weights []int64) (int64, error) {
	total := int64(0)
	for _, weight := range weights {
		if weight < 0 {
			return 0, fmt.Errorf("%w: allocate weights cannot be negative, got %d", ErrArithmetic, weight)
		}
		sum, err := addInt64(total, weight, 1)
		if err != nil {
			return 0, err
		}
		total = sum
	}
	if total == 0 {
		return 0, fmt.Errorf("%w: allocate needs a positive weight", ErrArithmetic)
	}
	return total, nil
}

// largestRemainders orders the shares of positive weight by what rounding
// down took from them, most first: the remainders share the total as their
// denominator, so they compare as they are. Ties go to the larger weight,
// then the earlier share.
func largestRemainders(positive []int, weights []int64, remainders []uint64) []int {
	slices.SortStableFunc(positive, func(a, b int) int {
		if byRemainder := cmp.Compare(remainders[b], remainders[a]); byRemainder != 0 {
			return byRemainder
		}
		return cmp.Compare(weights[b], weights[a])
	})
	return positive
}

// maxAllocation bounds Split, and allocate(m, n) in a rule, the way range is
// bounded: a count of shares comes from the input and makes a container.
const maxAllocation = 10_000

// Split is m in n equal shares, the first ones a unit larger when it does not
// divide evenly; n is 1 to 10000.
func (m Money) Split(n int) ([]Money, error) {
	return m.SplitBy(AllocateLargestRemainder, n)
}

// SplitBy is m in n equal shares, the units left over going where the
// strategy says: with equal weights the largest remainder and in order both
// start at the first share, reverse order and all last at the last.
func (m Money) SplitBy(strategy AllocationStrategy, n int) ([]Money, error) {
	if n <= 0 || n > maxAllocation {
		return nil, fmt.Errorf("%w: allocate needs 1 to %d shares, got %d", ErrArithmetic, maxAllocation, n)
	}
	weights := make([]int64, n)
	for i := range weights {
		weights[i] = 1
	}
	return m.AllocateBy(strategy, weights...)
}
