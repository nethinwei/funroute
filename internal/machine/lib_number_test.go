package machine_test

import (
	"cmp"
	"errors"
	"math"
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/nethinwei/funroute/internal/compile"
	"github.com/nethinwei/funroute/internal/machine"
)

// The statistics and powers a rule reaches for when it judges an SLA or
// backs off a retry.
func TestStatisticsAndPowers(t *testing.T) {
	t.Parallel()
	latencies := compile.ArgSpec{Name: "latencies", Type: machine.ArrayOf(machine.IntType)}
	args := map[string]any{"latencies": []any{100, 200, 300, 400}}
	for _, test := range []struct {
		name, source string
		specs        []compile.ArgSpec
		args         map[string]any
		want         any
	}{
		{"中位分位", `percentile(latencies, 0.5)`, []compile.ArgSpec{latencies}, args, 250.0},
		{"最低分位", `percentile(latencies, 0.0)`, []compile.ArgSpec{latencies}, args, 100.0},
		{"最高分位", `percentile(latencies, 1.0)`, []compile.ArgSpec{latencies}, args, 400.0},
		{"标准差", `stddev([2, 4, 4, 4, 5, 5, 7, 9])`, nil, nil, 2.0},
		// Past 2^53 a float64 does not tell neighbouring ints apart; their
		// distances from the mean are taken in integers.
		{"大整数标准差", `stddev([9007199254740993, 9007199254740995])`, nil, nil, 1.0},
		{"更大整数标准差", `stddev([4611686018427387905, 4611686018427387907])`, nil, nil, 1.0},
		{"负整数标准差", `stddev([-9007199254740995, -9007199254740993, -9007199254740994])`, nil, nil, math.Sqrt(2.0 / 3)},
		{"负整数余数标准差", `stddev([-1, -2, -4])`, nil, nil, math.Sqrt(14.0 / 9)},
		{"整数两端标准差", `stddev([-9223372036854775807 - 1, 9223372036854775807])`, nil, nil, 9223372036854775807.5},
		{"整数幂", `pow(2, 10)`, nil, nil, int64(1024)},
		{"浮点幂", `pow(9.0, 0.5)`, nil, nil, 3.0},
		{"指数退避", `pow(2, 3) * 200`, nil, nil, int64(1600)},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := libRun(t, test.source, test.args, test.specs...)
			if err != nil {
				t.Fatalf("run %s: %v", test.source, err)
			}
			if got != test.want {
				t.Fatalf("%s = %v (%T), want %v", test.source, got, got, test.want)
			}
		})
	}
	for _, source := range []string{
		`percentile(latencies, 1.5)`,
		`pow(2, 0 - 1)`,
		`stddev([n for n in latencies if n > 999])`,
	} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			if _, err := libRun(t, source, args, latencies); err == nil {
				t.Fatalf("%s must fail", source)
			}
		})
	}
}

// An integer power squares its way up, so its work is the exponent's bit
// length: the largest exponent is as quick as a small one, folded or run,
// and a result past int64 is an error.
func TestIntegerPowerIsQuickAndExact(t *testing.T) {
	t.Parallel()
	exponent := compile.ArgSpec{Name: "n", Type: machine.IntType}
	for source, want := range map[string]int64{
		`pow(1, 9223372036854775807)`:  1,
		`pow(-1, 9223372036854775807)`: -1,
		`pow(0, 0)`:                    1,
		`pow(0, 9223372036854775807)`:  0,
		`pow(2, 62)`:                   4611686018427387904,
		`pow(-2, 63)`:                  -9223372036854775808,
		`pow(3, 5)`:                    243,
		`pow(1, n)`:                    1,
	} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			got, err := libRun(t, source, map[string]any{"n": int64(9223372036854775807)}, exponent)
			if err != nil || got != want {
				t.Fatalf("%s = %v, %v, want %d", source, got, err, want)
			}
		})
	}
	for _, source := range []string{`pow(2, 63)`, `pow(3, 40)`, `pow(-2, 64)`, `pow(2, n)`} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			if _, err := libRun(t, source, map[string]any{"n": int64(9223372036854775807)}, exponent); !errors.Is(err, machine.ErrArithmetic) {
				t.Fatalf("%s error = %v, want ErrArithmetic", source, err)
			}
		})
	}
}

