package machine_test

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/nethinwei/funroute/internal/compile"
	"github.com/nethinwei/funroute/internal/machine"
	"github.com/nethinwei/funroute/internal/money"
)

// A handle flows from one model into the next: the second receives the very
// pointer the first returned, and the language cannot compare it.
func TestHandlesPassBetweenModelsUntouched(t *testing.T) {
	t.Parallel()
	var single, batched atomic.Int64
	registry := modelRegistry(t, &single, &batched)
	artifact, err := compile.CompileExpr(`let(e = model.embed_v1(features), model.fraud_v1(e))`, registry, compile.CompileOptions{
		Args: []compile.ArgSpec{{Name: "features", Type: machine.ArrayOf(machine.FloatType)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !artifact.Result().Equal(machine.FloatType) {
		t.Fatalf("result = %s, want float", artifact.Result())
	}
	runtime, err := machine.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	features := []float64{0.75, 0.1}
	value, _ := machine.ToValue(features)
	result, err := runtime.RunValues(t.Context(), []machine.Value{value})
	if err != nil {
		t.Fatal(err)
	}
	if score, _ := result.Float(); score != 0.75 {
		t.Fatalf("score = %v, want 0.75", score)
	}
	assertHandlesAreOpaque(t, registry)
}

// assertHandlesAreOpaque checks that equality on a handle is refused, and a
// handle type only unifies with itself.
func assertHandlesAreOpaque(t *testing.T, registry *machine.Registry) {
	t.Helper()
	handle := machine.HandleOf("demo.embedding")
	eq, err := compile.CompileExpr(`e == f`, registry, compile.CompileOptions{Args: []compile.ArgSpec{{Name: "e", Type: handle}, {Name: "f", Type: handle}}})
	if err != nil {
		t.Fatal(err)
	}
	rt, err := machine.Instantiate(eq, registry)
	if err != nil {
		t.Fatal(err)
	}
	h := machine.NewHandle("demo.embedding", &embedding{})
	if _, err := rt.RunValues(t.Context(), []machine.Value{h, h}); err == nil || !strings.Contains(err.Error(), "cannot be compared") {
		t.Fatalf("handle equality error = %v, want cannot be compared", err)
	}
	_, err = compile.CompileExpr(`model.fraud_v1(x)`, registry, compile.CompileOptions{Args: []compile.ArgSpec{{Name: "x", Type: machine.HandleOf("other.thing")}}})
	if err == nil || !strings.Contains(err.Error(), "handle<other.thing>") {
		t.Fatalf("wrong handle error = %v, want one naming handle<other.thing>", err)
	}
	if got, err := machine.ParseType(" handle<demo.embedding> "); err != nil || !got.Equal(handle) {
		t.Fatalf("ParseType(%q) = %s, %v, want %s", " handle<demo.embedding> ", got, err, handle)
	}
}

// gridTotal has the shape Go must read: a context, several parameters of
// Go's other scalar kinds, containers nested to any depth, a map result.
func gridTotal(ctx context.Context, rows [][]int32, weights map[string][]float64, scale int) (map[string]float64, error) {
	if ctx == nil {
		return nil, errors.New("no context")
	}
	out := map[string]float64{}
	for key, ws := range weights {
		for i, row := range rows {
			for _, cell := range row {
				out[key] += float64(cell) * ws[i%len(ws)] * float64(scale)
			}
		}
	}
	return out, nil
}

// Go reads any Go signature.
func TestGoReflectsArbitrarySignatures(t *testing.T) {
	t.Parallel()
	registry := machine.CoreRegistry()
	if err := registry.Register(machine.FunctionSpec{Name: "grid.total_v1", Go: gridTotal}); err != nil {
		t.Fatal(err)
	}
	function := registry.Overloads("grid.total_v1")[0]
	if got, want := function.Signature(), "grid.total_v1(array<array<int>>,dict<array<float>>,int)->dict<float>"; got != want {
		t.Fatalf("signature = %s, want %s", got, want)
	}
	artifact, err := compile.CompileExpr(`grid.total_v1(rows, weights, 2)`, registry, compile.CompileOptions{Args: []compile.ArgSpec{
		{Name: "rows", Type: machine.ArrayOf(machine.ArrayOf(machine.IntType))},
		{Name: "weights", Type: machine.DictOf(machine.ArrayOf(machine.FloatType))},
	}})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := machine.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	result, err := runtime.Run(t.Context(), map[string]any{
		"rows":    []any{[]any{1.0, 2.0}, []any{3.0}},
		"weights": map[string]any{"a": []any{1.0, 10.0}},
	})
	if err != nil {
		t.Fatal(err)
	}
	totals, err := machine.FromValue[map[string]float64](result)
	if want := float64((1+2)*1*2 + 3*10*2); err != nil || totals["a"] != want {
		t.Fatalf("totals = %v, %v, want a: %v", totals, err, want)
	}
}

// BenchmarkGoCall is the price of reflection at the boundary, next to the
// kernel's typed add in BenchmarkCall.
func BenchmarkGoCall(b *testing.B) {
	registry := benchRegistry(b)
	if err := registry.Register(machine.FunctionSpec{
		Name: "host.add_v1",
		Go:   func(a, c int64) (int64, error) { return a + c, nil },
	}); err != nil {
		b.Fatal(err)
	}
	artifact, err := compile.CompileExpr(`host.add_v1(a, b)`, registry, compile.CompileOptions{})
	if err != nil {
		b.Fatal(err)
	}
	runtime, err := machine.Instantiate(artifact, registry)
	if err != nil {
		b.Fatal(err)
	}
	ctx := b.Context()
	args := []machine.Value{machine.Int(7), machine.Int(3)}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := runtime.RunValues(ctx, args); err != nil {
			b.Fatal(err)
		}
	}
}

// quoteArgs is what fees.quote_v1 was last handed, for the test to look at.
type quoteArgs struct {
	items []money.Money
}

// Go reads the money types' Go forms as money, their units unknown, and
// hands an array of money over without a copy.
func TestGoTakesAndGivesMoney(t *testing.T) {
	t.Parallel()
	registry := moneyRegistry(t)
	var seen quoteArgs
	quote := func(m money.Money, r money.Ratio, c money.Currency, xs []money.Money) (money.Money, error) {
		seen.items = xs
		converted, err := m.MulRatio(r, money.RoundDown)
		return machine.NewMoney(c.Code(), converted.Minor()+int64(len(xs))), err
	}
	if err := registry.Register(machine.FunctionSpec{Name: "fees.quote_v1", Go: quote}); err != nil {
		t.Fatal(err)
	}
	function := registry.Overloads("fees.quote_v1")[0]
	if got, want := function.Signature(), "fees.quote_v1(money,ratio,currency,array<money>)->money"; got != want {
		t.Fatalf("signature = %s, want %s", got, want)
	}
	items := []money.Money{machine.NewMoney("EUR", 1), machine.NewMoney("EUR", 2)}
	args := []machine.Value{
		machine.MoneyValue(1000, "EUR"), ratioValue(t, "0.5"),
		machine.CurrencyValue("USD"), moneyValues(t, items),
	}
	artifact, err := compileMoney(t, registry, "fees.quote_v1(a, r, c, xs)", "a: money; r: ratio; c: currency; xs: array<money>", "")
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := machine.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	value, err := runtime.RunValues(t.Context(), args)
	if got, _ := value.Money(); err != nil || got != (machine.NewMoney("USD", 502)) {
		t.Fatalf("fees.quote_v1 = %v, %v, want USD 5.02", got, err)
	}
	if len(seen.items) != 2 || &seen.items[0] != &items[0] {
		t.Fatal("the array of money was copied on its way to the function")
	}
	if err := machine.CoreRegistry().Register(machine.FunctionSpec{Name: "fees.quote_v1", Go: quote}); err == nil || !strings.Contains(err.Error(), "declares no money") {
		t.Fatalf("Register with money before DeclareMoney: error = %v, want declares no money", err)
	}
}

// A Go function may return its result alone; one that returns an error too
// has the error reported. Go takes the place of a written signature, so a
// spec gives one or the other, a batch form needs its single form, and a
// signature the boundary cannot carry is refused.
func TestGoReturnsAResultWithOrWithoutAnError(t *testing.T) {
	t.Parallel()
	registry := machine.CoreRegistry()
	for name, fn := range map[string]any{
		"double_v1": func(x int64) int64 { return 2 * x },
		"halve_v1":  func(x int64) (int64, error) { return x / 2, nil },
	} {
		if err := registry.Register(machine.FunctionSpec{Name: "g." + name, Go: fn}); err != nil {
			t.Fatalf("Register(g.%s) error = %v", name, err)
		}
	}
	artifact, err := compile.CompileExpr(`g.double_v1(g.halve_v1(10))`, registry, compile.CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := machine.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := runtime.RunValues(t.Context(), nil); err != nil || got.Any() != int64(10) {
		t.Fatalf("g.double_v1(g.halve_v1(10)) = %v, %v, want 10", got.Any(), err)
	}
	// What the boundary cannot express is refused at registration, not at
	// the first call.
	for name, test := range map[string]struct {
		spec machine.FunctionSpec
		want string
	}{
		"a map keyed by int": {machine.FunctionSpec{Name: "g.keys_v1", Go: func(map[int]string) int { return 0 }}, "keys must be strings"},
		"two results":        {machine.FunctionSpec{Name: "g.pair_v1", Go: func(x int) (int, int) { return x, x }}, "a result, or (result, error)"},
		"a batch of another type": {machine.FunctionSpec{
			Name: "g.mismatch_v1", Go: func(x int) int { return x }, GoBatch: func([]string) []int { return nil },
		}, "must be []int"},
		"Go beside Eval":     {machine.FunctionSpec{Name: "g.both_v1", Go: func(int64) int64 { return 0 }, Eval: machine.CoreRegistry().Overloads("add")[0].Eval}, "Go takes the place of"},
		"Go beside a Result": {machine.FunctionSpec{Name: "g.typed_v1", Go: func(int64) int64 { return 0 }, Result: machine.IntType}, "Go takes the place of"},
		"GoBatch alone":      {machine.FunctionSpec{Name: "g.batch_v1", GoBatch: func([]int64) []int64 { return nil }}, "GoBatch needs Go"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if err := machine.CoreRegistry().Register(test.spec); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Register(%s) error = %v, want one containing %q", test.spec.Name, err, test.want)
			}
		})
	}
}
