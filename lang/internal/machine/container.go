package machine

// How a container is represented: the backing is chosen by element type, and
// this file is the only place that knows the choice. Everything else — the
// loop opcodes, the array primitives, equality, the host boundary — goes
// through the few operations here, so the mapping from FunRoute type to Go
// type is stated once. What a program can ask a container (at, in, len) is a
// different question, and lives in builtins_container.go.

import "maps"

// length is the item count of an array or the entry count of a dictionary.
func (v Value) length() int {
	switch box := v.box.(type) {
	case []bool:
		return len(box)
	case []int64:
		return len(box)
	case []float64:
		return len(box)
	case []string:
		return len(box)
	case *nestedArray:
		return len(box.items)
	case map[string]bool:
		return len(box)
	case map[string]int64:
		return len(box)
	case map[string]float64:
		return len(box)
	case map[string]string:
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
	case []bool:
		return Bool(box[i])
	case []int64:
		return Int(box[i])
	case []float64:
		return Float(box[i])
	case []string:
		return String(box[i])
	case *nestedArray:
		return box.items[i]
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
	case *nestedDict:
		value, ok := box.entries[key]
		return value, ok
	default:
		return Value{}, false
	}
}

// keys lists a dictionary's keys in sorted order: the language promises a
// program replays identically, and Go's map order does not.
func (v Value) keys() []string {
	switch box := v.box.(type) {
	case map[string]bool:
		return sortedKeys(box)
	case map[string]int64:
		return sortedKeys(box)
	case map[string]float64:
		return sortedKeys(box)
	case map[string]string:
		return sortedKeys(box)
	case *nestedDict:
		return sortedKeys(box.entries)
	default:
		return nil
	}
}

// tail shares the suffix of a non-empty array: values never change in place,
// so the two arrays can alias the same backing.
func (v Value) tail() Value {
	switch box := v.box.(type) {
	case []bool:
		return Value{kind: ArrayKind, box: box[1:]}
	case []int64:
		return Value{kind: ArrayKind, box: box[1:]}
	case []float64:
		return Value{kind: ArrayKind, box: box[1:]}
	case []string:
		return Value{kind: ArrayKind, box: box[1:]}
	case *nestedArray:
		return Value{kind: ArrayKind, box: &nestedArray{elem: box.elem, items: box.items[1:]}}
	default:
		return Value{}
	}
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
	values  []Value
}

func newArrayBuilder(elem Type, capacity int) arrayBuilder {
	builder := arrayBuilder{elem: elem}
	switch elem.Kind {
	case BoolKind:
		builder.bools = make([]bool, 0, capacity)
	case IntKind:
		builder.ints = make([]int64, 0, capacity)
	case FloatKind:
		builder.floats = make([]float64, 0, capacity)
	case StringKind:
		builder.strings = make([]string, 0, capacity)
	default:
		builder.values = make([]Value, 0, capacity)
	}
	return builder
}

// add appends a value the caller has already type checked.
func (b *arrayBuilder) add(value Value) {
	switch b.elem.Kind {
	case BoolKind:
		b.bools = append(b.bools, value.b)
	case IntKind:
		b.ints = append(b.ints, value.i)
	case FloatKind:
		b.floats = append(b.floats, value.f)
	case StringKind:
		b.strings = append(b.strings, value.s)
	default:
		b.values = append(b.values, value)
	}
}

// addAll splices an array the caller has already type checked: what a nested
// comprehension yields is one array per outer item, and the elements are what
// the output collects.
func (b *arrayBuilder) addAll(value Value) {
	for i := 0; i < value.length(); i++ {
		b.add(value.at(i))
	}
}

func (b *arrayBuilder) finish() Value {
	switch b.elem.Kind {
	case BoolKind:
		return Value{kind: ArrayKind, box: b.bools}
	case IntKind:
		return Value{kind: ArrayKind, box: b.ints}
	case FloatKind:
		return Value{kind: ArrayKind, box: b.floats}
	case StringKind:
		return Value{kind: ArrayKind, box: b.strings}
	default:
		return Value{kind: ArrayKind, box: &nestedArray{elem: CloneType(b.elem), items: b.values}}
	}
}

// packDict is the dictionary counterpart of arrayBuilder, for entries the
// caller has already type checked.
func packDict(elem Type, entries map[string]Value) Value {
	switch elem.Kind {
	case BoolKind:
		return Value{kind: DictKind, box: mapEntries(entries, func(v Value) bool { return v.b })}
	case IntKind:
		return Value{kind: DictKind, box: mapEntries(entries, func(v Value) int64 { return v.i })}
	case FloatKind:
		return Value{kind: DictKind, box: mapEntries(entries, func(v Value) float64 { return v.f })}
	case StringKind:
		return Value{kind: DictKind, box: mapEntries(entries, func(v Value) string { return v.s })}
	default:
		copied := make(map[string]Value, len(entries))
		maps.Copy(copied, entries)
		return Value{kind: DictKind, box: &nestedDict{elem: CloneType(elem), entries: copied}}
	}
}

func mapEntries[T any](entries map[string]Value, get func(Value) T) map[string]T {
	out := make(map[string]T, len(entries))
	for key, value := range entries {
		out[key] = get(value)
	}
	return out
}
