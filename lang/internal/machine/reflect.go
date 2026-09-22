package machine

import (
	"fmt"
	"math"
	"reflect"
)

// The reflective half of the boundary: any Go type built from the scalars,
// slices and string-keyed maps, at any depth, maps onto a FunRoute type, and a
// converter in each direction is resolved once at registration. The exact
// native forms ([]float64 for array<float>, and so on) still cross with no
// pass over the elements; every other shape is walked.

// reflectType is the FunRoute type of a Go type, or the handle it was defined
// as.
func reflectType(registry *Registry, typ reflect.Type) (Type, error) {
	if name, ok := registry.handleName(typ); ok {
		return HandleOf(name), nil
	}
	switch typ.Kind() {
	case reflect.Bool:
		return BoolType, nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32:
		return IntType, nil
	case reflect.Float32, reflect.Float64:
		return FloatType, nil
	case reflect.String:
		return StringType, nil
	case reflect.Slice:
		elem, err := reflectType(registry, typ.Elem())
		if err != nil {
			return Type{}, err
		}
		return ArrayOf(elem), nil
	case reflect.Struct:
		fields, _, err := structFields(registry, typ)
		if err != nil {
			return Type{}, err
		}
		return RecordOf(fields...), nil
	case reflect.Map:
		if typ.Key().Kind() != reflect.String {
			return Type{}, fmt.Errorf("unsupported Go type %s: dictionary keys must be strings", typ)
		}
		elem, err := reflectType(registry, typ.Elem())
		if err != nil {
			return Type{}, err
		}
		return DictOf(elem), nil
	default:
		return Type{}, fmt.Errorf("unsupported Go type %s", typ)
	}
}

// converterInto builds the Value → Go conversion for one parameter type.
func converterInto(registry *Registry, typ reflect.Type) func(Value) (reflect.Value, error) {
	if _, ok := registry.handleName(typ); ok {
		return func(value Value) (reflect.Value, error) {
			payload, ok := value.Payload()
			if !ok || !reflect.TypeOf(payload).AssignableTo(typ) {
				return reflect.Value{}, fmt.Errorf("argument is %s, want handle of %s", value.Type(), typ)
			}
			return reflect.ValueOf(payload), nil
		}
	}
	return func(value Value) (reflect.Value, error) { return intoGo(registry, value, typ) }
}

// intoGo converts one value into a Go type. A container whose backing already
// is the wanted Go type is handed over as it is.
func intoGo(registry *Registry, value Value, typ reflect.Type) (reflect.Value, error) {
	if value.box != nil && reflect.TypeOf(value.box) == typ {
		return reflect.ValueOf(value.box), nil
	}
	switch typ.Kind() {
	case reflect.Slice:
		return intoSlice(registry, value, typ)
	case reflect.Map:
		return intoMap(registry, value, typ)
	case reflect.Struct:
		return intoStruct(registry, value, typ)
	default:
		return intoScalar(value, typ)
	}
}

func intoScalar(value Value, typ reflect.Type) (reflect.Value, error) {
	out := reflect.New(typ).Elem()
	switch typ.Kind() {
	case reflect.Bool:
		if value.kind != BoolKind {
			return out, fmt.Errorf("argument is %s, want bool", value.Type())
		}
		out.SetBool(value.b)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if value.kind != IntKind || out.OverflowInt(value.i) {
			return out, fmt.Errorf("argument %s does not fit %s", value.Type(), typ)
		}
		out.SetInt(value.i)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32:
		if value.kind != IntKind || value.i < 0 || out.OverflowUint(uint64(value.i)) {
			return out, fmt.Errorf("argument %s does not fit %s", value.Type(), typ)
		}
		out.SetUint(uint64(value.i))
	case reflect.Float32, reflect.Float64:
		if value.kind != FloatKind || (typ.Kind() == reflect.Float32 && math.Abs(value.f) > math.MaxFloat32) {
			return out, fmt.Errorf("argument %s does not fit %s", value.Type(), typ)
		}
		out.SetFloat(value.f)
	case reflect.String:
		if value.kind != StringKind {
			return out, fmt.Errorf("argument is %s, want string", value.Type())
		}
		out.SetString(value.s)
	default:
		return out, fmt.Errorf("unsupported Go type %s", typ)
	}
	return out, nil
}

