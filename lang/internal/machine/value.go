package machine

import (
	"encoding/json"
	"fmt"
	"math"
	"slices"
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

// recordValue backs a record: the fields in the type's order, plus the type
// itself, because a record's fields are what it is. A field access already
// knows its index at compile time, so nothing here is looked up by name.
type recordValue struct {
	typ    Type
	fields []Value
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
		if err := value.validateInvariant(); err != nil {
			return Value{}, fmt.Errorf("array item %d: %w", i, err)
		}
		builder.add(value)
	}
	return builder.finish(), nil
}

// Record builds a record from the values of its fields, in the type's order.
func Record(typ Type, fields []Value) (Value, error) {
	if typ.Kind != RecordKind || !typ.IsConcrete() {
		return Value{}, fmt.Errorf("record type must be concrete: %s", typ)
	}
	if len(fields) != len(typ.Fields) {
		return Value{}, fmt.Errorf("record %s takes %d fields, got %d", typ, len(typ.Fields), len(fields))
	}
	stored := make([]Value, len(fields))
	for i, field := range fields {
		if !field.hasType(typ.Fields[i].Type) {
			return Value{}, fmt.Errorf("field %q has type %s, want %s",
				typ.Fields[i].Name, field.Type().Summary(), typ.Fields[i].Type.Summary())
		}
		if err := field.validateInvariant(); err != nil {
			return Value{}, fmt.Errorf("field %q: %w", typ.Fields[i].Name, err)
		}
		stored[i] = field
	}
	return Value{kind: RecordKind, box: &recordValue{typ: CloneType(typ), fields: stored}}, nil
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
		if err := value.validateInvariant(); err != nil {
			return Value{}, fmt.Errorf("dictionary entry %q: %w", key, err)
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
	if t.Kind == EnumKind {
		return v.kind == StringKind && slices.Contains(t.Values, v.s)
	}
	if v.kind != t.Kind {
		return false
	}
	if v.kind == HandleKind {
		return v.s == t.Name
	}
	if v.kind == RecordKind {
		return v.Type().Equal(t)
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
	case RecordKind:
		if record, ok := v.box.(*recordValue); ok {
			return CloneType(record.typ)
		}
		return Type{Kind: InvalidKind}
	default:
		return Type{Kind: InvalidKind}
	}
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
		out[record.typ.Fields[i].Name] = field.Any()
	}
	return out
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
	if v.kind == RecordKind {
		return v.marshalRecord()
	}
	// A container of records has to be walked too, or the field order would
	// hold at the top level and quietly go alphabetical one level down.
	if (v.kind == ArrayKind || v.kind == DictKind) && holdsRecord(v.Type()) {
		return v.marshalContainer()
	}
	return json.Marshal(v.Any())
}

func holdsRecord(typ Type) bool {
	if typ.Kind == RecordKind {
		return true
	}
	return typ.Elem != nil && holdsRecord(*typ.Elem)
}

func (v Value) marshalContainer() ([]byte, error) {
	if v.kind == ArrayKind {
		items := make([]json.RawMessage, v.length())
		for i := range items {
			encoded, err := json.Marshal(v.at(i))
			if err != nil {
				return nil, err
			}
			items[i] = encoded
		}
		return json.Marshal(items)
	}
	// Dictionary keys have no order of their own, so they are sorted, which is
	// what encoding/json does for a Go map and keeps the output stable.
	entries := make(map[string]json.RawMessage, v.length())
	for _, key := range v.keys() {
		item, _ := v.lookup(key)
		encoded, err := json.Marshal(item)
		if err != nil {
			return nil, err
		}
		entries[key] = encoded
	}
	return json.Marshal(entries)
}

// marshalRecord writes the fields in the type's order. A Go map would come out
// alphabetical, and the order of a record's fields is part of its type, so the
// JSON a host reads back matches the contract it wrote.
func (v Value) marshalRecord() ([]byte, error) {
	record, ok := v.box.(*recordValue)
	if !ok {
		return []byte("null"), nil
	}
	var out []byte
	out = append(out, '{')
	for i, field := range record.fields {
		if i > 0 {
			out = append(out, ',')
		}
		name, err := json.Marshal(record.typ.Fields[i].Name)
		if err != nil {
			return nil, err
		}
		value, err := json.Marshal(field)
		if err != nil {
			return nil, err
		}
		out = append(out, name...)
		out = append(out, ':')
		out = append(out, value...)
	}
	return append(out, '}'), nil
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
	case RecordKind:
		return v.equalRecord(other)
	default:
		return false
	}
}

// equalRecord compares field by field. The types matched already, so both
// records hold the same fields in the same order.
func (v Value) equalRecord(other Value) bool {
	left, leftOK := v.box.(*recordValue)
	right, rightOK := other.box.(*recordValue)
	if !leftOK || !rightOK || len(left.fields) != len(right.fields) {
		return false
	}
	for i := range left.fields {
		if !left.fields[i].Equal(right.fields[i]) {
			return false
		}
	}
	return true
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

func (v Value) validateInvariant() error {
	switch box := v.box.(type) {
	case []float64:
		return checkFloats(box)
	case map[string]float64:
		for key, value := range box {
			if math.IsNaN(value) || math.IsInf(value, 0) {
				return fmt.Errorf("entry %q: non-finite floats are not supported", key)
			}
		}
	case *recordValue:
		for i, value := range box.fields {
			if err := value.validateInvariant(); err != nil {
				return fmt.Errorf("field %q: %w", box.typ.Fields[i].Name, err)
			}
		}
	case *nestedArray:
		for i, value := range box.items {
			if err := value.validateInvariant(); err != nil {
				return fmt.Errorf("item %d: %w", i, err)
			}
		}
	case *nestedDict:
		for key, value := range box.entries {
			if err := value.validateInvariant(); err != nil {
				return fmt.Errorf("entry %q: %w", key, err)
			}
		}
	}
	if v.kind == FloatKind && (math.IsNaN(v.f) || math.IsInf(v.f, 0)) {
		return fmt.Errorf("non-finite floats are not supported")
	}
	return nil
}

func sortedKeys[T any](entries map[string]T) []string {
	keys := make([]string, 0, len(entries))
	for key := range entries {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
