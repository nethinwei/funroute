package machine

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/nethinwei/funroute/internal/kit"
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

	mu       sync.Mutex
	pending  []*batchRequest
	requests sync.Pool
	timer    *time.Timer
	closed   bool
}

// BatchOptions bounds a batch: it is flushed when MaxSize requests are waiting
// or MaxWait after the first one arrived, whichever comes first.
type BatchOptions struct {
	MaxSize int
	MaxWait time.Duration
}

type batchSite struct {
	PrefetchSite
	function *RegisteredFunction
	// call is the call of the register form the site is.
	call int
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
			batch.sites = append(batch.sites, batchSite{PrefetchSite: site, function: function, call: runtime.callAt(site.PC)})
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
	// Only their number here: admit holds each to its type in the batch.
	if err := b.runtime.checkKinds(args, true); err != nil {
		return Value{}, err
	}
	return b.submit(ctx, args, false)
}

func (b *Batch) submit(ctx context.Context, args []Value, typed bool) (Value, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return Value{}, kit.Classify(ErrDeadline, "", err)
	}
	request := b.newRequest(ctx, args, typed)
	if err := b.enqueue(request); err != nil {
		return Value{}, err
	}
	select {
	case result := <-request.done:
		// Answered, the request is nobody's any more.
		b.releaseRequest(request)
		return result.value, result.err
	case <-ctx.Done():
		// The batch still holds the request and will answer it: it is not
		// given back.
		return Value{}, kit.Classify(ErrDeadline, "", ctx.Err())
	}
}

// newRequest is a request from the batch's own pool of answered ones, its
// channel with it. The pool is the batch's: a channel belongs to where it
// was made — a synctest bubble refuses one from outside — and a batch is
// used where it was made.
func (b *Batch) newRequest(ctx context.Context, args []Value, typed bool) *batchRequest {
	request, ok := b.requests.Get().(*batchRequest)
	if !ok {
		request = &batchRequest{done: make(chan batchResult, 1)}
	}
	request.ctx, request.args, request.typed = ctx, args, typed
	return request
}

// releaseRequest gives an answered request back: its channel is empty.
func (b *Batch) releaseRequest(request *batchRequest) {
	request.ctx, request.args, request.result = nil, nil, batchResult{}
	b.requests.Put(request)
}

func (b *Batch) enqueue(request *batchRequest) error {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return errors.New("batch is closed")
	}
	b.pending = append(b.pending, request)
	if len(b.pending) >= b.options.MaxSize {
		requests := b.take()
		b.mu.Unlock()
		go b.execute(requests)
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

// execute is one batch from the queue. Its requests carry contexts of their
// own, so the engine calls run under the earliest of their deadlines, which
// has to be a context of its own.
func (b *Batch) execute(requests []*batchRequest) {
	active := b.rejectCanceled(requests)
	if len(active) == 0 {
		return
	}
	ctx, cancel := earliestDeadline(active)
	defer cancel()
	b.executeUnder(ctx, active)
}

// executeShared is one batch whose requests all carry ctx — a synchronous
// batch — so the engine calls run under it directly, with its values.
func (b *Batch) executeShared(ctx context.Context, requests []*batchRequest) {
	if active := b.rejectCanceled(requests); len(active) > 0 {
		b.executeUnder(ctx, active)
	}
}

// executeUnder is one batch: every hoisted call once under ctx, then every
// program under its request's own. A request whose arguments the program
// would refuse is answered first and left out: the engine sees only what a
// run of its own would have handed it.
func (b *Batch) executeUnder(ctx context.Context, active []*batchRequest) {
	active = b.admitted(active)
	// Every request's answers in one allocation: a share each, one slot
	// for each call of the program.
	calls := len(b.runtime.reg.calls)
	var answers []Prefetched
	if len(b.sites) > 0 {
		answers = make([]Prefetched, len(active)*calls)
	}
	for _, site := range b.sites {
		b.prefetch(ctx, site, active, answers)
	}
	for i, request := range active {
		var prefetched []Prefetched
		if answers != nil {
			prefetched = answers[i*calls : (i+1)*calls]
		}
		request.finish(b.run(request, prefetched))
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

func (b *Batch) run(request *batchRequest, prefetched []Prefetched) batchResult {
	if request.typed {
		value, err := b.runtime.runTyped(request.ctx, request.args, prefetched)
		return batchResult{value: value, err: err}
	}
	value, err := b.runtime.runValues(request.ctx, request.args, prefetched)
	return batchResult{value: value, err: err}
}

func (b *Batch) rejectCanceled(requests []*batchRequest) []*batchRequest {
	active := make([]*batchRequest, 0, len(requests))
	for _, request := range requests {
		if err := request.ctx.Err(); err != nil {
			request.finish(batchResult{err: kit.Classify(ErrDeadline, "", err)})
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
	// Requests that share a context — a Done channel — are watched once.
	stops := make([]func() bool, 0, len(requests))
	watched := make([]<-chan struct{}, 0, len(requests))
	for _, request := range requests {
		if done := request.ctx.Done(); done != nil && !slices.Contains(watched, done) {
			watched = append(watched, done)
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
// share of the answer in its slot for the call.
func (b *Batch) prefetch(ctx context.Context, site batchSite, requests []*batchRequest, answers []Prefetched) {
	calls := make([][]Value, len(requests))
	operands := make([]Value, len(requests)*len(site.Operands))
	for i, request := range requests {
		calls[i] = b.operands(site, request.args, operands[i*len(site.Operands):(i+1)*len(site.Operands)])
	}
	results, err := invokeBatch(ctx, site.function, calls)
	if err != nil {
		err = classifyUnder(ctx, err)
	} else if len(results) != len(requests) {
		err = fmt.Errorf("batch returned %d results for %d requests", len(results), len(requests))
	}
	stride := len(b.runtime.reg.calls)
	for i := range requests {
		slot := &answers[i*stride+site.call]
		if err != nil {
			*slot = Prefetched{Err: err, ready: true} // the call site adds the function's name
			continue
		}
		*slot = Prefetched{Value: results[i], ready: true}
	}
}

func invokeBatch(ctx context.Context, function *RegisteredFunction, calls [][]Value) ([]Value, error) {
	ctx, cancel := withTimeout(ctx, function)
	defer cancel()
	var results []Value
	var err error
	if function.Doc.Detached {
		results, err = detached(ctx, function, calls, callBatchSafely)
	} else {
		results, err = callBatchSafely(ctx, function, calls)
	}
	return results, timedOut(ctx, err)
}

func callBatchSafely(ctx context.Context, function *RegisteredFunction, calls [][]Value) (results []Value, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("batch extension panicked: %v", recovered)
		}
	}()
	return function.EvalBatch(ctx, calls)
}

// operands assembles one request's arguments for a hoisted call in values.
func (b *Batch) operands(site batchSite, args, values []Value) []Value {
	for i, operand := range site.Operands {
		if operand.Arg == NoArgument {
			values[i] = b.runtime.constants[operand.Constant]
		} else {
			values[i] = args[operand.Arg]
		}
	}
	return values
}
