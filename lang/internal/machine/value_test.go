package machine

import (
	"math"
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
	features := []float64{0.5, 1.5, 2.5}
	value, err := ToValue(features)
	if err != nil {
		t.Fatal(err)
	}
	if !value.hasType(ArrayOf(FloatType)) || value.length() != 3 {
		t.Fatalf("value = %s", value.Type())
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

// What the VM builds has the same backing as what a host supplies, so an
// extension sees a []float64 whichever way the array was made.
func TestBuiltArraysUseTheNativeBacking(t *testing.T) {
	built, err := Array(FloatType, []Value{Float(1), Float(2)})
	if err != nil {
		t.Fatal(err)
	}
	floats, err := FromValue[[]float64](built)
	if err != nil || len(floats) != 2 || floats[1] != 2 {
		t.Fatalf("floats = %v, %v", floats, err)
	}
	dict, err := Dict(IntType, map[string]Value{"a": Int(1)})
	if err != nil {
		t.Fatal(err)
	}
	ints, err := FromValue[map[string]int64](dict)
	if err != nil || ints["a"] != 1 {
		t.Fatalf("ints = %v, %v", ints, err)
	}
	// A container of containers is the one shape that needs a []Value.
	matrix, err := ToValue([][]float64{{1, 2}, {3}})
	if err != nil {
		t.Fatal(err)
	}
	rows, err := FromValue[[][]float64](matrix)
	if err != nil || len(rows) != 2 || rows[1][0] != 3 {
		t.Fatalf("rows = %v, %v", rows, err)
	}
	if !matrix.hasType(ArrayOf(ArrayOf(FloatType))) {
		t.Fatalf("matrix type = %s", matrix.Type())
	}
}

// The boundary keeps the language's invariants: no NaN gets in, and a native
// slice of the wrong element type is refused rather than converted.
func TestBoundaryKeepsTheInvariants(t *testing.T) {
	if _, err := ToValue([]float64{1, nan()}); err == nil || !strings.Contains(err.Error(), "item 1") {
		t.Fatalf("NaN error = %v", err)
	}
	if _, err := coerce([]float64{1}, ArrayOf(IntType)); err == nil || !strings.Contains(err.Error(), "want array<int>") {
		t.Fatalf("mismatch error = %v", err)
	}
	// JSON shapes still convert: float64 for an int, []any for an array.
	value, err := coerce([]any{float64(1), float64(2)}, ArrayOf(IntType))
	if err != nil {
		t.Fatal(err)
	}
	if ints, _ := FromValue[[]int64](value); len(ints) != 2 || ints[1] != 2 {
		t.Fatalf("ints = %v", ints)
	}
	if _, err := FromValue[[]int64](Float(1)); err == nil || !strings.Contains(err.Error(), "want []int64") {
		t.Fatalf("type error = %v", err)
	}
}

func TestValueConstructorsRejectNestedNonFiniteFloats(t *testing.T) {
	if _, err := Array(FloatType, []Value{Float(math.Inf(1))}); err == nil {
		t.Fatal("array accepted infinity")
	}
	if _, err := Dict(FloatType, map[string]Value{"risk": Float(math.NaN())}); err == nil {
		t.Fatal("dictionary accepted NaN")
	}
}

// Equality and the array primitives work the same on every backing.
func TestContainerOperationsSpanBackings(t *testing.T) {
	native, _ := ToValue([]string{"a", "b"})
	built, _ := Array(StringType, []Value{String("a"), String("b")})
	if !native.Equal(built) {
		t.Fatal("equal arrays compare unequal")
	}
	if got, _ := native.tail().Any().([]string); len(got) != 1 || got[0] != "b" {
		t.Fatalf("tail = %v", got)
	}
	items, _ := built.Array()
	if len(items) != 2 || items[1].s != "b" {
		t.Fatalf("items = %v", items)
	}
	dict, _ := ToValue(map[string]float64{"z": 1, "a": 2})
	if keys := dict.keys(); len(keys) != 2 || keys[0] != "a" {
		t.Fatalf("keys = %v", keys)
	}
}

func nan() float64 {
	zero := 0.0
	return zero / zero
}
