package machine_test

import (
	"math"
	"testing"

	"github.com/nethinwei/funroute/internal/machine"
)

// The extremes answer as Go's min and max do, the first of equal ones:
// a NaN is the answer when there is one, the first NaN; of 0 and -0, -0 is
// the smaller.
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
		{"arg_min", []float64{0, negativeZero, 1}, 1},
		{"arg_min", []float64{negativeZero, 0, negativeZero}, 0},
		{"arg_max", []float64{-1, negativeZero, 0}, 2},
		{"arg_max", []float64{-1, 0, negativeZero, 0}, 1},
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

// A name answers alike however it is called: min and max of an array give
// what min and max of its items pairwise give, bit for bit, -0 too.
func TestTheExtremesOfAnArrayAreThoseOfTwo(t *testing.T) {
	t.Parallel()
	for array, pair := range map[string]string{
		`string(min([0.0, -0.0]))`:                `string(min(0.0, -0.0))`,
		`string(min([-0.0, 0.0]))`:                `string(min(-0.0, 0.0))`,
		`string(max([0.0, -0.0]))`:                `string(max(0.0, -0.0))`,
		`string(max([-0.0, 0.0]))`:                `string(max(-0.0, 0.0))`,
		`string(min([1.0, 0.0 / 0.0]))`:           `string(min(1.0, 0.0 / 0.0))`,
		`string(min_by(["a", "b"], [0.0, -0.0]))`: `"b"`,
	} {
		t.Run(array, func(t *testing.T) {
			t.Parallel()
			got, err := libRun(t, array, nil)
			want, wantErr := libRun(t, pair, nil)
			if err != nil || wantErr != nil || got != want {
				t.Fatalf("%s = %v, %v; want %s = %v, %v", array, got, err, pair, want, wantErr)
			}
		})
	}
}
