package machine_test

import (
	"errors"
	"testing"

	"github.com/nethinwei/funroute/internal/compile"
	"github.com/nethinwei/funroute/internal/machine"
)

// Whether amounts meet is asked on every equality and membership that reads
// money, so the asking allocates nothing: not for an amount, an array or a
// dictionary of them, nor a record that holds one.
func TestMeetingAmountsDoesNotAllocate(t *testing.T) {
	registry := moneyRegistry(t)
	amounts := make([]machine.Value, 500)
	for i := range amounts {
		amounts[i] = machine.MoneyValue(int64(i), "USD")
	}
	xs, err := machine.Array(machine.MoneyType, amounts)
	if err != nil {
		t.Fatal(err)
	}
	entries := map[string]machine.Value{"a": machine.MoneyValue(1, "USD"), "b": machine.MoneyValue(2, "USD")}
	d, err := machine.Dict(machine.MoneyType, entries)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		source, contract string
		args             []machine.Value
	}{
		{"x == y", "x: money; y: money", []machine.Value{amounts[1], amounts[1]}},
		{"xs == ys", "xs: array<money>; ys: array<money>", []machine.Value{xs, xs}},
		{"x in ys", "x: money; ys: array<money>", []machine.Value{amounts[499], xs}},
		{"d == e", "d: dict<money>; e: dict<money>", []machine.Value{d, d}},
	} {
		artifact, err := compileMoney(t, registry, test.source, test.contract, "")
		if err != nil {
			t.Fatal(err)
		}
		runtime, err := machine.Instantiate(artifact, registry)
		if err != nil {
			t.Fatal(err)
		}
		ctx := t.Context()
		allocs := testing.AllocsPerRun(100, func() {
			if _, err := runtime.RunValues(ctx, test.args); err != nil {
				t.Fatal(err)
			}
		})
		if allocs != 0 {
			t.Errorf("%s allocated %v times, want 0", test.source, allocs)
		}
	}
}

// A dictionary whose amounts do not meet says which two in key order, the
// same every run, though the walk that finds they meet goes in the map's.
func TestADictionaryThatDoesNotMeetSaysSoInKeyOrder(t *testing.T) {
	t.Parallel()
	registry := moneyRegistry(t)
	entries := map[string]machine.Value{}
	for i, code := range []string{"USD", "USD", "EUR", "USD", "JPY", "USD", "EUR", "USD"} {
		entries[string(rune('a'+i))] = machine.MoneyValue(int64(i), code)
	}
	d, err := machine.Dict(machine.MoneyType, entries)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := compileMoney(t, registry, "d == e", "d: dict<money>; e: dict<money>", "")
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := machine.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	const want = `eq: entry "c": currency mismatch: cannot compare USD with EUR`
	for range 50 {
		_, err := runtime.RunValues(t.Context(), []machine.Value{d, d})
		if !errors.Is(err, machine.ErrCurrency) || err.Error() != want {
			t.Fatalf("d == e error = %v, want %s", err, want)
		}
	}
}

// The functions that compare by equality ask whether money meets without
// building the array's type: an array of ints costs them nothing for it —
// what is left is the answer, and the positions intersect and except keep.
func TestTheSetFunctionsBuildNoTypeToMeet(t *testing.T) {
	registry := machine.CoreRegistry()
	xs, err := machine.ToValue([]int64{1, 2, 2, 3, 5})
	if err != nil {
		t.Fatal(err)
	}
	ys, err := machine.ToValue([]int64{2, 5, 8})
	if err != nil {
		t.Fatal(err)
	}
	contract := compile.CompileOptions{Args: []compile.ArgSpec{{Name: "xs", Type: machine.ArrayOf(machine.IntType)}, {Name: "ys", Type: machine.ArrayOf(machine.IntType)}}}
	for source, want := range map[string]float64{"unique(xs)": 1, "intersect(xs, ys)": 2, "except(xs, ys)": 2} {
		artifact, err := compile.CompileExpr(source, registry, contract)
		if err != nil {
			t.Fatal(err)
		}
		runtime, err := machine.Instantiate(artifact, registry)
		if err != nil {
			t.Fatal(err)
		}
		ctx, args := t.Context(), []machine.Value{xs, ys}
		allocs := testing.AllocsPerRun(100, func() {
			if _, err := runtime.RunValues(ctx, args); err != nil {
				t.Fatal(err)
			}
		})
		if allocs != want {
			t.Errorf("%s allocated %v times, want %v", source, allocs, want)
		}
	}
}
