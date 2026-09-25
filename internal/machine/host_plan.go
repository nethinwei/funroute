package machine

import (
	"fmt"
	"reflect"
	"slices"
)

// A codec is how one Go type crosses into a Value of one FunRoute type and
// back, worked out once when a Program is instantiated instead of on every
// call. It is planned against the type the artifact declares, not the type the
// Go side would imply, and matched by name the way the Run(map) boundary is: a
// struct may carry more fields than the record asks for, and an argument
// struct more fields than the program takes. Types themselves are not
// stretched — an int field does not become a float — with one exception: a Go
// string carries an enum, checked for membership where it enters. A codec only
// adds where things sit in memory: field offsets and element sizes.
// access.go reads and writes by them.

// shape is what a codec does with the memory it is pointed at.
type shape uint8

const (
	shapeScalar shape = iota // bool, the int and float kinds, string
	shapeNative              // a slice or map that already is a Value backing
	shapeRecord              // a struct
	shapeSlice               // any other slice, walked element by element
	shapeBoxed               // a handle or any other map, through reflection
	shapeMoney               // money.Money, money.Ratio, money.FxRate or money.Currency
)

// native is which Value backing a shapeNative codec hands over.
type native uint8

const (
	nativeBools native = iota
	nativeInts
	nativeFloats
	nativeStrings
	nativeMonies
	nativeFxRates
	nativeBoolMap
	nativeIntMap
	nativeFloatMap
	nativeStringMap
	nativeMoneyMap
)

type codec struct {
	shape  shape
	goKind reflect.Kind // shapeScalar: which scalar
	native native       // shapeNative: which backing
	goType reflect.Type
	// typ is the FunRoute type. A record built by this codec shares it rather
	// than cloning it per value; nothing writes to a Type after it is built.
	typ      Type
	fields   []fieldCodec // shapeRecord, in the record's field order
	elem     *codec       // shapeSlice
	stride   uintptr      // shapeSlice: the element size
	registry *Registry    // shapeBoxed: the handles a nested type may name
}

type fieldCodec struct {
	offset uintptr
	codec  *codec
	// plain is the Go kind of a plain bool, int, float or string field — no
	// enum to hold it to — which is loaded in place, without the codec's
	// call; reflect.Invalid for any other.
	plain reflect.Kind
}

// newFieldCodec is the plan of a field at offset.
func newFieldCodec(offset uintptr, plan *codec) fieldCodec {
	field := fieldCodec{offset: offset, codec: plan}
	if plan.shape == shapeScalar && plan.typ.kind != EnumKind {
		switch kind := plan.goKind; kind {
		case reflect.Bool, reflect.Int, reflect.Int64, reflect.Float64, reflect.String:
			field.plain = kind
		}
	}
	return field
}

// newCodecFor plans how a Go type carries want. A Go type that cannot is
// refused here, before anything runs, naming what did not match.
func newCodecFor(registry *Registry, typ reflect.Type, want Type) (*codec, error) {
	c := &codec{goType: typ, typ: want, registry: registry}
	if name, ok := registry.handleName(typ); ok {
		if want.kind != HandleKind || want.name != name {
			return nil, c.mismatch()
		}
		c.shape = shapeBoxed
		return c, nil
	}
	if amount, ok := moneyGoKind(typ); ok {
		if amount.kind != want.kind {
			return nil, c.mismatch()
		}
		c.shape = shapeMoney
		return c, nil
	}
	switch typ.Kind() {
	case reflect.Struct:
		return c, c.planRecord(registry)
	case reflect.Slice:
		return c, c.planSlice(registry)
	case reflect.Map:
		return c, c.planMap(registry)
	default:
		return c, c.planScalar(registry)
	}
}

func (c *codec) mismatch() error {
	return fmt.Errorf("type %s cannot carry %s", c.goType, c.typ.Summary())
}

// planScalar accepts the scalar reflectType gives the Go type, or an enum
// carried by a string.
func (c *codec) planScalar(registry *Registry) error {
	derived, err := reflectType(registry, c.goType)
	if err != nil {
		return err
	}
	enum := c.typ.kind == EnumKind && derived.kind == StringKind
	if !enum && !derived.Equal(c.typ) {
		return c.mismatch()
	}
	c.shape, c.goKind = shapeScalar, c.goType.Kind()
	return nil
}

