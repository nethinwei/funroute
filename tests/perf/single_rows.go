package main

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nethinwei/funroute"
)

// feeIn and the fee rule are the everyday case: a fee in basis points plus a
// fixed part, read straight from the host's struct.
type feeIn struct {
	Amount int64 `funroute:"amount"`
	Bps    int64 `funroute:"bps"`
	Fixed  int64 `funroute:"fixed"`
}

const feeRule = "amount * bps / 10000 + fixed"

func singleRows(registry *funroute.Registry) ([]row, error) {
	var rows []row
	for _, section := range []func(*funroute.Registry) ([]row, error){feeRows, callRows, loopRows, vectorRows, batchRows} {
		more, err := section(registry)
		if err != nil {
			return nil, err
		}
		rows = append(rows, more...)
	}
	return rows, nil
}

func feeRows(registry *funroute.Registry) ([]row, error) {
	ints := []funroute.ArgSpec{{Name: "amount", Type: funroute.IntType}, {Name: "bps", Type: funroute.IntType}, {Name: "fixed", Type: funroute.IntType}}
	runtime, err := loaded(registry, feeRule, ints...)
	if err != nil {
		return nil, err
	}
	binding, err := funroute.Bind[feeIn, int64](registry)
	if err != nil {
		return nil, err
	}
	program, err := binding.Compile(feeRule)
	if err != nil {
		return nil, err
	}
	byName := map[string]any{"amount": 100000, "bps": 250, "fixed": 30}
	request := feeIn{Amount: 100000, Bps: 250, Fixed: 30}
	native := benchNative(func() { keep(nativeFee(&feeInput)) })
	return []row{
		{"`" + feeRule + "`：`RunValues`", runValues(runtime, funroute.Int(100000), funroute.Int(250), funroute.Int(30)), native},
		{"同上：`Run(map)`", bench(func(ctx context.Context) error {
			_, err := runtime.Run(ctx, byName, funroute.RunOptions{})
			return err
		}), native},
		{"同上：`Program.Run`（从宿主 struct 读参数）", bench(func(ctx context.Context) error {
			_, err := program.Run(ctx, &request, funroute.RunOptions{})
			return err
		}), native},
	}, nil
}

func callRows(registry *funroute.Registry) ([]row, error) {
	hostAdd := func(a, b int64) int64 { return a + b }
	if err := registry.Register(funroute.FunctionSpec{Name: "host.add_v1", Doc: funroute.Doc{Cost: 2}, Go: hostAdd}); err != nil {
		return nil, err
	}
	ints := []funroute.ArgSpec{{Name: "a", Type: funroute.IntType}, {Name: "b", Type: funroute.IntType}}
	kernel, err := loaded(registry, "a + b", ints...)
	if err != nil {
		return nil, err
	}
	host, err := loaded(registry, "host.add_v1(a, b)", ints...)
	if err != nil {
		return nil, err
	}
	fx, err := loaded(registry, "using(150.25 JPY / USD, round(m -> JPY, @half_even))", funroute.ArgSpec{Name: "m", Type: funroute.MoneyType})
	if err != nil {
		return nil, err
	}
	table, err := funroute.NewCurrencies(funroute.MoneySpec{Currencies: []funroute.CurrencySpec{{Code: "USD", Digits: 2}, {Code: "JPY"}}})
	if err != nil {
		return nil, err
	}
	dollar, err := table.Parse("USD 12.34")
	if err != nil {
		return nil, err
	}
	amount, err := funroute.ToValue(dollar)
	if err != nil {
		return nil, err
	}
	quote, err := table.FxRate("USD", "JPY", "150.25")
	if err != nil {
		return nil, err
	}
	return []row{
		{"一次内核函数调用 `a + b`", runValues(kernel, funroute.Int(7), funroute.Int(3)), benchNative(func() { keep(nativeAdd(addInputs[0], addInputs[1])) })},
		{"一次按 Go 签名注册的函数调用（常见签名，不经反射）", runValues(host, funroute.Int(7), funroute.Int(3)), benchNative(func() { keep(hostAdd(addInputs[0], addInputs[1])) })},
		{"`using` 里换汇一次", runValues(fx, amount), benchNative(func() {
			converted, _ := table.Convert(dollar, quote, funroute.RoundHalfEven)
			keep(converted)
		})},
	}, nil
}

