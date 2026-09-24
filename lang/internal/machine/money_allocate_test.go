package machine

import (
	"math"
	"math/big"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"
)

func TestAllocateNeverLosesAUnit(t *testing.T) {
	t.Parallel()
	shares, err := usd(1001).Allocate(2, 1, 1)
	if err != nil || !slices.Equal(shares, []Money{usd(501), usd(250), usd(250)}) {
		t.Fatalf("Allocate(USD 10.01, 2, 1, 1) = %v, %v", shares, err)
	}
	shares, err = usd(-1000).Split(3)
	if err != nil || !slices.Equal(shares, []Money{usd(-334), usd(-333), usd(-333)}) {
		t.Fatalf("Split(USD -10.00, 3) = %v, %v", shares, err)
	}
	if _, err := usd(1).Allocate(0, 0); err == nil {
		t.Fatal("allocating by zero weights succeeded")
	}
}

// The units rounding down leaves go one each to the shares it took the most
// from — the largest remainder — then to the larger weight, then the
// earlier; never to a zero weight, and a negative amount is split as its
// magnitude is, negated. The first cases are where the largest remainder
// and the largest weight part ways.
func TestAllocateHandsOutTheLargestRemainders(t *testing.T) {
	t.Parallel()
	third := int64(math.MinInt64 / 3)
	for name, test := range map[string]struct {
		m       Money
		weights []int64
		want    []int64
	}{
		"the smaller share lost more": {usd(8), []int64{6, 3, 1}, []int64{5, 2, 1}},
		"two fifths of a unit":        {usd(10), []int64{3, 3, 1}, []int64{4, 4, 2}},
		"negated as its magnitude":    {usd(-8), []int64{6, 3, 1}, []int64{-5, -2, -1}},
		"even thirds":                 {usd(100), []int64{1, 1, 1}, []int64{34, 33, 33}},
		"the larger weight first":     {usd(100), []int64{1, 2}, []int64{33, 67}},
		"a tie goes to the earlier":   {usd(100), []int64{3, 1, 3}, []int64{43, 14, 43}},
		"a zero weight gets nothing":  {usd(100), []int64{0, 1}, []int64{0, 100}},
		"a unit to the largest":       {usd(1), []int64{1, 2, 2}, []int64{0, 1, 0}},
		"one weight takes it all":     {usd(-7), []int64{5}, []int64{-7}},
		"negative units":              {usd(-3), []int64{1, 1}, []int64{-2, -1}},
		"zeros around one weight":     {usd(5), []int64{0, 1, 0}, []int64{0, 5, 0}},
		"the largest amount":          {usd(math.MaxInt64), []int64{math.MaxInt64}, []int64{math.MaxInt64}},
		"weights near the limit":      {usd(1), []int64{math.MaxInt64 / 2, math.MaxInt64 / 2}, []int64{1, 0}},
		"the smallest in halves":      {usd(math.MinInt64), []int64{1, 1}, []int64{math.MinInt64 / 2, math.MinInt64 / 2}},
		"the smallest in thirds":      {usd(math.MinInt64), []int64{1, 1, 1}, []int64{third - 1, third - 1, third}},
		"the largest in thirds":       {usd(math.MaxInt64), []int64{1, 1, 1}, []int64{math.MaxInt64/3 + 1, math.MaxInt64 / 3, math.MaxInt64 / 3}},
		"nothing to share":            {usd(0), []int64{2, 1}, []int64{0, 0}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			shares, err := test.m.Allocate(test.weights...)
			if err != nil || !slices.Equal(minors(shares), test.want) || !sharesInCurrency(shares, test.m.currency) {
				t.Fatalf("%v.Allocate(%v) = %v, %v, want %v in %s", test.m, test.weights, shares, err, test.want, test.m.currency)
			}
		})
	}
	shares, err := Money{}.Split(2)
	if err != nil || !slices.Equal(shares, []Money{{}, {}}) {
		t.Fatalf("Money{}.Split(2) = %v, %v, want two currency-less zeros", shares, err)
	}
}

func minors(shares []Money) []int64 {
	out := make([]int64, len(shares))
	for i, share := range shares {
		out[i] = share.minor
	}
	return out
}

func sharesInCurrency(shares []Money, currency string) bool {
	for _, share := range shares {
		if share.currency != currency {
			return false
		}
	}
	return true
}

