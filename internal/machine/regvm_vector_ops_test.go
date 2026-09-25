package machine

import (
	"math"
	"testing"
)

// The vector's int and float operations answer what the kernel's functions
// answer, and refuse where they fail, on the edges of both types.
func TestVectorOpsAnswerAsTheKernel(t *testing.T) {
	t.Parallel()
	registry := CoreRegistry()
	ints := []int64{0, 1, -1, 2, 7, -7, math.MaxInt64, math.MinInt64, math.MaxInt64 - 1, 1 << 32, 1 << 53, -(1 << 53) - 1}
	floats := []float64{0, math.Copysign(0, -1), 1.5, -3, math.MaxFloat64, -math.MaxFloat64, math.SmallestNonzeroFloat64}
	for key, kernel := range kernelOps {
		function, _ := registry.Resolve(key)
		for _, pair := range operandsOf(function.Params, ints, floats, nil) {
			args := pair[:len(function.Params)]
			want, wantErr := function.Eval(t.Context(), args)
			var got Value
			var ok, applies bool
			switch kernel.op {
			case rAddI, rSubI, rMulI, rDivI, rModI:
				var value int64
				value, ok = intOp(kernel.op, args[0].i, args[1].i)
				got, applies = Int(value), true
			case rAddF, rSubF, rMulF, rDivF:
				var value float64
				value, ok = floatOp(kernel.op, args[0].f, args[1].f)
				got, applies = Float(value), true
			case rIntToF:
				var value float64
				value, ok = intFloat(args[0].i)
				got, applies = Float(value), true
			}
			if applies && (ok != (wantErr == nil) || ok && !got.Equal(want)) {
				t.Errorf("%s%v: the vector gives %v, %v; the function %v, %v", key, args, got.Any(), ok, want.Any(), wantErr)
			}
		}
	}
}
