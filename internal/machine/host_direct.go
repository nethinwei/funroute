package machine

import (
	"context"
	"fmt"
)

// A Go function of a signature most hosts write — up to three scalars and
// vectors in, a scalar or a vector out, an error or not, the standard pack's
// among them — is called as itself: a type switch finds it at registration,
// and the call reads its arguments out of the Values and wraps its result
// without reflect.Call, which costs a few hundred nanoseconds and five
// allocations. Any other signature is still called through reflection.
// Either way the answer is the same, and so is every failure:
// TestDirectCallsAnswerAsReflection and TestTheStandardShapesAreCalledDirectly
// hold each shape here to its reflected call.

// directEval is the Eval of fn when its signature is one of those.
func directEval(fn any) (EvalFunc, bool) {
	for _, shapes := range []func(any) (EvalFunc, bool){directUnary, directVector, directBinary, directTernary} {
		if eval, ok := shapes(fn); ok {
			return eval, true
		}
	}
	return nil, false
}

func directUnary(fn any) (EvalFunc, bool) {
	switch fn := fn.(type) {
	case func(int64) int64:
		return unaryPlain(fn, intArg, intResult), true
	case func(int64) (int64, error):
		return unary(fn, intArg, intResult), true
	case func(float64) float64:
		return unaryPlain(fn, floatArg, floatResult), true
	case func(float64) (float64, error):
		return unary(fn, floatArg, floatResult), true
	case func(string) string:
		return unaryPlain(fn, stringArg, stringResult), true
	case func(string) (string, error):
		return unary(fn, stringArg, stringResult), true
	case func(string) bool:
		return unaryPlain(fn, stringArg, boolResult), true
	case func(string) float64:
		return unaryPlain(fn, stringArg, floatResult), true
	case func(float64) (int64, error):
		return unary(fn, floatArg, intResult), true
	}
	return nil, false
}

// directVector is the shapes of one vector in, the standard pack's among them.
func directVector(fn any) (EvalFunc, bool) {
	switch fn := fn.(type) {
	case func([]float64) float64:
		return unaryPlain(fn, floatsArg, floatResult), true
	case func([]float64) (float64, error):
		return unary(fn, floatsArg, floatResult), true
	case func(context.Context, []float64) (float64, error):
		return unaryWithContext(fn, floatsArg, floatResult), true
	case func([]float64) (int64, error):
		return unary(fn, floatsArg, intResult), true
	case func([]float64) []float64:
		return unaryPlain(fn, floatsArg, floatsResult), true
	case func([]float64) ([]float64, error):
		return unary(fn, floatsArg, floatsResult), true
	case func([]float64) ([]int64, error):
		return unary(fn, floatsArg, intsResult), true
	case func([]int64) int64:
		return unaryPlain(fn, intsArg, intResult), true
	case func([]int64) (int64, error):
		return unary(fn, intsArg, intResult), true
	case func([]int64) (float64, error):
		return unary(fn, intsArg, floatResult), true
	case func([]int64) []int64:
		return unaryPlain(fn, intsArg, intsResult), true
	case func([]int64) ([]int64, error):
		return unary(fn, intsArg, intsResult), true
	case func([]string) []string:
		return unaryPlain(fn, stringsArg, stringsResult), true
	case func([]string) (string, error):
		return unary(fn, stringsArg, stringResult), true
	case func([]string) (int64, error):
		return unary(fn, stringsArg, intResult), true
	case func([]string) ([]int64, error):
		return unary(fn, stringsArg, intsResult), true
	case func([]bool) (bool, error):
		return unary(fn, boolsArg, boolResult), true
	}
	return nil, false
}