func TestAllocateRefusesWeightsThatShareNothing(t *testing.T) {
	t.Parallel()
	for name, test := range map[string]struct {
		weights []int64
		want    string
	}{
		"no weights":         {nil, "positive weight"},
		"only zeros":         {[]int64{0, 0, 0}, "positive weight"},
		"a negative weight":  {[]int64{-1, 2}, "negative"},
		"a negative last":    {[]int64{3, math.MinInt64}, "negative"},
		"weights past int64": {[]int64{math.MaxInt64, 1}, "overflows"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if shares, err := usd(100).Allocate(test.weights...); err == nil || !strings.Contains(err.Error(), test.want) || shares != nil {
				t.Fatalf("Allocate(%v) = %v, %v, want an error containing %q", test.weights, shares, err, test.want)
			}
		})
	}
	for _, n := range []int{0, -1, math.MinInt} {
		if shares, err := usd(100).Split(n); err == nil || shares != nil {
			t.Fatalf("Split(%d) = %v, %v, want an error", n, shares, err)
		}
	}
}

// Whatever the amount and the weights, the shares add up to the amount, each
// is within a unit of its exact part on the amount's side of zero, and a
// zero weight gets nothing.
func TestAllocateNeverLosesAUnitWhateverTheWeights(t *testing.T) {
	t.Parallel()
	random := rand.New(rand.NewPCG(7, 8))
	for range 3000 {
		m := usd(random.Int64() >> random.IntN(64))
		if random.IntN(2) == 0 {
			m.minor = -m.minor - int64(random.IntN(2)) // reaches MinInt64 too
		}
		weights := make([]int64, 1+random.IntN(8))
		for i := range weights {
			weights[i] = random.Int64N(1<<uint(1+random.IntN(40))) * int64(random.IntN(3)%2)
		}
		weights[random.IntN(len(weights))] = 1 + random.Int64N(1<<20)
		shares, err := m.Allocate(weights...)
		if err != nil {
			t.Fatalf("%v.Allocate(%v) error = %v", m, weights, err)
		}
		assertFairShares(t, m, weights, shares)
	}
}

// Equal shares differ by at most one unit, the larger ones first.
func TestSplitSharesDifferByAUnitAtMost(t *testing.T) {
	t.Parallel()
	random := rand.New(rand.NewPCG(9, 10))
	for range 1000 {
		m, n := usd(random.Int64()-math.MaxInt64/2), 1+random.IntN(50)
		shares, err := m.Split(n)
		if err != nil || len(shares) != n {
			t.Fatalf("%v.Split(%d) = %v, %v", m, n, shares, err)
		}
		for i := 1; i < n; i++ {
			step := shares[i-1].minor - shares[i].minor
			if step*int64(m.Sign()) < 0 || step*int64(m.Sign()) > 1 || (shares[0].minor-shares[n-1].minor)*int64(m.Sign()) > 1 {
				t.Fatalf("%v.Split(%d) = %v, want shares a unit apart at most, larger first", m, n, shares)
			}
		}
	}
}

// Allocate agrees with the largest remainder method worked out in exact
// rationals, over amounts and weights across int64.
func TestAllocateAgreesWithExactLargestRemainders(t *testing.T) {
	t.Parallel()
	random := rand.New(rand.NewPCG(7, 11))
	for range 2000 {
		weights := make([]int64, 1+random.IntN(6))
		for i := range weights {
			weights[i] = random.Int64N(1 << uint(1+random.IntN(40)))
		}
		weights[random.IntN(len(weights))]++
		m := usd(random.Int64() >> uint(random.IntN(63)))
		if random.IntN(2) == 0 {
			m.minor = -m.minor
		}
		shares, err := m.Allocate(weights...)
		if want := exactLargestRemainders(m.minor, weights); err != nil || !slices.Equal(minors(shares), want) {
			t.Fatalf("%v.Allocate(%v) = %v, %v, want %v", m, weights, minors(shares), err, want)
		}
	}
}

// exactLargestRemainders is the largest remainder method in big rationals.
func exactLargestRemainders(amount int64, weights []int64) []int64 {
	total := big.NewInt(0)
	for _, weight := range weights {
		total.Add(total, big.NewInt(weight))
	}
	magnitude := new(big.Int).Abs(big.NewInt(amount))
	parts, rests := make([]*big.Int, len(weights)), make([]*big.Int, len(weights))
	left := new(big.Int).Set(magnitude)
	for i, weight := range weights {
		parts[i], rests[i] = new(big.Int).QuoRem(new(big.Int).Mul(magnitude, big.NewInt(weight)), total, new(big.Int))
		left.Sub(left, parts[i])
	}
	var order []int
	for i, weight := range weights {
		if weight > 0 {
			order = append(order, i)
		}
	}
	slices.SortStableFunc(order, func(a, b int) int {
		if byRest := rests[b].Cmp(rests[a]); byRest != 0 {
			return byRest
		}
		return big.NewInt(weights[b]).Cmp(big.NewInt(weights[a]))
	})
	out := make([]int64, len(weights))
	for k, i := range order {
		if int64(k) < left.Int64() {
			parts[i].Add(parts[i], big.NewInt(1))
		}
	}
	for i, part := range parts {
		if amount < 0 {
			part.Neg(part)
		}
		out[i] = part.Int64()
	}
	return out
}

