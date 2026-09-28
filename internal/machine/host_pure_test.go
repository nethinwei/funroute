package machine

import (
	"errors"
	"math"
	"strings"
	"testing"
)

// A pure call answers as the function's ordinary call does: the value, a
// failure the function returns, and a float answer that is not finite.
func TestPureCallsAnswerAsTheirEval(t *testing.T) {
	t.Parallel()
	errNegative := errors.New("negative")
	for _, fn := range []any{
		func(x float64) (int64, error) { return int64(x), failIf(x < 0, errNegative) },
		func(x float64) (float64, error) { return math.Inf(1) * x, failIf(x < -1, errNegative) },
		func(x int64) (int64, error) { return -x, failIf(x < 0, errNegative) },
		func(s string) (string, error) { return strings.ToUpper(s), failIf(s == "", errNegative) },
		strings.HasPrefix,
		func(a, b string) (string, error) { return a + b, failIf(a == "", errNegative) },
		func(a, b int64) (int64, error) { return a * b, failIf(a < 0, errNegative) },
		func(a, b float64) (float64, error) { return a / (b + 2), failIf(b < 0, errNegative) },
		func(s string, from, to int64) (string, error) { return s, failIf(from > to, errNegative) },
		func(x int64) int64 { return x * 3 },
		func(x float64) float64 { return -x },
		strings.ToLower,
		func(s string) bool { return s == "" },
		func(s string) float64 { return float64(len(s)) / 2 },
		func(a, b int64) int64 { return a - b },
		func(a, b float64) float64 { return a * b },
		func(x float64, n int64) float64 { return x * float64(n) },
		func(n int64, x float64) float64 { return float64(n) + x },
		func(s string, x float64) float64 { return float64(len(s)) * x },
		func(s string, n int64) int64 { return int64(len(s)) + n },
	} {
		assertPureAsReflected(t, fn)
	}
}

func assertPureAsReflected(t *testing.T, fn any) {
	t.Helper()
	pure := pureOf(fn)
	if pure == nil {
		t.Fatalf("%T has no pure call", fn)
	}
	reflected, err := reflectSignature(NewRegistry(), fn)
	if err != nil {
		t.Fatal(err)
	}
	for _, args := range argumentsFor(reflected.params) {
		// Each argument in the file of its kind, the answer after them.
		b := newBanks(int32(len(args)+1), nil)
		for i, arg := range args {
			b.setValue(int32(i), arg)
		}
		gotErr := pure(&b, 0, int32(len(args)))
		want, wantErr := reflected.call(t.Context(), args)
		got := b.valueAt(int32(len(args)), reflected.result.kind)
		if !sameOutcome(got, gotErr, want, wantErr) {
			t.Errorf("%T%v: pure %v, %v; by reflection %v, %v", fn, args, got.Any(), gotErr, want.Any(), wantErr)
		}
	}
}
