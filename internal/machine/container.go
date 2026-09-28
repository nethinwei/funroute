package machine

// How a container is represented: the backing is chosen by element type, and
// this file is the only place that knows the choice. Everything else — the
// loop opcodes, the array primitives, equality, the host boundary — goes
// through the few operations here, so the mapping from FunRoute type to Go
// type is stated once. What a program can ask a container (at, in, len) is a
// different question, and lives in builtins_container.go.

import (
	"fmt"
	"maps"
	"reflect"
	"unsafe"

	"github.com/nethinwei/funroute/internal/kit"
	"github.com/nethinwei/funroute/internal/money"
)

// nativeBacking is the Go element type containers of one element kind are
// held in, and whether a dictionary of them has a native backing too.
type nativeBacking struct {
	elem   Kind
	goElem reflect.Type
	dict   bool
}

// natives is every native backing, in the order of the native constants: the
// Go element type a container of each element kind is held in, as a slice,
// and as a map keyed by string where dict says one exists. It is the one
// list. The switches that read a backing — here, in fromGo and in
// host_access.go — stay switches, because the hot path cannot afford an
// indirect call, and TestEveryBackingIsHandledEverywhere holds each of them
// to every entry.
var natives = []nativeBacking{
	{BoolKind, reflect.TypeFor[bool](), true},
	{IntKind, reflect.TypeFor[int64](), true},
	{FloatKind, reflect.TypeFor[float64](), true},
	{StringKind, reflect.TypeFor[string](), true},
	{MoneyKind, reflect.TypeFor[money.Money](), true},
	// No backing holds exchange rates by key: a map of them is carried value
	// by value.
	{FxRateKind, reflect.TypeFor[money.FxRate](), false},
}

// Every array's i is its length, whatever backs it. A native array is held
// as where its items start and how many: box is a *T at the first item (nil
// for a nil slice). A slice put in an interface would allocate its header
// every time; a pointer does not. arrayOf puts a slice in a Value and
// nativeItems takes it back out, and nothing else builds or reads the pair.

// arrayOf is the array backed by items.
func arrayOf[T any](items []T) Value {
	return Value{kind: ArrayKind, i: int64(len(items)), box: unsafe.SliceData(items)}
}

// nestedOf is the array of items, containers or records of elem.
func nestedOf(elem Type, items []Value) Value {
	return Value{kind: ArrayKind, i: int64(len(items)), box: &nestedArray{elem: elem, items: items}}
}

// viewed is the array view is.
func viewed(view *recordsView) Value {
	return Value{kind: ArrayKind, i: int64(view.length), box: view}
}

// itemsAt is the n items from first: a native backing as it was put in.
func itemsAt[T any](first *T, n int64) []T { return unsafe.Slice(first, n) }

// nativeItems is the []T backing v, and whether one does.
func nativeItems[T any](v Value) ([]T, bool) {
	first, ok := v.box.(*T)
	if !ok || v.kind != ArrayKind {
		return nil, false
	}
	return unsafe.Slice(first, v.i), true
}

// nativeAny is the backing of a native array as the slice itself, and v's box
// for any other value.
func nativeAny(v Value) any {
	if v.kind != ArrayKind {
		return v.box
	}
	switch first := v.box.(type) {
	case *bool:
		return itemsAt(first, v.i)
	case *int64:
		return itemsAt(first, v.i)
	case *float64:
		return itemsAt(first, v.i)
	case *string:
		return itemsAt(first, v.i)
	case *money.Money:
		return itemsAt(first, v.i)
	case *money.FxRate:
		return itemsAt(first, v.i)
	}
	return v.box
}

// length is the item count of an array or the entry count of a dictionary.
func (v Value) length() int {
	if v.kind == ArrayKind {
		return int(v.i)
	}
	switch box := v.box.(type) {
	case map[string]bool:
		return len(box)
	case map[string]int64:
		return len(box)
	case map[string]float64:
		return len(box)
	case map[string]string:
		return len(box)
	case map[string]money.Money:
		return len(box)
	case *nestedDict:
		return len(box.entries)
	default:
		return 0
	}
}