// Each strategy hands the units rounding leaves to the shares it names, and
// only to shares of positive weight; every one of them keeps the total and
// splits a negative amount as its magnitude, negated.
func TestAllocateByEachStrategy(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		strategy AllocationStrategy
		m        Money
		weights  []int64
		want     []int64
	}{
		{AllocateLargestRemainder, usd(8), []int64{6, 3, 1}, []int64{5, 2, 1}},
		{AllocateLargestWeight, usd(8), []int64{6, 3, 1}, []int64{5, 3, 0}},
		{AllocateInOrder, usd(8), []int64{6, 3, 1}, []int64{5, 3, 0}},
		{AllocateInOrder, usd(8), []int64{0, 6, 3, 1}, []int64{0, 5, 3, 0}},
		{AllocateReverseOrder, usd(8), []int64{6, 3, 1}, []int64{4, 3, 1}},
		{AllocateReverseOrder, usd(8), []int64{6, 3, 1, 0}, []int64{4, 3, 1, 0}},
		{AllocateAllFirst, usd(10), []int64{1, 1, 1, 1}, []int64{4, 2, 2, 2}},
		{AllocateAllLast, usd(10), []int64{1, 1, 1, 1}, []int64{2, 2, 2, 4}},
		{AllocateAllLast, usd(10), []int64{1, 1, 1, 1, 0}, []int64{2, 2, 2, 4, 0}},
		{AllocateAllLast, usd(-10), []int64{1, 1, 1, 1}, []int64{-2, -2, -2, -4}},
		{AllocateAllFirst, usd(9), []int64{1, 2}, []int64{3, 6}},
	} {
		shares, err := test.m.AllocateBy(test.strategy, test.weights...)
		if err != nil || !slices.Equal(minors(shares), test.want) {
			t.Errorf("%v.AllocateBy(%s, %v) = %v, %v, want %v", test.m, test.strategy, test.weights, minors(shares), err, test.want)
		}
	}
	if _, err := usd(1).AllocateBy(AllocationStrategy(99), 1); err == nil {
		t.Fatal("AllocateBy with no strategy succeeded, want an error")
	}
}

// Every strategy keeps the total, whatever the amount and the weights.
func TestEveryStrategyKeepsTheTotal(t *testing.T) {
	t.Parallel()
	random := rand.New(rand.NewPCG(3, 5))
	for range 500 {
		weights := make([]int64, 1+random.IntN(5))
		for i := range weights {
			weights[i] = random.Int64N(1000)
		}
		weights[0]++
		m := usd(random.Int64N(1_000_000) - 500_000)
		for strategy := range AllocationStrategy(len(allocationNames)) {
			shares, err := m.AllocateBy(strategy, weights...)
			total := int64(0)
			for _, share := range shares {
				total += share.minor
			}
			if err != nil || total != m.minor {
				t.Fatalf("%v.AllocateBy(%s, %v) = %v, %v: total %d, want %d", m, strategy, weights, minors(shares), err, total, m.minor)
			}
		}
	}
}

// A strategy reads and writes as its name.
func TestAllocationStrategiesAreNamed(t *testing.T) {
	t.Parallel()
	for _, name := range AllocationStrategies() {
		strategy, err := ParseAllocation(name)
		text, _ := strategy.MarshalText()
		if err != nil || string(text) != name {
			t.Errorf("ParseAllocation(%s) = %v, %v, marshals %s", name, strategy, err, text)
		}
	}
	if _, err := ParseAllocation("round_robin"); err == nil {
		t.Fatal("ParseAllocation(round_robin) = nil error")
	}
	var strategy AllocationStrategy
	if err := strategy.UnmarshalText([]byte("all_last")); err != nil || strategy != AllocateAllLast {
		t.Fatalf("UnmarshalText(all_last) = %v, %v", strategy, err)
	}
}
