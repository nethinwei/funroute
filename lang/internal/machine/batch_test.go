package machine_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"funroute/lang/internal/compile"
	"funroute/lang/internal/machine"
)

// embedding stands in for an engine's tensor: the language passes it along and
// never looks inside.
type embedding struct{ features []float64 }

// modelRegistry has two models: embed turns features into a handle, fraud
// scores a handle. Both count how they were called so a test can see whether
// a batch happened.
func modelRegistry(t testing.TB, single, batched *atomic.Int64) *machine.Registry {
	t.Helper()
	registry := machine.CoreRegistry()
	if err := registry.EnableForm(machine.SwitchForm); err != nil {
		t.Fatal(err)
	}
	if err := machine.DefineHandle[*embedding](registry, "demo.embedding"); err != nil {
		t.Fatal(err)
	}
	embed := func(features []float64) (*embedding, error) {
		single.Add(1)
		return &embedding{features: features}, nil
	}
	embedBatch := func(features [][]float64) ([]*embedding, error) {
		batched.Add(1)
		out := make([]*embedding, len(features))
		for i, row := range features {
			out[i] = &embedding{features: row}
		}
		return out, nil
	}
	if err := machine.Model(registry, "model.embed_v1", machine.Doc{Cost: 10}, embed, embedBatch); err != nil {
		t.Fatal(err)
	}
	fraud := func(e *embedding) (float64, error) {
		single.Add(1)
		return e.features[0], nil
	}
	if err := machine.Logic(registry, "model.fraud_v1", machine.Doc{Cost: 10}, fraud); err != nil {
		t.Fatal(err)
	}
	return registry
}

// A batch of N requests makes one batched embed call, the programs take the
// answers, and each request still gets its own result. It runs on synctest's
// clock, so the batch fills before its MaxWait no matter how slowly the
// goroutines start.
func TestBatchRunsEachModelOncePerBatch(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var single, batched atomic.Int64
		runtime := modelRuntime(t, &single, &batched, `let(e = model.embed_v1(features), model.fraud_v1(e) * 2.0)`)
		batch := machine.NewBatch(runtime, machine.BatchOptions{MaxSize: 8, MaxWait: time.Second, Run: machine.RunOptions{Fuel: 100}})
		if batch.Sites() != 1 {
			t.Fatalf("sites = %d, want 1", batch.Sites())
		}
		const requests = 8
		results := runConcurrently(t, batch, requests)
		for i, result := range results {
			if result != float64(i)*2 {
				t.Fatalf("request %d = %v, want %v", i, result, float64(i)*2)
			}
		}
		// embed ran once for the batch; fraud has no batch implementation and ran
		// per request.
		if batched.Load() != 1 || single.Load() != requests {
			t.Fatalf("batched = %d, single = %d, want 1 and %d", batched.Load(), single.Load(), requests)
		}
	})
}

