package machine

import (
	"math"
	"testing"
)

// Every row of kernelOps is a kernel function's signature: a row with none
// would lower nothing, and a renamed function would quietly lose its
// operation.
func TestKernelOpsAreTheKernels(t *testing.T) {
	t.Parallel()
	registry := CoreRegistry()
	for key := range kernelOps {
		if function, ok := registry.Resolve(key); !ok || !function.builtin {
			t.Errorf("kernelOps has %s, which is not a kernel function", key)
		}
	}
}

// A kernel operation answers what its function answers, and refuses where
// the function fails, on the edges of every type it takes.
func TestKernelOpsAnswerAsTheirFunctions(t *testing.T) {
	t.Parallel()
	registry := CoreRegistry()
	ints := []int64{0, 1, -1, 2, -2, 3, 7, -7, math.MaxInt64, math.MinInt64, math.MaxInt64 - 1, math.MinInt64 + 1, 1 << 32, -(1 << 31)}
	floats := []float64{0, math.Copysign(0, -1), 1.5, -1.5, 3, math.MaxFloat64, -math.MaxFloat64, math.SmallestNonzeroFloat64, 1e308, 1e-308}
	strings := []string{"", "a", "b", "ab", "é"}
	for key, kernel := range kernelOps {
		function, _ := registry.Resolve(key)
		for _, pair := range operandsOf(function.Params, ints, floats, strings) {
			assertKernelOp(t, function, kernel, pair)
		}
	}
}

// operandsOf is every pair of edge values of the parameters' types; an
// array parameter gets a few arrays.
func operandsOf(params []Type, ints []int64, floats []float64, strings []string) [][2]Value {
	valuesOf := func(typ Type) []Value {
		var out []Value
		switch typ.kind {
		case IntKind:
			for _, i := range ints {
				out = append(out, Int(i))
			}
		case FloatKind:
			for _, f := range floats {
				out = append(out, Float(f))
			}
		case StringKind, VarKind:
			for _, s := range strings {
				out = append(out, String(s))
			}
		case ArrayKind, DictKind:
			out = append(out, arrayOf([]int64{}), arrayOf([]int64{4, 5, 6}))
		}
		return out
	}
	lefts, rights := valuesOf(params[0]), []Value{{}}
	if len(params) > 1 {
		rights = valuesOf(params[1])
	}
	pairs := make([][2]Value, 0, len(lefts)*len(rights))
	for _, left := range lefts {
		for _, right := range rights {
			pairs = append(pairs, [2]Value{left, right})
		}
	}
	return pairs
}

func assertKernelOp(t *testing.T, function *RegisteredFunction, kernel kernelOp, pair [2]Value) {
	t.Helper()
	args := pair[:len(function.Params)]
	want, wantErr := function.Eval(t.Context(), args)
	regs := []Value{pair[0], pair[1], {}}
	in := &rinstr{op: kernel.op, a: 0, b: 1, c: 2}
	if kernel.swap {
		in.a, in.b = 1, 0
	}
	ok := runKernelOp(&frame{regs: regs}, in)
	switch {
	case !ok && wantErr == nil:
		t.Errorf("%s%v: the operation refuses what the function answers, %v", function.key, args, want.Any())
	case ok && wantErr != nil:
		t.Errorf("%s%v: the operation answers %v where the function fails: %v", function.key, args, regs[2].Any(), wantErr)
	case ok && !identical(regs[2], want):
		t.Errorf("%s%v = %v, the function says %v", function.key, args, regs[2].Any(), want.Any())
	}
	// In place, a refusal leaves the operand as it was, for the function
	// to be asked about it.
	inPlace := []Value{pair[0], pair[1]}
	if !runKernelOp(&frame{regs: inPlace}, &rinstr{op: kernel.op, a: 0, b: 1, c: 0}) && !identical(inPlace[0], pair[0]) {
		t.Errorf("%s%v refused in place, and changed its operand to %v", function.key, args, inPlace[0].Any())
	}
}

// runKernelOp runs one kernel operation as the hot loop or cold would.
func runKernelOp(f *frame, in *rinstr) bool {
	regs := f.regs
	switch in.op {
	case rAddI:
		return addI(regs, in)
	case rSubI:
		return subI(regs, in)
	case rMulI:
		return mulI(regs, in)
	case rDivI:
		return divI(regs, in)
	case rAddF:
		return addF(regs, in)
	case rLen:
		lengthOp(regs, in)
		return true
	}
	if in.op == rIntToF {
		return intToFloat(regs, in)
	}
	_, ok := coldKernel(regs, 0, in)
	return ok
}
