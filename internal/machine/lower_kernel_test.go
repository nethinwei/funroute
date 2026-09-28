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
		pairs := operandsOf(function.Params, ints, floats, strings)
		if kernel.op == rEq {
			// eq(T, T) of ints and floats is eq_i and eq_f.
			pairs = append(pairs, operandsOf([]Type{IntType, IntType}, ints, floats, strings)...)
			pairs = append(pairs, operandsOf([]Type{FloatType, FloatType}, ints, floats, strings)...)
		}
		for _, pair := range pairs {
			assertKernelOp(t, function, kernel, pair)
		}
	}
}

// operandsOf is every pair of edge values of the parameters' types; an
// array parameter gets a few arrays, and a dictionary one a few dictionaries.
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
		case ArrayKind:
			out = append(out, arrayOf([]int64{}), arrayOf([]int64{4, 5, 6}))
		case DictKind:
			out = append(out, Value{kind: DictKind, box: map[string]int64{}}, Value{kind: DictKind, box: map[string]int64{"a": 4, "b": 5}})
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
	in := &rinstr{op: loweredOp(kernel.op, pair), a: 0, b: 1, c: 2}
	if kernel.swap {
		in.a, in.b = 1, 0
	}
	b := operandBanks(pair)
	ok := runKernelOp(b, in)
	got := b.valueAt(2, want.kind)
	switch {
	case !ok && wantErr == nil:
		t.Errorf("%s%v: the operation refuses what the function answers, %v", function.key, args, want.Any())
	case ok && wantErr != nil:
		t.Errorf("%s%v: the operation answers %v where the function fails: %v", function.key, args, got.Any(), wantErr)
	case ok && !identical(got, want):
		t.Errorf("%s%v = %v, the function says %v", function.key, args, got.Any(), want.Any())
	}
	// In place, a refusal leaves the operand as it was, for the function
	// to be asked about it.
	inPlace := operandBanks(pair)
	if !runKernelOp(inPlace, &rinstr{op: in.op, a: 0, b: 1, c: 0}) && !identical(inPlace.valueAt(0, pair[0].kind), pair[0]) {
		t.Errorf("%s%v refused in place, and changed its operand to %v", function.key, args, inPlace.valueAt(0, pair[0].kind).Any())
	}
}

// loweredOp is the operation lowering makes of op on the pair: an equality
// of the pair's file, an item read of the array's.
func loweredOp(op rop, pair [2]Value) rop {
	switch op {
	case rEq:
		return eqOp(pair[0].kind)
	case rAt:
		return atOp(pair[0].elemType().kind, false)
	case rAtD:
		return entryOp(pair[0].elemType().kind)
	}
	return op
}

// operandBanks is register files holding the pair in registers 0 and 1,
// each in the file of its kind, and room for an answer in 2.
func operandBanks(pair [2]Value) *banks {
	b := newBanks(3, nil)
	b.setValue(0, pair[0])
	b.setValue(1, pair[1])
	return &b
}

// runKernelOp runs one kernel operation as the hot loop or cold would.
func runKernelOp(b *banks, in *rinstr) bool {
	switch in.op {
	case rAddI:
		return addI(b.ints, in)
	case rSubI:
		return subI(b.ints, in)
	case rMulI:
		return mulI(b.ints, in)
	case rAddF:
		addF(b.floats, in)
		return true
	}
	_, ok := coldKernels[in.op](b, 0, in)
	return ok
}
