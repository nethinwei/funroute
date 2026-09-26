package machine

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"math"
	"slices"

	"github.com/nethinwei/funroute/internal/kit"
	"github.com/nethinwei/funroute/internal/money"
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

// recordValue backs a record: the fields in the type's order, plus the type
// itself, because a record's fields are what it is. A field access already
// knows its index at compile time, so nothing here is looked up by name. The
// type is shared — the artifact's, or a codec's — never written, and never
// copied per record.
type recordValue struct {
	typ    *Type
	fields []Value
}

// newRecord is a record of typ with n fields, all zero, for the caller to
// fill: for the sizes most records have, one allocation holds the fields
// and the record both.
func newRecord(typ *Type, n int) *recordValue {
	switch {
	case n <= 2:
		r := &struct {
			recordValue
			store [2]Value
		}{}
		r.recordValue = recordValue{typ: typ, fields: r.store[:n]}
		return &r.recordValue
	case n <= 4:
		r := &struct {
			recordValue
			store [4]Value
		}{}
		r.recordValue = recordValue{typ: typ, fields: r.store[:n]}
		return &r.recordValue
	case n <= 8:
		r := &struct {
			recordValue
			store [8]Value
		}{}
		r.recordValue = recordValue{typ: typ, fields: r.store[:n]}
		return &r.recordValue
	}
	return &recordValue{typ: typ, fields: make([]Value, n)}
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
			return Value{}, fmt.Errorf("array item %d has type %s, want %s", i, value.Type().Summary(), elem.Summary())
		}
		builder.add(value)
	}
	return builder.finish(), nil
}

// Record builds a record from the values of its fields, in the type's order.
func Record(typ Type, fields []Value) (Value, error) {
	if typ.kind != RecordKind || !typ.IsConcrete() {
		return Value{}, fmt.Errorf("record type must be concrete: %s", typ)
	}
	if len(fields) != len(typ.fields) {
		return Value{}, fmt.Errorf("record %s takes %d fields, got %d", typ, len(typ.fields), len(fields))
	}
	record := newRecord(&typ, len(fields))
	for i, field := range fields {
		if !field.hasType(typ.fields[i].typ) {
			return Value{}, fmt.Errorf("field %q has type %s, want %s",
				typ.fields[i].name, field.Type().Summary(), typ.fields[i].typ.Summary())
		}
		record.fields[i] = field
	}
	return Value{kind: RecordKind, box: record}, nil
}

// Field is the value at a record's i-th field. The compiler resolved the name
// to that index, so this is a slice read.
func (v Value) Field(i int) Value {
	record, ok := v.box.(*recordValue)
	if !ok || i < 0 || i >= len(record.fields) {
		return Value{}
	}
	return record.fields[i]
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
	case []money.Money, map[string]money.Money:
		return MoneyType
	case []money.FxRate:
		return FxRateType
	case *nestedArray:
		return box.elem
	case *nestedDict:
		return box.elem
	default:
		return Type{kind: InvalidKind}
	}
}

// hasType answers the same question as Type().Equal(t) without building a Type,
// which would allocate for every container check in the interpreter loop.
// Containers and records compare by shape: the currency of the money inside
// them is the currency checks' business, not the type test's.
func (v Value) hasType(t Type) bool {
	if t.kind == EnumKind {
		return v.kind == StringKind && slices.Contains(t.values, v.s)
	}
	if v.kind != t.kind {
		return false
	}
	switch v.kind {
	case HandleKind:
		return v.s == t.name
	case RecordKind:
		record, ok := v.box.(*recordValue)
		return ok && record.typ.Equal(t)
	case ArrayKind, DictKind:
		return t.elem != nil && v.elemType().Equal(*t.elem)
	case MoneyKind, CurrencyKind, FxRateKind:
		return v.hasUnits()
	default:
		return true
	}
}

func (v Value) Type() Type {
	switch v.kind {
	case BoolKind, IntKind, FloatKind, StringKind, RatioKind, MoneyKind, CurrencyKind, FxRateKind:
		return Type{kind: v.kind}
	case ArrayKind:
		return ArrayOf(v.elemType())
	case DictKind:
		return DictOf(v.elemType())
	case HandleKind:
		return HandleOf(v.s)
	case RecordKind:
		if record, ok := v.box.(*recordValue); ok {
			return *record.typ
		}
	}
	return Type{kind: InvalidKind}
}

func (v Value) Bool() (bool, bool)     { return v.b, v.kind == BoolKind }
func (v Value) Int() (int64, bool)     { return v.i, v.kind == IntKind }
func (v Value) Float() (float64, bool) { return v.f, v.kind == FloatKind }
func (v Value) String() (string, bool) { return v.s, v.kind == StringKind }