func directBinary(fn any) (EvalFunc, bool) {
	switch fn := fn.(type) {
	case func(int64, int64) int64:
		return binaryPlain(fn, intArg, intArg, intResult), true
	case func(int64, int64) (int64, error):
		return binary(fn, intArg, intArg, intResult), true
	case func(float64, float64) float64:
		return binaryPlain(fn, floatArg, floatArg, floatResult), true
	case func(float64, float64) (float64, error):
		return binary(fn, floatArg, floatArg, floatResult), true
	case func(string, float64) float64:
		return binaryPlain(fn, stringArg, floatArg, floatResult), true
	case func(string, float64) (float64, error):
		return binary(fn, stringArg, floatArg, floatResult), true
	case func(string, int64) int64:
		return binaryPlain(fn, stringArg, intArg, intResult), true
	case func(string, string) bool:
		return binaryPlain(fn, stringArg, stringArg, boolResult), true
	case func(string, string) (bool, error):
		return binary(fn, stringArg, stringArg, boolResult), true
	case func(string, string) (string, error):
		return binary(fn, stringArg, stringArg, stringResult), true
	case func(string, string) ([]string, error):
		return binary(fn, stringArg, stringArg, stringsResult), true
	case func(float64, int64) float64:
		return binaryPlain(fn, floatArg, intArg, floatResult), true
	case func(float64, int64) (float64, error):
		return binary(fn, floatArg, intArg, floatResult), true
	case func(int64, float64) float64:
		return binaryPlain(fn, intArg, floatArg, floatResult), true
	case func(int64, float64) (float64, error):
		return binary(fn, intArg, floatArg, floatResult), true
	case func([]string, string) (string, error):
		return binary(fn, stringsArg, stringArg, stringResult), true
	case func([]int64, float64) (float64, error):
		return binary(fn, intsArg, floatArg, floatResult), true
	case func([]float64, float64) (float64, error):
		return binary(fn, floatsArg, floatArg, floatResult), true
	}
	return nil, false
}

// directTernary is the shapes of three parameters: the standard pack's text
// functions.
func directTernary(fn any) (EvalFunc, bool) {
	switch fn := fn.(type) {
	case func(string, string, string) (string, error):
		return ternary(fn, stringArg, stringArg, stringArg, stringResult), true
	case func(string, int64, string) (string, error):
		return ternary(fn, stringArg, intArg, stringArg, stringResult), true
	case func(string, int64, int64) (string, error):
		return ternary(fn, stringArg, intArg, intArg, stringResult), true
	}
	return nil, false
}

func unary[A, R any](fn func(A) (R, error), arg func(*Value) A, result func(R) (Value, error)) EvalFunc {
	return func(_ context.Context, args []Value) (Value, error) {
		answer, err := fn(arg(&args[0]))
		if err != nil {
			return Value{}, err
		}
		return result(answer)
	}
}

func unaryWithContext[A, R any](fn func(context.Context, A) (R, error), arg func(*Value) A, result func(R) (Value, error)) EvalFunc {
	return func(ctx context.Context, args []Value) (Value, error) {
		answer, err := fn(ctx, arg(&args[0]))
		if err != nil {
			return Value{}, err
		}
		return result(answer)
	}
}

func binary[A, B, R any](fn func(A, B) (R, error), first func(*Value) A, second func(*Value) B, result func(R) (Value, error)) EvalFunc {
	return func(_ context.Context, args []Value) (Value, error) {
		answer, err := fn(first(&args[0]), second(&args[1]))
		if err != nil {
			return Value{}, err
		}
		return result(answer)
	}
}

// unaryPlain and binaryPlain are unary and binary for a function that
// returns no error: called as it is, with no wrapper around it.
func unaryPlain[A, R any](fn func(A) R, arg func(*Value) A, result func(R) (Value, error)) EvalFunc {
	return func(_ context.Context, args []Value) (Value, error) {
		return result(fn(arg(&args[0])))
	}
}

func binaryPlain[A, B, R any](fn func(A, B) R, first func(*Value) A, second func(*Value) B, result func(R) (Value, error)) EvalFunc {
	return func(_ context.Context, args []Value) (Value, error) {
		return result(fn(first(&args[0]), second(&args[1])))
	}
}

