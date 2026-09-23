package compile

import (
	"context"
	"errors"
	"math"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"funroute/lang/internal/machine"
)

// scoreIn carries an argument the program never reads, so the batch has to
// run arguments with an empty slot.
type scoreIn struct {
	Features []float64 `funroute:"features"`
	Amount   int64     `funroute:"amount"`
}

// failures collects what RunBatch reports, by request.
func failures() (map[int]error, func(int, error)) {
	failed := map[int]error{}
	return failed, func(i int, err error) { failed[i] = err }
}

func scoreProgram(t *testing.T, single, batched *atomic.Int64) *machine.Program[scoreIn, float64] {
	t.Helper()
	return bindProgram[scoreIn, float64](t, modelRegistry(t, single, batched),
		`let(e = model.embed_v1(features), model.fraud_v1(e) * 2.0)`)
}

// The host's N requests share one call of each batchable model, and a request
// that fails fails alone.
func TestRunBatchCallsEachModelOnce(t *testing.T) {
	var single, batched atomic.Int64
	program := scoreProgram(t, &single, &batched)
	requests := make([]scoreIn, 8)
	for i := range requests {
		requests[i] = scoreIn{Features: []float64{float64(i)}, Amount: int64(i)}
	}
	requests[3].Features = []float64{math.NaN()}
	errs, failed := failures()
	outs := program.RunBatch(context.Background(), requests, machine.RunOptions{}, failed)
	if len(errs) != 1 || !errors.Is(errs[3], machine.ErrContract) {
		t.Fatalf("errs = %v", errs)
	}
	for i, out := range outs {
		if i != 3 && out != float64(i)*2 {
			t.Fatalf("request %d = %v", i, out)
		}
	}
	if batched.Load() != 1 || single.Load() != 7 {
		t.Fatalf("batched = %d, single = %d", batched.Load(), single.Load())
	}
	errs, failed = failures()
	if program.RunBatch(context.Background(), requests[:3], machine.RunOptions{}, failed); len(errs) != 0 {
		t.Fatalf("a batch that succeeded reported errors: %v", errs)
	}
}

func TestRunBatchHonorsItsContext(t *testing.T) {
	var single, batched atomic.Int64
	program := scoreProgram(t, &single, &batched)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	errs, failed := failures()
	program.RunBatch(ctx, []scoreIn{{Features: []float64{1}}}, machine.RunOptions{}, failed)
	if len(errs) != 1 || !errors.Is(errs[0], machine.ErrDeadline) {
		t.Fatalf("errs = %v", errs)
	}
}

// Goroutines that each hold one request share the model call too.
func TestProgramBatchRunsConcurrentRequestsTogether(t *testing.T) {
	var single, batched atomic.Int64
	program := scoreProgram(t, &single, &batched)
	const requests = 8
	batch := program.Batch(machine.BatchOptions{MaxSize: requests, MaxWait: time.Second})
	defer batch.Close()
	if batch.Sites() != 1 {
		t.Fatalf("sites = %d", batch.Sites())
	}
	results := make([]float64, requests)
	var wg sync.WaitGroup
	for i := 0; i < requests; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			in := scoreIn{Features: []float64{float64(i)}}
			out, err := batch.Run(context.Background(), &in)
			if err != nil {
				t.Error(err)
			}
			results[i] = out
		}(i)
	}
	wg.Wait()
	for i, result := range results {
		if result != float64(i)*2 {
			t.Fatalf("request %d = %v", i, result)
		}
	}
	if batched.Load() != 1 {
		t.Fatalf("batched = %d", batched.Load())
	}
}

