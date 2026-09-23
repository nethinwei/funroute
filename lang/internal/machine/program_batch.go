package machine

import (
	"context"
	"errors"
	"fmt"
)

// RunBatch runs the program once per request the host already holds, calling
// each batchable model once for all of them — the synchronous counterpart of
// a Batch, for a host that has its N requests in hand (N candidate channels to
// score, say) rather than N goroutines each holding one. ctx bounds the whole
// batch and options applies to every program.
//
// outs[i] answers in[i], and failed(i, err) names in[i]: one index for a
// request and its result, in every batch shape. A request that fails leaves
// outs[i] zero and is reported to failed, once, in index order, on the calling
// goroutine before RunBatch returns; one request failing does not fail the
// others. failed is required, so that a failure cannot pass for a zero
// result. All the requests' arguments share one allocation, and a batch that
// succeeds allocates nothing for errors. failed is the host's code, so a panic
// in it is not recovered; it happens after every program has run and every
// frame is back in the pool, and leaves the Program as usable as before.
func (p *Program[In, Out]) RunBatch(ctx context.Context, in []In, options RunOptions, failed func(i int, err error)) []Out {
	requireFailed("RunBatch", failed)
	outs := make([]Out, len(in))
	p.runBatchFunc(ctx, len(in), func(i int) *In { return &in[i] }, func(i int) *Out { return &outs[i] }, options, failed)
	return outs
}

// RunBatchInto is RunBatch for results that belong in objects the host already
// has — each request's own response, say: the result of in[i] is written
// straight into *out[i], so nothing is allocated or copied for results. in
// and out must be the same length.
func (p *Program[In, Out]) RunBatchInto(ctx context.Context, in []In, out []*Out, options RunOptions, failed func(i int, err error)) {
	requireFailed("RunBatchInto", failed)
	if len(in) != len(out) {
		panic(fmt.Sprintf("RunBatchInto: %d requests but %d results", len(in), len(out)))
	}
	p.runBatchFunc(ctx, len(in), func(i int) *In { return &in[i] }, func(i int) *Out { return out[i] }, options, failed)
}

// RunBatchFunc is the batch in whatever shape the host keeps it: request i's
// arguments are at in(i) and its result goes to out(i) — fields of larger
// objects, requests that live apart, a result buffer reused across batches.
// in(i) is called once per request before anything runs, out(i) once after
// its program has, both on the calling goroutine. A nil from either fails
// that request with ErrContract. The result is written in place, over a
// destination zeroed first: the Out is the whole result, fields the artifact
// does not declare included, exactly as Run would return it — so a host keeps
// its own data out of the Out it hands over, and a buffer reused across rules
// keeps nothing from the last one. A failed request's destination is left
// zero, never half written. Failures are reported as
// RunBatch reports them. The whole batch runs under ctx, model calls
// included, so its deadline and its values reach the engine.
func (p *Program[In, Out]) RunBatchFunc(ctx context.Context, n int, in func(i int) *In, out func(i int) *Out, options RunOptions, failed func(i int, err error)) {
	requireFailed("RunBatchFunc", failed)
	p.runBatchFunc(ctx, n, in, out, options, failed)
}

// requireFailed is the check every batch entry makes first, naming the entry
// the host called: without failed, a failure would pass for a zero result.
func requireFailed(method string, failed func(int, error)) {
	if failed == nil {
		panic(method + ": failed must not be nil")
	}
}

// runBatchFunc is the one implementation behind the three entries, which have
// already checked what they were given.
func (p *Program[In, Out]) runBatchFunc(ctx context.Context, n int, in func(i int) *In, out func(i int) *Out, options RunOptions, failed func(i int, err error)) {
	if ctx == nil {
		ctx = context.Background()
	}
	requests, active := p.encodeBatch(ctx, n, in)
	if len(active) > 0 {
		p.hoisted.execute(ctx, active, options)
	}
	for i := range requests {
		if err := p.decodeInPlace(requests[i].result, out(i)); err != nil {
			failed(i, err)
		}
	}
}

// encodeBatch lays every request's arguments out in one slice, each request a
// window of it. A request whose arguments do not encode keeps the error as its
// result and does not run.
func (p *Program[In, Out]) encodeBatch(ctx context.Context, n int, arg func(int) *In) ([]batchRequest, []*batchRequest) {
	width := len(p.args.params)
	args := make([]Value, n*width)
	requests := make([]batchRequest, n)
	active := make([]*batchRequest, 0, n)
	for i := range requests {
		window := args[i*width : (i+1)*width : (i+1)*width]
		err := errNilArguments
		if in := arg(i); in != nil {
			err = encodeArgs(p.args, p.reads, window, in)
		}
		if err != nil {
			requests[i].result.err = fmt.Errorf("%w: %v", ErrContract, err)
			continue
		}
		requests[i] = batchRequest{ctx: ctx, args: window, typed: true}
		active = append(active, &requests[i])
	}
	return requests, active
}

var errNilArguments = errors.New("arguments are nil")

// decodeInPlace makes the host's Out the request's result: zeroed first, so
// it is the whole result whatever the Out held before — fields the artifact
// does not declare included, as Run's fresh Out would have them — and left
// zero when the request failed. The request's own error comes first: a
// missing destination does not hide why it failed.
func (p *Program[In, Out]) decodeInPlace(result batchResult, out *Out) error {
	var zero Out
	if result.err != nil {
		if out != nil {
			*out = zero
		}
		return result.err
	}
	if out == nil {
		return fmt.Errorf("%w: result destination is nil", ErrContract)
	}
	*out = zero
	if err := decodeResult(p.result, result.value, out); err != nil {
		*out = zero
		return fmt.Errorf("%w: result: %v", ErrContract, err)
	}
	return nil
}

// A ProgramBatch is a Batch that speaks the program's Go types: many
// goroutines each Run one request, and requests that arrive within MaxWait
// (or MaxSize of them) share each model call.
type ProgramBatch[In, Out any] struct {
	args   *argsCodec
	result *codec
	reads  []int
	batch  *Batch
}

// Batch starts a concurrent batch for this program. Close it when done.
func (p *Program[In, Out]) Batch(options BatchOptions) *ProgramBatch[In, Out] {
	return &ProgramBatch[In, Out]{args: p.args, result: p.result, reads: p.reads, batch: NewBatch(p.runtime, options)}
}

// Run submits one request and blocks until its batch has run. The request's
// arguments are converted before it is queued, so in is not read afterwards;
// that queued copy is the one allocation Program.Run does not make.
func (b *ProgramBatch[In, Out]) Run(ctx context.Context, in *In) (Out, error) {
	var zero Out
	if in == nil {
		return zero, fmt.Errorf("%w: arguments are nil", ErrContract)
	}
	args := make([]Value, len(b.args.params))
	if err := encodeArgs(b.args, b.reads, args, in); err != nil {
		return zero, fmt.Errorf("%w: %v", ErrContract, err)
	}
	value, err := b.batch.submit(ctx, args, true)
	if err != nil {
		return zero, err
	}
	return decodeInto[Out](b.result, value)
}

// Sites reports how many calls the batch hoists; zero means batching buys
// this program nothing.
func (b *ProgramBatch[In, Out]) Sites() int { return b.batch.Sites() }

// Close flushes what is waiting and refuses further requests.
func (b *ProgramBatch[In, Out]) Close() { b.batch.Close() }
