package machine

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
)

// Value is immutable by convention, not by copying.
//
// A container is stored in the Go form a host already has: array<float> is a
// []float64, dict<int> is a map[string]int64, and only a container of
// containers falls back to []Value. That is what makes the host boundary free:
// a feature vector handed to RunValues, passed to an extension and returned
// from it is one backing array the whole way, never converted and never
// copied. The price is a rule the package cannot enforce: whoever holds a
// slice a Value was built from, or received one from a Value, must not write
// to it. The VM never does.
//
// Scalars live inline. The struct is kept small on purpose — every push, pop
// and local binding copies one.
type Value struct {
	kind Kind
	b    bool
	i    int64
	f    float64
	s    string // a string's text, or a handle's type name
	// box is the container backing: []bool, []int64, []float64, []string,
	// *nestedArray, or the map[string] counterparts and *nestedDict. For a
	// handle it is the host's payload, untouched.
	box any
}

// nestedArray and nestedDict back a container whose elements are containers.
// They carry the element type, which a native slice implies.
type nestedArray struct {
	elem  Type
	items []Value
}

type nestedDict struct {
	elem    Type
	entries map[string]Value
}

func Bool(v bool) Value     { return Value{kind: BoolKind, b: v} }
func Int(v int64) Value     { return Value{kind: IntKind, i: v} }
func Float(v float64) Value { return Value{kind: FloatKind, f: v} }
func String(v string) Value { return Value{kind: StringKind, s: v} }

// NewHandle wraps a host value the language will only pass along — an
// inference engine's tensor, typically. The payload is not inspected, copied
// or compared; FromValue hands it back as it is.
func NewHandle(name string, payload any) Value {
	return Value{kind: HandleKind, s: name, box: payload}
}

// Payload returns a handle's host value.
func (v Value) Payload() (any, bool) { return v.box, v.kind == HandleKind }

// Array builds an array from values, checking each against elem and packing
// them into the canonical backing. A host that already holds a []float64
// should use ToValue instead, which wraps it without a pass.
func Array(elem Type, values []Value) (Value, error) {
	if !elem.IsConcrete() {
		return Value{}, fmt.Errorf("array element type must be concrete: %s", elem)
	}
	builder := newArrayBuilder(elem, len(values))
	for i, value := range values {
		if !value.hasType(elem) {
			return Value{}, fmt.Errorf("array item %d has type %s, want %s", i, value.Type(), elem)
		}
		builder.add(value)
	}
	return builder.finish(), nil
}

// Dict is Array's counterpart for dictionaries.
func Dict(elem Type, entries map[string]Value) (Value, error) {
	if !elem.IsConcrete() {
		return Value{}, fmt.Errorf("dictionary value type must be concrete: %s", elem)
	}
	for key, value := range entries {
		if !value.hasType(elem) {
			return Value{}, fmt.Errorf("dictionary entry %q has type %s, want %s", key, value.Type(), elem)
		}
	}
	return packDict(elem, entries), nil
}

func (v Value) Kind() Kind { return v.kind }

// elemType is the element type of a container. Native backings imply it, so
// the answer costs no allocation.
func (v Value) elemType() Type {
	switch box := v.box.(type) {
	case []bool, map[string]bool:
		return BoolType
	case []int64, map[string]int64:
		return IntType
	case []float64, map[string]float64:
		return FloatType
	case []string, map[string]string:
		return StringType
	case *nestedArray:
		return box.elem
	case *nestedDict:
		return box.elem
	default:
		return Type{Kind: InvalidKind}
	}
}

// hasType answers the same question as Type().Equal(t) without building a Type,
// which would allocate for every container check in the interpreter loop.
func (v Value) hasType(t Type) bool {
	if v.kind != t.Kind {
		return false
	}
	if v.kind == HandleKind {
		return v.s == t.Name
	}
	if v.kind != ArrayKind && v.kind != DictKind {
		return true
	}
	return t.Elem != nil && v.elemType().Equal(*t.Elem)
}

func (v Value) Type() Type {
	switch v.kind {
	case BoolKind:
		return BoolType
	case IntKind:
		return IntType
	case FloatKind:
		return FloatType
	case StringKind:
		return StringType
	case ArrayKind:
		return ArrayOf(CloneType(v.elemType()))
	case DictKind:
		return DictOf(CloneType(v.elemType()))
	case HandleKind:
		return HandleOf(v.s)
	default:
		return Type{Kind: InvalidKind}
	}
}

