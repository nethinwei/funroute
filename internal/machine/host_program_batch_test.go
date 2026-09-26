package machine_test

import (
	"context"
	"errors"
	"math"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/nethinwei/funroute/internal/compile"
	"github.com/nethinwei/funroute/internal/machine"
)

type scalarIn struct {
	Country string `funroute:"country"`
	Amount  int64  `funroute:"amount"`
}

func bindProgram[In, Out any](t *testing.T, registry *machine.Registry, source string) *machine.Program[In, Out] {
	t.Helper()
	binding, err := compile.Bind[In, Out](registry)
	if err != nil {
		t.Fatal(err)
	}
	program, err := binding.Compile(source)
	if err != nil {
		t.Fatal(err)
	}
	return program
}

// failures collects what RunBatch reports, by request.
func failures() (map[int]error, func(int, error)) {
	failed := map[int]error{}
	return failed, func(i int, err error) { failed[i] = err }
}

// scoreIn carries an argument the program never reads, so the batch has to
// run arguments with an empty slot, and one it does that no int holds past
// math.MaxInt64: a request with bad data.
type scoreIn struct {
	Features []float64 `funroute:"features"`
	Amount   int64     `funroute:"amount"`
	Weight   uint      `funroute:"weight"`
}

// badData is a weight the program cannot read.
const badData = math.MaxUint64

func scoreProgram(t *testing.T, single, batched *atomic.Int64) *machine.Program[scoreIn, float64] {
	t.Helper()
	return bindProgram[scoreIn, float64](t, modelRegistry(t, single, batched),
		`let(e = model.embed_v1(features), model.fraud_v1(e) * 2.0 + float(weight))`)
}

// The host's N requests share one call of each batchable model, and a request
// that fails fails alone.
func TestRunBatchCallsEachModelOnce(t *testing.T) {
	t.Parallel()
	var single, batched atomic.Int64
	program := scoreProgram(t, &single, &batched)
	requests := make([]scoreIn, 8)
	for i := range requests {
		requests[i] = scoreIn{Features: []float64{float64(i)}, Amount: int64(i)}
	}
	requests[3].Weight = badData
	errs, failed := failures()
	outs := runSlice(t.Context(), program, requests, failed)
	if len(errs) != 1 || !errors.Is(errs[3], machine.ErrContract) {
		t.Fatalf("errs = %v, want one ErrContract at 3", errs)
	}
	for i, out := range outs {
		if i != 3 && out != float64(i)*2 {
			t.Fatalf("request %d = %v, want %v", i, out, float64(i)*2)
		}
	}
	if batched.Load() != 1 || single.Load() != 7 {
		t.Fatalf("batched = %d, single = %d, want 1 and 7", batched.Load(), single.Load())
	}
	errs, failed = failures()
	if runSlice(t.Context(), program, requests[:3], failed); len(errs) != 0 {
		t.Fatalf("a batch that succeeded reported errors: %v", errs)
	}
}

func TestRunBatchHonorsItsContext(t *testing.T) {
	t.Parallel()
	var single, batched atomic.Int64
	program := scoreProgram(t, &single, &batched)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	errs, failed := failures()
	runSlice(ctx, program, []scoreIn{{Features: []float64{1}}}, failed)
	if len(errs) != 1 || !errors.Is(errs[0], machine.ErrDeadline) {
		t.Fatalf("errs = %v, want one ErrDeadline at 0", errs)
	}
}

// Goroutines that each hold one request share the model call too. The batch
// runs on synctest's clock, so it fills before its MaxWait however slowly the
// goroutines start.
func TestProgramBatchRunsConcurrentRequestsTogether(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var single, batched atomic.Int64
		program := scoreProgram(t, &single, &batched)
		const requests = 8
		batch := program.Batch(machine.BatchOptions{MaxSize: requests, MaxWait: time.Second})
		defer batch.Close()
		if batch.Sites() != 1 {
			t.Fatalf("sites = %d, want 1", batch.Sites())
		}
		results := scoreConcurrently(t, batch, requests)
		for i, result := range results {
			if result != float64(i)*2 {
				t.Fatalf("request %d = %v, want %v", i, result, float64(i)*2)
			}
		}
		if batched.Load() != 1 {
			t.Fatalf("batched = %d, want 1", batched.Load())
		}
	})
}

