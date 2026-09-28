package machine

import (
	"context"
	"fmt"

	"github.com/nethinwei/funroute/internal/kit"
)

// callSite runs one call. There are three ways to get the value, cheapest
// first: a Batch already computed it; the plain call every kernel function
// takes; or the bounded call of a function with a Timeout or Detached. A
// call past its deadline does not run.
//
// A host's result is then held to the call's type. Currencies first: money
// the registry cannot hold, or in the wrong currency, is an ErrCurrency like
// everywhere else, which fallback does not take; any other wrong result is
// the extension's fault. The result is also looked into all the way down,
// containers and records included, for currencies the registry did not
// declare and amounts with no currency that are not zero. A kernel
// function's result is of its signature's type, which loading proved.
func (f *frame) callSite(call int32) error {
	site := &f.runtime.reg.calls[call]
	// Lowering gave a banked call only to a site outside every fallback's
	// candidate: its arguments and its answer are in their files.
	if site.banked != nil {
		if site.deadline && f.deadline {
			if err := f.expired(); err != nil {
				return fmt.Errorf("%s: %w", site.fn.Name, err)
			}
		}
		if err := site.banked(&f.banks, site.args, site.dst); err != nil {
			return fmt.Errorf("%s: %w", site.fn.Name, f.classify(err))
		}
		return nil
	}
	function := site.fn
	if len(site.boxes) > 0 {
		f.boxArgs(site.boxes)
	}
	args := f.regs[site.args : site.args+site.argc : site.args+site.argc]
	if site.kernel && len(f.fallbacks) == 0 {
		value, err := function.Eval(f.ctx, args)
		if err != nil {
			return fmt.Errorf("%s: %w", function.Name, err)
		}
		f.put(site.dst, site.kind, value)
		return nil
	}
	if !function.IsBuiltin() && f.deadline {
		if err := f.expired(); err != nil {
			return fmt.Errorf("%s: %w", function.Name, err)
		}
	}
	if site.direct {
		return f.callHost(site, args)
	}
	value, err := f.invoke(int(call), function, args, site.typ)
	if err != nil {
		return err
	}
	f.put(site.dst, site.kind, value)
	return nil
}

// callHost calls a host's function no Batch hoists, with no Timeout and not
// Detached: invoke's plain call, and nothing it would ask first.
func (f *frame) callHost(site *rcall, args []Value) error {
	function := site.fn
	var value Value
	var err error
	if len(f.fallbacks) > 0 {
		value, err = callSafely(f.ctx, function, args)
	} else {
		value, err = function.Eval(f.ctx, args)
	}
	if err != nil {
		return fmt.Errorf("%s: %w", function.Name, f.classify(err))
	}
	if !function.madeResult {
		if err := f.hostResult(function, value, site.typ); err != nil {
			return err
		}
	}
	f.put(site.dst, site.kind, value)
	return nil
}

// invoke makes the call-th call of function with args and holds its answer
// to typ.
func (f *frame) invoke(call int, function *RegisteredFunction, callArgs []Value, typ *Type) (Value, error) {
	var value Value
	var err error
	if ready, ok := f.prefetchedAt(call); ok {
		value, err = ready.Value, ready.Err
	} else if function.Doc.Timeout == 0 && !function.Doc.Detached {
		ctx := f.ctx
		if function.readsRun {
			f.fxCtx.Context = f.ctx
			ctx = &f.fxCtx
		}
		if len(f.fallbacks) > 0 {
			value, err = callSafely(ctx, function, callArgs)
		} else {
			value, err = function.Eval(ctx, callArgs)
		}
	} else {
		value, err = f.invokeBounded(function, callArgs)
	}
	if err != nil {
		return Value{}, fmt.Errorf("%s: %w", function.Name, f.functionError(function, err))
	}
	// A kernel function's result is of its signature's type, which loading
	// proved the call's; a host's is looked at, unless its Eval made it.
	if !function.IsBuiltin() && !function.madeResult {
		if err := f.hostResult(function, value, typ); err != nil {
			return Value{}, err
		}
	}
	return value, nil
}

// hostResult holds a host function's result to the call's type: declared
// currencies and the type.
func (f *frame) hostResult(function *RegisteredFunction, value Value, typ *Type) error {
	if table := f.runtime.money.table; table != nil {
		if err := declaredValue(table, value, true); err != nil {
			return fmt.Errorf("%s: %w", function.Name, err)
		}
	}
	if !value.hasType(*typ) {
		return f.resultTypeError(function, value, *typ)
	}
	return nil
}

