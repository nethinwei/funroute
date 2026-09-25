package std_test

import (
	"math"
	"testing"

	"github.com/nethinwei/funroute"
)

// The shortcuts that exist so a rule writer does not have to compose them:
// "the three cheapest", "descending", "the last three", "the difference from
// the previous one".
func TestConvenienceShortcuts(t *testing.T) {
	t.Parallel()
	channels := funroute.ArgSpec{Name: "channels", Type: funroute.ArrayOf(funroute.StringType)}
	fees := funroute.ArgSpec{Name: "fees", Type: funroute.ArrayOf(funroute.IntType)}
	allow := funroute.ArgSpec{Name: "allow", Type: funroute.ArrayOf(funroute.StringType)}
	specs := []funroute.ArgSpec{channels, fees, allow}
	args := map[string]any{
		"channels": []any{"adyen", "stripe", "pix"},
		"fees":     []any{30, 10, 20},
		"allow":    []any{"stripe", "pix"},
	}
	for _, test := range []struct {
		name, source string
		want         any
	}{
		{"最便宜的一个", `first(bottom_k(channels, fees, 1))`, "stripe"},
		{"最贵的一个", `first(top_k(channels, fees, 1))`, "adyen"},
		{"按费用降序", `first(sort_by_desc(channels, fees))`, "adyen"},
		{"降序排序", `first(sort_desc(fees))`, int64(30)},
		{"白名单交集", `len(intersect(channels, allow))`, int64(2)},
		{"黑名单差集", `first(except(channels, allow))`, "adyen"},
		{"滑动窗口", `len(windows(fees, 2))`, int64(2)},
		{"窗口内合计", `first([sum(w) for w in windows(fees, 2)])`, int64(40)},
		{"分批", `len(chunk(fees, 2))`, int64(2)},
		{"相邻差", `first(deltas(fees))`, int64(-20)},
		{"k 超过长度就取完", `len(bottom_k(channels, fees, 99))`, int64(3)},
		{"窗口比数组宽就没有", `len(windows(fees, 9))`, int64(0)},
		{"一项没有相邻差", `len(deltas([1]))`, int64(0)},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := run(t, test.source, args, specs...)
			if err != nil {
				t.Fatalf("run %s: %v", test.source, err)
			}
			if got != test.want {
				t.Fatalf("%s = %v (%T), want %v", test.source, got, got, test.want)
			}
		})
	}
	for _, source := range []string{`windows(fees, 0)`, `chunk(fees, 0)`, `bottom_k(channels, fees, 0 - 1)`} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			if _, err := run(t, source, args, specs...); err == nil {
				t.Fatalf("%s must fail", source)
			}
		})
	}
}

// A sort is stable where it can show: 0 and -0 are equal and still two
// values, and keep their order; anything else sorts as it does.
func TestSortKeepsEqualsThatDifferInOrder(t *testing.T) {
	t.Parallel()
	negative := math.Copysign(0, -1)
	xs := funroute.ArgSpec{Name: "xs", Type: funroute.ArrayOf(funroute.FloatType)}
	for _, test := range []struct {
		source string
		in     []float64
		signs  []bool
	}{
		{"sort(xs)", []float64{0, 1, negative, -1, 0}, []bool{true, false, true, false, false}},
		{"sort_desc(xs)", []float64{negative, 1, 0, -1}, []bool{false, true, false, true}},
		{"sort(xs)", []float64{3, 1, 2}, []bool{false, false, false}},
	} {
		got, err := run(t, test.source, map[string]any{"xs": test.in}, xs)
		if err != nil {
			t.Fatal(err)
		}
		sorted, ok := got.([]float64)
		if !ok || len(sorted) != len(test.signs) {
			t.Fatalf("%s of %v = %v", test.source, test.in, got)
		}
		for i, value := range sorted {
			if math.Signbit(value) != test.signs[i] {
				t.Errorf("%s of %v = %v, want signs %v", test.source, test.in, sorted, test.signs)
			}
		}
	}
}
