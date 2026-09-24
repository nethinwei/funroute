package machine

import (
	"context"
	"fmt"

	"github.com/nethinwei/funroute/internal/money"
)

// call runs one call instruction. There are three ways to get the value,
// cheapest first: a Batch already computed it; the plain call every kernel
// function takes; or the bounded call of a function with a Timeout or
// Detached. A hoisted call still costs its fuel so a program's budget does
// not depend on how it ran.
//
// The result is then held to the call's type. Currencies first: money the
// registry cannot hold, or in the wrong currency, is an ErrCurrency like
// everywhere else, which fallback does not take; any other wrong result is
// the extension's fault. A host function's result is also looked into all
// the way down, containers and records included, for currencies the registry
// did not declare and amounts with no currency that are not zero. A kernel
// function's checks stay inline: a call of their own costs every kernel call
// a nanosecond or two.
func (f *frame) call(pc int, instruction Instruction) error {
	function := f.runtime.functions[instruction.A]
	if !function.IsBuiltin() && f.deadline && f.ctx.Err() != nil {
		return fmt.Errorf("%s: %w", function.Name, money.Classify(ErrDeadline, "", f.ctx.Err()))
	}
	if f.fuelLeft < function.Doc.Cost {
		return fmt.Errorf("%w before %s", ErrFuel, function.Name)
	}
	f.fuelLeft -= function.Doc.Cost
	callArgs, err := f.popN(instruction.B)
	if err != nil {
		return err
	}
	var value Value
	if ready, ok := f.prefetchedAt(pc); ok {
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
		return fmt.Errorf("%s: %w", function.Name, f.functionError(function, err))
	}
	// A kernel function's result is of its signature's type, which loading
	// proved the call's; a host's is looked at.
	if !function.IsBuiltin() {
		if err := f.hostResult(function, value, instruction.Type); err != nil {
			return err
		}
	}
	return f.push(value)
}

// hostResult holds a host function's result to the call's type: declared
// currencies, the type and the invariants.
func (f *frame) hostResult(function *RegisteredFunction, value Value, typ *Type) error {
	if table := f.runtime.money.table; table != nil {
		if err := declaredValue(table, value, true); err != nil {
			return fmt.Errorf("%s: %w", function.Name, err)
		}
	}
	if !value.hasType(*typ) {
		return f.resultTypeError(function, value, *typ)
	}
	if err := value.validateInvariant(); err != nil {
		return fmt.Errorf("%s: %w", function.Name, f.functionError(function, fmt.Errorf("invalid result: %w", err)))
	}
	return nil
}

// resultTypeError is a result of the wrong type: a currency mismatch when
// only the unit differs, the extension's fault otherwise.
func (f *frame) resultTypeError(function *RegisteredFunction, value Value, typ Type) error {
	err := fmt.Errorf("returned %s, contract requires %s", value.Type(), typ)
	if value.kind == typ.kind && IsUnitKind(value.kind) {
		return fmt.Errorf("%s: %w", function.Name, money.Classify(ErrCurrency, "", err))
	}
	return fmt.Errorf("%s: %w", function.Name, f.functionError(function, err))
}

// prefetchedAt is small enough to inline, so the common case — no batch — is
// one nil check on the call path.
func (f *frame) prefetchedAt(pc int) (Prefetched, bool) {
	if f.prefetched == nil {
		return Prefetched{}, false
	}
	ready, ok := f.prefetched[pc]
	return ready, ok
}

// invokeBounded caps one call by the function's Timeout and, for a Detached
// function, stops waiting when it passes. The deadline check before the call
// is what makes a program stop promptly once its time is up.
func (f *frame) invokeBounded(function *RegisteredFunction, args []Value) (Value, error) {
	if f.deadline && f.ctx.Err() != nil {
		return Value{}, money.Classify(ErrDeadline, "", f.ctx.Err())
	}
	ctx := f.ctx
	if function.Doc.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, function.Doc.Timeout)
		defer cancel()
	}
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
	if err != nil && ctx.Err() != nil && !keepsIdentity(err) {
		return Value{}, money.Classify(ErrDeadline, "", err)
	}
	return value, err
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
		return money.Classify(ErrDeadline, "", err)
	}
	return money.Classify(ErrExtension, "", err)
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
