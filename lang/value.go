package lang

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
)

// Value is immutable: items and entries are private, and the only way out of
// the package (Array, Dict, Any) copies. Nothing outside can reach the backing
// storage, so the VM passes values around without deep copying them.
type Value struct {
	kind     Kind
	i        int64
	f        float64
	s        string
	b        bool
	items    []Value
	entries  map[string]Value
	elemType Type
}

func Bool(v bool) Value     { return Value{kind: BoolKind, b: v} }
func Int(v int64) Value     { return Value{kind: IntKind, i: v} }
func Float(v float64) Value { return Value{kind: FloatKind, f: v} }
func String(v string) Value { return Value{kind: StringKind, s: v} }

func Array(elem Type, values []Value) (Value, error) {
	if !elem.IsConcrete() {
		return Value{}, fmt.Errorf("array element type must be concrete: %s", elem)
	}
	out := make([]Value, len(values))
	for i, value := range values {
		if !value.hasType(elem) {
			return Value{}, fmt.Errorf("array item %d has type %s, want %s", i, value.Type(), elem)
		}
		out[i] = value
	}
	return Value{kind: ArrayKind, items: out, elemType: cloneType(elem)}, nil
}

func Dict(elem Type, entries map[string]Value) (Value, error) {
	if !elem.IsConcrete() {
		return Value{}, fmt.Errorf("dictionary value type must be concrete: %s", elem)
	}
	out := make(map[string]Value, len(entries))
	for key, value := range entries {
		if !value.hasType(elem) {
			return Value{}, fmt.Errorf("dictionary entry %q has type %s, want %s", key, value.Type(), elem)
		}
		out[key] = value
	}
	return Value{kind: DictKind, entries: out, elemType: cloneType(elem)}, nil
}

func (v Value) Kind() Kind { return v.kind }

// hasType answers the same question as Type().Equal(t) without building a Type,
// which would allocate for every container check in the interpreter loop.
func (v Value) hasType(t Type) bool {
	if v.kind != t.Kind {
		return false
	}
	if v.kind != ArrayKind && v.kind != DictKind {
		return true
	}
	return t.Elem != nil && v.elemType.Equal(*t.Elem)
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
		return ArrayOf(cloneType(v.elemType))
	case DictKind:
		return DictOf(cloneType(v.elemType))
	default:
		return Type{Kind: InvalidKind}
	}
}

func (v Value) Bool() (bool, bool)             { return v.b, v.kind == BoolKind }
func (v Value) Int() (int64, bool)             { return v.i, v.kind == IntKind }
func (v Value) Float() (float64, bool)         { return v.f, v.kind == FloatKind }
func (v Value) String() (string, bool)         { return v.s, v.kind == StringKind }
func (v Value) Array() ([]Value, bool)         { return cloneSlice(v.items), v.kind == ArrayKind }
func (v Value) Dict() (map[string]Value, bool) { return cloneMap(v.entries), v.kind == DictKind }

func (v Value) Clone() Value {
	out := v
	out.elemType = cloneType(v.elemType)
	out.items = cloneSlice(v.items)
	out.entries = cloneMap(v.entries)
	return out
}

func cloneSlice(in []Value) []Value {
	if in == nil {
		return nil
	}
	out := make([]Value, len(in))
	for i := range in {
		out[i] = in[i].Clone()
	}
	return out
}

func cloneMap(in map[string]Value) map[string]Value {
	if in == nil {
		return nil
	}
	out := make(map[string]Value, len(in))
	for key, value := range in {
		out[key] = value.Clone()
	}
	return out
}

// Any returns a JSON-compatible representation of the value.
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
	case ArrayKind:
		out := make([]any, len(v.items))
		for i := range v.items {
			out[i] = v.items[i].Any()
		}
		return out
	case DictKind:
		out := make(map[string]any, len(v.entries))
		for key, value := range v.entries {
			out[key] = value.Any()
		}
		return out
	default:
		return nil
	}
}

func (v Value) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.Any())
}