// planRecord finds each field the record declares among the struct's tagged
// fields, by name, and lays the codec out in the record's order.
func (c *codec) planRecord(registry *Registry) error {
	if c.typ.kind != RecordKind {
		return c.mismatch()
	}
	declared, indexes, err := structFields(registry, c.goType)
	if err != nil {
		return err
	}
	c.shape = shapeRecord
	c.fields = make([]fieldCodec, len(c.typ.fields))
	for i, wanted := range c.typ.fields {
		field, err := structFieldFor(c.goType, declared, indexes, c.typ, wanted.name)
		if err != nil {
			return err
		}
		plan, err := newCodecFor(registry, field.Type, wanted.typ)
		if err != nil {
			return fmt.Errorf("field %q: %w", wanted.name, err)
		}
		c.fields[i] = newFieldCodec(field.Offset, plan)
	}
	return nil
}

func (c *codec) planSlice(registry *Registry) error {
	if c.typ.kind != ArrayKind {
		return c.mismatch()
	}
	if kind, ok := nativeSlice(c.goType); ok && nativeCarries(registry, c.goType, c.typ) {
		c.shape, c.native = shapeNative, kind
		return nil
	}
	elem, err := newCodecFor(registry, c.goType.Elem(), *c.typ.elem)
	if err != nil {
		return err
	}
	c.shape, c.elem, c.stride = shapeSlice, elem, c.goType.Elem().Size()
	return nil
}

// planMap hands over an exact native backing and otherwise goes through
// reflection, whose outOfGo and intoGo already match struct fields by name.
// The element is still planned, so a mismatch surfaces now and not per call.
func (c *codec) planMap(registry *Registry) error {
	if c.typ.kind != DictKind || c.goType.Key().Kind() != reflect.String {
		return c.mismatch()
	}
	if kind, ok := nativeMap(c.goType); ok && nativeCarries(registry, c.goType, c.typ) {
		c.shape, c.native = shapeNative, kind
		return nil
	}
	if _, err := newCodecFor(registry, c.goType.Elem(), *c.typ.elem); err != nil {
		return err
	}
	c.shape = shapeBoxed
	return nil
}

// nativeCarries reports whether a native backing is exactly the container
// wanted: an array<enum> held in a []string still has its members checked.
func nativeCarries(registry *Registry, typ reflect.Type, want Type) bool {
	derived, err := reflectType(registry, typ)
	return err == nil && derived.Equal(want)
}

// nativeSlice reports whether a slice has the memory layout of a Value
// backing: its elements are exactly one of natives' types. A named slice type
// (type Vector []float64) has the same header and qualifies.
func nativeSlice(typ reflect.Type) (native, bool) {
	i := slices.IndexFunc(natives, func(entry nativeBacking) bool { return entry.goElem == typ.Elem() })
	return native(i), i >= 0
}

// nativeMap is nativeSlice for maps. The key must be exactly string: a map is
// hashed by its key type, so a named key type is a different map.
func nativeMap(typ reflect.Type) (native, bool) {
	kind, ok := nativeSlice(typ)
	if !ok || typ.Key() != reflect.TypeFor[string]() || !natives[kind].dict {
		return 0, false
	}
	return kind + nativeBoolMap, true
}

// argsCodec plans the arguments an artifact takes, in its order, each found
// by name among the argument struct's tagged fields.
type argsCodec struct {
	params []Parameter
	fields []fieldCodec
}

func newArgsCodec(registry *Registry, typ reflect.Type, params []Parameter) (*argsCodec, error) {
	declared, indexes, err := taggedFields(registry, typ)
	if err != nil {
		return nil, err
	}
	fields := make([]fieldCodec, len(params))
	for i, param := range params {
		position := slices.IndexFunc(declared, func(field Field) bool { return field.name == param.name })
		if position < 0 {
			return nil, fmt.Errorf("argument %q: %s has no such field", param.name, typ)
		}
		field := typ.Field(indexes[position])
		plan, err := newCodecFor(registry, field.Type, param.typ)
		if err != nil {
			return nil, fmt.Errorf("argument %q: %w", param.name, err)
		}
		fields[i] = newFieldCodec(field.Offset, plan)
	}
	return &argsCodec{params: params, fields: fields}, nil
}