// Without somewhere to report a failure, a failed request would pass for a
// zero result; every batch entry refuses to run that way, and says which
// entry was called.
func TestRunBatchRequiresAFailureCallback(t *testing.T) {
	var single, batched atomic.Int64
	program := scoreProgram(t, &single, &batched)
	in := []scoreIn{{Features: []float64{1}}}
	out := []*float64{new(float64)}
	for method, call := range map[string]func(){
		"RunBatch":     func() { program.RunBatch(context.Background(), in, machine.RunOptions{}, nil) },
		"RunBatchInto": func() { program.RunBatchInto(context.Background(), in, out, machine.RunOptions{}, nil) },
		"RunBatchFunc": func() {
			program.RunBatchFunc(context.Background(), 1, func(int) *scoreIn { return &in[0] },
				func(int) *float64 { return out[0] }, machine.RunOptions{}, nil)
		},
	} {
		if got := panicOf(call); got != method+": failed must not be nil" {
			t.Errorf("%s panicked with %v", method, got)
		}
	}
}

func panicOf(call func()) (recovered any) {
	defer func() { recovered = recover() }()
	call()
	return nil
}

// failed is the host's own code on the host's goroutine, so its panic is the
// host's to see: it is not recovered into an error the way an extension's is.
// It runs after every program has finished and every frame is back in the
// pool, so the panic leaves nothing half done and the program runs on.
func TestRunBatchLetsTheCallbackPanicThrough(t *testing.T) {
	var single, batched atomic.Int64
	program := scoreProgram(t, &single, &batched)
	requests := []scoreIn{{Features: []float64{math.NaN()}}, {Features: []float64{1}}}
	func() {
		defer func() {
			if recover() != "host bug" {
				t.Fatal("the callback's panic did not reach the caller")
			}
		}()
		program.RunBatch(context.Background(), requests, machine.RunOptions{}, func(int, error) { panic("host bug") })
	}()
	errs, failed := failures()
	outs := program.RunBatch(context.Background(), requests, machine.RunOptions{}, failed)
	if len(errs) != 1 || outs[1] != 2 {
		t.Fatalf("after a panic: outs = %v, errs = %v", outs, errs)
	}
}

// Results land in the host's own objects, indexed like the requests; the
// models are still called once for the batch, and a request that fails — bad
// data, nowhere to put the result — fails alone, its destination zeroed.
func TestRunBatchIntoWritesWhereTheHostSays(t *testing.T) {
	var single, batched atomic.Int64
	program := scoreProgram(t, &single, &batched)
	in := make([]scoreIn, 5)
	out := make([]*float64, len(in))
	for i := range in {
		in[i] = scoreIn{Features: []float64{float64(i)}}
		sentinel := 99.0
		out[i] = &sentinel
	}
	in[1].Features = []float64{math.NaN()}
	out[2] = nil
	errs, failed := failures()
	program.RunBatchInto(context.Background(), in, out, machine.RunOptions{}, failed)
	if len(errs) != 2 || !errors.Is(errs[1], machine.ErrContract) || !errors.Is(errs[2], machine.ErrContract) {
		t.Fatalf("errs = %v", errs)
	}
	if *out[0] != 0 || *out[1] != 0 || *out[3] != 6 || *out[4] != 8 {
		t.Fatalf("out = %v %v %v %v", *out[0], *out[1], *out[3], *out[4])
	}
	if batched.Load() != 1 {
		t.Fatalf("batched = %d", batched.Load())
	}
}

type scoreRequest struct {
	ID    string
	Score scoreIn
}

type scoreResponse struct {
	ID    string
	Value float64
}

// The accessor shape reaches fields of larger objects, and one index names the
// request, its result and its failure. A nil request fails alone.
func TestRunBatchFuncReachesFieldsOfLargerObjects(t *testing.T) {
	var single, batched atomic.Int64
	program := scoreProgram(t, &single, &batched)
	requests := []*scoreRequest{{ID: "a", Score: scoreIn{Features: []float64{1}}}, nil, {ID: "c", Score: scoreIn{Features: []float64{3}}}}
	responses := make([]scoreResponse, len(requests))
	errs, failed := failures()
	program.RunBatchFunc(context.Background(), len(requests),
		func(i int) *scoreIn {
			if requests[i] == nil {
				return nil
			}
			return &requests[i].Score
		},
		func(i int) *float64 { return &responses[i].Value },
		machine.RunOptions{}, failed)
	if len(errs) != 1 || !errors.Is(errs[1], machine.ErrContract) {
		t.Fatalf("errs = %v", errs)
	}
	if responses[0].Value != 2 || responses[1].Value != 0 || responses[2].Value != 6 {
		t.Fatalf("responses = %+v", responses)
	}
}

