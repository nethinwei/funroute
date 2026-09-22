package compile

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

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

// A handle flows from one model into the next: the second receives the very
// pointer the first returned, and the language cannot compare it.
func TestHandlesPassBetweenModelsUntouched(t *testing.T) {
	var single, batched atomic.Int64
	registry := modelRegistry(t, &single, &batched)
	artifact, err := CompileExpr(`let(e = model.embed_v1(features), model.fraud_v1(e))`, registry, CompileOptions{
		Args: []ArgSpec{{Name: "features", Type: machine.ArrayOf(machine.FloatType)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !artifact.Result.Equal(machine.FloatType) {
		t.Fatalf("result = %s", artifact.Result)
	}
	runtime, err := machine.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	features := []float64{0.75, 0.1}
	value, _ := machine.ToValue(features)
	result, err := runtime.RunValues(context.Background(), []machine.Value{value}, machine.RunOptions{Fuel: 100})
	if err != nil {
		t.Fatal(err)
	}
	if score, _ := result.Float(); score != 0.75 {
		t.Fatalf("score = %v", score)
	}
	// Handles are opaque: equality is refused, and a handle type only unifies
	// with itself.
	handle := machine.HandleOf("demo.embedding")
	eq, err := CompileExpr(`e == f`, registry, CompileOptions{Args: []ArgSpec{{Name: "e", Type: handle}, {Name: "f", Type: handle}}})
	if err != nil {
		t.Fatal(err)
	}
	rt, err := machine.Instantiate(eq, registry)
	if err != nil {
		t.Fatal(err)
	}
	h := machine.NewHandle("demo.embedding", &embedding{})
	if _, err := rt.RunValues(context.Background(), []machine.Value{h, h}, machine.RunOptions{}); err == nil || !strings.Contains(err.Error(), "cannot be compared") {
		t.Fatalf("handle equality error = %v", err)
	}
	_, err = CompileExpr(`model.fraud_v1(x)`, registry, CompileOptions{Args: []ArgSpec{{Name: "x", Type: machine.HandleOf("other.thing")}}})
	if err == nil || !strings.Contains(err.Error(), "handle<other.thing>") {
		t.Fatalf("wrong handle error = %v", err)
	}
	if got, err := machine.ParseType(" handle<demo.embedding> "); err != nil || !got.Equal(handle) {
		t.Fatalf("ParseType = %s, %v", got, err)
	}
}

// Only a call whose arguments come straight from the request, outside every
// loop and every condition, is hoisted.
func TestPrefetchSitesAreTheUnconditionalTopLevelCalls(t *testing.T) {
	var single, batched atomic.Int64
	registry := modelRegistry(t, &single, &batched)
	if err := registry.EnableForm(machine.ForForm); err != nil {
		t.Fatal(err)
	}
	features := ArgSpec{Name: "features", Type: machine.ArrayOf(machine.FloatType)}
	flag := ArgSpec{Name: "flag", Type: machine.BoolType}
	for _, test := range []struct {
		source string
		sites  int
	}{
		{`model.fraud_v1(model.embed_v1(features))`, 1},                                // embed is hoistable; fraud's argument is a call
		{`if(flag, model.fraud_v1(model.embed_v1(features)), 0.0)`, 0},                 // inside a branch
		{`let(e = model.embed_v1(features), if(flag, model.fraud_v1(e), 0.0))`, 1},     // the binding is unconditional
		{`[model.fraud_v1(model.embed_v1(features)) for x in features]`, 0},            // inside a loop
		{`switch(case flag => model.fraud_v1(model.embed_v1(features)), else 1.0)`, 0}, // a case body
	} {
		artifact, err := CompileExpr(test.source, registry, CompileOptions{Args: []ArgSpec{features, flag}})
		if err != nil {
			t.Fatalf("%s: %v", test.source, err)
		}
		if got := len(machine.PrefetchSites(artifact)); got != test.sites {
			t.Errorf("%s: %d sites, want %d", test.source, got, test.sites)
		}
	}
}

// A batch of N requests makes one batched embed call, the programs take the
// answers, and each request still gets its own result.
func TestBatchRunsEachModelOncePerBatch(t *testing.T) {
	var single, batched atomic.Int64
	registry := modelRegistry(t, &single, &batched)
	artifact, err := CompileExpr(`let(e = model.embed_v1(features), model.fraud_v1(e) * 2.0)`, registry, CompileOptions{
		Args: []ArgSpec{{Name: "features", Type: machine.ArrayOf(machine.FloatType)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := machine.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	batch := machine.NewBatch(runtime, machine.BatchOptions{MaxSize: 8, MaxWait: time.Second, Run: machine.RunOptions{Fuel: 100}})
	if batch.Sites() != 1 {
		t.Fatalf("sites = %d", batch.Sites())
	}
	const requests = 8
	results := make([]float64, requests)
	var wg sync.WaitGroup
	for i := 0; i < requests; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			value, _ := machine.ToValue([]float64{float64(i)})
			result, err := batch.Run(context.Background(), []machine.Value{value})
			if err != nil {
				t.Error(err)
				return
			}
			results[i], _ = result.Float()
		}(i)
	}
	wg.Wait()
	for i, result := range results {
		if result != float64(i)*2 {
			t.Fatalf("request %d = %v", i, result)
		}
	}
	// embed ran once for the batch; fraud has no batch implementation and ran
	// per request.
	if batched.Load() != 1 || single.Load() != requests {
		t.Fatalf("batched = %d, single = %d", batched.Load(), single.Load())
	}
}

// A lone request is flushed by the timer, not stuck waiting for a batch, and a
// closed batch refuses new requests.
func TestBatchFlushesOnTheTimerAndCloses(t *testing.T) {
	var single, batched atomic.Int64
	registry := modelRegistry(t, &single, &batched)
	artifact, err := CompileExpr(`model.fraud_v1(model.embed_v1(features)) * 2.0`, registry, CompileOptions{
		Args: []ArgSpec{{Name: "features", Type: machine.ArrayOf(machine.FloatType)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := machine.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	quick := machine.NewBatch(runtime, machine.BatchOptions{MaxSize: 64, MaxWait: 5 * time.Millisecond, Run: machine.RunOptions{Fuel: 100}})
	value, _ := machine.ToValue([]float64{3})
	result, err := quick.Run(context.Background(), []machine.Value{value})
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := result.Float(); got != 6 || batched.Load() != 1 {
		t.Fatalf("lone result = %v, batched = %d", got, batched.Load())
	}
	quick.Close()
	if _, err := quick.Run(context.Background(), []machine.Value{value}); err == nil {
		t.Fatal("a closed batch accepted a request")
	}
}

// An engine error is filed per request and surfaces where the program would
// have made the call, with the function's name.
func TestBatchErrorsSurfaceAtTheCall(t *testing.T) {
	registry := machine.CoreRegistry()
	err := machine.Model(registry, "model.flaky_v1", machine.Doc{Cost: 1},
		func(x float64) (float64, error) { return x, nil },
		func(xs []float64) ([]float64, error) { return nil, fmt.Errorf("engine down") })
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := CompileExpr(`model.flaky_v1(x)`, registry, CompileOptions{Args: []ArgSpec{{Name: "x", Type: machine.FloatType}}})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := machine.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	batch := machine.NewBatch(runtime, machine.BatchOptions{MaxSize: 1})
	_, err = batch.Run(context.Background(), []machine.Value{machine.Float(1)})
	if !errors.Is(err, machine.ErrExtension) || !strings.Contains(err.Error(), "model.flaky_v1") || !strings.Contains(err.Error(), "engine down") {
		t.Fatalf("error = %v", err)
	}
	// Without a batch the per-request implementation answers.
	result, err := runtime.RunValues(context.Background(), []machine.Value{machine.Float(1)}, machine.RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := result.Float(); got != 1 {
		t.Fatalf("result = %v", got)
	}
}

func TestBatchPanicsAreContainedAndTyped(t *testing.T) {
	registry := machine.CoreRegistry()
	err := machine.Model(registry, "model.panic_v1", machine.Doc{Cost: 1},
		func(x float64) (float64, error) { return x, nil },
		func([]float64) ([]float64, error) { panic("batch exploded") })
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := CompileExpr(`model.panic_v1(x)`, registry, CompileOptions{Args: []ArgSpec{{Name: "x", Type: machine.FloatType}}})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := machine.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	batch := machine.NewBatch(runtime, machine.BatchOptions{MaxSize: 1})
	_, err = batch.Run(context.Background(), []machine.Value{machine.Float(1)})
	if !errors.Is(err, machine.ErrExtension) || !strings.Contains(err.Error(), "batch exploded") {
		t.Fatalf("batch panic = %v", err)
	}
}

func TestCanceledBatchRequestReturnsPromptly(t *testing.T) {
	var single, batched atomic.Int64
	registry := modelRegistry(t, &single, &batched)
	artifact, err := CompileExpr(`model.embed_v1(features)`, registry, CompileOptions{
		Args: []ArgSpec{{Name: "features", Type: machine.ArrayOf(machine.FloatType)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := machine.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	batch := machine.NewBatch(runtime, machine.BatchOptions{MaxSize: 64, MaxWait: time.Second})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	value, _ := machine.ToValue([]float64{1})
	go func() {
		_, runErr := batch.Run(ctx, []machine.Value{value})
		done <- runErr
	}()
	time.Sleep(5 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, machine.ErrDeadline) {
			t.Fatalf("canceled request = %v", err)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("canceled request stayed queued")
	}
	batch.Close()
	if batched.Load() != 0 {
		t.Fatalf("canceled request reached model batch %d times", batched.Load())
	}
}

func TestPrefetchedCallStillHonorsRequestCancellation(t *testing.T) {
	registry := machine.CoreRegistry()
	if err := machine.Model(registry, "model.score_v1", machine.Doc{Cost: 1},
		func(x float64) (float64, error) { return x, nil },
		func(xs []float64) ([]float64, error) { return xs, nil }); err != nil {
		t.Fatal(err)
	}
	artifact, err := CompileExpr(`model.score_v1(x)`, registry, CompileOptions{Args: []ArgSpec{{Name: "x", Type: machine.FloatType}}})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := machine.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	sites := machine.PrefetchSites(artifact)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = runtime.RunValues(ctx, []machine.Value{machine.Float(1)}, machine.RunOptions{
		Prefetched: map[int]machine.Prefetched{sites[0].PC: {Value: machine.Float(1)}},
	})
	if !errors.Is(err, machine.ErrDeadline) {
		t.Fatalf("canceled prefetched call = %v", err)
	}
}

func TestDetachedBatchStopsWaitingAtItsTimeout(t *testing.T) {
	registry := machine.CoreRegistry()
	release := make(chan struct{})
	err := machine.Model(registry, "model.stubborn_v1", machine.Doc{Cost: 1, Timeout: 5 * time.Millisecond, Detached: true},
		func(x float64) (float64, error) { return x, nil },
		func(xs []float64) ([]float64, error) { <-release; return xs, nil })
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := CompileExpr(`model.stubborn_v1(x)`, registry, CompileOptions{Args: []ArgSpec{{Name: "x", Type: machine.FloatType}}})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := machine.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	batch := machine.NewBatch(runtime, machine.BatchOptions{MaxSize: 1})
	started := time.Now()
	_, err = batch.Run(context.Background(), []machine.Value{machine.Float(1)})
	close(release)
	if !errors.Is(err, machine.ErrDeadline) || time.Since(started) > time.Second {
		t.Fatalf("detached batch = %v after %s", err, time.Since(started))
	}
}

// BenchmarkBatchVersusSingle models an engine with a fixed per-call overhead:
// batching amortises it across the requests of one batch.
func BenchmarkBatchVersusSingle(b *testing.B) {
	const overhead = 20 * time.Microsecond
	registry := machine.CoreRegistry()
	err := machine.Model(registry, "model.score_v1", machine.Doc{Cost: 10},
		func(x float64) (float64, error) { time.Sleep(overhead); return x, nil },
		func(xs []float64) ([]float64, error) { time.Sleep(overhead); return xs, nil })
	if err != nil {
		b.Fatal(err)
	}
	artifact, err := CompileExpr(`model.score_v1(x) + 1.0`, registry, CompileOptions{Args: []ArgSpec{{Name: "x", Type: machine.FloatType}}})
	if err != nil {
		b.Fatal(err)
	}
	runtime, err := machine.Instantiate(artifact, registry)
	if err != nil {
		b.Fatal(err)
	}
	args := []machine.Value{machine.Float(1)}
	b.Run("single", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			if _, err := runtime.RunValues(context.Background(), args, machine.RunOptions{}); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("batch=64", func(b *testing.B) {
		batch := machine.NewBatch(runtime, machine.BatchOptions{MaxSize: 64, MaxWait: time.Millisecond})
		defer batch.Close()
		var wg sync.WaitGroup
		for i := 0; i < b.N; i++ {
			wg.Add(1)
			go runInBatch(b, batch, args, &wg)
		}
		wg.Wait()
	})
}

func runInBatch(b *testing.B, batch *machine.Batch, args []machine.Value, wg *sync.WaitGroup) {
	defer wg.Done()
	if _, err := batch.Run(context.Background(), args); err != nil {
		b.Error(err)
	}
}