// scoreConcurrently submits requests to batch from a goroutine each, request
// i with features [i], and returns the results by request.
func scoreConcurrently(t *testing.T, batch *machine.ProgramBatch[scoreIn, float64], requests int) []float64 {
	t.Helper()
	ctx := t.Context()
	results := make([]float64, requests)
	var wg sync.WaitGroup
	for i := range requests {
		wg.Go(func() {
			in := scoreIn{Features: []float64{float64(i)}}
			out, err := batch.Run(ctx, &in)
			if err != nil {
				t.Error(err)
			}
			results[i] = out
		})
	}
	wg.Wait()
	return results
}

// Without somewhere to report a failure, a failed request would pass for a
// zero result; every batch entry refuses to run that way, and says which
// entry was called.
func TestRunBatchRequiresAFailureCallback(t *testing.T) {
	t.Parallel()
	var single, batched atomic.Int64
	program := scoreProgram(t, &single, &batched)
	in, out := scoreIn{Features: []float64{1}}, 0.0
	call := func() {
		program.RunBatch(t.Context(), 1, func(int) *scoreIn { return &in }, func(int) *float64 { return &out }, nil)
	}
	if got, want := panicOf(call), "RunBatch: failed must not be nil"; got != want {
		t.Errorf("RunBatch panicked with %v, want %q", got, want)
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
	t.Parallel()
	var single, batched atomic.Int64
	program := scoreProgram(t, &single, &batched)
	requests := []scoreIn{{Features: []float64{0}, Weight: badData}, {Features: []float64{1}}}
	func() {
		defer func() {
			if recover() != "host bug" {
				t.Fatal("the callback's panic did not reach the caller")
			}
		}()
		runSlice(t.Context(), program, requests, func(int, error) { panic("host bug") })
	}()
	errs, failed := failures()
	outs := runSlice(t.Context(), program, requests, failed)
	if len(errs) != 1 || outs[1] != 2 {
		t.Fatalf("after a panic: outs = %v, errs = %v, want outs[1] = 2 and one error", outs, errs)
	}
}

// Results land in the host's own objects, indexed like the requests; the
// models are still called once for the batch, and a request that fails — bad
// data, nowhere to put the result — fails alone, its destination zeroed.
func TestRunBatchWritesWhereTheHostSays(t *testing.T) {
	t.Parallel()
	var single, batched atomic.Int64
	program := scoreProgram(t, &single, &batched)
	in := make([]scoreIn, 5)
	out := make([]*float64, len(in))
	for i := range in {
		in[i] = scoreIn{Features: []float64{float64(i)}}
		sentinel := 99.0
		out[i] = &sentinel
	}
	in[1].Weight = badData
	out[2] = nil
	errs, failed := failures()
	runInto(t.Context(), program, in, out, failed)
	if len(errs) != 2 || !errors.Is(errs[1], machine.ErrContract) || !errors.Is(errs[2], machine.ErrContract) {
		t.Fatalf("errs = %v, want ErrContract at 1 and 2", errs)
	}
	if *out[0] != 0 || *out[1] != 0 || *out[3] != 6 || *out[4] != 8 {
		t.Fatalf("out = %v %v %v %v, want 0 0 6 8", *out[0], *out[1], *out[3], *out[4])
	}
	if batched.Load() != 1 {
		t.Fatalf("batched = %d, want 1", batched.Load())
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
func TestRunBatchReachesFieldsOfLargerObjects(t *testing.T) {
	t.Parallel()
	var single, batched atomic.Int64
	program := scoreProgram(t, &single, &batched)
	requests := []*scoreRequest{{ID: "a", Score: scoreIn{Features: []float64{1}}}, nil, {ID: "c", Score: scoreIn{Features: []float64{3}}}}
	responses := make([]scoreResponse, len(requests))
	errs, failed := failures()
	program.RunBatch(t.Context(), len(requests),
		func(i int) *scoreIn {
			if requests[i] == nil {
				return nil
			}
			return &requests[i].Score
		},
		func(i int) *float64 { return &responses[i].Value }, failed)
	if len(errs) != 1 || !errors.Is(errs[1], machine.ErrContract) {
		t.Fatalf("errs = %v, want one ErrContract at 1", errs)
	}
	if responses[0].Value != 2 || responses[1].Value != 0 || responses[2].Value != 6 {
		t.Fatalf("responses = %+v, want values 2, 0, 6", responses)
	}
}

type traceKey struct{}

// A synchronous batch runs its model calls under the host's own context, so
// what the host put there — a trace, a tenant — reaches the engine.
func TestRunBatchHandsTheEngineTheHostsContext(t *testing.T) {
	t.Parallel()
	registry := machine.CoreRegistry()
	var seen any
	err := registry.Register(machine.FunctionSpec{
		Name: "model.echo_v1",
		Go:   func(ctx context.Context, x float64) (float64, error) { return x, nil },
		GoBatch: func(ctx context.Context, xs []float64) ([]float64, error) {
			seen = ctx.Value(traceKey{})
			return xs, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	type echoIn struct {
		X float64 `funroute:"x"`
	}
	program := bindProgram[echoIn, float64](t, registry, `model.echo_v1(x)`)
	ctx := context.WithValue(t.Context(), traceKey{}, "trace-1")
	_, failed := failures()
	if outs := runSlice(ctx, program, []echoIn{{X: 1}, {X: 2}}, failed); outs[1] != 2 {
		t.Fatalf("outs = %v, want outs[1] = 2", outs)
	}
	if seen != "trace-1" {
		t.Fatalf("the engine saw %v, want trace-1", seen)
	}
}

type pairOut struct {
	Channel string `funroute:"channel"`
	Net     int64  `funroute:"net"`
}

// A reused destination is the whole result every time: a rule that declares
// fewer fields than the Out has leaves the others zero, as Run would, not what
// the previous rule wrote there.
func TestRunBatchLeavesNothingFromTheLastRule(t *testing.T) {
	t.Parallel()
	registry := machine.CoreRegistry()
	binding, err := compile.Bind[scalarIn, pairOut](registry)
	if err != nil {
		t.Fatal(err)
	}
	both, err := binding.Compile(`{channel: country, net: amount}`)
	if err != nil {
		t.Fatal(err)
	}
	netOnly, err := compile.CompileExpr(`{net: amount}`, registry, compile.CompileOptions{
		Args:   []compile.ArgSpec{{Name: "amount", Type: machine.IntType}},
		Result: new(machine.RecordOf(machine.FieldOf("net", machine.IntType))),
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
	runInto(t.Context(), both, in, out, failed)
	runInto(t.Context(), narrow, in, out, failed)
	if *out[0] != (pairOut{Net: 1}) {
		t.Fatalf("out = %+v, want %+v", *out[0], pairOut{Net: 1})
	}
}

// A request that failed on its own is reported for why it failed, even when
// it also has nowhere to put a result.
func TestRunBatchReportsTheRequestsOwnError(t *testing.T) {
	t.Parallel()
	var single, batched atomic.Int64
	program := scoreProgram(t, &single, &batched)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	errs, failed := failures()
	in := scoreIn{Features: []float64{1}}
	program.RunBatch(ctx, 1, func(int) *scoreIn { return &in }, func(int) *float64 { return nil }, failed)
	if !errors.Is(errs[0], machine.ErrDeadline) {
		t.Fatalf("errs = %v, want ErrDeadline at 0", errs)
	}
}

// runSlice runs a batch whose requests and results are slices, the shape a
// host most often has.
func runSlice[In, Out any](ctx context.Context, program *machine.Program[In, Out], in []In, failed func(int, error)) []Out {
	out := make([]Out, len(in))
	program.RunBatch(ctx, len(in), func(i int) *In { return &in[i] }, func(i int) *Out { return &out[i] }, failed)
	return out
}

// runInto runs a batch whose results go into objects the host already has.
func runInto[In, Out any](ctx context.Context, program *machine.Program[In, Out], in []In, out []*Out, failed func(int, error)) {
	program.RunBatch(ctx, len(in), func(i int) *In { return &in[i] }, func(i int) *Out { return out[i] }, failed)
}