// at returns array item i as a value. The scalar cases build one inline, which
// is exactly what loading a []Value item would copy anyway.
func (v Value) at(i int) Value {
	switch box := v.box.(type) {
	case *bool:
		return Bool(itemsAt(box, v.i)[i])
	case *int64:
		return Int(itemsAt(box, v.i)[i])
	case *float64:
		return Float(itemsAt(box, v.i)[i])
	case *string:
		return String(itemsAt(box, v.i)[i])
	case *money.Money:
		amount := itemsAt(box, v.i)[i]
		return MoneyValue(amount.Minor(), amount.Currency())
	case *money.FxRate:
		return FxRateValue(itemsAt(box, v.i)[i])
	case *nestedArray:
		return box.items[i]
	case *recordsView:
		return box.record(i)
	default:
		return Value{}
	}
}

// lookup returns a dictionary entry.
func (v Value) lookup(key string) (Value, bool) {
	switch box := v.box.(type) {
	case map[string]bool:
		value, ok := box[key]
		return Bool(value), ok
	case map[string]int64:
		value, ok := box[key]
		return Int(value), ok
	case map[string]float64:
		value, ok := box[key]
		return Float(value), ok
	case map[string]string:
		value, ok := box[key]
		return String(value), ok
	case map[string]money.Money:
		value, ok := box[key]
		return MoneyValue(value.Minor(), value.Currency()), ok
	case *nestedDict:
		value, ok := box.entries[key]
		return value, ok
	default:
		return Value{}, false
	}
}

// eachEntry visits a dictionary's entries in the map's order, which is none:
// for a walk whose outcome does not depend on the order, such as checking
// every entry, without sorting or copying the keys as keys does.
func (v Value) eachEntry(visit func(key string, entry Value) error) error {
	switch box := v.box.(type) {
	case map[string]money.Money:
		for key, amount := range box {
			if err := visit(key, MoneyValue(amount.Minor(), amount.Currency())); err != nil {
				return err
			}
		}
	case *nestedDict:
		for key, entry := range box.entries {
			if err := visit(key, entry); err != nil {
				return err
			}
		}
	default:
		for _, key := range v.keys() {
			entry, _ := v.lookup(key)
			if err := visit(key, entry); err != nil {
				return err
			}
		}
	}
	return nil
}

// eachPart checks each item of an array, entry of a dictionary or field of a
// record, naming the part in the error. Dictionaries go in the map's order,
// which costs nothing, or in key order when ordered, for an error said the
// same every time.
func (v Value) eachPart(ordered bool, check func(Value) error) error {
	entry := func(key string, entry Value) error {
		if err := check(entry); err != nil {
			return fmt.Errorf("entry %q: %w", key, err)
		}
		return nil
	}
	switch record, _ := v.box.(*recordValue); {
	case v.kind == ArrayKind:
		for i := range v.length() {
			if err := check(v.at(i)); err != nil {
				return fmt.Errorf("item %d: %w", i, err)
			}
		}
	case v.kind == DictKind && !ordered:
		return v.eachEntry(entry)
	case v.kind == DictKind:
		for _, key := range v.keys() {
			value, _ := v.lookup(key)
			if err := entry(key, value); err != nil {
				return err
			}
		}
	case record != nil:
		for i, field := range record.fields {
			if err := check(field); err != nil {
				return fmt.Errorf("field %q: %w", record.typ.fields[i].name, err)
			}
		}
	}
	return nil
}

// keys lists a dictionary's keys in sorted order: the language promises a
// program replays identically, and Go's map order does not.
func (v Value) keys() []string {
	switch box := v.box.(type) {
	case map[string]bool:
		return kit.SortedKeys(box)
	case map[string]int64:
		return kit.SortedKeys(box)
	case map[string]float64:
		return kit.SortedKeys(box)
	case map[string]string:
		return kit.SortedKeys(box)
	case map[string]money.Money:
		return kit.SortedKeys(box)
	case *nestedDict:
		return kit.SortedKeys(box.entries)
	default:
		return nil
	}
}

// Slice is items [from, to) of an array, sharing its backing: values never
// change in place, so two arrays can alias one. It is read-only, as every
// backing is. It is false for what is not an array, and for a range that is
// not inside it.
func (v Value) Slice(from, to int) (Value, bool) {
	if v.kind != ArrayKind || from < 0 || to < from || to > v.length() {
		return Value{}, false
	}
	switch box := v.box.(type) {
	case *bool:
		return arrayOf(itemsAt(box, v.i)[from:to:to]), true
	case *int64:
		return arrayOf(itemsAt(box, v.i)[from:to:to]), true
	case *float64:
		return arrayOf(itemsAt(box, v.i)[from:to:to]), true
	case *string:
		return arrayOf(itemsAt(box, v.i)[from:to:to]), true
	case *money.Money:
		return arrayOf(itemsAt(box, v.i)[from:to:to]), true
	case *money.FxRate:
		return arrayOf(itemsAt(box, v.i)[from:to:to]), true
	case *nestedArray:
		return nestedOf(box.elem, box.items[from:to:to]), true
	case *recordsView:
		return viewed(box.slice(from, to)), true
	}
	return Value{}, false
}