// modelRuntime compiles source over one features argument against
// modelRegistry and loads it.
func modelRuntime(t *testing.T, single, batched *atomic.Int64, source string) *machine.Runtime {
	t.Helper()
	registry := modelRegistry(t, single, batched)
	artifact, err := compile.CompileExpr(source, registry, compile.CompileOptions{
		Args: []compile.ArgSpec{{Name: "features", Type: machine.ArrayOf(machine.FloatType)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := machine.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	return runtime
}

// runConcurrently submits requests to batch from a goroutine each, request i
// with features [i], and returns the results by request.
func runConcurrently(t *testing.T, batch *machine.Batch, requests int) []float64 {
	t.Helper()
	ctx := t.Context()
	results := make([]float64, requests)
	var wg sync.WaitGroup
	for i := range requests {
		wg.Go(func() {
			value, _ := machine.ToValue([]float64{float64(i)})
			result, err := batch.Run(ctx, []machine.Value{value})
			if err != nil {
				t.Error(err)
				return
			}
			results[i], _ = result.Float()
		})
	}
	wg.Wait()
	return results
}

// A lone request is flushed by the timer, not stuck waiting for a batch, and a
// closed batch refuses new requests. The timer is synctest's, so the request
// is answered exactly MaxWait after it arrived.
func TestBatchFlushesOnTheTimerAndCloses(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const wait = 5 * time.Millisecond
		var single, batched atomic.Int64
		runtime := modelRuntime(t, &single, &batched, `model.fraud_v1(model.embed_v1(features)) * 2.0`)
		quick := machine.NewBatch(runtime, machine.BatchOptions{MaxSize: 64, MaxWait: wait, Run: machine.RunOptions{Fuel: 100}})
		value, _ := machine.ToValue([]float64{3})
		started := time.Now()
		result, err := quick.Run(t.Context(), []machine.Value{value})
		if err != nil {
			t.Fatal(err)
		}
		if elapsed := time.Since(started); elapsed != wait {
			t.Fatalf("lone request answered after %s, want %s", elapsed, wait)
		}
		if got, _ := result.Float(); got != 6 || batched.Load() != 1 {
			t.Fatalf("lone result = %v, batched = %d, want 6 and 1", got, batched.Load())
		}
		quick.Close()
		if _, err := quick.Run(t.Context(), []machine.Value{value}); err == nil {
			t.Fatal("a closed batch accepted a request")
		}
	})
}

// An engine error is filed per request and surfaces where the program would
// have made the call, with the function's name.
func TestBatchErrorsSurfaceAtTheCall(t *testing.T) {
	t.Parallel()
	registry := machine.CoreRegistry()
	err := machine.Model(registry, "model.flaky_v1", machine.Doc{Cost: 1},
		func(x float64) (float64, error) { return x, nil },
		func(xs []float64) ([]float64, error) { return nil, fmt.Errorf("engine down") })
	if err != nil {
		t.Fatal(err)
	}
	runtime := floatRuntime(t, registry, `model.flaky_v1(x)`)
	batch := machine.NewBatch(runtime, machine.BatchOptions{MaxSize: 1})
	_, err = batch.Run(t.Context(), []machine.Value{machine.Float(1)})
	if !errors.Is(err, machine.ErrExtension) || !strings.Contains(err.Error(), "model.flaky_v1") || !strings.Contains(err.Error(), "engine down") {
		t.Fatalf("error = %v, want ErrExtension naming model.flaky_v1 and engine down", err)
	}
	// Without a batch the per-request implementation answers.
	result, err := runtime.RunValues(t.Context(), []machine.Value{machine.Float(1)}, machine.RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := result.Float(); got != 1 {
		t.Fatalf("result = %v, want 1", got)
	}
}

// floatRuntime compiles source over one float argument x and loads it.
func floatRuntime(t *testing.T, registry *machine.Registry, source string) *machine.Runtime {
	t.Helper()
	artifact, err := compile.CompileExpr(source, registry, compile.CompileOptions{Args: []compile.ArgSpec{{Name: "x", Type: machine.FloatType}}})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := machine.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	return runtime
}

func TestBatchPanicsAreContainedAndTyped(t *testing.T) {
	t.Parallel()
	registry := machine.CoreRegistry()
	err := machine.Model(registry, "model.panic_v1", machine.Doc{Cost: 1},
		func(x float64) (float64, error) { return x, nil },
		func([]float64) ([]float64, error) { panic("batch exploded") })
	if err != nil {
		t.Fatal(err)
	}
	batch := machine.NewBatch(floatRuntime(t, registry, `model.panic_v1(x)`), machine.BatchOptions{MaxSize: 1})
	_, err = batch.Run(t.Context(), []machine.Value{machine.Float(1)})
	if !errors.Is(err, machine.ErrExtension) || !strings.Contains(err.Error(), "batch exploded") {
		t.Fatalf("batch panic = %v, want ErrExtension naming the panic", err)
	}
}

// A request canceled while it waits for its batch returns at once, without
// waiting out MaxWait. synctest.Wait stands where a sleep used to: it returns
// once the request is queued and blocked.
func TestCanceledBatchRequestReturnsPromptly(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var single, batched atomic.Int64
		runtime := modelRuntime(t, &single, &batched, `model.embed_v1(features)`)
		batch := machine.NewBatch(runtime, machine.BatchOptions{MaxSize: 64, MaxWait: time.Second})
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan error, 1)
		value, _ := machine.ToValue([]float64{1})
		go func() {
			_, runErr := batch.Run(ctx, []machine.Value{value})
			done <- runErr
		}()
		synctest.Wait()
		cancel()
		synctest.Wait()
		select {
		case err := <-done:
			if !errors.Is(err, machine.ErrDeadline) {
				t.Fatalf("canceled request = %v, want ErrDeadline", err)
			}
		default:
			t.Fatal("canceled request stayed queued")
		}
		batch.Close()
		if batched.Load() != 0 {
			t.Fatalf("canceled request reached model batch %d times, want 0", batched.Load())
		}
	})
}

// On synctest's clock the detached batch call is abandoned exactly at its
// Timeout.
func TestDetachedBatchStopsWaitingAtItsTimeout(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const timeout = 5 * time.Millisecond
		registry := machine.CoreRegistry()
		release := make(chan struct{})
		err := machine.Model(registry, "model.stubborn_v1", machine.Doc{Cost: 1, Timeout: timeout, Detached: true},
			func(x float64) (float64, error) { return x, nil },
			func(xs []float64) ([]float64, error) { <-release; return xs, nil })
		if err != nil {
			t.Fatal(err)
		}
		batch := machine.NewBatch(floatRuntime(t, registry, `model.stubborn_v1(x)`), machine.BatchOptions{MaxSize: 1})
		started := time.Now()
		_, err = batch.Run(t.Context(), []machine.Value{machine.Float(1)})
		elapsed := time.Since(started)
		close(release)
		if !errors.Is(err, machine.ErrDeadline) || elapsed != timeout {
			t.Fatalf("detached batch = %v after %s, want ErrDeadline after %s", err, elapsed, timeout)
		}
	})
}

