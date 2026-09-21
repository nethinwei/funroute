package machine

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Batch runs one artifact for many requests, calling each model once per batch
// instead of once per request.
//
// The expression is not the expensive part of a routing decision; the model
// behind model.fraud_v3(features) is, and an inference engine wants a batch.
// So a Batch collects requests for up to MaxWait or MaxSize, runs every
// hoistable model call (see PrefetchSites) through the function's EvalBatch
// with all requests' arguments at once, then runs each request's program with
// those results already in hand. Calls the analysis cannot hoist, and
// functions without EvalBatch, still run per request inside the program, so a
// program behaves exactly as under Run — only faster.
type Batch struct {
	runtime *Runtime
	sites   []batchSite
	options BatchOptions

	mu      sync.Mutex
	pending []*batchRequest
	timer   *time.Timer
	closed  bool
}

// BatchOptions bounds a batch: it is flushed when MaxSize requests are waiting
// or MaxWait after the first one arrived, whichever comes first. Run applies
// to each program.
type BatchOptions struct {
	MaxSize int
	MaxWait time.Duration
	Run     RunOptions
}

type batchSite struct {
	PrefetchSite
	function *RegisteredFunction
}

type batchRequest struct {
	ctx   context.Context
	args  []Value
	done  chan struct{}
	value Value
	err   error
}

// NewBatch prepares the runtime's hoistable calls. Only functions registered
// with a batch implementation take part.
func NewBatch(runtime *Runtime, options BatchOptions) *Batch {
	if options.MaxSize <= 0 {
		options.MaxSize = 64
	}
	if options.MaxWait <= 0 {
		options.MaxWait = 2 * time.Millisecond
	}
	batch := &Batch{runtime: runtime, options: options}
	for _, site := range PrefetchSites(runtime.artifact) {
		function := runtime.functions[site.Call]
		if function.EvalBatch != nil {
			batch.sites = append(batch.sites, batchSite{PrefetchSite: site, function: function})
		}
	}
	return batch
}

// Sites reports how many calls this batch hoists, so a host can see whether a
// program benefits from batching at all.
func (b *Batch) Sites() int { return len(b.sites) }

// Run submits one request and blocks until its batch has run. It is safe to
// call from many goroutines; that is the point. The hoisted engine calls run
// under the earliest deadline in the batch, and each program under its own.
func (b *Batch) Run(ctx context.Context, args []Value) (Value, error) {
	if len(args) != len(b.runtime.artifact.Args) {
		return Value{}, fmt.Errorf("expected %d arguments, got %d", len(b.runtime.artifact.Args), len(args))
	}
	if ctx == nil {
		ctx = context.Background()
	}
	request := &batchRequest{ctx: ctx, args: args, done: make(chan struct{})}
	if err := b.enqueue(request); err != nil {
		return Value{}, err
	}
	<-request.done
	return request.value, request.err
}

func (b *Batch) enqueue(request *batchRequest) error {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return fmt.Errorf("batch is closed")
	}
	b.pending = append(b.pending, request)
	if len(b.pending) >= b.options.MaxSize {
		requests := b.take()
		b.mu.Unlock()
		b.execute(requests)
		return nil
	}
	if len(b.pending) == 1 {
		b.timer = time.AfterFunc(b.options.MaxWait, b.flush)
	}
	b.mu.Unlock()
	return nil
}

// take removes the pending requests; the caller holds the lock.
func (b *Batch) take() []*batchRequest {
	requests := b.pending
	b.pending = nil
	if b.timer != nil {
		b.timer.Stop()
		b.timer = nil
	}
	return requests
}

func (b *Batch) flush() {
	b.mu.Lock()
	requests := b.take()
	b.mu.Unlock()
	if len(requests) > 0 {
		b.execute(requests)
	}
}

// Close flushes what is waiting and refuses further requests.
func (b *Batch) Close() {
	b.mu.Lock()
	b.closed = true
	b.mu.Unlock()
	b.flush()
}

// execute is one batch: every hoisted call once, then every program.
func (b *Batch) execute(requests []*batchRequest) {
	prefetched := make([]map[int]Prefetched, len(requests))
	ctx, cancel := earliestDeadline(requests)
	defer cancel()
	for _, site := range b.sites {
		b.prefetch(ctx, site, requests, prefetched)
	}
	options := b.options.Run
	for i, request := range requests {
		options.Prefetched = prefetched[i]
		request.value, request.err = b.runtime.RunValues(request.ctx, request.args, options)
		close(request.done)
	}
}

// earliestDeadline is the budget a shared engine call runs under: the tightest
// deadline among the batch, or none when no request has one.
func earliestDeadline(requests []*batchRequest) (context.Context, context.CancelFunc) {
	var earliest time.Time
	for _, request := range requests {
		if deadline, ok := request.ctx.Deadline(); ok && (earliest.IsZero() || deadline.Before(earliest)) {
			earliest = deadline
		}
	}
	if earliest.IsZero() {
		return context.Background(), func() {}
	}
	return context.WithDeadline(context.Background(), earliest)
}

// prefetch runs one hoisted call for the whole batch and files each request's
// share of the answer under the call's program counter.
func (b *Batch) prefetch(ctx context.Context, site batchSite, requests []*batchRequest, prefetched []map[int]Prefetched) {
	calls := make([][]Value, len(requests))
	for i, request := range requests {
		calls[i] = b.operands(site, request.args)
	}
	if site.function.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, site.function.Timeout)
		defer cancel()
	}
	results, err := site.function.EvalBatch(ctx, calls)
	if err != nil && ctx.Err() != nil {
		err = fmt.Errorf("%w: %v", ErrDeadline, err)
	} else if err != nil {
		err = fmt.Errorf("%w: %v", ErrExtension, err)
	}
	if err == nil && len(results) != len(requests) {
		err = fmt.Errorf("batch returned %d results for %d requests", len(results), len(requests))
	}
	for i := range requests {
		if prefetched[i] == nil {
			prefetched[i] = make(map[int]Prefetched, len(b.sites))
		}
		if err != nil {
			prefetched[i][site.PC] = Prefetched{Err: err} // the call site adds the function's name
			continue
		}
		prefetched[i][site.PC] = Prefetched{Value: results[i]}
	}
}

// operands assembles one request's arguments for a hoisted call.
func (b *Batch) operands(site batchSite, args []Value) []Value {
	values := make([]Value, len(site.Operands))
	for i, operand := range site.Operands {
		if operand.Arg == NoArgument {
			values[i] = b.runtime.constants[operand.Constant]
		} else {
			values[i] = args[operand.Arg]
		}
	}
	return values
}
