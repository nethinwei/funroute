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
)

// native is which Value backing a shapeNative codec hands over.
type native uint8

const (
	nativeBools native = iota
	nativeInts
	nativeFloats
	nativeStrings
	nativeBoolMap
	nativeIntMap
	nativeFloatMap
	nativeStringMap
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
}

// newCodecFor plans how a Go type carries want. A Go type that cannot is
// refused here, before anything runs, naming what did not match.
func newCodecFor(registry *Registry, typ reflect.Type, want Type) (*codec, error) {
	c := &codec{goType: typ, typ: want, registry: registry}
	if name, ok := registry.handleName(typ); ok {
		if want.Kind != HandleKind || want.Name != name {
			return nil, c.mismatch()
		}
		c.shape = shapeBoxed
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
	enum := c.typ.Kind == EnumKind && derived.Kind == StringKind
	if !enum && !derived.Equal(c.typ) {
		return c.mismatch()
	}
	c.shape, c.goKind = shapeScalar, c.goType.Kind()
	return nil
}

// planRecord finds each field the record declares among the struct's tagged
// fields, by name, and lays the codec out in the record's order.
func (c *codec) planRecord(registry *Registry) error {
	if c.typ.Kind != RecordKind {
		return c.mismatch()
	}
	declared, indexes, err := structFields(registry, c.goType)
	if err != nil {
		return err
	}
	c.shape = shapeRecord
	c.fields = make([]fieldCodec, len(c.typ.Fields))
	for i, wanted := range c.typ.Fields {
		position := slices.IndexFunc(declared, func(field Field) bool { return field.Name == wanted.Name })
		if position < 0 {
			return fmt.Errorf("%s has no field %q for %s", c.goType, wanted.Name, c.typ.Summary())
		}
		field := c.goType.Field(indexes[position])
		plan, err := newCodecFor(registry, field.Type, wanted.Type)
		if err != nil {
			return fmt.Errorf("field %q: %w", wanted.Name, err)
		}
		c.fields[i] = fieldCodec{offset: field.Offset, codec: plan}
	}
	return nil
}

func (c *codec) planSlice(registry *Registry) error {
	if c.typ.Kind != ArrayKind {
		return c.mismatch()
	}
	if kind, ok := nativeSlice(c.goType); ok && nativeCarries(registry, c.goType, c.typ) {
		c.shape, c.native = shapeNative, kind
		return nil
	}
	elem, err := newCodecFor(registry, c.goType.Elem(), *c.typ.Elem)
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
	if c.typ.Kind != DictKind || c.goType.Key().Kind() != reflect.String {
		return c.mismatch()
	}
	if kind, ok := nativeMap(c.goType); ok && nativeCarries(registry, c.goType, c.typ) {
		c.shape, c.native = shapeNative, kind
		return nil
	}
	if _, err := newCodecFor(registry, c.goType.Elem(), *c.typ.Elem); err != nil {
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

var (
	boolGo    = reflect.TypeFor[bool]()
	int64Go   = reflect.TypeFor[int64]()
	float64Go = reflect.TypeFor[float64]()
	stringGo  = reflect.TypeFor[string]()
)

// nativeSlice reports whether a slice has the memory layout of a Value
// backing: its elements are exactly bool, int64, float64 or string. A named
// slice type (type Vector []float64) has the same header and qualifies.
func nativeSlice(typ reflect.Type) (native, bool) {
	switch typ.Elem() {
	case boolGo:
		return nativeBools, true
	case int64Go:
		return nativeInts, true
	case float64Go:
		return nativeFloats, true
	case stringGo:
		return nativeStrings, true
	default:
		return 0, false
	}
}

// nativeMap is nativeSlice for maps. The key must be exactly string: a map is
// hashed by its key type, so a named key type is a different map.
func nativeMap(typ reflect.Type) (native, bool) {
	if typ.Key() != stringGo {
		return 0, false
	}
	kind, ok := nativeSlice(typ)
	return kind + nativeBoolMap, ok
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
		position := slices.IndexFunc(declared, func(field Field) bool { return field.Name == param.Name })
		if position < 0 {
			return nil, fmt.Errorf("argument %q: %s has no such field", param.Name, typ)
		}
		field := typ.Field(indexes[position])
		plan, err := newCodecFor(registry, field.Type, param.Type)
		if err != nil {
			return nil, fmt.Errorf("argument %q: %w", param.Name, err)
		}
		fields[i] = fieldCodec{offset: field.Offset, codec: plan}
	}
	return &argsCodec{params: params, fields: fields}, nil
}
