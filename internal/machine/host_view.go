package machine

import (
	"reflect"
	"unsafe"

	"github.com/nethinwei/funroute/internal/money"
)

// An array of plain records — every field a bool, an int, a float or a
// string, or an amount or a currency — is held as the host's slice, as .NET holds an array of values or
// a Span: the Value's backing is a recordsView of the slice's memory, and a
// record is made of an item only where one is asked for (at, Array). The
// library sorts and cuts a view into another view of the same memory, and a
// view answered into a slice of the same fields is copied field by field,
// never through a record.
//
// A program that only walks or measures the array (lower_escape.go) is
// given the view in the frame's own slot; any other, a view of its own. A
// loop whose item is only read field by field loads the fields it reads of
// each item into registers, and one that collects its item as it is makes a
// view of the items it picked: no record is made per item, as the value
// flow proved nothing keeps one. Any other loop over a view makes a record
// per item.
// Run the same program through RunValues and it walks the records it is
// given.

// recordsView is an array of plain records in the host's memory: its items,
// how many, and the plan of the slice, whose element is a record every field
// of which loads in place (viewable).
type recordsView struct {
	data   unsafe.Pointer
	length int
	plan   *codec
	// perm, when set, is which item of the host's slice each item is: a
	// view the library sorted or cut, not the slice as it is. An int, so a
	// slice past 2^31 items cannot wrap to another item.
	perm []int
}

// viewable reports a slice plan a recordsView can carry: every field of its
// records is a plain bool, int, float or string, or an amount or a currency.
// Those two load as they are, never failing — the boundary checks their
// currencies, in the host's memory (units) — so a field loaded only when a
// record is asked for answers as one loaded at the boundary. A time is not
// one: one outside the years a time spans fails as it loads.
func viewable(plan *codec) bool {
	if plan.shape != shapeSlice || plan.elem.shape != shapeRecord {
		return false
	}
	for _, field := range plan.elem.fields {
		if field.plain == reflect.Invalid && !viewUnit(field.codec) {
			return false
		}
	}
	return true
}

// viewUnit reports a field a view loads by its codec: an amount or a
// currency.
func viewUnit(plan *codec) bool {
	return plan.shape == shapeOwn && (plan.goType == moneyGoType || plan.goType == currencyGoType)
}

// loadViewField loads a field of a view's item at p into slot: a plain one
// in place, an amount or a currency by loadUnit.
func loadViewField(field *fieldCodec, p unsafe.Pointer, slot *Value) {
	if field.plain == reflect.Invalid {
		*slot = loadUnit(field, p)
		return
	}
	loadPlain(field.plain, p, slot)
}

// loadUnit is an amount or a currency field at p, by loadOwn, which never
// fails on them; out of the way of plain fields.
//
//go:noinline
func loadUnit(field *fieldCodec, p unsafe.Pointer) Value {
	unit, _ := loadOwn(field.codec.goType, p)
	return unit
}

// loadUnitInto loads an amount or a currency field at p into register reg,
// out of the loop's way: a loop over plain fields reads none.
//
//go:noinline
func loadUnitInto(field *fieldCodec, p unsafe.Pointer, b *banks, reg int32) {
	b.regs[reg], _ = loadOwn(field.codec.goType, p)
}

// units hands check each amount and currency of the view, by item and
// field, read where it is in the host's memory: the boundary's walk, with
// no record made of an item.
func (v *recordsView) units(check func(item, field int, unit Value) error) error {
	plan := v.plan.elem
	for i := range v.length {
		item := v.item(i)
		for j := range plan.fields {
			field := &plan.fields[j]
			if field.plain != reflect.Invalid {
				continue
			}
			unit, _ := loadOwn(field.codec.goType, unsafe.Add(item, field.offset))
			if err := check(i, j, unit); err != nil {
				return err
			}
		}
	}
	return nil
}

// viewRecords makes view the host's slice at p, and is the array there:
// the frame's own view, when inPlace, and one of its own otherwise.
func viewRecords(view *recordsView, plan *codec, p unsafe.Pointer, inPlace bool) Value {
	header := (*sliceHeader)(p)
	if !inPlace {
		view = new(recordsView)
	}
	*view = recordsView{data: header.data, length: header.len, plan: plan}
	return viewed(view)
}