func (v Value) Bool() (bool, bool)     { return v.b, v.kind == BoolKind }
func (v Value) Int() (int64, bool)     { return v.i, v.kind == IntKind }
func (v Value) Float() (float64, bool) { return v.f, v.kind == FloatKind }
func (v Value) String() (string, bool) { return v.s, v.kind == StringKind }

// Array returns the items as values. For a nested array this is the backing
// itself, read-only; for a native one it is built, so a host that wants the
// []float64 should ask FromValue for it and get the backing with no pass.
func (v Value) Array() ([]Value, bool) {
	if v.kind != ArrayKind {
		return nil, false
	}
	if nested, ok := v.box.(*nestedArray); ok {
		return nested.items, true
	}
	out := make([]Value, v.length())
	for i := range out {
		out[i] = v.at(i)
	}
	return out, true
}

// Dict is Array's counterpart for dictionaries.
func (v Value) Dict() (map[string]Value, bool) {
	if v.kind != DictKind {
		return nil, false
	}
	if nested, ok := v.box.(*nestedDict); ok {
		return nested.entries, true
	}
	out := make(map[string]Value, v.length())
	for _, key := range v.keys() {
		out[key], _ = v.lookup(key)
	}
	return out, true
}

// Any returns a JSON-compatible representation. A native container is handed
// over as it is — a []float64 is already JSON — so the result is read-only.
func (v Value) Any() any {
	switch v.kind {
	case BoolKind:
		return v.b
	case IntKind:
		return v.i
	case FloatKind:
		return v.f
	case StringKind:
		return v.s
	case ArrayKind, DictKind:
		return v.containerAny()
	case HandleKind:
		// A handle has no JSON form; naming its type is all a log can show.
		return v.Type().String()
	default:
		return nil
	}
}

func (v Value) containerAny() any {
	switch box := v.box.(type) {
	case *nestedArray:
		out := make([]any, len(box.items))
		for i := range box.items {
			out[i] = box.items[i].Any()
		}
		return out
	case *nestedDict:
		out := make(map[string]any, len(box.entries))
		for key, value := range box.entries {
			out[key] = value.Any()
		}
		return out
	default:
		return box
	}
}

func (v Value) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.Any())
}

// Equal compares two values of one type. Handles are never equal: the
// language cannot see into them, and the VM refuses to compare them at all
// (see compareEqual), so this answer is only a safe default.
func (v Value) Equal(other Value) bool {
	if !v.hasType(other.Type()) {
		return false
	}
	switch v.kind {
	case BoolKind:
		return v.b == other.b
	case IntKind:
		return v.i == other.i
	case FloatKind:
		return v.f == other.f
	case StringKind:
		return v.s == other.s
	case ArrayKind:
		return v.equalArray(other)
	case DictKind:
		return v.equalDict(other)
	default:
		return false
	}
}

func (v Value) equalArray(other Value) bool {
	if v.length() != other.length() {
		return false
	}
	for i := 0; i < v.length(); i++ {
		if !v.at(i).Equal(other.at(i)) {
			return false
		}
	}
	return true
}

func (v Value) equalDict(other Value) bool {
	if v.length() != other.length() {
		return false
	}
	for _, key := range v.keys() {
		mine, _ := v.lookup(key)
		theirs, ok := other.lookup(key)
		if !ok || !mine.Equal(theirs) {
			return false
		}
	}
	return true
}

// compareEqual is the VM's equality: eq and switch both use it. Handles are
// opaque, so comparing them is an error rather than a guess.
func compareEqual(left, right Value) (Value, error) {
	if left.kind == HandleKind || right.kind == HandleKind {
		return Value{}, fmt.Errorf("handles cannot be compared")
	}
	if !left.hasType(right.Type()) {
		return Value{}, fmt.Errorf("equality requires one type, got %s and %s", left.Type(), right.Type())
	}
	return Bool(left.Equal(right)), nil
}

// CheckedFloat builds a float value, refusing the non-finite ones: a routing
// decision has no meaning for NaN or infinity, so they are rejected where they
// enter rather than checked at every use.
func CheckedFloat(value float64) (Value, error) {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return Value{}, fmt.Errorf("non-finite floats are not supported")
	}
	return Float(value), nil
}

func sortedKeys[T any](entries map[string]T) []string {
	keys := make([]string, 0, len(entries))
	for key := range entries {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
