package machine

import (
	"strings"
	"testing"
	"unsafe"
)

func sameBacking[T any](a, b []T) bool {
	return len(a) == len(b) && len(a) > 0 && unsafe.SliceData(a) == unsafe.SliceData(b)
}

// A host's []float64 is the Value's backing, and comes back out as the same
// slice: no conversion, no copy, in either direction.
func TestNativeContainersAreWrappedNotCopied(t *testing.T) {
	t.Parallel()
	features := []float64{0.5, 1.5, 2.5}
	value, err := ToValue(features)
	if err != nil {
		t.Fatal(err)
	}
	if !value.hasType(ArrayOf(FloatType)) || value.length() != 3 {
		t.Fatalf("ToValue(%v) = %s of length %d, want array<float> of length 3", features, value.Type(), value.length())
	}
	back, err := FromValue[[]float64](value)
	if err != nil {
		t.Fatal(err)
	}
	if !sameBacking(features, back) {
		t.Fatal("FromValue copied the slice")
	}
	if exported, _ := value.Any().([]float64); !sameBacking(features, exported) {
		t.Fatal("Any copied the slice")
	}
	// Run's by-name path takes the same shortcut for an exact Go type.
	coerced, err := coerce(features, ArrayOf(FloatType))
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := coerced.box.([]float64); !sameBacking(features, got) {
		t.Fatal("coerce copied the slice")
	}
}

// The boundary keeps the language's invariants: no NaN gets in, and a native
// slice of the wrong element type is refused rather than converted.
//
// This is the whole defence, not the first half of one. RunValues does not
// re-check the values it is handed, because a Value holding a NaN cannot be
// built in the first place — so a new public constructor that skips the check
// would open the hole here, and this test is what stops it.
func TestBoundaryKeepsTheInvariants(t *testing.T) {
	t.Parallel()
	if _, err := ToValue([]float64{1, nan()}); err == nil || !strings.Contains(err.Error(), "item 1") {
		t.Fatalf("ToValue([1 NaN]) error = %v, want one naming %q", err, "item 1")
	}
	if _, err := coerce([]float64{1}, ArrayOf(IntType)); err == nil || !strings.Contains(err.Error(), "want array<int>") {
		t.Fatalf("coerce([1], array<int>) error = %v, want one containing %q", err, "want array<int>")
	}
	// JSON shapes still convert: float64 for an int, []any for an array.
	value, err := coerce([]any{float64(1), float64(2)}, ArrayOf(IntType))
	if err != nil {
		t.Fatal(err)
	}
	if ints, _ := FromValue[[]int64](value); len(ints) != 2 || ints[1] != 2 {
		t.Fatalf("FromValue[[]int64](coerce([1 2], array<int>)) = %v, want [1 2]", ints)
	}
	if _, err := FromValue[[]int64](Float(1)); err == nil || !strings.Contains(err.Error(), "want []int64") {
		t.Fatalf("FromValue[[]int64](Float(1)) error = %v, want one containing %q", err, "want []int64")
	}
}

func nan() float64 {
	zero := 0.0
	return zero / zero
}
