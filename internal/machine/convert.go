package machine

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"

	"github.com/nethinwei/funroute/internal/money"
)

// The host boundary, in both directions, with one list of Go types.
//
// fromGo is that list. ToValue, the Go functions a FunctionSpec names in Go
// and Run's argument binding all go through it, and goType derives a function's
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
		return Value{kind: DictKind, box: x}, checkFloatMap(x)
	case money.Money:
		return MoneyValue(x.Minor(), x.Currency()), nil
	case money.Ratio:
		return RatioValue(x), nil
	case money.FxRate:
		return FxRateValue(x), money.CheckFxRate(x)
	case money.Currency:
		return CurrencyValue(x.Code()), nil
	case []money.Money:
		return Value{kind: ArrayKind, box: x}, nil
	case []money.FxRate:
		return Value{kind: ArrayKind, box: x}, checkFxRates(x)
	case map[string]money.Money:
		return Value{kind: DictKind, box: x}, nil
	default:
		return structFromGo(input)
	}
}

// structFromGo is the one open-ended case: a struct is a record, and which
// struct it is only reflection can say. Everything above is a closed list, so
// this is the only place the boundary pays for reflection.
func structFromGo(input any) (Value, error) {
	value := reflect.ValueOf(input)
	for value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return Value{}, errUnsupportedGoType
		}
		value = value.Elem()
	}
	if value.Kind() != reflect.Struct {
		return Value{}, errUnsupportedGoType
	}
	return structValue(value)
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

// checkFxRates refuses the zero FxRate in a host's slice: it is no rate.
func checkFxRates(rates []money.FxRate) error {
	for i, rate := range rates {
		if err := money.CheckFxRate(rate); err != nil {
			return fmt.Errorf("item %d: %w", i, err)
		}
	}
	return nil
}

// checkFloatMap is checkFloats for a dictionary's backing.
func checkFloatMap(entries map[string]float64) error {
	for key, value := range entries {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return fmt.Errorf("entry %q: non-finite floats are not supported", key)
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
	case *money.Money:
		return MoneyValue(scalar.Minor(), scalar.Currency()), nil
	case *money.Ratio:
		return RatioValue(*scalar), nil
	case *money.Currency:
		return CurrencyValue(scalar.Code()), nil
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
	case *money.Money:
		return out, assign(target, Value.Money, value)
	case *money.Ratio:
		return out, assign(target, Value.Ratio, value)
	case *money.FxRate:
		return out, assign(target, Value.FxRate, value)
	case *money.Currency:
		code, ok := value.Currency()
		*target = code
		if !ok {
			return out, fmt.Errorf("argument is %s, want %T", value.Type(), out)
		}
		return out, nil
	}
	if boxed, ok := value.box.(T); ok {
		return boxed, nil
	}
	if value.kind == RecordKind {
		return fromRecord[T](value)
	}
	return out, fmt.Errorf("argument is %s, want %T", value.Type(), out)
}

// fromRecord fills the struct (or map) a host asked for from a record.
func fromRecord[T any](value Value) (T, error) {
	var out T
	target := reflect.ValueOf(&out).Elem()
	if target.Kind() == reflect.Map || target.Kind() == reflect.Interface {
		converted, ok := value.Any().(T)
		if !ok {
			return out, fmt.Errorf("argument is %s, want %T", value.Type().Summary(), out)
		}
		return converted, nil
	}
	filled, err := intoStruct(nil, value, target.Type())
	if err != nil {
		return out, err
	}
	target.Set(filled)
	return out, nil
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
	return coerceWith(input, expected, nil)
}

// coerceWith is coerce with the registry's currency table, which reading
// money written as "USD 1.70" needs: the places belong to the currency.
func coerceWith(input any, expected Type, table *money.Currencies) (Value, error) {
	if value, err := fromGo(input); err == nil {
		if value.hasType(expected) {
			return value, nil
		}
		// A typed Value of the wrong type is a host bug. A float64 where an
		// int is expected is a JSON decoder at work, and the lenient path below
		// takes it — but a []float64 never becomes an array<int>.
		if _, isValue := input.(Value); isValue || (!leniently(expected.kind) && !containerKind(expected.kind)) {
			return Value{}, fmt.Errorf("got %s, want %s", value.Type().Summary(), expected.Summary())
		}
	}
	switch expected.kind {
	case BoolKind:
		return Value{}, fmt.Errorf("got %T, want bool", input)
	case IntKind:
		return coerceInt(input)
	case FloatKind:
		return coerceFloat(input)
	case StringKind:
		return Value{}, fmt.Errorf("got %T, want string", input)
	case ArrayKind:
		return coerceArray(input, expected, table)
	case DictKind:
		return coerceDict(input, expected, table)
	case RecordKind:
		return coerceRecord(input, expected, table)
	case MoneyKind, RatioKind, FxRateKind, CurrencyKind:
		return coerceMoneyKind(input, expected, table)
	default:
		return Value{}, fmt.Errorf("unsupported expected type %s", expected)
	}
}

