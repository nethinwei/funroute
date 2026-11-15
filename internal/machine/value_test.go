package machine

import (
	"encoding/json"
	"fmt"
	"math"
	"testing"
	"time"
	"unsafe"

	"github.com/nethinwei/funroute/internal/money"
)

// identical reports two values that are one value, as two ways of computing
// it must agree: Equal, except that a NaN is the NaN it is — Equal, as ==,
// holds no float equal to a NaN — and 0 and -0 are told apart.
func identical(a, b Value) bool {
	if a.kind == FloatKind && b.kind == FloatKind {
		return math.Float64bits(a.f) == math.Float64bits(b.f) || a.f != a.f && b.f != b.f
	}
	return a.Equal(b) || a.kind == b.kind && fmt.Sprint(a.Any()) == fmt.Sprint(b.Any())
}

// What the VM builds has the same backing as what a host supplies, so an
// extension sees a []float64 whichever way the array was made.
func TestBuiltArraysUseTheNativeBacking(t *testing.T) {
	t.Parallel()
	built, err := Array(FloatType, []Value{Float(1), Float(2)})
	if err != nil {
		t.Fatal(err)
	}
	floats, err := FromValue[[]float64](built)
	if err != nil || len(floats) != 2 || floats[1] != 2 {
		t.Fatalf("FromValue[[]float64](Array(float, [1 2])) = %v, %v, want [1 2], nil", floats, err)
	}
	dict, err := Dict(IntType, map[string]Value{"a": Int(1)})
	if err != nil {
		t.Fatal(err)
	}
	ints, err := FromValue[map[string]int64](dict)
	if err != nil || ints["a"] != 1 {
		t.Fatalf("FromValue[map[string]int64](Dict(int, {a: 1})) = %v, %v, want map[a:1], nil", ints, err)
	}
	// A container of containers is the one shape that needs a []Value.
	matrix, err := ToValue([][]float64{{1, 2}, {3}})
	if err != nil {
		t.Fatal(err)
	}
	rows, err := FromValue[[][]float64](matrix)
	if err != nil || len(rows) != 2 || rows[1][0] != 3 {
		t.Fatalf("FromValue[[][]float64](ToValue([[1 2] [3]])) = %v, %v, want [[1 2] [3]], nil", rows, err)
	}
	if !matrix.hasType(ArrayOf(ArrayOf(FloatType))) {
		t.Fatalf("ToValue([[1 2] [3]]) type = %s, want array<array<float>>", matrix.Type())
	}
}

// A float that is not finite is a float like any other: the constructors
// pack it as they pack the rest.
func TestValueConstructorsHoldNonFiniteFloats(t *testing.T) {
	t.Parallel()
	array, err := Array(FloatType, []Value{Float(math.Inf(1))})
	if items, _ := FromValue[[]float64](array); err != nil || len(items) != 1 || !math.IsInf(items[0], 1) {
		t.Fatalf("Array([+Inf]) = %v, %v; want [+Inf]", items, err)
	}
	dict, err := Dict(FloatType, map[string]Value{"risk": Float(math.NaN())})
	if entries, _ := FromValue[map[string]float64](dict); err != nil || !math.IsNaN(entries["risk"]) {
		t.Fatalf("Dict({risk: NaN}) = %v, %v; want {risk: NaN}", entries, err)
	}
}

// An empty container writes [] or {}, whether its backing is empty or nil —
// a host function may well answer nil — so its JSON reads back as an
// argument; inside another container too.
func TestAnEmptyContainerWritesItsEmptyJSON(t *testing.T) {
	t.Parallel()
	nested, err := ToValue([][]float64{nil})
	if err != nil {
		t.Fatal(err)
	}
	for want, input := range map[string]any{"[]": []float64(nil), "{}": map[string]int64(nil), "[[]]": nested} {
		value, ok := input.(Value)
		if !ok {
			if value, err = fromGo(input); err != nil {
				t.Fatal(err)
			}
		}
		if encoded, err := json.Marshal(value); err != nil || string(encoded) != want {
			t.Errorf("json.Marshal(%#v) = %s, %v, want %s", input, encoded, err, want)
		}
	}
}