// At is the i-th item of an array, handed over without copying the container.
// It is what a pack like extensions/std needs to write a generic container
// function: the element is read-only, like everything else a Value gives out.
func (v Value) At(i int) (Value, bool) {
	if v.kind != ArrayKind || i < 0 || i >= v.length() {
		return Value{}, false
	}
	return v.at(i), true
}

// Lookup is At's counterpart for dictionaries: one entry, without building the
// map that Dict() would. A pack like extensions/std needs it to answer "this
// key, or the default" in constant time and no allocations.
func (v Value) Lookup(key string) (Value, bool) {
	if v.kind != DictKind {
		return Value{}, false
	}
	return v.lookup(key)
}

// Length is how many items a container holds, without building any of them:
// count needs the number, not the values.
func (v Value) Length() (int, bool) {
	if v.kind != ArrayKind && v.kind != DictKind {
		return 0, false
	}
	return v.length(), true
}

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
	case RecordKind:
		return v.recordAny()
	case HandleKind:
		// A handle has no JSON form; naming its type is all a log can show.
		return v.Type().String()
	case MoneyKind:
		if IsExact(v) {
			return exactOf(v)
		}
		amount, _ := v.Money()
		return amount
	case RatioKind:
		return ratioFrom(v)
	case FxRateKind:
		rate, _ := v.FxRate()
		return rate
	case CurrencyKind:
		return v.s
	default:
		return nil
	}
}

func (v Value) recordAny() any {
	record, ok := v.box.(*recordValue)
	if !ok {
		return nil
	}
	out := make(map[string]any, len(record.fields))
	for i, field := range record.fields {
		out[record.typ.fields[i].name] = field.Any()
	}
	return out
}