// coerceRecord builds a record from the object a host or a JSON decoder hands
// over. Every field the type declares must be there — a record with a missing
// field is not that record, and there is no null to stand in for one. Fields
// the contract does not declare are ignored: one payload serves many rules,
// and a misspelled name still shows up as the missing one.
func coerceRecord(input any, expected Type, table *money.Currencies) (Value, error) {
	entries, ok := input.(map[string]any)
	if !ok {
		return Value{}, fmt.Errorf("got %T, want %s", input, expected.Summary())
	}
	fields := make([]Value, len(expected.fields))
	for i, field := range expected.fields {
		raw, present := entries[field.name]
		if !present {
			return Value{}, fmt.Errorf("field %q is missing", field.name)
		}
		value, err := coerceWith(raw, field.typ, table)
		if err != nil {
			return Value{}, fmt.Errorf("field %q: %w", field.name, err)
		}
		fields[i] = value
	}
	return Record(expected, fields)
}

// leniently reports whether a mismatched native value may still convert: a
// float64 holding 3 is a fine int, a []float64 is not an array<int>.
func leniently(kind Kind) bool {
	return kind == IntKind || kind == FloatKind || IsMoneyKind(kind)
}

func containerKind(kind Kind) bool {
	return kind == ArrayKind || kind == DictKind || kind == RecordKind
}

// maxExactFloatInt is where float64 stops counting whole numbers exactly.
// Widening an integer past it would round, so it is refused instead.
const maxExactFloatInt = int64(1) << 53

func coerceInt(input any) (Value, error) {
	switch value := input.(type) {
	case uint8:
		return Int(int64(value)), nil
	case uint16:
		return Int(int64(value)), nil
	case uint32:
		return Int(int64(value)), nil
	case uint:
		return unsignedInt(uint64(value))
	case uint64:
		return unsignedInt(value)
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
		// A float64 past 2^53 has already been rounded — JSON decoded into
		// any turns 9007199254740993 into 9007199254740992 — so the int it
		// holds is not the one that was written.
		whole, ok := floatInt(value)
		if !ok || whole > maxExactFloatInt || whole < -maxExactFloatInt {
			return Value{}, fmt.Errorf("%v is not an int a float64 holds exactly: decode JSON with UseNumber, or pass an int", value)
		}
		return Int(whole), nil
	default:
		return Value{}, fmt.Errorf("got %T, want int", input)
	}
}

// floatInt is the int a float holds, and false for one with a fraction or
// outside int64. The upper bound is 2^63 itself, exclusive: math.MaxInt64
// converts to that float, so comparing against it lets 2^63 through, and
// int64(2^63) is not defined.
func floatInt(value float64) (int64, bool) {
	if value < -0x1p63 || value >= 0x1p63 || math.Trunc(value) != value {
		return 0, false
	}
	return int64(value), true
}

func unsignedInt(value uint64) (Value, error) {
	if value > uint64(math.MaxInt64) {
		return Value{}, fmt.Errorf("%d does not fit in an int", value)
	}
	return Int(int64(value)), nil
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
		// A whole number is a float with nothing lost, which is what a host
		// means by passing 1 where a rate is wanted — the same thing the JSON
		// boundary already accepts. Past 2^53 it would round, so it stops.
		if integer, err := coerceInt(input); err == nil {
			whole, _ := integer.Int()
			if whole < -maxExactFloatInt || whole > maxExactFloatInt {
				return Value{}, fmt.Errorf("%d cannot be represented exactly as float", whole)
			}
			return CheckedFloat(float64(whole))
		}
		return Value{}, fmt.Errorf("got %T, want float", input)
	}
}

func coerceArray(input any, expected Type, table *money.Currencies) (Value, error) {
	if expected.elem == nil {
		return Value{}, errors.New("array type is missing its element type")
	}
	raw, err := anySlice(input, expected)
	if err != nil {
		return Value{}, err
	}
	builder := newArrayBuilder(*expected.elem, len(raw))
	for i := range raw {
		value, err := coerceWith(raw[i], *expected.elem, table)
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
		return toAnys(values), nil
	case []string:
		return toAnys(values), nil
	default:
		return nil, fmt.Errorf("got %T, want %s", input, expected.Summary())
	}
}

// toAnys boxes each item, the []any a decoder would have produced.
func toAnys[T any](values []T) []any {
	raw := make([]any, len(values))
	for i := range values {
		raw[i] = values[i]
	}
	return raw
}

func coerceDict(input any, expected Type, table *money.Currencies) (Value, error) {
	if expected.elem == nil {
		return Value{}, errors.New("dictionary type is missing its value type")
	}
	raw, err := anyMap(input, expected)
	if err != nil {
		return Value{}, err
	}
	entries := make(map[string]Value, len(raw))
	for key, item := range raw {
		value, err := coerceWith(item, *expected.elem, table)
		if err != nil {
			return Value{}, fmt.Errorf("dictionary entry %q: %w", key, err)
		}
		entries[key] = value
	}
	return packDict(*expected.elem, entries), nil
}

func anyMap(input any, expected Type) (map[string]any, error) {
	if raw, ok := input.(map[string]any); ok {
		return raw, nil
	}
	values, ok := input.(map[string]string)
	if !ok {
		return nil, fmt.Errorf("got %T, want %s", input, expected.Summary())
	}
	raw := make(map[string]any, len(values))
	for key, value := range values {
		raw[key] = value
	}
	return raw, nil
}
