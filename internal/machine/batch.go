package machine

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"
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
// functions without EvalBatch, still run per request inside the program.
//
// A program answers as under Run when the batched functions are pure and
// read no more of a context than the first request's values: one engine call
// serves the batch, so it is made once, under the first request's values,
// and fails for all its requests if it fails. No request ends it for the
// others — it is cancelled once none is waiting, and bounded by the latest
// deadline among them — and each request waits no longer than its own
// context (docs/semantics.md).
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
}

// batchPlan is the calls of a program a Batch hoists, found once when the
// program loads: each site, and by call of the register form its slot among
// the sites, -1 for a call no batch hoists. A request's answers are one slot
// a site, not one a call. Empty for a program that calls no function with a
// batch implementation.
type batchPlan struct {
	sites []batchSite
	slots []int32
}

func planBatch(r *Runtime) batchPlan {
	if !slices.ContainsFunc(r.functions, func(function *RegisteredFunction) bool { return function.EvalBatch != nil }) {
		return batchPlan{}
	}
	plan := batchPlan{slots: slices.Repeat([]int32{-1}, len(r.reg.calls))}
	for _, site := range PrefetchSites(r.artifact) {
		function, call := r.functions[site.Call], r.callAt(site.PC)
		if function.EvalBatch == nil || call < 0 {
			continue
		}
		plan.slots[call] = int32(len(plan.sites))
		plan.sites = append(plan.sites, batchSite{PrefetchSite: site, function: function})
	}
	return plan
}

// batchRequest is one program run inside a batch. A request that someone is
// waiting on has a done channel; one from a synchronous RunBatch has none, and
// its result is simply left in place.
type batchRequest struct {
	ctx    context.Context
	args   []Value
	done   chan batchResult
	result batchResult
	// state is whether the request is still waiting to be taken, taken by
	// the batch that runs it, or abandoned by a Run whose context ended
	// first: one the batch has not taken is never read.
	state atomic.Int32
	// run is the batch that takes the request, set before it is taken;
	// stopped says the request no longer waits for it (stopWaiting).
	run     *batchRun
	stopped atomic.Bool
	// typed says a codec produced args: typed by construction, with the slots
	// the program never reads left empty (see Runtime.runTyped).
	typed bool
}

const (
	requestWaiting int32 = iota
	requestTaken
	requestAbandoned
)

func (r *batchRequest) finish(result batchResult) {
	r.stopWaiting()
	if r.done == nil {
		r.result = result
		return
	}
	r.done <- result
}

// stopWaiting is the request no longer waiting for the batch that took it:
// answered, or its Run gave up. Either may come first, and the second does
// nothing, so the batch's count goes down once a request.
func (r *batchRequest) stopWaiting() {
	if r.run != nil && r.stopped.CompareAndSwap(false, true) {
		r.run.stop()
	}
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
	return &Batch{runtime: runtime, options: options, sites: runtime.batch.sites}
}

// Sites reports how many calls this batch hoists, so a host can see whether a
// program benefits from batching at all.
func (b *Batch) Sites() int { return len(b.sites) }

// Run submits one request and blocks until its batch has run. It is safe to
// call from many goroutines; that is the point. The hoisted engine calls run
// under the latest deadline in the batch, and each program under its own.
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
		// Not taken yet, the request is dropped, and the batch never reads
		// its arguments. Taken, its run reads them still: Run waits for it,
		// so the caller may reuse them once Run returns.
		if !request.state.CompareAndSwap(requestWaiting, requestAbandoned) {
			request.stopWaiting()
			<-request.done
			b.releaseRequest(request)
		}
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
	request.state.Store(requestWaiting)
	request.stopped.Store(false)
	return request
}

// releaseRequest gives an answered request back: its channel is empty.
func (b *Batch) releaseRequest(request *batchRequest) {
	request.ctx, request.args, request.result, request.run = nil, nil, batchResult{}, nil
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

// execute is one batch from the queue: the requests it can still take.
// They carry contexts of their own, so the engine calls run under one made
// for the batch: the first request's values, the latest deadline among the
// requests — none when one has none — and a cancellation once every request
// has stopped waiting (batchRun), so that none fails for another's sake.
func (b *Batch) execute(requests []*batchRequest) {
	run := &batchRun{}
	ctx, cancel := context.WithCancel(context.WithoutCancel(requests[0].ctx))
	run.cancel = cancel
	defer cancel()
	run.waiting.Store(int64(len(requests)))
	active := b.rejectCanceled(b.taken(requests, run))
	if len(active) == 0 {
		return
	}
	if latest, bounded := latestDeadline(active); bounded {
		var stop context.CancelFunc
		ctx, stop = context.WithDeadline(ctx, latest)
		defer stop()
	}
	b.executeUnder(ctx, active)
}

// batchRun counts the requests of one batch still waiting for it: each
// whose context ends stops waiting — a Run that abandons it, or one that
// finds it taken — and so does each the batch answers, one it refuses before
// the engine calls among them; when none is left the engine calls are
// cancelled. A request whose context never ends waits until it is answered.
type batchRun struct {
	waiting atomic.Int64
	cancel  context.CancelFunc
}

func (r *batchRun) stop() {
	if r.waiting.Add(-1) == 0 {
		r.cancel()
	}
}

// taken is the requests the batch takes: those no Run has abandoned, which
// stop waiting and go back to the pool unread. Each learns its run first,
// so a Run that finds it taken has one to tell.
func (b *Batch) taken(requests []*batchRequest, run *batchRun) []*batchRequest {
	kept := requests[:0]
	for _, request := range requests {
		request.run = run
		if request.state.CompareAndSwap(requestWaiting, requestTaken) {
			kept = append(kept, request)
		} else {
			run.stop()
			b.releaseRequest(request)
		}
	}
	return kept
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
	if active = b.admitted(active); len(active) == 0 {
		return
	}
	// Every request's answers in one allocation: a share each, one slot
	// for each site the batch hoists.
	sites := len(b.sites)
	var answers []Prefetched
	if sites > 0 {
		answers = make([]Prefetched, len(active)*sites)
	}
	for slot, site := range b.sites {
		b.prefetch(ctx, slot, site, active, answers)
	}
	for i, request := range active {
		var prefetched []Prefetched
		if answers != nil {
			prefetched = answers[i*sites : (i+1)*sites]
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

// latestDeadline is the latest deadline among the requests, and false when
// one has none.
func latestDeadline(requests []*batchRequest) (time.Time, bool) {
	var latest time.Time
	for _, request := range requests {
		deadline, ok := request.ctx.Deadline()
		if !ok {
			return time.Time{}, false
		}
		if deadline.After(latest) {
			latest = deadline
		}
	}
	return latest, true
}

// prefetch runs one hoisted call for the whole batch and files each request's
// share of the answer in its slot for the call.
func (b *Batch) prefetch(ctx context.Context, slot int, site batchSite, requests []*batchRequest, answers []Prefetched) {
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
	stride := len(b.sites)
	for i := range requests {
		answer := &answers[i*stride+slot]
		if err != nil {
			*answer = Prefetched{Err: err, ready: true} // the call site adds the function's name
			continue
		}
		*answer = Prefetched{Value: results[i], ready: true}
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
