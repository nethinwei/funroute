package machine

import (
	"encoding/json"
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

// The boundary keeps the language's invariants: a native slice of the wrong
// element type is refused rather than converted. A float slice goes in as
// it is, NaN and all: it is neither read nor copied.
func TestBoundaryKeepsTheInvariants(t *testing.T) {
	t.Parallel()
	floats := []float64{1, nan()}
	if value, err := ToValue(floats); err != nil || !sameBacking(floats, backing[float64](&value)) {
		t.Fatalf("ToValue([1 NaN]) = %v, %v; want the slice itself", value.Any(), err)
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

// floatInt stops below 2^63 itself: math.MaxInt64 converts to 2^63, so a
// comparison against it let that float through to an undefined conversion.
func TestFloatIntStopsBelowTwoToTheSixtyThree(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		value float64
		want  int64
		ok    bool
	}{
		{0x1p63, 0, false},
		{-0x1p63, -1 << 63, true},
		{0x1p63 - 1024, 1<<63 - 1024, true},
		{1.5, 0, false},
		{-7, -7, true},
	} {
		if got, ok := floatInt(test.value); got != test.want || ok != test.ok {
			t.Errorf("floatInt(%v) = %d, %v, want %d, %v", test.value, got, ok, test.want, test.ok)
		}
	}
}

// A float64 past 2^53 is an int JSON decoding has already rounded, so the
// boundary refuses it rather than take the wrong number; json.Number and Go
// ints carry it exactly.
func TestCoerceIntRefusesFloatsPastExactIntegers(t *testing.T) {
	t.Parallel()
	for _, input := range []any{float64(1<<53 + 2), float64(-(1<<53 + 2)), 0x1p63, 1.5} {
		if got, err := coerceInt(input); err == nil {
			t.Errorf("coerceInt(%v) = %v, want an error", input, got.i)
		}
	}
	for input, want := range map[any]int64{float64(1 << 53): 1 << 53, json.Number("9007199254740993"): 9007199254740993, int64(9007199254740993): 9007199254740993} {
		if got, err := coerceInt(input); err != nil || got.i != want {
			t.Errorf("coerceInt(%v) = %d, %v, want %d", input, got.i, err, want)
		}
	}
}
