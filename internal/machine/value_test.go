package machine

import (
	"encoding/json"
	"fmt"
	"math"
	"testing"
)

// identical reports two values that are one value, as two ways of computing
// it must agree: Equal, except that a NaN is the NaN it is — Equal, as ==,
// holds no float equal to a NaN — and 0 and -0 are told apart.
func identical(a, b Value) bool {
	if a.kind == FloatKind && b.kind == FloatKind {
		return math.Float64bits(a.f) == math.Float64bits(b.f) || a.f != a.f && b.f != b.f
	}
	return a.Equal(b) || a.kind == b.kind && fmt.Sprint(a.Any()) == fmt.Sprint(b.Any())
}

// What the VM builds has the same backing as what a host supplies, so an
// extension sees a []float64 whichever way the array was made.
func TestBuiltArraysUseTheNativeBacking(t *testing.T) {
	t.Parallel()
	built, err := Array(FloatType, []Value{Float(1), Float(2)})
	if err != nil {
		t.Fatal(err)
	}
	floats, err := FromValue[[]float64](built)
	if err != nil || len(floats) != 2 || floats[1] != 2 {
		t.Fatalf("FromValue[[]float64](Array(float, [1 2])) = %v, %v, want [1 2], nil", floats, err)
	}
	dict, err := Dict(IntType, map[string]Value{"a": Int(1)})
	if err != nil {
		t.Fatal(err)
	}
	ints, err := FromValue[map[string]int64](dict)
	if err != nil || ints["a"] != 1 {
		t.Fatalf("FromValue[map[string]int64](Dict(int, {a: 1})) = %v, %v, want map[a:1], nil", ints, err)
	}
	// A container of containers is the one shape that needs a []Value.
	matrix, err := ToValue([][]float64{{1, 2}, {3}})
	if err != nil {
		t.Fatal(err)
	}
	rows, err := FromValue[[][]float64](matrix)
	if err != nil || len(rows) != 2 || rows[1][0] != 3 {
		t.Fatalf("FromValue[[][]float64](ToValue([[1 2] [3]])) = %v, %v, want [[1 2] [3]], nil", rows, err)
	}
	if !matrix.hasType(ArrayOf(ArrayOf(FloatType))) {
		t.Fatalf("ToValue([[1 2] [3]]) type = %s, want array<array<float>>", matrix.Type())
	}
}

// A float that is not finite is a float like any other: the constructors
// pack it as they pack the rest.
func TestValueConstructorsHoldNonFiniteFloats(t *testing.T) {
	t.Parallel()
	array, err := Array(FloatType, []Value{Float(math.Inf(1))})
	if items, _ := FromValue[[]float64](array); err != nil || len(items) != 1 || !math.IsInf(items[0], 1) {
		t.Fatalf("Array([+Inf]) = %v, %v; want [+Inf]", items, err)
	}
	dict, err := Dict(FloatType, map[string]Value{"risk": Float(math.NaN())})
	if entries, _ := FromValue[map[string]float64](dict); err != nil || !math.IsNaN(entries["risk"]) {
		t.Fatalf("Dict({risk: NaN}) = %v, %v; want {risk: NaN}", entries, err)
	}
}

// An empty container writes [] or {}, whether its backing is empty or nil —
// a host function may well answer nil — so its JSON reads back as an
// argument; inside another container too.
func TestAnEmptyContainerWritesItsEmptyJSON(t *testing.T) {
	t.Parallel()
	nested, err := ToValue([][]float64{nil})
	if err != nil {
		t.Fatal(err)
	}
	for want, input := range map[string]any{"[]": []float64(nil), "{}": map[string]int64(nil), "[[]]": nested} {
		value, ok := input.(Value)
		if !ok {
			if value, err = fromGo(input); err != nil {
				t.Fatal(err)
			}
		}
		if encoded, err := json.Marshal(value); err != nil || string(encoded) != want {
			t.Errorf("json.Marshal(%#v) = %s, %v, want %s", input, encoded, err, want)
		}
	}
}