func TestRunBatchIntoRequiresOneResultPerRequest(t *testing.T) {
	var single, batched atomic.Int64
	program := scoreProgram(t, &single, &batched)
	defer func() {
		if recover() == nil {
			t.Fatal("two requests ran into one result")
		}
	}()
	_, failed := failures()
	program.RunBatchInto(context.Background(), make([]scoreIn, 2), make([]*float64, 1), machine.RunOptions{}, failed)
}

type traceKey struct{}

// A synchronous batch runs its model calls under the host's own context, so
// what the host put there — a trace, a tenant — reaches the engine.
func TestRunBatchHandsTheEngineTheHostsContext(t *testing.T) {
	registry := machine.CoreRegistry()
	var seen any
	err := machine.Model(registry, "model.echo_v1", machine.Doc{Cost: 1},
		func(ctx context.Context, x float64) (float64, error) { return x, nil },
		func(ctx context.Context, xs []float64) ([]float64, error) {
			seen = ctx.Value(traceKey{})
			return xs, nil
		})
	if err != nil {
		t.Fatal(err)
	}
	type echoIn struct {
		X float64 `funroute:"x"`
	}
	program := bindProgram[echoIn, float64](t, registry, `model.echo_v1(x)`)
	ctx := context.WithValue(context.Background(), traceKey{}, "trace-1")
	_, failed := failures()
	if outs := program.RunBatch(ctx, []echoIn{{X: 1}, {X: 2}}, machine.RunOptions{}, failed); outs[1] != 2 {
		t.Fatalf("outs = %v", outs)
	}
	if seen != "trace-1" {
		t.Fatalf("the engine saw %v", seen)
	}
}

type pairOut struct {
	Channel string `funroute:"channel"`
	Net     int64  `funroute:"net"`
}

// A reused destination is the whole result every time: a rule that declares
// fewer fields than the Out has leaves the others zero, as Run would, not what
// the previous rule wrote there.
func TestRunBatchIntoLeavesNothingFromTheLastRule(t *testing.T) {
	registry := machine.CoreRegistry()
	binding, err := Bind[scalarIn, pairOut](registry)
	if err != nil {
		t.Fatal(err)
	}
	both, err := binding.Compile(`{channel: country, net: amount}`)
	if err != nil {
		t.Fatal(err)
	}
	netOnly, err := CompileExpr(`{net: amount}`, registry, CompileOptions{
		Args:   []ArgSpec{{Name: "amount", Type: machine.IntType}},
		Result: &machine.Type{Kind: machine.RecordKind, Fields: []machine.Field{{Name: "net", Type: machine.IntType}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	narrow, err := binding.Load(netOnly)
	if err != nil {
		t.Fatal(err)
	}
	in := []scalarIn{{Country: "SG", Amount: 1}}
	out := []*pairOut{{}}
	_, failed := failures()
	both.RunBatchInto(context.Background(), in, out, machine.RunOptions{}, failed)
	narrow.RunBatchInto(context.Background(), in, out, machine.RunOptions{}, failed)
	if *out[0] != (pairOut{Net: 1}) {
		t.Fatalf("out = %+v", *out[0])
	}
}

// A request that failed on its own is reported for why it failed, even when
// it also has nowhere to put a result.
func TestRunBatchFuncReportsTheRequestsOwnError(t *testing.T) {
	var single, batched atomic.Int64
	program := scoreProgram(t, &single, &batched)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	errs, failed := failures()
	in := scoreIn{Features: []float64{1}}
	program.RunBatchFunc(ctx, 1, func(int) *scoreIn { return &in }, func(int) *float64 { return nil }, machine.RunOptions{}, failed)
	if !errors.Is(errs[0], machine.ErrDeadline) {
		t.Fatalf("errs = %v", errs)
	}
}
