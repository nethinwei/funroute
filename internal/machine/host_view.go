package machine

import (
	"reflect"
	"slices"
	"unsafe"
)

// An array of plain records a program only walks or measures, reading each
// item's fields and nothing else of it (lower_escape.go), is handed over as
// the host's slice: the frame keeps the slice's header — never a pointer to
// the host's struct — and each item of a loop over it is loaded into the
// loop's own record in the frame, field by field, as it is reached. No
// record is made per item, and none outlives its item: the value flow proved
// nothing keeps one. Run the same program through RunValues and the loop
// walks the records it is given.

// recordsView is an array argument of plain records in the host's memory:
// its items, how many, and the plan of the slice, whose element is a record
// every field of which loads in place.
type recordsView struct {
	data   unsafe.Pointer
	length int
	plan   *codec
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

// viewRecords makes view the host's slice at p, and is the array there.
func viewRecords(view *recordsView, plan *codec, p unsafe.Pointer) Value {
	header := (*sliceHeader)(p)
	*view = recordsView{data: header.data, length: header.len, plan: plan}
	return Value{kind: ArrayKind, box: view}
}

// load loads item index into record, the loop's own.
func (v *recordsView) load(index int, record *recordValue) {
	plan := v.plan.elem
	item := unsafe.Add(v.data, uintptr(index)*v.plan.stride)
	record.typ = &plan.typ
	record.fields = slices.Grow(record.fields[:0], len(plan.fields))[:len(plan.fields)]
	for i, field := range plan.fields {
		// Every field is plain.
		loadPlain(field.plain, unsafe.Add(item, field.offset), &record.fields[i])
	}
}