// item is where item i is in the host's memory.
func (v *recordsView) item(i int) unsafe.Pointer {
	if v.perm != nil {
		i = v.perm[i]
	}
	return unsafe.Add(v.data, uintptr(i)*v.plan.stride)
}

// loadInto loads the fields of item index a loop reads into their
// registers, regs[i] the register of fields[i], each in the file of its kind.
func (v *recordsView) loadInto(index int, fields, regs []int32, b *banks) {
	plan, item := v.plan.elem, v.item(index)
	for i, at := range fields {
		field := &plan.fields[at]
		if field.plain == reflect.Invalid {
			loadUnitInto(field, unsafe.Add(item, field.offset), b, regs[i])
			continue
		}
		loadPlainInto(field.plain, unsafe.Add(item, field.offset), b, regs[i])
	}
}

// equalTo reports two views of one plan whose items are equal field by
// field, as their records would be, read where they are: no record made.
func (v *recordsView) equalTo(other *recordsView) bool {
	fields := v.plan.elem.fields
	for i := range v.length {
		a, b := v.item(i), other.item(i)
		for j := range fields {
			var x, y Value
			loadViewField(&fields[j], unsafe.Add(a, fields[j].offset), &x)
			loadViewField(&fields[j], unsafe.Add(b, fields[j].offset), &y)
			if !x.Equal(y) {
				return false
			}
		}
	}
	return true
}

// record is item i as a record of its own.
func (v *recordsView) record(i int) Value {
	plan := v.plan.elem
	record := newRecord(&plan.typ, len(plan.fields))
	item := v.item(i)
	for j := range plan.fields {
		loadViewField(&plan.fields[j], unsafe.Add(item, plan.fields[j].offset), &record.fields[j])
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
		for j := range plan.fields {
			loadViewField(&plan.fields[j], unsafe.Add(item, plan.fields[j].offset), &record.fields[j])
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
	perm := make([]int, n)
	for i := range perm {
		j := at(i)
		if v.perm != nil {
			j = v.perm[j]
		}
		perm[i] = j
	}
	return &recordsView{data: v.data, length: n, plan: v.plan, perm: perm}
}

// storeView writes the view into the slice at p of plan, a slice of records
// of the same type: field by field from the host's memory, when every field
// is of the same Go type in both — a plain one of one kind, or an amount or
// a currency, which the boundary checked as the view came in and which reads
// back as itself — which is what a slice of the same struct is. It reports
// false for a slice it does not answer so.
func storeView(plan *codec, p unsafe.Pointer, v *recordsView) bool {
	from, to := v.plan.elem.fields, plan.elem.fields
	if !viewable(plan) || len(from) != len(to) {
		return false
	}
	for i := range from {
		if !copiedAlike(&from[i], &to[i]) {
			return false
		}
	}
	items := reflect.MakeSlice(plan.goType, v.length, v.length)
	data := items.UnsafePointer()
	for i := range v.length {
		source, target := v.item(i), unsafe.Add(data, uintptr(i)*plan.stride)
		for j := range from {
			copyField(&from[j], unsafe.Add(target, to[j].offset), unsafe.Add(source, from[j].offset))
		}
	}
	reflect.NewAt(plan.goType, noescape(p)).Elem().Set(items)
	return true
}

// copiedAlike reports two fields of views a field is copied between as it
// is: plain of one kind, or both the same unit's Go type.
func copiedAlike(from, to *fieldCodec) bool {
	if from.plain != reflect.Invalid || to.plain != reflect.Invalid {
		return from.plain == to.plain
	}
	return viewUnit(from.codec) && from.codec.goType == to.codec.goType
}

// copyField copies a field from source to target, a plain one by its kind
// and an amount or a currency whole.
func copyField(field *fieldCodec, target, source unsafe.Pointer) {
	switch {
	case field.plain != reflect.Invalid:
		copyPlain(field.plain, target, source)
	case field.codec.goType == moneyGoType:
		*(*money.Money)(target) = *(*money.Money)(source)
	default:
		*(*money.Currency)(target) = *(*money.Currency)(source)
	}
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