func intoSlice(registry *Registry, value Value, typ reflect.Type) (reflect.Value, error) {
	if value.kind != ArrayKind {
		return reflect.Value{}, fmt.Errorf("argument is %s, want an array", value.Type())
	}
	out := reflect.MakeSlice(typ, value.length(), value.length())
	for i := 0; i < value.length(); i++ {
		item, err := intoGo(registry, value.at(i), typ.Elem())
		if err != nil {
			return reflect.Value{}, fmt.Errorf("item %d: %w", i, err)
		}
		out.Index(i).Set(item)
	}
	return out, nil
}

func intoMap(registry *Registry, value Value, typ reflect.Type) (reflect.Value, error) {
	if value.kind != DictKind {
		return reflect.Value{}, fmt.Errorf("argument is %s, want a dictionary", value.Type())
	}
	out := reflect.MakeMapWithSize(typ, value.length())
	for _, key := range value.keys() {
		entry, _ := value.lookup(key)
		item, err := intoGo(registry, entry, typ.Elem())
		if err != nil {
			return reflect.Value{}, fmt.Errorf("entry %q: %w", key, err)
		}
		out.SetMapIndex(reflect.ValueOf(key), item)
	}
	return out, nil
}

// converterOutOf builds the Go → Value conversion for a result type.
func converterOutOf(registry *Registry, result Type) func(reflect.Value) (Value, error) {
	if result.Kind == HandleKind {
		return func(value reflect.Value) (Value, error) { return NewHandle(result.Name, value.Interface()), nil }
	}
	return func(value reflect.Value) (Value, error) { return outOfGo(registry, value, result) }
}

// outOfGo converts a Go value into a Value of the known FunRoute type. An
// exact native container is wrapped through fromGo; anything else is walked.
func outOfGo(registry *Registry, value reflect.Value, typ Type) (Value, error) {
	switch typ.Kind {
	case BoolKind:
		return Bool(value.Bool()), nil
	case IntKind:
		return outOfInt(value)
	case FloatKind:
		return CheckedFloat(value.Float())
	case StringKind:
		return String(value.String()), nil
	case HandleKind:
		return NewHandle(typ.Name, value.Interface()), nil
	case ArrayKind:
		return outOfSlice(registry, value, typ)
	case RecordKind:
		return outOfStruct(registry, value, typ)
	default:
		return outOfMap(registry, value, typ)
	}
}

func outOfInt(value reflect.Value) (Value, error) {
	if value.CanUint() {
		if value.Uint() > math.MaxInt64 {
			return Value{}, fmt.Errorf("result %d does not fit int", value.Uint())
		}
		return Int(int64(value.Uint())), nil
	}
	return Int(value.Int()), nil
}

func outOfSlice(registry *Registry, value reflect.Value, typ Type) (Value, error) {
	if wrapped, err := fromGo(value.Interface()); err == nil && wrapped.hasType(typ) {
		return wrapped, nil
	}
	builder := newArrayBuilder(*typ.Elem, value.Len())
	for i := 0; i < value.Len(); i++ {
		item, err := outOfGo(registry, value.Index(i), *typ.Elem)
		if err != nil {
			return Value{}, fmt.Errorf("item %d: %w", i, err)
		}
		builder.add(item)
	}
	return builder.finish(), nil
}

func outOfMap(registry *Registry, value reflect.Value, typ Type) (Value, error) {
	if wrapped, err := fromGo(value.Interface()); err == nil && wrapped.hasType(typ) {
		return wrapped, nil
	}
	entries := make(map[string]Value, value.Len())
	iter := value.MapRange()
	for iter.Next() {
		item, err := outOfGo(registry, iter.Value(), *typ.Elem)
		if err != nil {
			return Value{}, fmt.Errorf("entry %q: %w", iter.Key().String(), err)
		}
		entries[iter.Key().String()] = item
	}
	return packDict(*typ.Elem, entries), nil
}