func (v Value) Equal(other Value) bool {
	if !v.Type().Equal(other.Type()) {
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
		if len(v.items) != len(other.items) {
			return false
		}
		for i := range v.items {
			if !v.items[i].Equal(other.items[i]) {
				return false
			}
		}
		return true
	case DictKind:
		if len(v.entries) != len(other.entries) {
			return false
		}
		for key, item := range v.entries {
			otherItem, ok := other.entries[key]
			if !ok || !item.Equal(otherItem) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func coerce(input any, expected Type) (Value, error) {
	if value, ok := input.(Value); ok {
		if !value.Type().Equal(expected) {
			return Value{}, fmt.Errorf("got %s, want %s", value.Type(), expected)
		}
		return value.Clone(), nil
	}
	switch expected.Kind {
	case BoolKind:
		value, ok := input.(bool)
		if !ok {
			return Value{}, fmt.Errorf("got %T, want bool", input)
		}
		return Bool(value), nil
	case IntKind:
		return coerceInt(input)
	case FloatKind:
		return coerceFloat(input)
	case StringKind:
		value, ok := input.(string)
		if !ok {
			return Value{}, fmt.Errorf("got %T, want string", input)
		}
		return String(value), nil
	case ArrayKind:
		return coerceArray(input, expected)
	case DictKind:
		return coerceDict(input, expected)
	default:
		return Value{}, fmt.Errorf("unsupported expected type %s", expected)
	}
}

func coerceInt(input any) (Value, error) {
	switch value := input.(type) {
	case int:
		return Int(int64(value)), nil
	case int8:
		return Int(int64(value)), nil
	case int16:
		return Int(int64(value)), nil
	case int32:
		return Int(int64(value)), nil
	case int64:
		return Int(value), nil
	case json.Number:
		parsed, err := value.Int64()
		if err != nil {
			return Value{}, fmt.Errorf("%q is not an int", value)
		}
		return Int(parsed), nil
	case float64:
		if value < math.MinInt64 || value > math.MaxInt64 || math.Trunc(value) != value {
			return Value{}, fmt.Errorf("%v is not an int", value)
		}
		return Int(int64(value)), nil
	default:
		return Value{}, fmt.Errorf("got %T, want int", input)
	}
}

func coerceFloat(input any) (Value, error) {
	switch value := input.(type) {
	case float32:
		return checkedFloat(float64(value))
	case float64:
		return checkedFloat(value)
	case json.Number:
		parsed, err := value.Float64()
		if err != nil {
			return Value{}, fmt.Errorf("%q is not a float", value)
		}
		return checkedFloat(parsed)
	default:
		return Value{}, fmt.Errorf("got %T, want float", input)
	}
}

func coerceArray(input any, expected Type) (Value, error) {
	if expected.Elem == nil {
		return Value{}, fmt.Errorf("array type is missing its element type")
	}
	raw, err := anySlice(input, expected)
	if err != nil {
		return Value{}, err
	}
	values := make([]Value, len(raw))
	for i := range raw {
		value, err := coerce(raw[i], *expected.Elem)
		if err != nil {
			return Value{}, fmt.Errorf("array item %d: %w", i, err)
		}
		values[i] = value
	}
	return Value{kind: ArrayKind, items: values, elemType: cloneType(*expected.Elem)}, nil
}

func anySlice(input any, expected Type) ([]any, error) {
	switch values := input.(type) {
	case []any:
		return values, nil
	case []Value:
		raw := make([]any, len(values))
		for i := range values {
			raw[i] = values[i]
		}
		return raw, nil
	default:
		return nil, fmt.Errorf("got %T, want %s", input, expected)
	}
}

func coerceDict(input any, expected Type) (Value, error) {
	if expected.Elem == nil {
		return Value{}, fmt.Errorf("dictionary type is missing its value type")
	}
	raw, ok := input.(map[string]any)
	if !ok {
		return Value{}, fmt.Errorf("got %T, want %s", input, expected)
	}
	entries := make(map[string]Value, len(raw))
	for key, item := range raw {
		value, err := coerce(item, *expected.Elem)
		if err != nil {
			return Value{}, fmt.Errorf("dictionary entry %q: %w", key, err)
		}
		entries[key] = value
	}
	return Value{kind: DictKind, entries: entries, elemType: cloneType(*expected.Elem)}, nil
}

func checkedFloat(value float64) (Value, error) {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return Value{}, fmt.Errorf("non-finite floats are not supported")
	}
	return Float(value), nil
}

func sortedKeys(entries map[string]Value) []string {
	keys := make([]string, 0, len(entries))
	for key := range entries {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
