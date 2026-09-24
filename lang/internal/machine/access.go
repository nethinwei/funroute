package machine

import (
	"fmt"
	"math"
	"reflect"
	"slices"
	"unsafe"
)

// This file is the only one that uses unsafe. A Program reads its arguments
// straight out of the host's struct and writes its result straight into the
// host's Go value, at offsets plan.go worked out with reflection when it was
// instantiated — so the hot path neither reflects, nor looks a name up, nor builds a
// map or a []Value in between.
//
// Every read and write is typed: *(*int64)(p) where the plan says the field is
// an int64, never a byte copy, so the garbage collector sees each pointer it
// should. The codecs dispatch with a switch rather than through func values,
// because an indirect call makes the compiler assume the pointer escapes, and
// then every host struct passed to Run would be moved to the heap. The two
// places that must reflect — a handle's payload and a map that is not a
// native backing — go through noescape, and take a copy of the field before
// anything else sees it, so nothing built here points back at the host's
// struct.

// sliceHeader is the layout of every Go slice.
type sliceHeader struct {
	data unsafe.Pointer
	len  int
	cap  int
}

// encodeArgs fills a program's argument slots from the host's struct. Only
// the arguments the bytecode reads are loaded; the others stay zero, and the
// program never looks at them.
func encodeArgs[In any](plan *argsCodec, reads []int, args []Value, in *In) error {
	base := unsafe.Pointer(in)
	for _, i := range reads {
		field := plan.fields[i]
		value, err := field.codec.load(unsafe.Add(base, field.offset))
		if err != nil {
			return fmt.Errorf("argument %q: %w", plan.params[i].name, err)
		}
		args[i] = value
	}
	return nil
}

// decodeResult writes a program's result into the host's Go value. A
// container's backing is handed over as it is and is read-only from then on.
func decodeResult[Out any](plan *codec, value Value, out *Out) error {
	return plan.store(unsafe.Pointer(out), value)
}

// load reads the Go value at p as a Value. A container keeps its backing; a
// float is checked, because this is where the value enters the language.
func (c *codec) load(p unsafe.Pointer) (Value, error) {
	switch c.shape {
	case shapeScalar:
		return c.loadScalar(p)
	case shapeNative:
		return loadNative(c.native, p)
	case shapeRecord:
		return c.loadRecord(p)
	case shapeSlice:
		return c.loadSlice(p)
	case shapeMoney:
		return loadMoney(c.goType, p), nil
	default:
		return c.loadBoxed(p)
	}
}

// loadMoney reads a money Go type. An exchange rate boxes its quote.
func loadMoney(typ reflect.Type, p unsafe.Pointer) Value {
	switch typ {
	case moneyGoType:
		money := (*Money)(p)
		return MoneyValue(money.minor, money.currency)
	case rateGoType:
		return RateValue(*(*Rate)(p))
	case fxRateGoType:
		return FxRateValue(*(*FxRate)(p))
	default:
		return CurrencyValue((*(*Currency)(p)).code)
	}
}

// storeMoney writes a money Go type.
func storeMoney(typ reflect.Type, p unsafe.Pointer, v Value) error {
	if want, _ := moneyGoKind(typ); v.kind != want.kind {
		return fmt.Errorf("value is %s, want %s", v.Type().Summary(), want)
	}
	switch typ {
	case moneyGoType:
		*(*Money)(p) = Money{currency: v.s, minor: v.i}
	case rateGoType:
		*(*Rate)(p) = newRate(v.i)
	case fxRateGoType:
		*(*FxRate)(p), _ = v.FxRate()
	default:
		*(*Currency)(p) = Currency{code: v.s}
	}
	return nil
}

// loadScalar also holds a string to the enum it carries: a member name is the
// enum's value, anything else is not that enum.
func (c *codec) loadScalar(p unsafe.Pointer) (Value, error) {
	value, err := loadScalar(c.goKind, p)
	if err == nil && c.typ.kind == EnumKind && !slices.Contains(c.typ.values, value.s) {
		return Value{}, fmt.Errorf("%q is not a member of %s", value.s, c.typ.Summary())
	}
	return value, err
}

