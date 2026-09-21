package machine

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
)

// The host boundary, in both directions, with one list of Go types.
//
// fromGo is that list. ToValue, Fn1/Fn2/Fn3's result conversion and Run's
// argument binding all go through it, and goType derives a function's
// signature from it, so a Go type is supported everywhere or nowhere. A
// container is wrapped, not converted: the Value holds the caller's slice.

// errUnsupportedGoType is a sentinel so the by-name path, which tries fromGo
// first on every argument, pays nothing when it has to fall through.
var errUnsupportedGoType = errors.New("unsupported Go type")

// fromGo wraps a Go value. Containers keep their backing.
func fromGo(input any) (Value, error) {
	switch x := input.(type) {
	case Value:
		return x, nil
	case bool:
		return Bool(x), nil
	case int64:
		return Int(x), nil
	case float64:
		return CheckedFloat(x)
	case string:
		return String(x), nil
	case []bool, []int64, []string:
		return Value{kind: ArrayKind, box: x}, nil
	case []float64:
		return Value{kind: ArrayKind, box: x}, checkFloats(x)
	case [][]float64:
		return nestedFloats(x)
	case map[string]bool, map[string]int64, map[string]string:
		return Value{kind: DictKind, box: x}, nil
	case map[string]float64:
		for key, value := range x {
			if math.IsNaN(value) || math.IsInf(value, 0) {
				return Value{}, fmt.Errorf("entry %q: non-finite floats are not supported", key)
			}
		}
		return Value{kind: DictKind, box: x}, nil
	default:
		return Value{}, errUnsupportedGoType
	}
}

// checkFloats is the one pass a float slice gets: a read, not a copy, and it
// keeps the invariant that no NaN or infinity is ever inside the VM.
func checkFloats(values []float64) error {
	for i, value := range values {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return fmt.Errorf("item %d: non-finite floats are not supported", i)
		}
	}
	return nil
}

// nestedFloats wraps a matrix. Each row is wrapped, not copied; the outer
// slice of Values is the only allocation.
func nestedFloats(rows [][]float64) (Value, error) {
	items := make([]Value, len(rows))
	for i, row := range rows {
		if err := checkFloats(row); err != nil {
			return Value{}, fmt.Errorf("row %d: %w", i, err)
		}
		items[i] = Value{kind: ArrayKind, box: row}
	}
	return Value{kind: ArrayKind, box: &nestedArray{elem: ArrayOf(FloatType), items: items}}, nil
}

// ToValue wraps a Go value for RunValues or for an extension's result. The
// caller must not write to a slice or map after handing it over.
//
// Scalars are matched through a pointer so they are never boxed; a container
// is boxed once, and that box — a slice header, not the elements — is the
// Value's backing.
func ToValue[T any](input T) (Value, error) {
	switch scalar := any(&input).(type) {
	case *bool:
		return Bool(*scalar), nil
	case *int64:
		return Int(*scalar), nil
	case *float64:
		return CheckedFloat(*scalar)
	case *string:
		return String(*scalar), nil
	}
	value, err := fromGo(input)
	if errors.Is(err, errUnsupportedGoType) {
		return Value{}, fmt.Errorf("unsupported Go type %T", input)
	}
	return value, err
}

// FromValue returns the Go form of a value. For a container it is the backing
// itself — no pass over the elements — and the caller must treat it as
// read-only.
func FromValue[T any](value Value) (T, error) {
	var out T
	switch target := any(&out).(type) {
	case *bool:
		return out, assign(target, Value.Bool, value)
	case *int64:
		return out, assign(target, Value.Int, value)
	case *float64:
		return out, assign(target, Value.Float, value)
	case *string:
		return out, assign(target, Value.String, value)
	case *[][]float64:
		return out, assignRows(target, value)
	}
	if boxed, ok := value.box.(T); ok {
		return boxed, nil
	}
	return out, fmt.Errorf("argument is %s, want %T", value.Type(), out)
}

func assign[T any](target *T, get func(Value) (T, bool), value Value) error {
	element, ok := get(value)
	if !ok {
		return fmt.Errorf("argument is %s, want %T", value.Type(), element)
	}
	*target = element
	return nil
}

// assignRows unwraps a matrix: the rows are the backings themselves.
func assignRows(target *[][]float64, value Value) error {
	nested, ok := value.box.(*nestedArray)
	if !ok || !nested.elem.Equal(ArrayOf(FloatType)) {
		return fmt.Errorf("argument is %s, want [][]float64", value.Type())
	}
	rows := make([][]float64, len(nested.items))
	for i, item := range nested.items {
		rows[i], _ = item.box.([]float64)
	}
	*target = rows
	return nil
}

// coerce converts an argument Run received by name. A Go value of the exact
// type is wrapped as it is; the lenient cases below exist for what a JSON
// decoder produces — float64 for every number, []any for every array.
func coerce(input any, expected Type) (Value, error) {
	if value, err := fromGo(input); err == nil {
		if value.hasType(expected) {
			return value, nil
		}
		// A typed Value of the wrong type is a host bug. A float64 where an
		// int is expected is a JSON decoder at work, and the lenient path below
		// takes it — but a []float64 never becomes an array<int>.
		if _, isValue := input.(Value); isValue || !leniently(expected.Kind) {
			return Value{}, fmt.Errorf("got %s, want %s", value.Type(), expected)
		}
	}
	switch expected.Kind {
	case BoolKind:
		return Value{}, fmt.Errorf("got %T, want bool", input)
	case IntKind:
		return coerceInt(input)
	case FloatKind:
		return coerceFloat(input)
	case StringKind:
		return Value{}, fmt.Errorf("got %T, want string", input)
	case ArrayKind:
		return coerceArray(input, expected)
	case DictKind:
		return coerceDict(input, expected)
	default:
		return Value{}, fmt.Errorf("unsupported expected type %s", expected)
	}
}

// leniently reports whether a mismatched native value may still convert: a
// float64 holding 3 is a fine int, a []float64 is not an array<int>.
func leniently(kind Kind) bool {
	return kind == IntKind || kind == FloatKind
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
		return CheckedFloat(float64(value))
	case float64:
		return CheckedFloat(value)
	case json.Number:
		parsed, err := value.Float64()
		if err != nil {
			return Value{}, fmt.Errorf("%q is not a float", value)
		}
		return CheckedFloat(parsed)
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
	builder := newArrayBuilder(*expected.Elem, len(raw))
	for i := range raw {
		value, err := coerce(raw[i], *expected.Elem)
		if err != nil {
			return Value{}, fmt.Errorf("array item %d: %w", i, err)
		}
		builder.add(value)
	}
	return builder.finish(), nil
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
	return packDict(*expected.Elem, entries), nil
}