// median and percentile select, rather than sort, the items they read, and
// answer as sorting does: NaN first, -0 before 0, sorted, reversed, all
// equal or random, bit for bit.
func TestMedianAndPercentileAnswerAsSorting(t *testing.T) {
	t.Parallel()
	random := rand.New(rand.NewPCG(9, 10))
	median, _ := machine.CoreRegistry().Resolve("median(array<float>)->float")
	percentile, _ := machine.CoreRegistry().Resolve("percentile(array<float>,float)->float")
	for range 3000 {
		items := shapedFloats(random)
		ratio := []float64{0, 0.5, 0.95, 1, random.Float64()}[random.IntN(5)]
		want, wantP := sortedAnswers(items, ratio)
		array, err := machine.ToValue(items)
		if err != nil {
			t.Fatal(err)
		}
		got, err := median.Eval(t.Context(), []machine.Value{array})
		gotP, errP := percentile.Eval(t.Context(), []machine.Value{array, machine.Float(ratio)})
		m, _ := got.Float()
		p, _ := gotP.Float()
		if err != nil || errP != nil || !sameFloat(m, want) || !sameFloat(p, wantP) {
			t.Fatalf("median, percentile(%v) of %v = %v, %v (%v, %v); want %v, %v", ratio, items, m, p, err, errP, want, wantP)
		}
	}
}

// The ints' median answers as slices.Sort's.
func TestTheMedianOfIntsAnswersAsSorting(t *testing.T) {
	t.Parallel()
	random := rand.New(rand.NewPCG(11, 12))
	median, _ := machine.CoreRegistry().Resolve("median(array<int>)->float")
	for range 3000 {
		ints := make([]int64, 1+random.IntN(200))
		for i := range ints {
			ints[i] = int64(random.IntN(50)) - 25
		}
		sorted := slices.Sorted(slices.Values(ints))
		middle := len(sorted) / 2
		want := float64(sorted[middle])
		if len(sorted)%2 == 0 {
			want = (float64(sorted[middle-1]) + float64(sorted[middle])) / 2
		}
		array, err := machine.ToValue(ints)
		if err != nil {
			t.Fatal(err)
		}
		got, err := median.Eval(t.Context(), []machine.Value{array})
		if m, _ := got.Float(); err != nil || m != want {
			t.Fatalf("median of %v = %v, %v; want %v", ints, m, err, want)
		}
	}
}

// shapedFloats is up to 200 small floats with NaNs, infinities and both
// zeros among them, sorted, reversed, all one, or as they came.
func shapedFloats(random *rand.Rand) []float64 {
	special := []float64{0, math.Copysign(0, -1), -1, 2.5, math.NaN(), math.Inf(1), math.Inf(-1)}
	items := make([]float64, 1+random.IntN(200))
	for i := range items {
		items[i] = float64(random.IntN(1 + random.IntN(100)))
		if random.IntN(4) == 0 {
			items[i] = special[random.IntN(len(special))]
		}
	}
	switch random.IntN(4) {
	case 0:
		slices.SortFunc(items, medianOrder)
	case 1:
		slices.SortFunc(items, medianOrder)
		slices.Reverse(items)
	case 2:
		items = slices.Repeat(items[:1], len(items))
	}
	return items
}

// sortedAnswers is the median and the percentile at ratio of items, read
// off them sorted.
func sortedAnswers(items []float64, ratio float64) (float64, float64) {
	sorted := slices.Clone(items)
	slices.SortFunc(sorted, medianOrder)
	middle := len(sorted) / 2
	median := sorted[middle]
	if len(sorted)%2 == 0 {
		median = (sorted[middle-1] + sorted[middle]) / 2
	}
	position := float64(ratio * float64(len(sorted)-1))
	lower, upper := int(math.Floor(position)), int(math.Ceil(position))
	if lower == upper {
		return median, sorted[lower]
	}
	weight := position - float64(lower)
	return median, float64(sorted[lower]*(1-weight)) + float64(sorted[upper]*weight)
}

// medianOrder is slices.Sort's order of floats, NaN first, with -0 before 0.
func medianOrder(a, b float64) int {
	if c := cmp.Compare(a, b); c != 0 || a != 0 {
		return c
	}
	return cmp.Compare(boolBit(!math.Signbit(a)), boolBit(!math.Signbit(b)))
}

func boolBit(b bool) int {
	if b {
		return 1
	}
	return 0
}

// sameFloat is whether two floats are the same bits, or both NaN.
func sameFloat(a, b float64) bool {
	return math.Float64bits(a) == math.Float64bits(b) || math.IsNaN(a) && math.IsNaN(b)
}
