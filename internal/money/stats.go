package money

import (
	"cmp"
	"fmt"
	"slices"
)

// The statistics of amounts that need a rounding: std's avg and median are
// these, so a host averaging amounts gets a rule's answer.

// AverageMoney is the mean of amounts in one currency, rounded once by mode.
// An average lies between the smallest and the largest amount, so it always
// fits even where the total would not.
func AverageMoney(amounts []Money, mode Rounding) (Money, error) {
	unit, err := oneCurrency(amounts)
	if err != nil {
		return Money{}, err
	}
	minor, err := averageMinor(amounts, mode)
	return unit.of(minor), err
}

// MedianMoney is the median of amounts in one currency; of an even count,
// the mean of the middle two rounded by mode. The mode is checked whatever
// the count, as AverageMoney checks it.
func MedianMoney(amounts []Money, mode Rounding) (Money, error) {
	unit, err := oneCurrency(amounts)
	if err != nil {
		return Money{}, err
	}
	if err := checkMode(mode); err != nil {
		return Money{}, err
	}
	sorted := slices.Clone(amounts)
	slices.SortFunc(sorted, func(a, b Money) int { return cmp.Compare(a.minor, b.minor) })
	middle := len(sorted) / 2
	if len(sorted)%2 == 1 {
		return unit.of(sorted[middle].minor), nil
	}
	minor, err := averageMinor(sorted[middle-1:middle+1], mode)
	return unit.of(minor), err
}

// oneCurrency is the currency and places a non-empty list of amounts is in,
// as an amount of nothing; the currency-less zero goes with any.
func oneCurrency(amounts []Money) (Money, error) {
	if len(amounts) == 0 {
		return Money{}, fmt.Errorf("%w: an empty array has no average", ErrDomain)
	}
	var unit Money
	for _, amount := range amounts {
		next, err := unit.meet(amount)
		if err != nil {
			return Money{}, err
		}
		unit = next
	}
	return unit, nil
}

// averageMinor divides first and carries the remainders: the value is
// q + r/n with r/n the same sign as q.
func averageMinor(amounts []Money, mode Rounding) (int64, error) {
	n := int64(len(amounts))
	var q, r int64
	for _, amount := range amounts {
		q, r = q+amount.minor/n, r+amount.minor%n
		q, r = q+r/n, r%n
	}
	switch {
	case q > 0 && r < 0:
		q, r = q-1, r+n
	case q < 0 && r > 0:
		q, r = q+1, r-n
	}
	// With the fraction on q's side of zero every mode rounds it alone,
	// but for a half_even tie, which goes to whichever neighbour is even.
	step, err := mulDivRound(r, 1, n, mode)
	if mode == RoundHalfEven && 2*max(r, -r) == n && q&1 != 0 {
		step = r / max(r, -r)
	}
	return q + step, err
}