func ternary[A, B, C, R any](fn func(A, B, C) (R, error), first func(*Value) A, second func(*Value) B, third func(*Value) C, result func(R) (Value, error)) EvalFunc {
	return func(_ context.Context, args []Value) (Value, error) {
		answer, err := fn(first(&args[0]), second(&args[1]), third(&args[2]))
		if err != nil {
			return Value{}, err
		}
		return result(answer)
	}
}

func infallible[A, R any](fn func(A) R) func(A) (R, error) {
	return func(a A) (R, error) { return fn(a), nil }
}

// The arguments as the Go types: loading proved each of its parameter's
// type, and a vector is its backing, handed over as reflection hands it.
func intArg(v *Value) int64                { return v.i }
func floatArg(v *Value) float64            { return v.f }
func stringArg(v *Value) string            { return v.s }
func floatsArg(v *Value) []float64         { return backing[float64](v) }
func intsArg(v *Value) []int64             { return backing[int64](v) }
func stringsArg(v *Value) []string         { return backing[string](v) }
func boolsArg(v *Value) []bool             { return backing[bool](v) }
func intResult(r int64) (Value, error)     { return Int(r), nil }
func stringResult(r string) (Value, error) { return String(r), nil }
func boolResult(r bool) (Value, error)     { return Bool(r), nil }

// backing is an array's native backing. An array with none is empty, and
// reflection hands it over as an empty slice, not a nil one.
func backing[T any](v *Value) []T {
	if items, ok := nativeItems[T](*v); ok {
		return items
	}
	return []T{}
}

func floatResult(r float64) (Value, error) { return Float(r), nil }

// A vector result is wrapped as its backing, as fromGo wraps it.
func intsResult(r []int64) (Value, error)     { return arrayOf(r), nil }
func floatsResult(r []float64) (Value, error) { return arrayOf(r), nil }
func stringsResult(r []string) (Value, error) { return arrayOf(r), nil }

// directBatch is the EvalBatch of a GoBatch of one parameter, of the shapes
// directEval calls a single call of.
func directBatch(fn any) (BatchEvalFunc, bool) {
	switch fn := fn.(type) {
	case func([]float64) ([]float64, error):
		return batchOf(withoutContext(fn), floatArg, floatResult), true
	case func([]float64) []float64:
		return batchOf(withoutContext(infallible(fn)), floatArg, floatResult), true
	case func(context.Context, []float64) ([]float64, error):
		return batchOf(fn, floatArg, floatResult), true
	case func([][]float64) ([]float64, error):
		return batchOf(withoutContext(fn), floatsArg, floatResult), true
	case func(context.Context, [][]float64) ([]float64, error):
		return batchOf(fn, floatsArg, floatResult), true
	case func([]int64) ([]int64, error):
		return batchOf(withoutContext(fn), intArg, intResult), true
	case func([]string) ([]float64, error):
		return batchOf(withoutContext(fn), stringArg, floatResult), true
	}
	return nil, false
}

func withoutContext[A, R any](fn func(A) (R, error)) func(context.Context, A) (R, error) {
	return func(_ context.Context, a A) (R, error) { return fn(a) }
}

// batchOf calls fn with every request's argument in one slice, and reads
// one result per answer, as reflection's batch call does.
func batchOf[A, R any](fn func(context.Context, []A) ([]R, error), arg func(*Value) A, result func(R) (Value, error)) BatchEvalFunc {
	return func(ctx context.Context, calls [][]Value) ([]Value, error) {
		column := make([]A, len(calls))
		for i := range calls {
			column[i] = arg(&calls[i][0])
		}
		answers, err := fn(ctx, column)
		if err != nil {
			return nil, err
		}
		out := make([]Value, len(answers))
		for i, answer := range answers {
			value, err := result(answer)
			if err != nil {
				return nil, fmt.Errorf("result %d: %w", i, err)
			}
			out[i] = value
		}
		return out, nil
	}
}
