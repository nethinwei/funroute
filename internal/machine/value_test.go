package machine

import (
	"encoding/json"
	"math"
	"testing"
)

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

func TestValueConstructorsRejectNestedNonFiniteFloats(t *testing.T) {
	t.Parallel()
	if _, err := Array(FloatType, []Value{Float(math.Inf(1))}); err == nil {
		t.Fatal("array accepted infinity")
	}
	if _, err := Dict(FloatType, map[string]Value{"risk": Float(math.NaN())}); err == nil {
		t.Fatal("dictionary accepted NaN")
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
