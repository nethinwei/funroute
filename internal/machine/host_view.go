package machine

import (
	"reflect"
	"slices"
	"unsafe"
)

// An array of plain records — every field a bool, an int, a float or a
// string — is held as the host's slice, as .NET holds an array of values or
// a Span: the Value's backing is a recordsView of the slice's memory, and a
// record is made of an item only where one is asked for (at, Array). The
// library sorts and cuts a view into another view of the same memory, and a
// view answered into a slice of the same fields is copied field by field,
// never through a record.
//
// A program that only walks or measures the array (lower_escape.go) is
// given the view in the frame's own slot; any other, a view of its own. A
// loop whose item is only read field by field loads each item into the
// loop's own record in the frame: no record is made per item, and none
// outlives its item, as the value flow proved nothing keeps one. Any other
// loop over a view makes a record per item.
// Run the same program through RunValues and it walks the records it is
// given.

// recordsView is an array of plain records in the host's memory: its items,
// how many, and the plan of the slice, whose element is a record every field
// of which loads in place.
type recordsView struct {
	data   unsafe.Pointer
	length int
	plan   *codec
	// perm, when set, is which item of the host's slice each item is: a
	// view the library sorted or cut, not the slice as it is.
	perm []int32
}

// viewable reports a slice plan a recordsView can carry: every field of its
// records is a plain bool, int, float or string.
func viewable(plan *codec) bool {
	if plan.shape != shapeSlice || plan.elem.shape != shapeRecord {
		return false
	}
	for _, field := range plan.elem.fields {
		if field.plain == reflect.Invalid {
			return false
		}
	}
	return true
}

// viewRecords makes view the host's slice at p, and is the array there:
// the frame's own view, when inPlace, and one of its own otherwise.
func viewRecords(view *recordsView, plan *codec, p unsafe.Pointer, inPlace bool) Value {
	header := (*sliceHeader)(p)
	if !inPlace {
		view = new(recordsView)
	}
	*view = recordsView{data: header.data, length: header.len, plan: plan}
	return Value{kind: ArrayKind, box: view}
}

// item is where item i is in the host's memory.
func (v *recordsView) item(i int) unsafe.Pointer {
	if v.perm != nil {
		i = int(v.perm[i])
	}
	return unsafe.Add(v.data, uintptr(i)*v.plan.stride)
}

// load loads item index into record, the loop's own.
func (v *recordsView) load(index int, record *recordValue) {
	plan := v.plan.elem
	item := v.item(index)
	record.typ = &plan.typ
	record.fields = slices.Grow(record.fields[:0], len(plan.fields))[:len(plan.fields)]
	for i, field := range plan.fields {
		// Every field is plain.
		loadPlain(field.plain, unsafe.Add(item, field.offset), &record.fields[i])
	}
}

// record is item i as a record of its own.
func (v *recordsView) record(i int) Value {
	plan := v.plan.elem
	record := newRecord(&plan.typ, len(plan.fields))
	item := v.item(i)
	for j, field := range plan.fields {
		loadPlain(field.plain, unsafe.Add(item, field.offset), &record.fields[j])
	}
	return Value{kind: RecordKind, box: record}
}

// items is every item as a record of its own: the records in one
// allocation, their fields in another, as loading the slice whole makes them.
func (v *recordsView) items() []Value {
	plan, width := v.plan.elem, len(v.plan.elem.fields)
	records, fields, items := make([]recordValue, v.length), make([]Value, v.length*width), make([]Value, v.length)
	for i := range v.length {
		record, item := &records[i], v.item(i)
		record.typ, record.fields = &plan.typ, fields[i*width:(i+1)*width:(i+1)*width]
		for j, field := range plan.fields {
			loadPlain(field.plain, unsafe.Add(item, field.offset), &record.fields[j])
		}
		items[i] = Value{kind: RecordKind, box: record}
	}
	return items
}

// slice is items [from, to) of the view, the same memory.
func (v *recordsView) slice(from, to int) *recordsView {
	out := &recordsView{data: v.data, length: to - from, plan: v.plan}
	switch {
	case v.perm != nil:
		out.perm = v.perm[from:to:to]
	case to > from:
		out.data = unsafe.Add(v.data, uintptr(from)*v.plan.stride)
	}
	return out
}

// arranged is n items of the view, item i of it being item at(i) of v: a
// view of the same memory in another order.
func (v *recordsView) arranged(n int, at func(int) int) *recordsView {
	perm := make([]int32, n)
	for i := range perm {
		j := at(i)
		if v.perm != nil {
			j = int(v.perm[j])
		}
		perm[i] = int32(j)
	}
	return &recordsView{data: v.data, length: n, plan: v.plan, perm: perm}
}

// storeView writes the view into the slice at p of plan, a slice of records
// of the same type: field by field from the host's memory, when every field
// is the same Go kind in both, which is what a slice of the same struct is.
// It reports false for a slice it does not answer so.
func storeView(plan *codec, p unsafe.Pointer, v *recordsView) bool {
	from, to := v.plan.elem.fields, plan.elem.fields
	if !viewable(plan) || len(from) != len(to) {
		return false
	}
	for i := range from {
		if from[i].plain != to[i].plain {
			return false
		}
	}
	items := reflect.MakeSlice(plan.goType, v.length, v.length)
	data := items.UnsafePointer()
	for i := range v.length {
		source, target := v.item(i), unsafe.Add(data, uintptr(i)*plan.stride)
		for j := range from {
			copyPlain(from[j].plain, unsafe.Add(target, to[j].offset), unsafe.Add(source, from[j].offset))
		}
	}
	reflect.NewAt(plan.goType, noescape(p)).Elem().Set(items)
	return true
}

// copyPlain copies a plain field of kind from source to target.
func copyPlain(kind reflect.Kind, target, source unsafe.Pointer) {
	switch kind {
	case reflect.Bool:
		*(*bool)(target) = *(*bool)(source)
	case reflect.Int:
		*(*int)(target) = *(*int)(source)
	case reflect.Int64:
		*(*int64)(target) = *(*int64)(source)
	case reflect.Float64:
		*(*float64)(target) = *(*float64)(source)
	default:
		*(*string)(target) = *(*string)(source)
	}
}
