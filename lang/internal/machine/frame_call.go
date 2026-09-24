package machine

import (
	"context"
	"errors"
	"fmt"
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
// the extension's fault. A host function whose result type names currencies
// — a code, or a contract variable the arguments bound — is held to them all
// the way into its containers and records: hasType compares a container's
// shape only, and the compiler trusts the signature there. A kernel
// function's checks stay inline: a call of their own costs every kernel call
// a nanosecond or two.
func (f *frame) call(pc int, instruction Instruction) error {
	function := f.runtime.functions[instruction.A]
	if !function.IsBuiltin() && f.deadline && f.ctx.Err() != nil {
		return fmt.Errorf("%s: %w: %v", function.Name, ErrDeadline, f.ctx.Err())
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
		if len(f.fallbacks) > 0 {
			value, err = callSafely(f.ctx, function, callArgs)
		} else {
			value, err = function.Eval(f.ctx, callArgs)
		}
	} else {
		value, err = f.invokeBounded(function, callArgs)
	}
	if err != nil {
		return fmt.Errorf("%s: %w", function.Name, f.functionError(function, err))
	}
	if !function.IsBuiltin() {
		if err := f.hostResult(pc, function, value, instruction.Type); err != nil {
			return err
		}
	} else if !value.hasType(*instruction.Type) {
		return f.resultTypeError(function, value, *instruction.Type)
	} else if err := value.validateInvariant(); err != nil {
		return fmt.Errorf("%s: invalid result: %w", function.Name, err)
	}
	return f.push(value)
}

// hostResult holds a host function's result to the call's type: declared
// currencies, the type, the invariants, and every currency the type states.
func (f *frame) hostResult(pc int, function *RegisteredFunction, value Value, typ *Type) error {
	if err := f.declaredResult(function, value); err != nil {
		return fmt.Errorf("%s: %w", function.Name, err)
	}
	if !value.hasType(*typ) {
		return f.resultTypeError(function, value, *typ)
	}
	if err := value.validateInvariant(); err != nil {
		return fmt.Errorf("%s: %w", function.Name, f.functionError(function, fmt.Errorf("invalid result: %v", err)))
	}
	if f.runtime.money.checksResult(pc) {
		if err := f.checkPattern(value, *typ); err != nil {
			return fmt.Errorf("%s: returned %s: %w", function.Name, value.Type(), err)
		}
	}
	return nil
}

// resultTypeError is a result of the wrong type: a currency mismatch when
// only the unit differs, the extension's fault otherwise.
func (f *frame) resultTypeError(function *RegisteredFunction, value Value, typ Type) error {
	err := fmt.Errorf("returned %s, contract requires %s", value.Type(), typ)
	if value.kind == typ.kind && IsUnitKind(value.kind) {
		return fmt.Errorf("%s: %w: %w", function.Name, ErrCurrency, err)
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
		return Value{}, fmt.Errorf("%w: %v", ErrDeadline, f.ctx.Err())
	}
	ctx := f.ctx
	if function.Doc.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, function.Doc.Timeout)
		defer cancel()
	}
	var value Value
	var err error
	if function.Doc.Detached {
		value, err = callDetached(ctx, function, args)
	} else if len(f.fallbacks) > 0 {
		value, err = callSafely(ctx, function, args)
	} else {
		value, err = function.Eval(ctx, args)
	}
	if err != nil && ctx.Err() != nil && !keepsIdentity(err) {
		return Value{}, fmt.Errorf("%w: %v", ErrDeadline, err)
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
func (f *frame) classify(err error) error {
	if keepsIdentity(err) {
		return err
	}
	if f.ctx.Err() != nil {
		return fmt.Errorf("%w: %v", ErrDeadline, err)
	}
	return fmt.Errorf("%w: %v", ErrExtension, err)
}

// keepsIdentity reports an error that already has a class a host tells
// apart with errors.Is: one a Batch or invokeBounded classified, or the
// rule's or the data's own — a currency mismatch or an arithmetic failure,
// which fallback must not take however late it came, and a missing exchange
// rate, which fallback takes as data not yet at hand whoever reported it.
// Wrapping one of these as an extension failure would lose that.
func keepsIdentity(err error) bool {
	return errors.Is(err, ErrDeadline) || errors.Is(err, ErrExtension) || errors.Is(err, ErrCurrency) ||
		errors.Is(err, ErrArithmetic) || errors.Is(err, ErrNoRate)
}

// callDetached runs the function on its own goroutine and stops waiting at the
// deadline. The arguments are copied first: the stack window they live in is
// reused once this returns, and the abandoned call may still be reading them.
func callDetached(ctx context.Context, function *RegisteredFunction, args []Value) (Value, error) {
	type outcome struct {
		value Value
		err   error
	}
	owned := make([]Value, len(args))
	copy(owned, args)
	done := make(chan outcome, 1)
	go func() {
		value, err := callSafely(ctx, function, owned)
		done <- outcome{value, err}
	}()
	select {
	case result := <-done:
		return result.value, result.err
	case <-ctx.Done():
		return Value{}, ctx.Err()
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
