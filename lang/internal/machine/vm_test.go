package machine_test

import (
	"errors"
	"fmt"
	"slices"
	"testing"

	"funroute/lang/internal/compile"
	"funroute/lang/internal/machine"
)

// Instructions that carry no operands must still be rejected when they are
// malformed, and the table is where that check now lives.
func TestInstructionValidationUsesTheTable(t *testing.T) {
	t.Parallel()
	artifact := machine.ArtifactWith(machine.ArtifactParts{Instructions: make([]machine.Instruction, 3)})
	for _, test := range []struct {
		name        string
		instruction machine.Instruction
		wantError   bool
	}{
		{"unknown opcode", machine.Instruction{Op: machine.OpCode(200)}, true},
		{"invalid opcode", machine.Instruction{Op: machine.OpInvalid}, true},
		{"constant out of range", machine.Instruction{Op: machine.OpConstant, A: 7}, true},
		{"jump past the end", machine.Instruction{Op: machine.OpJump, A: 99}, true},
		{"fallback handler past the end", machine.Instruction{Op: machine.OpBeginFallback, A: 99}, true},
		{"jump to the end is the normal exit", machine.Instruction{Op: machine.OpJump, A: 3}, false},
		{"equal takes no operands", machine.Instruction{Op: machine.OpEqual}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := machine.ValidateInstruction(0, test.instruction, artifact)
			if (err != nil) != test.wantError {
				t.Fatalf("validateInstruction(0, %+v) error = %v, want error = %v", test.instruction, err, test.wantError)
			}
		})
	}
}

// benchRegistry is the full language: kernel, list primitives and every form.
func benchRegistry(b *testing.B) *machine.Registry {
	b.Helper()
	registry := machine.CoreRegistry()
	if err := registry.EnableForm(machine.SwitchForm, machine.ForForm, machine.ReduceForm); err != nil {
		b.Fatal(err)
	}
	return registry
}

func benchRuntime(b *testing.B, source string, contract []compile.ArgSpec) *machine.Runtime {
	b.Helper()
	registry := benchRegistry(b)
	artifact, err := compile.CompileExpr(source, registry, compile.CompileOptions{Args: contract, MaxInstructions: 100_000})
	if err != nil {
		b.Fatal(err)
	}
	runtime, err := machine.Instantiate(artifact, registry)
	if err != nil {
		b.Fatal(err)
	}
	return runtime
}

