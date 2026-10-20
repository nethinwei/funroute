package machine

import (
	"reflect"
	"testing"
	"unsafe"

	"github.com/nethinwei/funroute/internal/money"
)

// Equality and the array primitives work the same on every backing.
func TestContainerOperationsSpanBackings(t *testing.T) {
	t.Parallel()
	native, _ := ToValue([]string{"a", "b"})
	built, _ := Array(StringType, []Value{String("a"), String("b")})
	if !native.Equal(built) {
		t.Fatal("equal arrays compare unequal")
	}
	if tail, ok := native.Slice(1, 2); !ok || tail.length() != 1 || !tail.at(0).Equal(String("b")) {
		t.Fatalf("Slice(1, 2) of [a b] = %v, %v, want [b]", tail.Any(), ok)
	}
	items, _ := built.Array()
	if len(items) != 2 || items[1].s != "b" {
		t.Fatalf("Array() of [a b] = %v, want [a b]", items)
	}
	dict, _ := ToValue(map[string]float64{"z": 1, "a": 2})
	if keys := dict.keys(); len(keys) != 2 || keys[0] != "a" {
		t.Fatalf("keys of {z: 1, a: 2} = %v, want [a z]", keys)
	}
}

// nativeSamples is one element of each native backing's Go type.
var nativeSamples = map[Kind]any{
	BoolKind: true, IntKind: int64(7), FloatKind: 1.5, StringKind: "a",
	MoneyKind: money.Make("USD", 5), FxRateKind: money.FxRateFrom(&money.Pair{Base: "USD", Quote: "JPY"}, money.RatioFromParts(601, 4)),
}

// natives is the one list of backings, and every switch that reads one
// answers for each entry: the builders build it, the operations read it,
// fromGo takes it as it is and the unsafe codecs hand it over without a
// copy. A backing added to the list and missed by any of them fails here.
func TestEveryBackingIsHandledEverywhere(t *testing.T) {
	t.Parallel()
	if nativeBoolMap != native(len(natives)) {
		t.Fatalf("the map natives start at %d, want right after the %d slices", nativeBoolMap, len(natives))
	}
	registry := declared(t, money.CurrencySpec{Code: "USD", Digits: 2}, money.CurrencySpec{Code: "JPY", Digits: 0})
	for i, entry := range natives {
		t.Run(entry.elem.String(), func(t *testing.T) {
			t.Parallel()
			sample := reflect.ValueOf(nativeSamples[entry.elem])
			item, err := fromGo(sample.Interface())
			if err != nil || item.kind != entry.elem || sample.Type() != entry.goElem {
				t.Fatalf("the sample %v is %v, %v, want a %s held as %s", sample, item, err, entry.elem, entry.goElem)
			}
			slice := reflect.Append(reflect.MakeSlice(reflect.SliceOf(entry.goElem), 0, 1), sample)
			checkBacking(t, registry, slice, ArrayOf(item.Type()), item, native(i))
			if entry.dict {
				dict := reflect.MakeMap(reflect.MapOf(reflect.TypeFor[string](), entry.goElem))
				dict.SetMapIndex(reflect.ValueOf("k"), sample)
				checkBacking(t, registry, dict, DictOf(item.Type()), item, native(i)+nativeBoolMap)
			}
		})
	}
}

// checkBacking holds one native container, holding item alone, to every
// place a backing is read.
func checkBacking(t *testing.T, registry *Registry, container reflect.Value, typ Type, item Value, want native) {
	t.Helper()
	value, err := fromGo(container.Interface())
	if err != nil || reflect.TypeOf(nativeAny(value)) != container.Type() || value.length() != 1 {
		t.Fatalf("fromGo(%s) = %T, %v, want the %s itself", container.Type(), nativeAny(value), err, container.Type())
	}
	one, ok := value.at(0), true
	if typ.kind == DictKind {
		one, ok = value.lookup("k")
		if keys := value.keys(); len(keys) != 1 || keys[0] != "k" {
			t.Fatalf("keys of %s = %v, want [k]", container.Type(), keys)
		}
		if packed := packDict(item.Type(), map[string]Value{"k": item}); reflect.TypeOf(packed.box) != container.Type() {
			t.Fatalf("packDict(%s) is a %T, want a %s", item.Type(), packed.box, container.Type())
		}
	} else {
		builder := newArrayBuilder(item.Type(), 1)
		builder.add(item)
		part, ok := value.Slice(0, 1)
		if built := builder.finish(); reflect.TypeOf(nativeAny(built)) != container.Type() || !ok || reflect.ValueOf(nativeAny(part)).UnsafePointer() != container.UnsafePointer() {
			t.Fatalf("the builder of %s makes a %T, want a %s", item.Type(), nativeAny(built), container.Type())
		}
		if moved := rearranged(value, 1, func(int) int { return 0 }); reflect.TypeOf(nativeAny(moved)) != container.Type() || !moved.at(0).Equal(item) {
			t.Fatalf("rearranged(%s) is a %T holding %v, want a %s holding %v", container.Type(), nativeAny(moved), moved.at(0), container.Type(), item)
		}
		checkArena(t, item)
	}
	if !ok || !one.Equal(item) {
		t.Fatalf("the item of %s reads %v, want %v", container.Type(), one, item)
	}
	if copied := owned(value); reflect.ValueOf(nativeAny(copied)).UnsafePointer() == container.UnsafePointer() || !copied.Equal(value) {
		t.Fatalf("owned(%s) shares its memory or differs from it", container.Type())
	}
	checkCodec(t, registry, container, typ, want)
}

// checkArena holds an array of item's kind, when a frame's slot may hold
// one, to the slot's builder: the item reads back as it went in.
func checkArena(t *testing.T, item Value) {
	t.Helper()
	if !arenaElem(item.kind) {
		return
	}
	var slot arenaSlot
	builder := builderIn(&slot, item.Type(), 1)
	builder.add(item)
	built := finishIn(&slot, &builder)
	if held := slotArray(built); built.length() != 1 || held.length() != 1 || !held.at(0).Equal(item) {
		t.Fatalf("an arena array of %s is a %T, want the slot's slice holding %v", item.Type(), built.box, item)
	}
}

// slotArray is an arena array as the native array its slot's slice is; the
// zero Value for a box no slot holds.
func slotArray(v Value) Value {
	switch box := v.box.(type) {
	case *[]bool:
		return arrayOf(*box)
	case *[]int64:
		return arrayOf(*box)
	case *[]float64:
		return arrayOf(*box)
	case *[]string:
		return arrayOf(*box)
	}
	return Value{}
}

// checkCodec plans a codec for the container's Go type and has it load the
// container and store it back without a copy.
func checkCodec(t *testing.T, registry *Registry, container reflect.Value, typ Type, want native) {
	t.Helper()
	plan, err := newCodecFor(registry, container.Type(), typ)
	if err != nil || plan.shape != shapeNative || plan.native != want {
		t.Fatalf("the codec of %s = %v, %v, want native backing %d", container.Type(), plan, err, want)
	}
	in := reflect.New(container.Type())
	in.Elem().Set(container)
	loaded, err := plan.load(unsafe.Pointer(in.Pointer()))
	if err != nil {
		t.Fatalf("load(%s) error = %v", container.Type(), err)
	}
	out := reflect.New(container.Type())
	if err := plan.store(unsafe.Pointer(out.Pointer()), loaded); err != nil || out.Elem().UnsafePointer() != container.UnsafePointer() {
		t.Fatalf("store(load(%s)) = %v, want the same backing back", container.Type(), err)
	}
}