func (v Value) containerAny() any {
	switch box := v.box.(type) {
	case *nestedArray:
		return kit.Map(box.items, Value.Any)
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

// MarshalJSON writes a record's fields in the type's order. A Go map would come
// out alphabetical, and the order of a record's fields is part of its type, so
// the JSON a host reads back matches the contract it wrote. A container of
// records is walked too, or the field order would hold at the top level and
// quietly go alphabetical one level down.
func (v Value) MarshalJSON() ([]byte, error) {
	return json.Marshal(jsonTree(v, opensForJSON, Value.jsonLeaf))
}

// jsonLeaf is Any as json.Marshal is to write it: an empty container is []
// or {} whether its backing is empty or nil, which encoding/json would write
// as null — a value no argument reads back.
func (v Value) jsonLeaf() any {
	switch {
	case v.kind == ArrayKind && v.length() == 0:
		return emptyJSONArray
	case v.kind == DictKind && v.length() == 0:
		return emptyJSONObject
	}
	return v.Any()
}

var (
	emptyJSONArray  = []any{}
	emptyJSONObject = map[string]any{}
)

// opensForJSON reports whether v's JSON is walked rather than handed to
// json.Marshal whole: a record or a container with one inside, whose field
// order a Go map would lose, and a container of containers, whose empty ones
// still write [] or {}.
func opensForJSON(v Value) bool {
	switch v.box.(type) {
	case *nestedArray, *nestedDict:
		return true
	}
	return v.kind == RecordKind || (v.kind == ArrayKind || v.kind == DictKind) && TypeContains(v.Type(), RecordKind)
}

// jsonTree is v as the tree json.Marshal writes. The values walk accepts are
// opened — a record into orderedFields, which keeps its field order, a
// container into its items — and every other value is what leaf makes of it.
// Dictionary keys have no order of their own; encoding/json sorts a Go map's,
// which keeps the output stable.
func jsonTree(v Value, walk func(Value) bool, leaf func(Value) any) any {
	if !walk(v) {
		return leaf(v)
	}
	switch v.kind {
	case RecordKind:
		record, ok := v.box.(*recordValue)
		if !ok {
			return nil
		}
		out := orderedFields{names: make([]string, len(record.fields)), values: make([]any, len(record.fields))}
		for i, field := range record.fields {
			out.names[i] = record.typ.fields[i].name
			out.values[i] = jsonTree(field, walk, leaf)
		}
		return out
	case ArrayKind:
		items := make([]any, v.length())
		for i := range items {
			items[i] = jsonTree(v.at(i), walk, leaf)
		}
		return items
	case DictKind:
		entries := make(map[string]any, v.length())
		for _, key := range v.keys() {
			entry, _ := v.lookup(key)
			entries[key] = jsonTree(entry, walk, leaf)
		}
		return entries
	}
	return leaf(v)
}

// Equal compares two values of one type. Handles are never equal: the
// language cannot see into them, and the VM refuses to compare them at all
// (see compareEqual), so this answer is only a safe default.
func (v Value) Equal(other Value) bool {
	if equal, native := nativeEqual(v, other); native {
		return equal
	}
	if IsUnitKind(v.kind) {
		return v.kind == other.kind && v.equalUnits(other)
	}
	if !v.hasType(other.Type()) {
		return false
	}
	switch v.kind {
	case BoolKind, IntKind, FloatKind, StringKind:
		// A scalar's other fields are zero, so they match too.
		return v.b == other.b && v.i == other.i && v.f == other.f && v.s == other.s
	case ArrayKind, DictKind, RecordKind:
		equal, _ := equalItems(v, other, func(a, b Value) (bool, error) { return a.Equal(b), nil })
		return equal
	case RatioKind:
		return ratioFrom(v).Cmp(ratioFrom(other)) == 0
	default:
		return false
	}
}

// equalItems compares two containers or records of one type item by item
// with same. The first difference or error decides; containers of different
// sizes, and a key only one dictionary has, are simply unequal.
func equalItems(left, right Value, same func(a, b Value) (bool, error)) (bool, error) {
	n, m := left.length(), right.length()
	pair := func(i int) (Value, Value, bool) { return left.at(i), right.at(i), true }
	switch left.kind {
	case DictKind:
		keys := left.keys()
		pair = func(i int) (Value, Value, bool) {
			a, _ := left.lookup(keys[i])
			b, ok := right.lookup(keys[i])
			return a, b, ok
		}
	case RecordKind:
		a, aOK := left.box.(*recordValue)
		b, bOK := right.box.(*recordValue)
		if !aOK || !bOK {
			return false, nil
		}
		n, m = len(a.fields), len(b.fields)
		pair = func(i int) (Value, Value, bool) { return a.fields[i], b.fields[i], true }
	}
	if n != m {
		return false, nil
	}
	for i := range n {
		a, b, ok := pair(i)
		if !ok {
			return false, nil
		}
		if equal, err := same(a, b); err != nil || !equal {
			return false, err
		}
	}
	return true, nil
}

// compareEqual is the VM's equality: eq and switch both use it. Handles are
// opaque, so comparing them is an error rather than a guess.
func compareEqual(left, right Value) (Value, error) {
	if equal, native := nativeEqual(left, right); native {
		return Bool(equal), nil
	}
	if left.kind == HandleKind || right.kind == HandleKind {
		return Value{}, errors.New("handles cannot be compared")
	}
	if err := sameUnits(left, right); err != nil {
		return Value{}, err
	}
	if IsUnitKind(left.kind) && left.kind == right.kind {
		return Bool(left.Equal(right)), nil
	}
	typ := right.Type()
	if !left.hasType(typ) {
		return Value{}, fmt.Errorf("equality requires one type, got %s and %s", left.Type(), typ)
	}
	if (typ.kind == ArrayKind || typ.kind == DictKind || typ.kind == RecordKind) && containsUnits(typ) {
		equal, err := equalInUnits(left, right)
		return Bool(equal), err
	}
	return Bool(left.Equal(right)), nil
}

// CheckedFloat builds a float value, refusing the non-finite ones: what a
// float literal reads as, which a literal past the range of a float is not.
// Every other float may be NaN or an infinity, as IEEE 754 has them.
func CheckedFloat(value float64) (Value, error) {
	if !finite(value) {
		return Value{}, errors.New("non-finite floats are not supported")
	}
	return Float(value), nil
}

// finite reports a float that is neither NaN nor an infinity.
func finite(value float64) bool { return !math.IsNaN(value) && !math.IsInf(value, 0) }

// nativeEqual compares two containers of one native backing of scalars —
// no units to hold to, and a type that is the backing's — as Go compares
// them: what the item-by-item walk would find, without building a type or
// boxing an item. native is false for anything else.
func nativeEqual(left, right Value) (equal, native bool) {
	switch a := left.box.(type) {
	case []int64:
		return sameSlice(a, right.box)
	case []float64:
		return sameSlice(a, right.box)
	case []string:
		return sameSlice(a, right.box)
	case []bool:
		return sameSlice(a, right.box)
	case map[string]int64:
		return sameMap(a, right.box)
	case map[string]float64:
		return sameMap(a, right.box)
	case map[string]string:
		return sameMap(a, right.box)
	case map[string]bool:
		return sameMap(a, right.box)
	}
	return false, false
}

func sameSlice[T comparable](a []T, other any) (equal, native bool) {
	b, ok := other.([]T)
	if !ok {
		return false, false
	}
	if len(a) == len(b) && (len(a) == 0 || &a[0] == &b[0]) {
		return true, true // one backing
	}
	return slices.Equal(a, b), true
}

func sameMap[T comparable](a map[string]T, other any) (equal, native bool) {
	b, ok := other.(map[string]T)
	if !ok {
		return false, false
	}
	return maps.Equal(a, b), true
}
