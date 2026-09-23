package machine

import "testing"

// Equality and the array primitives work the same on every backing.
func TestContainerOperationsSpanBackings(t *testing.T) {
	t.Parallel()
	native, _ := ToValue([]string{"a", "b"})
	built, _ := Array(StringType, []Value{String("a"), String("b")})
	if !native.Equal(built) {
		t.Fatal("equal arrays compare unequal")
	}
	if got, _ := native.tail().Any().([]string); len(got) != 1 || got[0] != "b" {
		t.Fatalf("tail of [a b] = %v, want [b]", got)
	}
	items, _ := built.Array()
	if len(items) != 2 || items[1].s != "b" {
		t.Fatalf("Array() of [a b] = %v, want [a b]", items)
	}
	dict, _ := ToValue(map[string]float64{"z": 1, "a": 2})
	if keys := dict.keys(); len(keys) != 2 || keys[0] != "a" {
		t.Fatalf("keys of {z: 1, a: 2} = %v, want [a z]", keys)
	}
}