// BenchmarkBatchVersusSingle models an engine with a fixed per-call overhead:
// batching amortises it across the requests of one batch. The overhead is a
// sleep, because an engine call blocks rather than computes, and the batched
// requests come from RunParallel's goroutines, enough of them to fill a batch
// of 64, so the time measured includes every request's answer.
func BenchmarkBatchVersusSingle(b *testing.B) {
	const overhead = 20 * time.Microsecond
	registry := machine.CoreRegistry()
	err := machine.Model(registry, "model.score_v1", machine.Doc{Cost: 10},
		func(x float64) (float64, error) { time.Sleep(overhead); return x, nil },
		func(xs []float64) ([]float64, error) { time.Sleep(overhead); return xs, nil })
	if err != nil {
		b.Fatal(err)
	}
	artifact, err := compile.CompileExpr(`model.score_v1(x) + 1.0`, registry, compile.CompileOptions{Args: []compile.ArgSpec{{Name: "x", Type: machine.FloatType}}})
	if err != nil {
		b.Fatal(err)
	}
	runtime, err := machine.Instantiate(artifact, registry)
	if err != nil {
		b.Fatal(err)
	}
	args := []machine.Value{machine.Float(1)}
	b.Run("single", func(b *testing.B) {
		ctx := b.Context()
		for b.Loop() {
			if _, err := runtime.RunValues(ctx, args, machine.RunOptions{}); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("batch=64", func(b *testing.B) {
		batch := machine.NewBatch(runtime, machine.BatchOptions{MaxSize: 64, MaxWait: time.Millisecond})
		defer batch.Close()
		ctx := b.Context()
		b.SetParallelism(64)
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				runInBatch(b, ctx, batch, args)
			}
		})
	})
}

func runInBatch(b *testing.B, ctx context.Context, batch *machine.Batch, args []machine.Value) {
	b.Helper()
	if _, err := batch.Run(ctx, args); err != nil {
		b.Error(err)
	}
}

// A batch checks each request's arguments before the engine sees any: a
// request in the wrong currency, in an undeclared one, with a currency-less
// amount that is not zero or of the wrong kind is answered with the
// contract's error, and the engine is handed only the dollars the model's
// signature asks for.
func TestBatchHandsTheEngineOnlyAdmittedArguments(t *testing.T) {
	t.Parallel()
	registry := moneyRegistry(t)
	var seen []string
	err := registry.Register(machine.FunctionSpec{
		Name: "m.score_v1", Params: []machine.Type{machine.MoneyOf("USD")}, Result: machine.IntType,
		Eval: func(context.Context, []machine.Value) (machine.Value, error) { return machine.Int(1), nil },
		EvalBatch: func(_ context.Context, calls [][]machine.Value) ([]machine.Value, error) {
			out := make([]machine.Value, len(calls))
			for i, call := range calls {
				m, _ := call[0].Money()
				seen = append(seen, fmt.Sprint(m.Currency(), " ", m.Minor()))
				out[i] = machine.Int(1)
			}
			return out, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := compileMoney(t, registry, "m.score_v1(a)", "a: money<USD>", "")
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := machine.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	batch := machine.NewBatch(runtime, machine.BatchOptions{MaxSize: 1})
	defer batch.Close()
	for _, arg := range []machine.Value{machine.MoneyValue(5, "EUR"), machine.MoneyValue(5, "XYZ"), machine.MoneyValue(5, ""), machine.Int(3)} {
		if got, err := batch.Run(t.Context(), []machine.Value{arg}); !errors.Is(err, machine.ErrContract) {
			t.Errorf("batch.Run(%v) = %v, %v, want ErrContract", arg.Any(), got.Any(), err)
		}
	}
	if got, err := batch.Run(t.Context(), []machine.Value{machine.MoneyValue(5, "USD")}); err != nil || got.Any() != int64(1) {
		t.Fatalf("batch.Run(USD 0.05) = %v, %v, want 1", got.Any(), err)
	}
	if want := []string{"USD 5"}; !slices.Equal(seen, want) {
		t.Fatalf("the engine saw %q, want only %q", seen, want)
	}
}

// A batched engine's currency mismatch is the rule's error at every call it
// answered: the batch does not wrap it as an extension's failure, which
// would make it one fallback takes. (A call inside a fallback is not hoisted
// at all: the batch runs only what the program would run unconditionally.)
func TestABatchedCurrencyMismatchIsNotAnExtensionFailure(t *testing.T) {
	t.Parallel()
	registry := machine.CoreRegistry()
	err := machine.Model(registry, "model.mixed_v1", machine.Doc{Cost: 1},
		func(x float64) (float64, error) { return x, nil },
		func([]float64) ([]float64, error) { return nil, fmt.Errorf("engine: %w", machine.ErrCurrency) })
	if err != nil {
		t.Fatal(err)
	}
	runtime := floatRuntime(t, registry, `model.mixed_v1(x)`)
	batch := machine.NewBatch(runtime, machine.BatchOptions{MaxSize: 1})
	defer batch.Close()
	if got, err := batch.Run(t.Context(), []machine.Value{machine.Float(1)}); !errors.Is(err, machine.ErrCurrency) || errors.Is(err, machine.ErrExtension) {
		t.Fatalf("batch.Run = %v, %v, want ErrCurrency and not ErrExtension", got.Any(), err)
	}
}
