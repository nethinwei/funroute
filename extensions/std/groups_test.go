package std_test

import (
	"errors"
	"math"
	"slices"
	"strings"
	"testing"

	"github.com/nethinwei/funroute"
)

// Grouping and running totals: the two list operations a single fold cannot do.
func TestGroupingAndRunningTotals(t *testing.T) {
	t.Parallel()
	specs := []funroute.ArgSpec{
		{Name: "amounts", Type: funroute.ArrayOf(funroute.IntType)},
		{Name: "channels", Type: funroute.ArrayOf(funroute.StringType)},
	}
	args := map[string]any{"amounts": []any{100, 200, 300}, "channels": []any{"adyen", "stripe", "adyen"}}
	totals, err := run(t, `{k: sum(v) for k, v in group_by(amounts, channels)}`, args, specs...)
	if err != nil {
		t.Fatal(err)
	}
	// dict<int> hands back its backing, which is a map[string]int64.
	grouped, _ := totals.(map[string]int64)
	if grouped["adyen"] != 400 || grouped["stripe"] != 200 {
		t.Fatalf("totals = %v, want map[adyen:400 stripe:200]", totals)
	}
	over, err := run(t, `first([i for i in indices(amounts) if cumsum(amounts)[i] > 250])`, args, specs...)
	if err != nil || over != int64(1) {
		t.Fatalf("the first transaction over the limit = %v, %v; want 1", over, err)
	}
}

// rank orders as sort does, NaN first, so every NaN shares the first rank
// and the numbers come after it; ties share a rank and the next skips.
func TestRankOrdersAsSortDoes(t *testing.T) {
	t.Parallel()
	specs := []funroute.ArgSpec{{Name: "xs", Type: funroute.ArrayOf(funroute.FloatType)}}
	ranks, err := run(t, `rank(xs)`, map[string]any{"xs": []float64{1, 2, math.NaN(), 1, math.NaN()}}, specs...)
	if got, isInts := ranks.([]int64); err != nil || !isInts || !slices.Equal(got, []int64{3, 5, 1, 3, 1}) {
		t.Fatalf("rank([1, 2, NaN, 1, NaN]) = %v, %v; want [3 5 1 3 1]", ranks, err)
	}
}

// A running total that overflows says it was the running total.
func TestCumsumNamesItsOverflow(t *testing.T) {
	t.Parallel()
	specs := []funroute.ArgSpec{{Name: "xs", Type: funroute.ArrayOf(funroute.IntType)}}
	_, err := run(t, `cumsum(xs)`, map[string]any{"xs": []int64{math.MaxInt64, 1}}, specs...)
	if !errors.Is(err, funroute.ErrArithmetic) || !strings.Contains(err.Error(), "overflow in cumsum") {
		t.Fatalf("cumsum([MaxInt64, 1]) error = %v, want an overflow in cumsum", err)
	}
}
