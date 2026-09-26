package machine

import (
	"context"
	"fmt"
	"math"
	"reflect"
	"slices"
	"unsafe"

	"github.com/nethinwei/funroute/internal/kit"
	"github.com/nethinwei/funroute/internal/money"
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

// argRead is one argument a program reads from the host's struct: its
// slot, where it is in the struct and how to load it. A plain field has its
// Go kind here (fieldCodec.plain) and is loaded in place.
type argRead struct {
	index     int
	offset    uintptr
	kind      reflect.Kind
	codec     *codec
	fieldOnly bool
	view      bool
	// records is an array of plain records handed over as a recordsView,
	// in the frame's own slot when inPlace: the program only walks it.
	records, inPlace bool
	// promoted is the record's promoted fields, which a run with a frame
	// loads into their registers in place of the record.
	promoted []promotion
}

func newArgRead(index int, plan *argsCodec, fieldOnly, viewOnly bool) argRead {
	field := plan.fields[index]
	read := argRead{index: index, offset: field.offset, kind: field.plain, codec: field.codec, fieldOnly: fieldOnly && field.codec.shape == shapeRecord}
	if viewOnly && field.codec.shape == shapeNative {
		switch field.codec.native {
		case nativeBools, nativeInts, nativeFloats, nativeStrings:
			read.view = true
		}
	}
	read.records = viewable(field.codec)
	read.inPlace = viewOnly && read.records
	return read
}

// encodeArgs fills a program's argument slots from the host's struct. Only
// the arguments the bytecode reads are loaded; the others stay zero, and the
// program never looks at them.
//
// With a frame, a record argument the program only reads field by field is
// loaded into the frame's own record, and an array it only walks, measures
// or indexes into the frame's own slot — the slice's header, not its items.
func encodeArgs[In any](plan *argsCodec, reads []argRead, args []Value, in *In, f *frame) error {
	base := unsafe.Pointer(in)
	for _, read := range reads {
		p := unsafe.Add(base, read.offset)
		if loadPlain(read.kind, p, &args[read.index]) {
			continue
		}
		if f != nil && read.promoted != nil {
			if err := read.codec.loadPromoted(p, read.promoted, f.regs); err != nil {
				return fmt.Errorf("argument %q: %w", plan.params[read.index].name, err)
			}
			continue
		}
		value, err := read.load(p, f)
		if err != nil {
			return fmt.Errorf("argument %q: %w", plan.params[read.index].name, err)
		}
		args[read.index] = value
	}
	return nil
}

// load reads the argument at p: into the frame's record when the program
// only reads its fields.
// Nothing in the frame points into the host's struct — it may be on the
// host's stack — only at what its fields point at.
func (read argRead) load(p unsafe.Pointer, f *frame) (Value, error) {
	switch {
	case f == nil:
	case read.fieldOnly:
		f.borrows = append(f.borrows, read.index)
		return read.codec.loadRecordInto(p, &f.records[read.index])
	case read.view:
		f.borrows = append(f.borrows, read.index)
		return viewIn(&f.views[read.index], read.codec.native, p), nil
	case read.inPlace:
		f.borrows = append(f.borrows, read.index)
		return viewRecords(&f.recordViews[read.index], read.codec, p, true), nil
	case read.records:
		return viewRecords(nil, read.codec, p, false), nil
	}
	return read.codec.load(p)
}

// viewIn copies the host's slice at p into slot and is the array there.
func viewIn(slot *arenaSlot, kind native, p unsafe.Pointer) Value {
	switch kind {
	case nativeBools:
		slot.bools = *(*[]bool)(p)
		return Value{kind: ArrayKind, box: &slot.bools}
	case nativeInts:
		slot.ints = *(*[]int64)(p)
		return Value{kind: ArrayKind, box: &slot.ints}
	case nativeFloats:
		slot.floats = *(*[]float64)(p)
		return Value{kind: ArrayKind, box: &slot.floats}
	}
	slot.strings = *(*[]string)(p)
	return Value{kind: ArrayKind, box: &slot.strings}
}

// loadPlain loads a plain field of kind at p into slot, and reports whether
// it did.
func loadPlain(kind reflect.Kind, p unsafe.Pointer, slot *Value) bool {
	switch kind {
	case reflect.Int64:
		*slot = Value{kind: IntKind, i: *(*int64)(p)}
	case reflect.Int:
		*slot = Value{kind: IntKind, i: int64(*(*int)(p))}
	case reflect.Bool:
		*slot = Value{kind: BoolKind, b: *(*bool)(p)}
	case reflect.String:
		*slot = Value{kind: StringKind, s: *(*string)(p)}
	case reflect.Float64:
		*slot = Value{kind: FloatKind, f: *(*float64)(p)}
	default:
		return false
	}
	return true
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
		amount := (*money.Money)(p)
		return MoneyValue(amount.Minor(), amount.Currency())
	case ratioGoType:
		return RatioValue(*(*money.Ratio)(p))
	case fxRateGoType:
		return FxRateValue(*(*money.FxRate)(p))
	default:
		return CurrencyValue((*money.Currency)(p).Code())
	}
}