func loadScalar(kind reflect.Kind, p unsafe.Pointer) (Value, error) {
	switch kind {
	case reflect.Bool:
		return Bool(*(*bool)(p)), nil
	case reflect.String:
		return String(*(*string)(p)), nil
	case reflect.Float64:
		return CheckedFloat(*(*float64)(p))
	case reflect.Float32:
		return CheckedFloat(float64(*(*float32)(p)))
	default:
		return loadInt(kind, p)
	}
}

func loadInt(kind reflect.Kind, p unsafe.Pointer) (Value, error) {
	switch kind {
	case reflect.Int:
		return Int(int64(*(*int)(p))), nil
	case reflect.Int8:
		return Int(int64(*(*int8)(p))), nil
	case reflect.Int16:
		return Int(int64(*(*int16)(p))), nil
	case reflect.Int32:
		return Int(int64(*(*int32)(p))), nil
	case reflect.Uint:
		return unsignedInt(uint64(*(*uint)(p)))
	case reflect.Uint8:
		return Int(int64(*(*uint8)(p))), nil
	case reflect.Uint16:
		return Int(int64(*(*uint16)(p))), nil
	case reflect.Uint32:
		return Int(int64(*(*uint32)(p))), nil
	default:
		return Int(*(*int64)(p)), nil
	}
}

// loadNative wraps a slice or map that already is a Value backing. Boxing its
// header is the one allocation; the elements are neither copied nor read,
// except that floats are checked.
func loadNative(kind native, p unsafe.Pointer) (Value, error) {
	switch kind {
	case nativeBools:
		return loadBacking[[]bool](ArrayKind, p), nil
	case nativeInts:
		return loadBacking[[]int64](ArrayKind, p), nil
	case nativeFloats:
		return loadBacking[[]float64](ArrayKind, p), checkFloats(*(*[]float64)(p))
	case nativeStrings:
		return loadBacking[[]string](ArrayKind, p), nil
	case nativeMonies:
		return loadBacking[[]Money](ArrayKind, p), nil
	case nativeMoneyMap:
		return loadBacking[map[string]Money](DictKind, p), nil
	case nativeBoolMap:
		return loadBacking[map[string]bool](DictKind, p), nil
	case nativeIntMap:
		return loadBacking[map[string]int64](DictKind, p), nil
	case nativeFloatMap:
		return loadBacking[map[string]float64](DictKind, p), checkFloatMap(*(*map[string]float64)(p))
	default:
		return loadBacking[map[string]string](DictKind, p), nil
	}
}

func loadBacking[T any](kind Kind, p unsafe.Pointer) Value {
	return Value{kind: kind, box: *(*T)(p)}
}

// loadRecord builds the record directly: the plan's type is shared, and the
// fields are already known to have it, so Record's clone and checks would only
// repeat what planning established.
func (c *codec) loadRecord(p unsafe.Pointer) (Value, error) {
	fields := make([]Value, len(c.fields))
	for i, field := range c.fields {
		value, err := field.codec.load(unsafe.Add(p, field.offset))
		if err != nil {
			return Value{}, fmt.Errorf("field %q: %w", c.typ.fields[i].name, err)
		}
		fields[i] = value
	}
	return Value{kind: RecordKind, box: &recordValue{typ: c.typ, fields: fields}}, nil
}

func (c *codec) loadSlice(p unsafe.Pointer) (Value, error) {
	header := (*sliceHeader)(p)
	builder := newArrayBuilder(*c.typ.elem, header.len)
	for i := 0; i < header.len; i++ {
		item, err := c.elem.load(unsafe.Add(header.data, uintptr(i)*c.stride))
		if err != nil {
			return Value{}, fmt.Errorf("item %d: %w", i, err)
		}
		builder.add(item)
	}
	return builder.finish(), nil
}

