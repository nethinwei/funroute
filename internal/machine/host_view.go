package machine

import (
	"fmt"
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

// viewRecords makes view the host's slice at p, and is the array there. A
// float that is not finite is refused here, before the program runs, in the
// words loading the slice would have used.
func viewRecords(view *recordsView, plan *codec, p unsafe.Pointer) (Value, error) {
	header := (*sliceHeader)(p)
	if slices.ContainsFunc(plan.elem.fields, func(field fieldCodec) bool { return field.plain == reflect.Float64 }) {
		for i := range header.len {
			if item := unsafe.Add(header.data, uintptr(i)*plan.stride); !finiteFields(plan.elem, item) {
				_, err := plan.elem.load(item)
				return Value{}, fmt.Errorf("item %d: %w", i, err)
			}
		}
	}
	*view = recordsView{data: header.data, length: header.len, plan: plan}
	return Value{kind: ArrayKind, box: view}, nil
}

// finiteFields reports whether every float field of the record at p is
// finite.
func finiteFields(plan *codec, p unsafe.Pointer) bool {
	for _, field := range plan.fields {
		if field.plain == reflect.Float64 && !finite(*(*float64)(unsafe.Add(p, field.offset))) {
			return false
		}
	}
	return true
}

// load loads item index into record, the loop's own.
func (v *recordsView) load(index int, record *recordValue) {
	plan := v.plan.elem
	item := unsafe.Add(v.data, uintptr(index)*v.plan.stride)
	record.typ = &plan.typ
	record.fields = slices.Grow(record.fields[:0], len(plan.fields))[:len(plan.fields)]
	for i, field := range plan.fields {
		// Every field is plain, and every float was found finite.
		loadPlain(field.plain, unsafe.Add(item, field.offset), &record.fields[i])
	}
}
