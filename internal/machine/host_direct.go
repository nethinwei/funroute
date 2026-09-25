package machine

import "context"

// A Go function of a signature most hosts write — scalars and float or int
// vectors in, a scalar out, an error or not — is called as itself: a type
// switch finds it at registration, and the call reads its arguments out of
// the Values and wraps its result without reflect.Call, which costs a few
// hundred nanoseconds and five allocations. Any other signature is still
// called through reflection. Either way the answer is the same, and so is
// every failure: TestDirectCallsAnswerAsReflection holds each shape here to
// its reflected call.

// directEval is the Eval of fn when its signature is one of those.
func directEval(fn any) (EvalFunc, bool) {
	if eval, ok := directUnary(fn); ok {
		return eval, true
	}
	return directBinary(fn)
}

func directUnary(fn any) (EvalFunc, bool) {
	switch fn := fn.(type) {
	case func(int64) int64:
		return unary(infallible(fn), intArg, intResult), true
	case func(int64) (int64, error):
		return unary(fn, intArg, intResult), true
	case func(float64) float64:
		return unary(infallible(fn), floatArg, floatResult), true
	case func(float64) (float64, error):
		return unary(fn, floatArg, floatResult), true
	case func(string) string:
		return unary(infallible(fn), stringArg, stringResult), true
	case func(string) (string, error):
		return unary(fn, stringArg, stringResult), true
	case func(string) bool:
		return unary(infallible(fn), stringArg, boolResult), true
	case func(string) float64:
		return unary(infallible(fn), stringArg, floatResult), true
	case func([]float64) float64:
		return unary(infallible(fn), floatsArg, floatResult), true
	case func([]float64) (float64, error):
		return unary(fn, floatsArg, floatResult), true
	case func(context.Context, []float64) (float64, error):
		return unaryWithContext(fn, floatsArg, floatResult), true
	case func([]int64) int64:
		return unary(infallible(fn), intsArg, intResult), true
	}
	return nil, false
}

func directBinary(fn any) (EvalFunc, bool) {
	switch fn := fn.(type) {
	case func(int64, int64) int64:
		return binary(infallible2(fn), intArg, intArg, intResult), true
	case func(int64, int64) (int64, error):
		return binary(fn, intArg, intArg, intResult), true
	case func(float64, float64) float64:
		return binary(infallible2(fn), floatArg, floatArg, floatResult), true
	case func(float64, float64) (float64, error):
		return binary(fn, floatArg, floatArg, floatResult), true
	case func(string, float64) float64:
		return binary(infallible2(fn), stringArg, floatArg, floatResult), true
	case func(string, float64) (float64, error):
		return binary(fn, stringArg, floatArg, floatResult), true
	case func(string, int64) int64:
		return binary(infallible2(fn), stringArg, intArg, intResult), true
	case func(string, string) bool:
		return binary(infallible2(fn), stringArg, stringArg, boolResult), true
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

func infallible[A, R any](fn func(A) R) func(A) (R, error) {
	return func(a A) (R, error) { return fn(a), nil }
}

func infallible2[A, B, R any](fn func(A, B) R) func(A, B) (R, error) {
	return func(a A, b B) (R, error) { return fn(a, b), nil }
}

// The arguments as the Go types: loading proved each of its parameter's
// type, and a vector is its backing, handed over as reflection hands it.
func intArg(v *Value) int64                { return v.i }
func floatArg(v *Value) float64            { return v.f }
func stringArg(v *Value) string            { return v.s }
func floatsArg(v *Value) []float64         { floats, _ := v.box.([]float64); return floats }
func intsArg(v *Value) []int64             { ints, _ := v.box.([]int64); return ints }
func intResult(r int64) (Value, error)     { return Int(r), nil }
func stringResult(r string) (Value, error) { return String(r), nil }
func boolResult(r bool) (Value, error)     { return Bool(r), nil }

// floatResult refuses a result that is not finite, as reflection's does.
func floatResult(r float64) (Value, error) { return CheckedFloat(r) }