// Whether two arrays are equal does not hang on whether they share a backing:
// a NaN is equal to nothing, itself included, in an array as alone, so an
// array of one is not equal to itself either; an array of anything else is.
func TestArrayEqualityDoesNotDependOnBacking(t *testing.T) {
	t.Parallel()
	nan := arrayOf([]float64{math.NaN()})
	copied := arrayOf([]float64{math.NaN()})
	ints := arrayOf([]int64{1, 2})
	for name, test := range map[string]struct {
		left, right Value
		want        bool
	}{
		"a NaN's array and itself":     {nan, nan, false},
		"a NaN's array and a copy":     {nan, copied, false},
		"an int array and itself":      {ints, ints, true},
		"an int array and a copy":      {ints, arrayOf([]int64{1, 2}), true},
		"nested, a NaN's and itself":   {nestedOf(ArrayOf(FloatType), []Value{nan}), nestedOf(ArrayOf(FloatType), []Value{nan}), false},
		"a float array of no NaN, one": {arrayOf([]float64{1.5}), arrayOf([]float64{1.5}), true},
	} {
		if got, err := compareEqual(test.left, test.right); err != nil || got.b != test.want || test.left.Equal(test.right) != test.want {
			t.Errorf("%s: == is %v, %v and Equal %v, want %v", name, got.b, err, test.left.Equal(test.right), test.want)
		}
	}
}

// Every kind of value goes through the switches a value meets — its type,
// equality, JSON and back as an artifact's constant, its Go form and back —
// and comes out as itself. A kind with no sample here fails, so a new one is
// taken through all of them before it is used.
func TestEveryKindOfValueRoundTrips(t *testing.T) {
	t.Parallel()
	// Kinds that are types only: no value has them.
	typesOnly := map[Kind]string{
		InvalidKind: "no type", VarKind: "an open type", EnumKind: "an enum's values are strings",
	}
	samples := kindSamples(t)
	for _, kind := range Kinds() {
		value, ok := samples[kind]
		if _, only := typesOnly[kind]; only == ok {
			t.Errorf("%s: a sample %v and a type only %v; want exactly one", kind, ok, only)
			continue
		}
		if ok {
			expectRoundTrips(t, kind, value)
		}
	}
}

func expectRoundTrips(t *testing.T, kind Kind, value Value) {
	t.Helper()
	if got := value.Type().Kind(); got != kind {
		t.Errorf("%s sample's type is %s", kind, got)
	}
	if kind == HandleKind {
		return // a handle is equal to nothing and has no JSON: it is the host's
	}
	constant, ok := ConstantOf(value, value.Type())
	back, err := constant.value()
	if !ok || err != nil || !back.Equal(value) {
		t.Errorf("%s sample as a constant %s reads back as %v, %v", kind, constant.Value, back.Any(), err)
	}
	again, err := coerce(value.Any(), value.Type())
	if err != nil || !again.Equal(value) {
		t.Errorf("%s sample's Go form %v reads back as %v, %v", kind, value.Any(), again.Any(), err)
	}
}

// kindSamples is a value of every kind a value can have.
func kindSamples(t *testing.T) map[Kind]Value {
	t.Helper()
	of := func(input any) Value {
		value, err := ToValue(input)
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	record, err := Record(RecordOf(FieldOf("a", IntType)), []Value{Int(1)})
	if err != nil {
		t.Fatal(err)
	}
	rate, err := money.ReadFxRate("USD", "JPY", "150.25")
	if err != nil {
		t.Fatal(err)
	}
	return map[Kind]Value{
		BoolKind: Bool(true), IntKind: Int(-3), FloatKind: Float(0.5), StringKind: String("x"),
		ArrayKind: of([]int64{1, 2}), DictKind: of(map[string]int64{"a": 1}), RecordKind: record,
		HandleKind: NewHandle("demo.engine", struct{}{}), MoneyKind: MoneyValue(170, "USD"),
		RatioKind: RatioValue(money.RatioFromParts(29, 1000)), FxRateKind: FxRateValue(rate),
		CurrencyKind: CurrencyValue("EUR"), TimeKind: of(time.Date(2026, 10, 21, 8, 0, 0, 0, time.UTC)),
		DurationKind: Duration(90 * time.Second),
	}
}

// A Value is passed and kept by value wherever the machine runs, so its size
// is part of how fast it runs. Money's places sit in the padding after kind
// and b: a Value is the size it would be without them, on every platform. A
// field that does not fit the padding fails here.
func TestMoneysPlacesCostAValueNothing(t *testing.T) {
	t.Parallel()
	type withoutPlaces struct { // Value's fields, p left out
		_ Kind
		_ bool
		_ int64
		_ float64
		_ string
		_ any
	}
	if got, want := unsafe.Sizeof(Value{}), unsafe.Sizeof(withoutPlaces{}); got != want {
		t.Fatalf("a Value is %d bytes, %d without money's places", got, want)
	}
}