// arrayBuilder collects values into the canonical backing for their element
// type. It is how the VM assembles an array — a literal, a comprehension's
// output, a prepend — so a program-built array<float> is a []float64 just like
// a host-supplied one, and an extension receiving either gets the slice itself.
type arrayBuilder struct {
	elem    Type
	bools   []bool
	ints    []int64
	floats  []float64
	strings []string
	monies  []money.Money
	fxRates []money.FxRate
	values  []Value
}

func newArrayBuilder(elem Type, capacity int) arrayBuilder {
	builder := arrayBuilder{elem: elem}
	switch elem.kind {
	case BoolKind:
		builder.bools = make([]bool, 0, capacity)
	case IntKind:
		builder.ints = make([]int64, 0, capacity)
	case FloatKind:
		builder.floats = make([]float64, 0, capacity)
	case StringKind:
		builder.strings = make([]string, 0, capacity)
	case MoneyKind:
		builder.monies = make([]money.Money, 0, capacity)
	case FxRateKind:
		builder.fxRates = make([]money.FxRate, 0, capacity)
	default:
		builder.values = make([]Value, 0, capacity)
	}
	return builder
}

// add appends a value the caller has already type checked.
func (b *arrayBuilder) add(value Value) {
	switch b.elem.kind {
	case BoolKind:
		b.bools = append(b.bools, value.b)
	case IntKind:
		b.ints = append(b.ints, value.i)
	case FloatKind:
		b.floats = append(b.floats, value.f)
	case StringKind:
		b.strings = append(b.strings, value.s)
	case MoneyKind:
		b.monies = append(b.monies, money.Make(value.s, value.i))
	case FxRateKind:
		fx, _ := value.FxRate()
		b.fxRates = append(b.fxRates, fx)
	default:
		b.values = append(b.values, value)
	}
}

// addAll splices an array the caller has already type checked: what a nested
// comprehension yields is one array per outer item, and the elements are what
// the output collects.
func (b *arrayBuilder) addAll(value Value) {
	for i := range value.length() {
		b.add(value.at(i))
	}
}

func (b *arrayBuilder) finish() Value {
	switch b.elem.kind {
	case BoolKind:
		return arrayOf(b.bools)
	case IntKind:
		return arrayOf(b.ints)
	case FloatKind:
		return arrayOf(b.floats)
	case StringKind:
		return arrayOf(b.strings)
	case MoneyKind:
		return arrayOf(b.monies)
	case FxRateKind:
		return arrayOf(b.fxRates)
	default:
		return nestedOf(b.elem, b.values)
	}
}

// packDict is the dictionary counterpart of arrayBuilder, for entries the
// caller has already type checked.
func packDict(elem Type, entries map[string]Value) Value {
	switch elem.kind {
	case BoolKind:
		return Value{kind: DictKind, box: mapEntries(entries, func(v Value) bool { return v.b })}
	case IntKind:
		return Value{kind: DictKind, box: mapEntries(entries, func(v Value) int64 { return v.i })}
	case FloatKind:
		return Value{kind: DictKind, box: mapEntries(entries, func(v Value) float64 { return v.f })}
	case StringKind:
		return Value{kind: DictKind, box: mapEntries(entries, func(v Value) string { return v.s })}
	case MoneyKind:
		return Value{kind: DictKind, box: mapEntries(entries, func(v Value) money.Money { return money.Make(v.s, v.i) })}
	default:
		copied := make(map[string]Value, len(entries))
		maps.Copy(copied, entries)
		return Value{kind: DictKind, box: &nestedDict{elem: elem, entries: copied}}
	}
}

func mapEntries[T any](entries map[string]Value, get func(Value) T) map[string]T {
	out := make(map[string]T, len(entries))
	for key, value := range entries {
		out[key] = get(value)
	}
	return out
}
