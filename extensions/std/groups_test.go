package std_test

import (
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
