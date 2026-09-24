package machine

import (
	"reflect"
	"strings"
	"testing"
	"unsafe"
)

type narrowOrder struct {
	Amount int64 `funroute:"amount"`
}

type wideOrder struct {
	Amount int64  `funroute:"amount"`
	Note   string `funroute:"note"`
}

// Both bridges put a record into a struct by one rule: every field of the
// record needs a place, and a field of the struct the record does not have
// keeps its zero value. Nothing is dropped on the way.
func TestBothBridgesMatchStructFieldsByOneRule(t *testing.T) {
	t.Parallel()
	record, err := Record(RecordOf(FieldOf("amount", IntType), FieldOf("note", StringType)), []Value{Int(5), String("x")})
	if err != nil {
		t.Fatal(err)
	}
	if narrow, err := FromValue[narrowOrder](record); err == nil || !strings.Contains(err.Error(), `no field "note"`) {
		t.Fatalf("FromValue[narrowOrder](%v) = %v, %v, want the note refused rather than dropped", record.Any(), narrow, err)
	}
	if _, err := newCodecFor(nil, reflect.TypeFor[narrowOrder](), record.Type()); err == nil || !strings.Contains(err.Error(), `no field "note"`) {
		t.Fatalf("the codec of narrowOrder for %s = %v, want the same refusal", record.Type(), err)
	}
	amountOnly, err := Record(RecordOf(FieldOf("amount", IntType)), []Value{Int(5)})
	if err != nil {
		t.Fatal(err)
	}
	wide, err := FromValue[wideOrder](amountOnly)
	if err != nil || wide != (wideOrder{Amount: 5}) {
		t.Fatalf("FromValue[wideOrder](%v) = %v, %v, want {5 \"\"}", amountOnly.Any(), wide, err)
	}
	plan, err := newCodecFor(nil, reflect.TypeFor[wideOrder](), amountOnly.Type())
	if err != nil {
		t.Fatal(err)
	}
	var stored wideOrder
	if err := plan.store(unsafe.Pointer(&stored), amountOnly); err != nil || stored != wide {
		t.Fatalf("the codec stores %v, %v, want what FromValue gives, %v", stored, err, wide)
	}
}