func loopRows(registry *funroute.Registry) ([]row, error) {
	xs := funroute.ArgSpec{Name: "xs", Type: funroute.ArrayOf(funroute.IntType)}
	items := make([]int64, 500)
	for i := range items {
		items[i] = int64(i)
	}
	arg, err := funroute.ToValue(items)
	if err != nil {
		return nil, err
	}
	var rows []row
	for _, c := range []struct {
		name, source string
		native       func()
	}{
		{"500 个元素的 `reduce`", "reduce(x in xs, acc = 0, acc + x)", func() { keep(nativeSum(items)) }},
		{"500 个元素的推导式", "[x + 1 for x in xs]", func() { keep(nativeIncrement(items)) }},
		{"500 对元素的嵌套推导式（25 × 20）", "[x + y for x in take(xs, 25) for y in take(xs, 20)]", func() { keep(nativePairs(items[:25], items[:20])) }},
	} {
		runtime, err := loaded(registry, c.source, xs)
		if err != nil {
			return nil, err
		}
		rows = append(rows, row{c.name, runValues(runtime, arg), benchNative(c.native)})
	}
	return rows, nil
}

func vectorRows(registry *funroute.Registry) ([]row, error) {
	score := func(xs []float64) float64 { return xs[0] + xs[len(xs)-1] }
	if err := registry.Register(funroute.FunctionSpec{Name: "model.edges_v1", Doc: funroute.Doc{Cost: 10}, Go: score}); err != nil {
		return nil, err
	}
	runtime, err := loaded(registry, "model.edges_v1(features)", funroute.ArgSpec{Name: "features", Type: funroute.ArrayOf(funroute.FloatType)})
	if err != nil {
		return nil, err
	}
	var rows []row
	for _, n := range []int{16, 1024, 65536} {
		features := make([]float64, n)
		arg, err := funroute.ToValue(features)
		if err != nil {
			return nil, err
		}
		native := benchNative(func() { keep(score(features)) })
		rows = append(rows, row{fmt.Sprintf("把 %d 个 float 交给宿主函数（与长度无关：不拷贝）", n), runValues(runtime, arg), native})
	}
	return rows, nil
}

// batchRows is a model that costs 20 µs a call whatever it is handed:
// called once a request, and once a batch of 64.
func batchRows(registry *funroute.Registry) ([]row, error) {
	const overhead = 20 * time.Microsecond
	single := func(x float64) float64 { time.Sleep(overhead); return x }
	many := func(xs []float64) []float64 { time.Sleep(overhead); return xs }
	err := registry.Register(funroute.FunctionSpec{Name: "model.score_v1", Doc: funroute.Doc{Cost: 10}, Go: single, GoBatch: many})
	if err != nil {
		return nil, err
	}
	runtime, err := loaded(registry, "model.score_v1(x) + 1.0", funroute.ArgSpec{Name: "x", Type: funroute.FloatType})
	if err != nil {
		return nil, err
	}
	one, err := funroute.Float(1)
	if err != nil {
		return nil, err
	}
	perBatch := benchNative(func() { keep(many(make([]float64, 64))) })
	perBatch.nanoseconds /= 64
	return []row{
		{"模型调用（引擎每次 20 µs），一条一条", runValues(runtime, one), benchNative(func() { keep(single(scoreInput)) })},
		{"同上，64 条一批，折合每条", batched(runtime, one), perBatch},
	}, nil
}

// batched measures a request that 64 goroutines submit at once, so a batch
// fills and the engine runs once for all of them.
func batched(runtime *funroute.Runtime, arg funroute.Value) measured {
	batch := funroute.NewBatch(runtime, funroute.BatchOptions{MaxSize: 64, MaxWait: time.Millisecond})
	defer batch.Close()
	var failed atomic.Pointer[error]
	result := testing.Benchmark(func(b *testing.B) {
		b.SetParallelism(64)
		b.RunParallel(func(pb *testing.PB) { submit(b.Context(), pb, batch, arg, &failed) })
	})
	measure := measured{nanoseconds: perOp(result), allocations: result.AllocsPerOp()}
	if err := failed.Load(); err != nil {
		measure.err = *err
	}
	return measure
}

// submit runs requests through batch until the benchmark has enough,
// keeping the first error any of them met.
func submit(ctx context.Context, pb *testing.PB, batch *funroute.Batch, arg funroute.Value, failed *atomic.Pointer[error]) {
	for pb.Next() {
		if _, err := batch.Run(ctx, []funroute.Value{arg}); err != nil {
			failed.CompareAndSwap(nil, &err)
		}
	}
}