// storeMoney writes a money Go type.
func storeMoney(typ reflect.Type, p unsafe.Pointer, v Value) error {
	if want, _ := moneyGoKind(typ); v.kind != want.kind {
		return fmt.Errorf("value is %s, want %s", v.Type().Summary(), want)
	}
	switch typ {
	case moneyGoType:
		*(*money.Money)(p) = money.Make(v.s, v.i)
	case ratioGoType:
		*(*money.Ratio)(p) = ratioFrom(v)
	case fxRateGoType:
		*(*money.FxRate)(p), _ = v.FxRate()
	default:
		*(*money.Currency)(p) = money.CurrencyOf(v.s)
	}
	return nil
}

// newMoneyGo is a new money Go value of typ holding v, whose kind is typ's.
func newMoneyGo(typ reflect.Type, v Value) reflect.Value {
	out := reflect.New(typ)
	_ = storeMoney(typ, out.UnsafePointer(), v)
	return out.Elem()
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
		return Float(*(*float64)(p)), nil
	case reflect.Float32:
		return Float(float64(*(*float32)(p))), nil
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
// except that exchange rates are checked.
func loadNative(kind native, p unsafe.Pointer) (Value, error) {
	switch kind {
	case nativeBools:
		return loadBacking[[]bool](ArrayKind, p), nil
	case nativeInts:
		return loadBacking[[]int64](ArrayKind, p), nil
	case nativeFloats:
		return loadBacking[[]float64](ArrayKind, p), nil
	case nativeStrings:
		return loadBacking[[]string](ArrayKind, p), nil
	case nativeMonies:
		return loadBacking[[]money.Money](ArrayKind, p), nil
	case nativeFxRates:
		return loadBacking[[]money.FxRate](ArrayKind, p), checkFxRates(*(*[]money.FxRate)(p))
	case nativeMoneyMap:
		return loadBacking[map[string]money.Money](DictKind, p), nil
	case nativeBoolMap:
		return loadBacking[map[string]bool](DictKind, p), nil
	case nativeIntMap:
		return loadBacking[map[string]int64](DictKind, p), nil
	case nativeFloatMap:
		return loadBacking[map[string]float64](DictKind, p), nil
	case nativeStringMap:
		return loadBacking[map[string]string](DictKind, p), nil
	}
	panic(fmt.Sprintf("machine: native backing %d has no Go form", kind))
}

func loadBacking[T any](kind Kind, p unsafe.Pointer) Value {
	return Value{kind: kind, box: *(*T)(p)}
}

// loadRecord builds the record directly: the plan's type is shared, and the
// fields are already known to have it, so Record's clone and checks would only
// repeat what planning established.
func (c *codec) loadRecord(p unsafe.Pointer) (Value, error) {
	record := newRecord(&c.typ, len(c.fields))
	for i, field := range c.fields {
		value, err := field.codec.load(unsafe.Add(p, field.offset))
		if err != nil {
			return Value{}, fmt.Errorf("field %q: %w", c.typ.fields[i].name, err)
		}
		record.fields[i] = value
	}
	return Value{kind: RecordKind, box: record}, nil
}

func (c *codec) loadSlice(p unsafe.Pointer) (Value, error) {
	header := (*sliceHeader)(p)
	if c.elem.shape == shapeRecord {
		return c.loadRecords(header)
	}
	builder := newArrayBuilder(*c.typ.elem, header.len)
	for i := range header.len {
		item, err := c.elem.load(unsafe.Add(header.data, uintptr(i)*c.stride))
		if err != nil {
			return Value{}, fmt.Errorf("item %d: %w", i, err)
		}
		builder.add(item)
	}
	return builder.finish(), nil
}

// loadRecords loads a slice of records in one allocation for the records
// and one for their fields, not one a record. Each record is still its own,
// its fields capped at its share, so nothing written to one reaches the next;
// one kept alone keeps the others' memory with it.
func (c *codec) loadRecords(header *sliceHeader) (Value, error) {
	plan, width := c.elem, len(c.elem.fields)
	records, fields, items := make([]recordValue, header.len), make([]Value, header.len*width), make([]Value, header.len)
	for i := range header.len {
		p, record := unsafe.Add(header.data, uintptr(i)*c.stride), &records[i]
		record.typ, record.fields = &plan.typ, fields[i*width:(i+1)*width:(i+1)*width]
		for j, field := range plan.fields {
			value, err := field.codec.load(unsafe.Add(p, field.offset))
			if err != nil {
				return Value{}, fmt.Errorf("item %d: field %q: %w", i, plan.typ.fields[j].name, err)
			}
			record.fields[j] = value
		}
		items[i] = Value{kind: RecordKind, box: record}
	}
	return Value{kind: ArrayKind, box: &nestedArray{elem: *c.typ.elem, items: items}}, nil
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

// contentSink exists for leakContents and is never set: the store through it
// is never made, but the compiler cannot know that, so what p points to
// escapes to the heap.
var contentSink *unsafe.Pointer

func leakContents(p unsafe.Pointer) {
	if contentSink != nil {
		*contentSink = *(*unsafe.Pointer)(p)
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
	var fits bool
	switch kind {
	case reflect.Int:
		fits = put[int](p, i)
	case reflect.Int8:
		fits = put[int8](p, i)
	case reflect.Int16:
		fits = put[int16](p, i)
	case reflect.Int32:
		fits = put[int32](p, i)
	case reflect.Uint:
		fits = put[uint](p, i)
	case reflect.Uint8:
		fits = put[uint8](p, i)
	case reflect.Uint16:
		fits = put[uint16](p, i)
	case reflect.Uint32:
		fits = put[uint32](p, i)
	default:
		fits = put[int64](p, i)
	}
	if !fits {
		return fmt.Errorf("%d does not fit %s", i, kind)
	}
	return nil
}

// put writes i at p as a T when T holds it exactly, and reports whether it
// did: the conversion must come back to i with its sign.
func put[T int | int8 | int16 | int32 | int64 | uint | uint8 | uint16 | uint32](p unsafe.Pointer, i int64) bool {
	v := T(i)
	if int64(v) != i || (v < 0) != (i < 0) {
		return false
	}
	*(*T)(p) = v
	return true
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
		return storeBacking[[]money.Money](p, v)
	case nativeFxRates:
		return storeBacking[[]money.FxRate](p, v)
	case nativeMoneyMap:
		return storeBacking[map[string]money.Money](p, v)
	case nativeBoolMap:
		return storeBacking[map[string]bool](p, v)
	case nativeIntMap:
		return storeBacking[map[string]int64](p, v)
	case nativeFloatMap:
		return storeBacking[map[string]float64](p, v)
	case nativeStringMap:
		return storeBacking[map[string]string](p, v)
	}
	panic(fmt.Sprintf("machine: native backing %d has no Go form", kind))
}

func storeBacking[T any](p unsafe.Pointer, v Value) error {
	// An answer built in a frame's slot points at the slot's slice.
	if slot, ok := v.box.(*T); ok {
		*(*T)(p) = *slot
		return nil
	}
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
	if view, ok := v.box.(*recordsView); ok && storeView(c, p, view) {
		return nil
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

// The answer of a Program is written into the host's Out before the frame
// goes, which lets the run build it in place: a record answered in the
// frame's own record, an array in the memory of the slice the host already
// has there (Program.RunInto), and nothing boxed on the way. It is clang's
// return-value optimization, with the host's Out as the caller's slot.
//
// resultSink is where a run delivers its answer: a codec and the host's Out,
// and whether the slices already in Out may be built into.
type resultSink struct {
	plan  *codec
	out   unsafe.Pointer
	reuse bool
}

// placeOf is where a Program's answer goes.
func placeOf[Out any](out *Out) unsafe.Pointer { return noescape(unsafe.Pointer(out)) }

// runProgram runs the frame for a Program, delivering the answer by plan
// to out.
func (r *Runtime) runProgram(ctx context.Context, f *frame, args []Value, plan *codec, out unsafe.Pointer, reuse bool) error {
	_, err := r.runFrame(ctx, f, args, nil, resultSink{plan: plan, out: out, reuse: reuse})
	return err
}

// deliver writes the answer into the host's Out.
func (s resultSink) deliver(value Value) error {
	if err := s.plan.store(s.out, value); err != nil {
		return kit.Classify(ErrContract, "result: ", err)
	}
	return nil
}

// lend readies the frame's answer slots with the slices the host's Out
// holds — the answer itself, or the answer record's fields — when the host
// said they may be built into and none shares memory with an argument: an
// answer built over its own input would read what it had written.
func (s resultSink) lend(f *frame, args []Value) {
	for dest := range f.dest {
		p, plan := s.out, s.plan
		if dest > 0 {
			if plan.shape != shapeRecord || dest > len(plan.fields) {
				continue
			}
			field := plan.fields[dest-1]
			p, plan = unsafe.Add(p, field.offset), field.codec
		}
		if plan.shape == shapeNative {
			lendSlot(&f.dest[dest], plan.native, p, args)
		}
	}
}

// lendSlot takes the host's slice at p into slot, unless an argument's
// memory overlaps it.
func lendSlot(slot *arenaSlot, kind native, p unsafe.Pointer, args []Value) {
	header := (*sliceHeader)(p)
	if header.cap == 0 || overlapsAny(header, args) {
		return
	}
	switch kind {
	case nativeBools:
		slot.bools = (*(*[]bool)(p))[:0]
	case nativeInts:
		slot.ints = (*(*[]int64)(p))[:0]
	case nativeFloats:
		slot.floats = (*(*[]float64)(p))[:0]
	case nativeStrings:
		slot.strings = (*(*[]string)(p))[:0]
	}
}

// overlapsAny reports an argument, or a field of a record argument, whose
// native slice shares memory with the one header describes.
func overlapsAny(header *sliceHeader, args []Value) bool {
	meets := func(v Value) bool { return overlaps(header, v.box) }
	for _, arg := range args {
		if record, ok := arg.box.(*recordValue); ok && slices.ContainsFunc(record.fields, meets) || meets(arg) {
			return true
		}
	}
	return false
}

// overlaps reports a native slice in box whose memory meets header's.
func overlaps(header *sliceHeader, box any) bool {
	var other sliceHeader
	var size uintptr
	// An argument handed over in place points at the host's slice.
	box = unarena(Value{box: box}).box
	switch items := box.(type) {
	case []bool:
		other, size = headerOf(items), unsafe.Sizeof(false)
	case []int64:
		other, size = headerOf(items), unsafe.Sizeof(int64(0))
	case []float64:
		other, size = headerOf(items), unsafe.Sizeof(float64(0))
	case []string:
		other, size = headerOf(items), unsafe.Sizeof("")
	default:
		return false
	}
	if other.cap == 0 {
		return false
	}
	// Both are measured in the other's items: a slice of a different kind
	// never shares memory with an array of this one, so sizes only matter
	// for slices of one kind.
	lo, hi := uintptr(header.data), uintptr(header.data)+uintptr(header.cap)*size
	otherLo, otherHi := uintptr(other.data), uintptr(other.data)+uintptr(other.cap)*size
	return lo < otherHi && otherLo < hi
}

func headerOf[T any](items []T) sliceHeader {
	return sliceHeader{data: unsafe.Pointer(unsafe.SliceData(items)), len: len(items), cap: cap(items)}
}

// loadRecordInto loads a record argument into record, the frame's own: a
// program that only reads the record's fields keeps nothing of it past the
// run.
func (c *codec) loadRecordInto(p unsafe.Pointer, record *recordValue) (Value, error) {
	record.typ = &c.typ
	record.fields = slices.Grow(record.fields[:0], len(c.fields))[:len(c.fields)]
	for i := range c.fields {
		if err := c.loadField(p, i, &record.fields[i]); err != nil {
			return Value{}, err
		}
	}
	return Value{kind: RecordKind, box: record}, nil
}

// loadPromoted loads the promoted fields of the record at p into their
// registers.
func (c *codec) loadPromoted(p unsafe.Pointer, promoted []promotion, regs []Value) error {
	for _, field := range promoted {
		if err := c.loadField(p, int(field.field), &regs[field.reg]); err != nil {
			return err
		}
	}
	return nil
}

// loadField loads the record's field i, of the struct at p, into slot.
func (c *codec) loadField(p unsafe.Pointer, i int, slot *Value) error {
	field := c.fields[i]
	at := unsafe.Add(p, field.offset)
	if loadPlain(field.plain, at, slot) {
		return nil
	}
	value, err := field.codec.load(at)
	if err != nil {
		return fmt.Errorf("field %q: %w", c.typ.fields[i].name, err)
	}
	*slot = value
	return nil
}