func (c *codec) loadBoxed(p unsafe.Pointer) (Value, error) {
	boxed := c.box(p)
	if c.typ.kind == HandleKind {
		return NewHandle(c.typ.name, boxed), nil
	}
	return outOfGo(c.registry, reflect.ValueOf(boxed), c.typ)
}

// box copies the field at p into an interface. What comes back never refers
// to the field itself, but it does hold what the field points to — a map, a
// handle's payload — and that goes on into Values that outlive the call. So
// the compiler must treat *p's contents as escaping, and noescape would hide
// that along with p. leakContents says it again, on a branch that never runs:
// box carries its own "leaking param content" rather than relying on load's
// other branches to have it.
func (c *codec) box(p unsafe.Pointer) any {
	leakContents(p)
	return reflect.NewAt(c.goType, noescape(p)).Elem().Interface()
}

// contentSink and neverTrue exist for leakContents: the store is never made,
// but the compiler cannot know that, so what p points to escapes to the heap.
var (
	//lint:ignore U1000 written only on the branch that never runs; the write is the point, see leakContents
	contentSink unsafe.Pointer
	neverTrue   bool
)

func leakContents(p unsafe.Pointer) {
	if neverTrue {
		contentSink = *(*unsafe.Pointer)(p)
	}
}

// store writes a Value into the Go value at p. The codec was planned against
// the artifact's result type, so a record's fields arrive in the order the
// codec lists them and are written by index; struct fields the record does
// not have keep their zero value.
func (c *codec) store(p unsafe.Pointer, v Value) error {
	switch c.shape {
	case shapeScalar:
		return storeScalar(c.goKind, p, v)
	case shapeNative:
		return storeNative(c.native, p, v)
	case shapeRecord:
		return c.storeRecord(p, v)
	case shapeSlice:
		return c.storeSlice(p, v)
	case shapeMoney:
		return storeMoney(c.goType, p, v)
	default:
		return c.storeBoxed(p, v)
	}
}

func storeScalar(kind reflect.Kind, p unsafe.Pointer, v Value) error {
	switch kind {
	case reflect.Bool:
		*(*bool)(p) = v.b
	case reflect.String:
		*(*string)(p) = v.s
	case reflect.Float64:
		*(*float64)(p) = v.f
	case reflect.Float32:
		if math.Abs(v.f) > math.MaxFloat32 {
			return fmt.Errorf("%v does not fit float32", v.f)
		}
		*(*float32)(p) = float32(v.f)
	default:
		return storeInt(kind, p, v.i)
	}
	return nil
}

func storeInt(kind reflect.Kind, p unsafe.Pointer, i int64) error {
	if !intFits(kind, i) {
		return fmt.Errorf("%d does not fit %s", i, kind)
	}
	switch kind {
	case reflect.Int:
		*(*int)(p) = int(i)
	case reflect.Int8:
		*(*int8)(p) = int8(i)
	case reflect.Int16:
		*(*int16)(p) = int16(i)
	case reflect.Int32:
		*(*int32)(p) = int32(i)
	case reflect.Uint:
		*(*uint)(p) = uint(i)
	case reflect.Uint8:
		*(*uint8)(p) = uint8(i)
	case reflect.Uint16:
		*(*uint16)(p) = uint16(i)
	case reflect.Uint32:
		*(*uint32)(p) = uint32(i)
	default:
		*(*int64)(p) = i
	}
	return nil
}

func intFits(kind reflect.Kind, i int64) bool {
	switch kind {
	case reflect.Int:
		return i >= math.MinInt && i <= math.MaxInt
	case reflect.Int8:
		return i >= math.MinInt8 && i <= math.MaxInt8
	case reflect.Int16:
		return i >= math.MinInt16 && i <= math.MaxInt16
	case reflect.Int32:
		return i >= math.MinInt32 && i <= math.MaxInt32
	case reflect.Uint:
		return i >= 0 && uint64(i) <= math.MaxUint
	case reflect.Uint8:
		return i >= 0 && i <= math.MaxUint8
	case reflect.Uint16:
		return i >= 0 && i <= math.MaxUint16
	case reflect.Uint32:
		return i >= 0 && i <= math.MaxUint32
	default:
		return true
	}
}

