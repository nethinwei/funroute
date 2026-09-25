package main

import (
	"runtime"
	"slices"
	"strconv"
	"testing"
)

// The same work written in Go the way a host would, for a reference: no
// overflow check, no fuel, no boundary to cross. Each is kept out of line,
// its inputs are variables and its answer goes to keep, so the
// compiler can neither fold the inputs in nor drop the work.

var (
	feeInput   = feeIn{Amount: 100000, Bps: 250, Fixed: 30}
	addInputs  = [2]int64{7, 3}
	scoreInput = 1.0
)

// keep hands a result to runtime.KeepAlive, which the compiler cannot see
// through, so the work that made it is done.
func keep[T any](value T) { runtime.KeepAlive(value) }

// benchNative measures run the way go test -bench does.
func benchNative(run func()) measured {
	result := testing.Benchmark(func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			run()
		}
	})
	return measured{nanoseconds: perOp(result), allocations: result.AllocsPerOp()}
}

// perOp is the time one run took, to a fraction of a nanosecond: NsPerOp
// rounds down to whole ones, and much of Go's own work takes less.
func perOp(result testing.BenchmarkResult) float64 {
	return float64(result.T.Nanoseconds()) / float64(result.N)
}

//go:noinline
func nativeFee(in *feeIn) int64 { return in.Amount*in.Bps/10000 + in.Fixed }

//go:noinline
func nativeAdd(a, b int64) int64 { return a + b }

//go:noinline
func nativeSum(xs []int64) int64 {
	total := int64(0)
	for _, x := range xs {
		total += x
	}
	return total
}

//go:noinline
func nativeIncrement(xs []int64) []int64 {
	out := make([]int64, len(xs))
	for i, x := range xs {
		out[i] = x + 1
	}
	return out
}

//go:noinline
func nativeFilterSum(xs []int64) int64 {
	total := int64(0)
	for _, x := range xs {
		if x%3 == 0 {
			total += x * 2
		}
	}
	return total
}

//go:noinline
func nativeIndex(xs []int64) map[string]int64 {
	out := make(map[string]int64, len(xs))
	for _, x := range xs {
		out[strconv.FormatInt(x, 10)] = x
	}
	return out
}

//go:noinline
func nativeSort(xs []int64) []int64 {
	out := slices.Clone(xs)
	slices.Sort(out)
	return out
}

//go:noinline
func nativePairs(xs, ys []int64) []int64 {
	out := make([]int64, 0, len(xs)*len(ys))
	for _, x := range xs {
		for _, y := range ys {
			out = append(out, x+y)
		}
	}
	return out
}
