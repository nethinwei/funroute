package machine_test

import (
	"reflect"
	"testing"
	"time"
	"unsafe"

	"github.com/nethinwei/funroute/internal/machine"
	"github.com/nethinwei/funroute/internal/money"
)

// Each of the language's own Go types is written and read as itself, and a
// type with no case of its own is an error both ways, never another type's
// memory.
func TestEveryOwnGoTypeIsReadAndWrittenAsItself(t *testing.T) {
	t.Parallel()
	for _, own := range []any{
		machine.NewMoney("USD", 170), machine.NewRatio(1, 3), machine.NewCurrency("EUR"),
		money.FxRateFrom(&money.Pair{Base: "USD", Quote: "JPY"}, machine.NewRatio(150, 1)),
		time.Date(2026, 10, 21, 8, 0, 0, 0, time.UTC), 90 * time.Second,
	} {
		value, err := machine.ToValue(own)
		if err != nil {
			t.Fatal(err)
		}
		back, err := machine.OwnRoundTrip(reflect.TypeOf(own), value)
		if err != nil || !back.Equal(value) {
			t.Errorf("%T %v reads back as %v, %v", own, own, back.Any(), err)
		}
	}
	var other struct{ code string }
	if _, err := machine.LoadOwn(reflect.TypeOf(other), unsafe.Pointer(&other)); err == nil {
		t.Error("loadOwn read a type it has no case for")
	}
	if _, err := machine.OwnRoundTrip(reflect.TypeOf(other), machine.String("x")); err == nil {
		t.Error("storeOwn wrote a type it has no case for")
	}
}