func storeNative(kind native, p unsafe.Pointer, v Value) error {
	switch kind {
	case nativeBools:
		return storeBacking[[]bool](p, v)
	case nativeInts:
		return storeBacking[[]int64](p, v)
	case nativeFloats:
		return storeBacking[[]float64](p, v)
	case nativeStrings:
		return storeBacking[[]string](p, v)
	case nativeMonies:
		return storeBacking[[]Money](p, v)
	case nativeMoneyMap:
		return storeBacking[map[string]Money](p, v)
	case nativeBoolMap:
		return storeBacking[map[string]bool](p, v)
	case nativeIntMap:
		return storeBacking[map[string]int64](p, v)
	case nativeFloatMap:
		return storeBacking[map[string]float64](p, v)
	default:
		return storeBacking[map[string]string](p, v)
	}
}

func storeBacking[T any](p unsafe.Pointer, v Value) error {
	backing, ok := v.box.(T)
	if !ok {
		return fmt.Errorf("value is %s, want %T", v.Type().Summary(), backing)
	}
	*(*T)(p) = backing
	return nil
}

func (c *codec) storeRecord(p unsafe.Pointer, v Value) error {
	record, ok := v.box.(*recordValue)
	if !ok || len(record.fields) != len(c.fields) {
		return fmt.Errorf("value is %s, want %s", v.Type().Summary(), c.typ.Summary())
	}
	for i, field := range c.fields {
		if err := field.codec.store(unsafe.Add(p, field.offset), record.fields[i]); err != nil {
			return fmt.Errorf("field %q: %w", c.typ.fields[i].name, err)
		}
	}
	return nil
}

func (c *codec) storeSlice(p unsafe.Pointer, v Value) error {
	if v.kind != ArrayKind {
		return fmt.Errorf("value is %s, want an array", v.Type().Summary())
	}
	n := v.length()
	items := reflect.MakeSlice(c.goType, n, n)
	data := items.UnsafePointer()
	for i := range n {
		if err := c.elem.store(unsafe.Add(data, uintptr(i)*c.stride), v.at(i)); err != nil {
			return fmt.Errorf("item %d: %w", i, err)
		}
	}
	reflect.NewAt(c.goType, noescape(p)).Elem().Set(items)
	return nil
}

func (c *codec) storeBoxed(p unsafe.Pointer, v Value) error {
	converted, err := c.intoGo(v)
	if err != nil {
		return err
	}
	reflect.NewAt(c.goType, noescape(p)).Elem().Set(converted)
	return nil
}

func (c *codec) intoGo(v Value) (reflect.Value, error) {
	if c.typ.kind != HandleKind {
		return intoGo(c.registry, v, c.goType)
	}
	payload := reflect.ValueOf(v.box)
	if v.kind != HandleKind || !payload.IsValid() || !payload.Type().AssignableTo(c.goType) {
		return reflect.Value{}, fmt.Errorf("value is %s, want handle of %s", v.Type(), c.goType)
	}
	return payload, nil
}

// noescape hides p from escape analysis, the way the runtime does before
// handing a pointer to code the compiler cannot see through. It is sound here
// because p itself is only used while the caller's struct is alive — for the
// duration of one Run — and is never retained: box copies the field out (and
// declares what the field points to as escaping, see leakContents), and a
// store only writes into it.
//
//go:nosplit
func noescape(p unsafe.Pointer) unsafe.Pointer {
	address := uintptr(p)
	return *(*unsafe.Pointer)(unsafe.Pointer(&address))
}
