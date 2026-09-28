package machine_test

import (
	"math"
	"testing"

	"github.com/nethinwei/funroute/internal/machine"
)

// The extremes answer as Go's min and max do, the first of equal ones:
// a NaN is the answer when there is one, the first NaN; 0 and -0 are equal,
// so the first of them is.
func TestTheExtremesTakeTheFirstOfEqualOnesAndANaN(t *testing.T) {
	t.Parallel()
	nan, negativeZero := math.NaN(), math.Copysign(0, -1)
	for _, test := range []struct {
		name  string
		items []float64
		want  int64
	}{
		{"arg_min", []float64{3, 1, 2, 1}, 1},
		{"arg_max", []float64{3, 1, 3, 2}, 0},
		{"arg_min", []float64{1, nan, 0, nan}, 1},
		{"arg_max", []float64{5, 7, nan, 9, nan}, 2},
		{"arg_min", []float64{0, negativeZero, 1}, 0},
		{"arg_min", []float64{negativeZero, 0, 1}, 0},
		{"arg_max", []float64{-1, 0, negativeZero}, 1},
		{"arg_max", []float64{2}, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			function, ok := machine.CoreRegistry().Resolve(test.name + "(array<float>)->int")
			if !ok {
				t.Fatalf("%s is not registered", test.name)
			}
			items, err := machine.ToValue(test.items)
			if err != nil {
				t.Fatal(err)
			}
			got, err := function.Eval(t.Context(), []machine.Value{items})
			if at, _ := got.Int(); err != nil || at != test.want {
				t.Fatalf("%s(%v) = %d, %v; want %d", test.name, test.items, at, err, test.want)
			}
		})
	}
}
