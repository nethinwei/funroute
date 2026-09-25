package machine

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"
)

// Each signature called as itself answers as the reflected call does: the
// value, a failure the function returns, and a result that is not finite.
func TestDirectCallsAnswerAsReflection(t *testing.T) {
	t.Parallel()
	errNegative := errors.New("negative")
	half := func(x float64) float64 { return x / 2 }
	for _, fn := range []any{
		func(x int64) int64 { return x * 2 },
		func(x int64) (int64, error) { return x, failIf(x < 0, errNegative) },
		half,
		func(x float64) (float64, error) { return math.Inf(1) * x, failIf(x < 0, errNegative) },
		strings.ToUpper,
		func(s string) (string, error) { return s + "!", failIf(s == "", errNegative) },
		func(s string) bool { return s == "a" },
		func(s string) float64 { return float64(len(s)) },
		func(xs []float64) float64 { return float64(len(xs)) },
		func(xs []float64) (float64, error) { return math.NaN(), failIf(len(xs) == 0, errNegative) },
		func(_ context.Context, xs []float64) (float64, error) { return float64(len(xs)), nil },
		func(xs []int64) int64 { return int64(len(xs)) },
		func(a, b int64) int64 { return a - b },
		func(a, b int64) (int64, error) { return a + b, failIf(a < 0, errNegative) },
		math.Max,
		func(a, b float64) (float64, error) { return a / b, nil },
		func(s string, x float64) float64 { return x * float64(len(s)) },
		func(s string, x float64) (float64, error) { return x, failIf(s == "", errNegative) },
		func(s string, x int64) int64 { return x + int64(len(s)) },
		strings.HasPrefix,
	} {
		assertDirectAsReflected(t, fn)
	}
}

func failIf(fail bool, err error) error {
	if fail {
		return err
	}
	return nil
}

func assertDirectAsReflected(t *testing.T, fn any) {
	t.Helper()
	direct, ok := directEval(fn)
	if !ok {
		t.Fatalf("%T is not called directly", fn)
	}
	reflected, err := reflectSignature(NewRegistry(), fn)
	if err != nil {
		t.Fatal(err)
	}
	for _, args := range argumentsFor(reflected.params) {
		want, wantErr := reflected.call(t.Context(), args)
		got, gotErr := direct(t.Context(), args)
		if !sameOutcome(got, gotErr, want, wantErr) {
			t.Errorf("%T%v: directly %v, %v; by reflection %v, %v", fn, args, got.Any(), gotErr, want.Any(), wantErr)
		}
	}
}

func sameOutcome(got Value, gotErr error, want Value, wantErr error) bool {
	if gotErr != nil || wantErr != nil {
		return gotErr != nil && wantErr != nil && gotErr.Error() == wantErr.Error()
	}
	return got.Equal(want)
}

// argumentsFor is a few calls' arguments of the parameters' types, the
// edges each function above fails on among them.
func argumentsFor(params []Type) [][]Value {
	samples := func(typ Type) []Value {
		switch {
		case typ.kind == IntKind:
			return []Value{Int(3), Int(-4)}
		case typ.kind == FloatKind:
			return []Value{Float(1.5), Float(-2)}
		case typ.kind == StringKind:
			return []Value{String(""), String("a")}
		case typ.elem.kind == IntKind:
			return []Value{{kind: ArrayKind, box: []int64{}}, {kind: ArrayKind, box: []int64{1, 2}}}
		}
		return []Value{{kind: ArrayKind, box: []float64{}}, {kind: ArrayKind, box: []float64{1, 2}}}
	}
	calls := [][]Value{nil}
	for _, param := range params {
		var next [][]Value
		for _, call := range calls {
			for _, sample := range samples(param) {
				next = append(next, append(append([]Value(nil), call...), sample))
			}
		}
		calls = next
	}
	return calls
}
