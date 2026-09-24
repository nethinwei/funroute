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

// batchRequest is one program run inside a batch. A request that someone is
// waiting on has a done channel; one from a synchronous RunBatch has none, and
// its result is simply left in place.
type batchRequest struct {
	ctx    context.Context
	args   []Value
	done   chan batchResult
	result batchResult
	// typed says a codec produced args: typed by construction, with the slots
	// the program never reads left empty (see Runtime.runTyped).
	typed bool
}

func (r *batchRequest) finish(result batchResult) {
	if r.done == nil {
		r.result = result
		return
	}
	r.done <- result
}

type batchResult struct {
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
	if len(args) != len(b.runtime.artifact.parts.Args) {
		return Value{}, fmt.Errorf("%w: expected %d arguments, got %d", ErrContract, len(b.runtime.artifact.parts.Args), len(args))
	}
	return b.submit(ctx, args, false)
}

func (b *Batch) submit(ctx context.Context, args []Value, typed bool) (Value, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return Value{}, fmt.Errorf("%w: %v", ErrDeadline, err)
	}
	request := &batchRequest{ctx: ctx, args: args, done: make(chan batchResult, 1), typed: typed}
	if err := b.enqueue(request); err != nil {
		return Value{}, err
	}
	select {
	case result := <-request.done:
		return result.value, result.err
	case <-ctx.Done():
		return Value{}, fmt.Errorf("%w: %v", ErrDeadline, ctx.Err())
	}
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
		go b.execute(requests, b.options.Run)
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
		b.execute(requests, b.options.Run)
	}
}

// Close flushes what is waiting and refuses further requests.
func (b *Batch) Close() {
	b.mu.Lock()
	b.closed = true
	b.mu.Unlock()
	b.flush()
}

// execute is one batch from the queue. Its requests carry contexts of their
// own, so the engine calls run under the earliest of their deadlines, which
// has to be a context of its own.
func (b *Batch) execute(requests []*batchRequest, options RunOptions) {
	active := b.rejectCanceled(requests)
	if len(active) == 0 {
		return
	}
	ctx, cancel := earliestDeadline(active)
	defer cancel()
	b.executeUnder(ctx, active, options)
}

// executeShared is one batch whose requests all carry ctx — a synchronous
// batch — so the engine calls run under it directly, with its values.
func (b *Batch) executeShared(ctx context.Context, requests []*batchRequest, options RunOptions) {
	active := b.rejectCanceled(requests)
	if len(active) == 0 {
		return
	}
	b.executeUnder(ctx, active, options)
}

// executeUnder is one batch: every hoisted call once under ctx, then every
// program under its request's own. A request whose arguments the program
// would refuse is answered first and left out: the engine sees only what a
// run of its own would have handed it.
func (b *Batch) executeUnder(ctx context.Context, active []*batchRequest, options RunOptions) {
	active = b.admitted(active)
	prefetched := make([]map[int]Prefetched, len(active))
	for _, site := range b.sites {
		b.prefetch(ctx, site, active, prefetched)
	}
	for i, request := range active {
		options.prefetched = prefetched[i]
		request.finish(b.run(request, options))
	}
}

// admitted answers the requests whose arguments do not fit the contract and
// keeps the rest, in order.
func (b *Batch) admitted(active []*batchRequest) []*batchRequest {
	kept := active[:0]
	for _, request := range active {
		if err := b.runtime.admit(request.args, request.typed); err != nil {
			request.finish(batchResult{err: err})
			continue
		}
		kept = append(kept, request)
	}
	return kept
}

func (b *Batch) run(request *batchRequest, options RunOptions) batchResult {
	if request.typed {
		value, err := b.runtime.runTyped(request.ctx, request.args, options)
		return batchResult{value: value, err: err}
	}
	value, err := b.runtime.RunValues(request.ctx, request.args, options)
	return batchResult{value: value, err: err}
}

func (b *Batch) rejectCanceled(requests []*batchRequest) []*batchRequest {
	active := make([]*batchRequest, 0, len(requests))
	for _, request := range requests {
		if err := request.ctx.Err(); err != nil {
			request.finish(batchResult{err: fmt.Errorf("%w: %v", ErrDeadline, err)})
			continue
		}
		active = append(active, request)
	}
	return active
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
	var ctx context.Context
	var cancel context.CancelFunc
	if earliest.IsZero() {
		ctx, cancel = context.WithCancel(context.Background())
	} else {
		ctx, cancel = context.WithDeadline(context.Background(), earliest)
	}
	stops := make([]func() bool, 0, len(requests))
	for _, request := range requests {
		if request.ctx.Done() != nil {
			stops = append(stops, context.AfterFunc(request.ctx, cancel))
		}
	}
	return ctx, func() {
		for _, stop := range stops {
			stop()
		}
		cancel()
	}
}

// prefetch runs one hoisted call for the whole batch and files each request's
// share of the answer under the call's program counter.
func (b *Batch) prefetch(ctx context.Context, site batchSite, requests []*batchRequest, prefetched []map[int]Prefetched) {
	calls := make([][]Value, len(requests))
	for i, request := range requests {
		calls[i] = b.operands(site, request.args)
	}
	results, err := invokeBatch(ctx, site.function, calls)
	if err != nil && !keepsIdentity(err) && ctx.Err() != nil {
		err = fmt.Errorf("%w: %v", ErrDeadline, err)
	} else if err != nil && !keepsIdentity(err) {
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

func invokeBatch(ctx context.Context, function *RegisteredFunction, calls [][]Value) ([]Value, error) {
	if function.Doc.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, function.Doc.Timeout)
		defer cancel()
	}
	var results []Value
	var err error
	if function.Doc.Detached {
		results, err = callBatchDetached(ctx, function, calls)
	} else {
		results, err = callBatchSafely(ctx, function, calls)
	}
	if err != nil && ctx.Err() != nil && !keepsIdentity(err) {
		return nil, fmt.Errorf("%w: %v", ErrDeadline, err)
	}
	return results, err
}

func callBatchSafely(ctx context.Context, function *RegisteredFunction, calls [][]Value) (results []Value, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("batch extension panicked: %v", recovered)
		}
	}()
	return function.EvalBatch(ctx, calls)
}

func callBatchDetached(ctx context.Context, function *RegisteredFunction, calls [][]Value) ([]Value, error) {
	type outcome struct {
		values []Value
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		values, err := callBatchSafely(ctx, function, calls)
		done <- outcome{values: values, err: err}
	}()
	select {
	case result := <-done:
		return result.values, result.err
	case <-ctx.Done():
		return nil, ctx.Err()
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