// resultTypeError is a result of the wrong type: a currency mismatch when
// only the unit differs, the extension's fault otherwise.
func (f *frame) resultTypeError(function *RegisteredFunction, value Value, typ Type) error {
	err := fmt.Errorf("returned %s, contract requires %s", value.Type(), typ)
	if value.kind == typ.kind && IsUnitKind(value.kind) {
		return fmt.Errorf("%s: %w", function.Name, kit.Classify(ErrCurrency, "", err))
	}
	return fmt.Errorf("%s: %w", function.Name, f.functionError(function, err))
}

// prefetchedAt is small enough to inline, so the common case — no batch — is
// one nil check on the call path.
func (f *frame) prefetchedAt(call int) (Prefetched, bool) {
	if f.prefetched == nil || !f.prefetched[call].ready {
		return Prefetched{}, false
	}
	return f.prefetched[call], true
}

// invokeBounded caps one call by the function's Timeout and, for a Detached
// function, stops waiting when it passes. The deadline check before the call
// is what makes a program stop promptly once its time is up.
func (f *frame) invokeBounded(function *RegisteredFunction, args []Value) (Value, error) {
	if f.deadline {
		if err := f.expired(); err != nil {
			return Value{}, err
		}
	}
	ctx, cancel := withTimeout(f.ctx, function)
	defer cancel()
	var value Value
	var err error
	switch {
	case function.Doc.Detached:
		value, err = callDetached(ctx, function, args)
	case len(f.fallbacks) > 0:
		value, err = callSafely(ctx, function, args)
	default:
		value, err = function.Eval(ctx, args)
	}
	return value, timedOut(ctx, err)
}

// withTimeout is ctx capped by the function's Timeout, when it has one.
func withTimeout(ctx context.Context, function *RegisteredFunction) (context.Context, context.CancelFunc) {
	if function.Doc.Timeout > 0 {
		return context.WithTimeout(ctx, function.Doc.Timeout)
	}
	return ctx, func() {}
}

// timedOut is a failure under ctx as ErrDeadline once ctx has run out,
// unless the failure has a class of its own.
func timedOut(ctx context.Context, err error) error {
	if err != nil && ctx.Err() != nil && !keepsIdentity(err) {
		return kit.Classify(ErrDeadline, "", err)
	}
	return err
}

func (f *frame) functionError(function *RegisteredFunction, err error) error {
	if function.IsBuiltin() {
		return err
	}
	return f.classify(err)
}

// classify wraps an extension's failure as ErrDeadline when the request's
// budget ran out and ErrExtension otherwise, so fallback and monitoring can
// tell the two apart. An error that already has its class keeps it
// (keepsIdentity).
func (f *frame) classify(err error) error { return classifyUnder(f.ctx, err) }

// classifyUnder is classify for a failure under ctx.
func classifyUnder(ctx context.Context, err error) error {
	if keepsIdentity(err) {
		return err
	}
	if ctx.Err() != nil {
		return kit.Classify(ErrDeadline, "", err)
	}
	return kit.Classify(ErrExtension, "", err)
}

// keepsIdentity reports an error that already has one of the classes a host
// tells apart with errors.Is — one a Batch or invokeBounded classified, the
// rule's or the data's own, or any other — so it is passed on as it is.
// Wrapping it as an extension failure would add a class it does not have: a
// currency mismatch or an arithmetic failure must stay what fallback does not
// take however late it came.
func keepsIdentity(err error) bool {
	_, classified := classOf(err)
	return classified
}

// callDetached runs the function on its own goroutine and stops waiting at the
// deadline. The arguments are copied first: the stack window they live in is
// reused once this returns, and the abandoned call may still be reading them.
func callDetached(ctx context.Context, function *RegisteredFunction, args []Value) (Value, error) {
	owned := make([]Value, len(args))
	copy(owned, args)
	return detached(ctx, function, owned, callSafely)
}

// detached makes call on its own goroutine and stops waiting when ctx is done;
// the call keeps running until it returns on its own.
func detached[A, R any](ctx context.Context, function *RegisteredFunction, args A,
	call func(context.Context, *RegisteredFunction, A) (R, error)) (R, error) {
	type outcome struct {
		result R
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		result, err := call(ctx, function, args)
		done <- outcome{result, err}
	}()
	select {
	case got := <-done:
		return got.result, got.err
	case <-ctx.Done():
		var zero R
		return zero, ctx.Err()
	}
}

func callSafely(ctx context.Context, function *RegisteredFunction, args []Value) (value Value, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("extension panicked: %v", recovered)
		}
	}()
	return function.Eval(ctx, args)
}
