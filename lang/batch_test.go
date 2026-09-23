package lang_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"funroute/lang"
)

// A model's tensor crosses the expression as an opaque handle, and a batch of
// requests calls the model once. This is the deep-learning integration a host
// writes, end to end, with the public API only.

// tensor stands in for an engine's tensor type.
type tensor struct{ rows [][]float64 }

// modelRegistry defines the engine's type as a handle and registers a model
// with a batch implementation that counts how often it ran.
func modelRegistry(t *testing.T, batches *int) *lang.Registry {
	t.Helper()
	registry := lang.CoreRegistry()
	if err := lang.DefineHandle[*tensor](registry, "engine.tensor"); err != nil {
		t.Fatal(err)
	}
	err := lang.Model(registry, "model.embed_v1", lang.Doc{Cost: 10},
		func(features []float64) (*tensor, error) { return &tensor{rows: [][]float64{features}}, nil },
		func(features [][]float64) ([]*tensor, error) {
			*batches++
			out := make([]*tensor, len(features))
			for i, row := range features {
				out[i] = &tensor{rows: [][]float64{row}}
			}
			return out, nil
		})
	if err != nil {
		t.Fatal(err)
	}
	err = lang.Logic(registry, "model.score_v1", lang.Doc{Cost: 10}, func(t *tensor) (float64, error) { return t.rows[0][0], nil })
	if err != nil {
		t.Fatal(err)
	}
	return registry
}

// Not parallel: the two requests must reach the batch within MaxWait of each
// other to be one batch, and a loaded machine running every other test at the
// same time is what could stretch that.
func TestHostBatchesModelCallsAcrossRequests(t *testing.T) {
	var batches int
	registry := modelRegistry(t, &batches)
	artifact, err := lang.CompileExpr(`let(e = model.embed_v1(features), if(model.score_v1(e) > 0.5, "review", "accept"))`, registry,
		lang.CompileOptions{Args: []lang.ArgSpec{{Name: "features", Type: lang.ArrayOf(lang.FloatType)}}})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := lang.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	batch := lang.NewBatch(runtime, lang.BatchOptions{MaxSize: 2, MaxWait: time.Second})
	defer batch.Close()
	results := make([]string, 2)
	ctx := t.Context()
	var wg sync.WaitGroup
	for i, score := range []float64{0.9, 0.1} {
		wg.Go(func() {
			features, _ := lang.ToValue([]float64{score})
			result, err := batch.Run(ctx, []lang.Value{features})
			if err != nil {
				t.Errorf("batch.Run(features=[%v]) error = %v", score, err)
				return
			}
			results[i], _ = result.String()
		})
	}
	wg.Wait()
	if results[0] != "review" || results[1] != "accept" || batches != 1 {
		t.Fatalf("results = %v in %d batches, want [review accept] in 1", results, batches)
	}
	// An expired budget is refused at the first model call, typed.
	expired, cancel := context.WithCancel(t.Context())
	cancel()
	features, _ := lang.ToValue([]float64{0.9})
	if _, err := runtime.RunValues(expired, []lang.Value{features}, lang.RunOptions{}); !errors.Is(err, lang.ErrDeadline) {
		t.Fatalf("RunValues(cancelled ctx) error = %v, want ErrDeadline", err)
	}
	if handles := registry.Handles(); len(handles) != 1 || handles[0].String() != "handle<engine.tensor>" {
		t.Fatalf("handles = %+v, want [handle<engine.tensor>]", handles)
	}
}