func run(b *testing.B, runtime *machine.Runtime, args map[string]any, fuel uint64) {
	b.Helper()
	ctx := b.Context()
	b.ReportAllocs()
	for b.Loop() {
		if _, err := runtime.Run(ctx, args, machine.RunOptions{Fuel: fuel}); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkDispatch is the interpreter loop with no containers involved: a
// deeply nested arithmetic expression.
func BenchmarkDispatch(b *testing.B) {
	source := "a"
	for range 200 {
		source = "add(" + source + ",b)"
	}
	runtime := benchRuntime(b, source, nil)
	run(b, runtime, map[string]any{"a": 1, "b": 2}, 10_000_000)
}

// BenchmarkNestedComprehension builds an array from an array, which is where
// the loop opcodes and value copying show up.
func BenchmarkNestedComprehension(b *testing.B) {
	items := make([]any, 500)
	for i := range items {
		items[i] = i
	}
	runtime := benchRuntime(b, `[add(x,1) for x in [mul(y,2) for y in items]]`,
		[]compile.ArgSpec{{Name: "items", Type: machine.ArrayOf(machine.IntType)}})
	run(b, runtime, map[string]any{"items": items}, 10_000_000)
}

// BenchmarkReduce is the same sum through the fold form.
func BenchmarkReduce(b *testing.B) {
	items := make([]any, 500)
	for i := range items {
		items[i] = i
	}
	runtime := benchRuntime(b, `reduce(item in items, total = 0, add(total,item))`,
		[]compile.ArgSpec{{Name: "items", Type: machine.ArrayOf(machine.IntType)}})
	run(b, runtime, map[string]any{"items": items}, 10_000_000)
}

// BenchmarkFor builds a new array, exercising the collect path.
func BenchmarkFor(b *testing.B) {
	items := make([]any, 500)
	for i := range items {
		items[i] = i
	}
	runtime := benchRuntime(b, `[add(item,1) for item in items]`,
		[]compile.ArgSpec{{Name: "items", Type: machine.ArrayOf(machine.IntType)}})
	run(b, runtime, map[string]any{"items": items}, 10_000_000)
}

// BenchmarkCall measures the extension-call boundary.
func BenchmarkCall(b *testing.B) {
	runtime := benchRuntime(b, `add(mul(a,b),sub(a,b))`, nil)
	run(b, runtime, map[string]any{"a": 7, "b": 3}, 1_000)
}

func TestBenchSanity(t *testing.T) {
	t.Parallel()
	registry := machine.CoreRegistry()
	if err := registry.EnableForm(machine.ReduceForm); err != nil {
		t.Fatal(err)
	}
	artifact, err := compile.CompileExpr(`reduce(x in items, total = 0, add(total,x))`, registry, compile.CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := machine.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	value, err := runtime.Run(t.Context(), map[string]any{"items": []any{1, 2, 3, 4, 5}}, machine.RunOptions{Fuel: 1_000})
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := value.Int(); got != 15 {
		t.Fatalf("sum = %v, want 15", value.Any())
	}
	// The accumulator is local to reduce, so it never reaches the signature.
	if len(artifact.Args()) != 1 || artifact.Args()[0].Name() != "items" {
		t.Fatalf("args = %#v, want only items", artifact.Args())
	}
}

// The two ways a host can run the same artifact. Run converts by name, which
// is what a console does with a filled form; RunValues takes typed values in
// ABI order, which is what a service does on the hot path.
func BenchmarkRunPaths(b *testing.B) {
	registry := benchRegistry(b)
	artifact, err := compile.CompileExpr(
		`if(country == "SG", amount * 2, amount)`,
		registry,
		compile.CompileOptions{Args: []compile.ArgSpec{
			{Name: "country", Type: machine.StringType},
			{Name: "amount", Type: machine.IntType},
		}},
	)
	if err != nil {
		b.Fatal(err)
	}
	runtime, err := machine.Instantiate(artifact, registry)
	if err != nil {
		b.Fatal(err)
	}

	b.Run("Run/byName", func(b *testing.B) {
		args := map[string]any{"country": "SG", "amount": 1000}
		ctx := b.Context()
		b.ReportAllocs()
		for b.Loop() {
			if _, err := runtime.Run(ctx, args, machine.RunOptions{Fuel: 1000}); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("RunValues/byOrder", func(b *testing.B) {
		args := []machine.Value{machine.String("SG"), machine.Int(1000)}
		ctx := b.Context()
		b.ReportAllocs()
		for b.Loop() {
			if _, err := runtime.RunValues(ctx, args, machine.RunOptions{Fuel: 1000}); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("Program/typed", func(b *testing.B) { benchTyped(b, registry, artifact) })
}

// benchTyped is the same artifact through a Program: the host's struct in,
// an int64 out.
func benchTyped(b *testing.B, registry *machine.Registry, artifact *machine.Artifact) {
	b.Helper()
	binding, err := compile.Bind[scalarIn, int64](registry)
	if err != nil {
		b.Fatal(err)
	}
	program, err := binding.Load(artifact)
	if err != nil {
		b.Fatal(err)
	}
	ctx := b.Context()
	b.ReportAllocs()
	for b.Loop() {
		in := scalarIn{Country: "SG", Amount: 1000}
		if _, err := program.Run(ctx, &in, machine.RunOptions{Fuel: 1000}); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkVectorPassThrough is the deep-learning shape: a feature vector goes
// from the host through the VM into an extension and a score comes back. The
// three sizes must print the same ns/op — nothing along the way converts,
// copies, or even reads the elements. The allocations that remain are
// reflect.Call's, because this extension is registered with Logic; they are
// the same count at every size, and a FunctionSpec has none.
func BenchmarkVectorPassThrough(b *testing.B) {
	for _, size := range []int{16, 1024, 65536} {
		b.Run(fmt.Sprintf("n=%d", size), func(b *testing.B) { benchVector(b, size) })
	}
}

func benchVector(b *testing.B, size int) {
	b.Helper()
	features := make([]float64, size)
	for i := range features {
		features[i] = float64(i) / float64(size)
	}
	registry := benchRegistry(b)
	err := machine.Logic(registry, "model.score_v1", machine.Doc{Cost: 10}, func(xs []float64) (float64, error) {
		return xs[0] + xs[len(xs)-1], nil
	})
	if err != nil {
		b.Fatal(err)
	}
	artifact, err := compile.CompileExpr(`model.score_v1(features)`, registry, compile.CompileOptions{
		Args: []compile.ArgSpec{{Name: "features", Type: machine.ArrayOf(machine.FloatType)}},
	})
	if err != nil {
		b.Fatal(err)
	}
	runtime, err := machine.Instantiate(artifact, registry)
	if err != nil {
		b.Fatal(err)
	}
	value, err := machine.ToValue(features)
	if err != nil {
		b.Fatal(err)
	}
	ctx := b.Context()
	args := []machine.Value{value}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := runtime.RunValues(ctx, args, machine.RunOptions{Fuel: 1000}); err != nil {
			b.Fatal(err)
		}
	}
}

// A frame makes its locals before running, so an artifact that claims more
// than its instructions could write is refused on load, digest or not.
func TestAnArtifactClaimingTooManyLocalsIsRefused(t *testing.T) {
	t.Parallel()
	registry := machine.CoreRegistry()
	artifact, err := compile.CompileExpr(`let(a = x + 1, a * a)`, registry, compile.CompileOptions{Args: []compile.ArgSpec{{Name: "x", Type: machine.IntType}}})
	if err != nil {
		t.Fatal(err)
	}
	parts := machine.PartsOf(artifact)
	for _, locals := range []int{-1, 1 << 40} {
		parts.Locals = locals
		tampered, err := machine.SealArtifact(parts, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := machine.Instantiate(tampered, registry); err == nil {
			t.Errorf("Instantiate(an artifact claiming %d locals) = nil error, want a refusal", locals)
		}
	}
}

// forgedLoad compiles source, lets forge rewrite the artifact's parts, seals
// them again — a correct digest proves nothing about who sealed them — and
// loads the result.
func forgedLoad(t *testing.T, source, contract string, forge func(*machine.ArtifactParts)) error {
	t.Helper()
	registry := moneyRegistry(t)
	artifact, err := compileMoney(t, registry, source, contract, "")
	if err != nil {
		t.Fatalf("CompileExpr(%q) error = %v", source, err)
	}
	parts := machine.PartsOf(artifact)
	forge(&parts)
	forged, err := machine.SealArtifact(parts, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = machine.Instantiate(forged, registry)
	return err
}

// Instantiate holds an artifact's constants to the registry's currencies as
// the boundary holds arguments: an amount with no currency that is not zero,
// an undeclared currency — alone, in an exchange rate, in an array — are
// refused at load, not left for the program to meet.
func TestInstantiateRefusesForgedMoneyConstants(t *testing.T) {
	t.Parallel()
	text := func(s string) *string { return &s }
	for _, test := range []struct {
		name, source, contract string
		forge                  func(*machine.ArtifactParts)
	}{
		{"an amount with no currency", "a + USD 0.05", "a: money<USD>", func(p *machine.ArtifactParts) { p.Constants[0].String = text("") }},
		{"an undeclared currency", "a + USD 0.05", "a: money<USD>", func(p *machine.ArtifactParts) { p.Constants[0].String = text("XYZ") }},
		{"a rate in an undeclared currency", "using(150 JPY / USD, a -> JPY)", "a: money<USD>", func(p *machine.ArtifactParts) { p.Constants[0].String = text("XYZ") }},
		{"an item of an array", "[USD 0.05, USD 0.10]", "", func(p *machine.ArtifactParts) { p.Constants[0].Items[1].String = text("XYZ") }},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if err := forgedLoad(t, test.source, test.contract, test.forge); err == nil {
				t.Fatalf("Instantiate of %s with %s = nil, want a refusal", test.source, test.name)
			}
		})
	}
	if err := forgedLoad(t, "a + USD 0.05", "a: money<USD>", func(*machine.ArtifactParts) {}); err != nil {
		t.Fatalf("Instantiate of an honest artifact = %v", err)
	}
}

// A run takes a rate table of the registry's currencies whatever its default
// rounding: the rates convert amounts, and the amounts are the same. A table
// of other currencies or other places is ErrCurrency.
func TestARunTakesARateTableOfTheSameCurrencies(t *testing.T) {
	t.Parallel()
	registry := moneyRegistry(t)
	spec, _ := registry.Money()
	artifact, err := compileMoney(t, registry, "a -> JPY", "a: money<USD>", "")
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := machine.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	for name, test := range map[string]struct {
		rounding machine.Rounding
		edit     func([]machine.CurrencySpec) []machine.CurrencySpec
		works    bool
	}{
		"the registry's own": {spec.Rounding, nil, true},
		"another rounding":   {machine.RoundDown, nil, true},
		"another currency": {spec.Rounding, func(c []machine.CurrencySpec) []machine.CurrencySpec {
			return append(c, machine.CurrencySpec{Code: "GBP", Digits: 2})
		}, false},
		"another place for yen": {spec.Rounding, func(c []machine.CurrencySpec) []machine.CurrencySpec { return withDigits(c, "JPY", 2) }, false},
	} {
		currencies := slices.Clone(spec.Currencies)
		if test.edit != nil {
			currencies = test.edit(currencies)
		}
		table, err := machine.NewCurrencies(machine.MoneySpec{Rounding: test.rounding, Currencies: currencies})
		if err != nil {
			t.Fatal(err)
		}
		rates := table.NewRates()
		if err := rates.Add("USD", "JPY", "150"); err != nil {
			t.Fatal(err)
		}
		_, err = runtime.RunValues(t.Context(), []machine.Value{machine.MoneyValue(100, "USD")}, machine.RunOptions{Rates: rates})
		if works := err == nil; works != test.works || (!works && !errors.Is(err, machine.ErrCurrency)) {
			t.Errorf("%s: error = %v, want it to run %v", name, err, test.works)
		}
	}
}

// withDigits is currencies with code's places changed.
func withDigits(currencies []machine.CurrencySpec, code string, digits int) []machine.CurrencySpec {
	for i := range currencies {
		if currencies[i].Code == code {
			currencies[i].Digits = digits
		}
	}
	return currencies
}
