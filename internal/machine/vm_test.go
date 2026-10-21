package machine_test

import (
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strings"
	"testing"

	"github.com/nethinwei/funroute/internal/compile"
	"github.com/nethinwei/funroute/internal/machine"
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

func run(b *testing.B, runtime *machine.Runtime, args map[string]any) {
	b.Helper()
	ctx := b.Context()
	b.ReportAllocs()
	for b.Loop() {
		if _, err := runtime.Run(ctx, args); err != nil {
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
	run(b, runtime, map[string]any{"a": 1, "b": 2})
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
	run(b, runtime, map[string]any{"items": items})
}

// BenchmarkReduce is the same sum through the fold form.
func BenchmarkReduce(b *testing.B) {
	items := make([]any, 500)
	for i := range items {
		items[i] = i
	}
	runtime := benchRuntime(b, `reduce(item in items, total = 0, add(total,item))`,
		[]compile.ArgSpec{{Name: "items", Type: machine.ArrayOf(machine.IntType)}})
	run(b, runtime, map[string]any{"items": items})
}

// BenchmarkFor builds a new array, exercising the collect path.
func BenchmarkFor(b *testing.B) {
	items := make([]any, 500)
	for i := range items {
		items[i] = i
	}
	runtime := benchRuntime(b, `[add(item,1) for item in items]`,
		[]compile.ArgSpec{{Name: "items", Type: machine.ArrayOf(machine.IntType)}})
	run(b, runtime, map[string]any{"items": items})
}

// BenchmarkCall measures the extension-call boundary.
func BenchmarkCall(b *testing.B) {
	runtime := benchRuntime(b, `add(mul(a,b),sub(a,b))`, nil)
	run(b, runtime, map[string]any{"a": 7, "b": 3})
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
	value, err := runtime.Run(t.Context(), map[string]any{"items": []any{1, 2, 3, 4, 5}})
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
			if _, err := runtime.Run(ctx, args); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("RunValues/byOrder", func(b *testing.B) {
		args := []machine.Value{machine.String("SG"), machine.Int(1000)}
		ctx := b.Context()
		b.ReportAllocs()
		for b.Loop() {
			if _, err := runtime.RunValues(ctx, args); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("Program/typed", func(b *testing.B) { benchTyped(b, registry, artifact) })
}

// benchTyped is the same artifact through a Program: the host's struct in,
// an int64 out.
// BenchmarkRunParallel runs one program on every core at once: ns/op falls
// as the cores are added, since runs share nothing they write.
func BenchmarkRunParallel(b *testing.B) {
	registry := benchRegistry(b)
	binding, err := compile.Bind[scalarIn, int64](registry)
	if err != nil {
		b.Fatal(err)
	}
	program, err := binding.Compile(`if(country == "SG", amount * 2, amount)`)
	if err != nil {
		b.Fatal(err)
	}
	ctx := b.Context()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		in := scalarIn{Country: "SG", Amount: 1000}
		for pb.Next() {
			if _, err := program.Run(ctx, &in); err != nil {
				panic(err)
			}
		}
	})
}

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
		if _, err := program.Run(ctx, &in); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkVectorPassThrough is the deep-learning shape: a feature vector goes
// from the host through the VM into an extension and a score comes back. The
// three sizes must print the same ns/op — nothing along the way converts,
// copies, or even reads the elements. The allocations that remain are
// reflect.Call's, because this extension is registered by its Go signature; they are
// the same count at every size, and a written Eval has none.
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
	err := registry.Register(machine.FunctionSpec{
		Name: "model.score_v1",
		Go: func(xs []float64) (float64, error) {
			return xs[0] + xs[len(xs)-1], nil
		},
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
		if _, err := runtime.RunValues(ctx, args); err != nil {
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
	registry := fxRegistry(t)
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
	forge := func(value string) func(*machine.ArtifactParts) {
		return func(p *machine.ArtifactParts) { p.Constants[0].Value = json.RawMessage(value) }
	}
	for _, test := range []struct {
		name, source, contract string
		forge                  func(*machine.ArtifactParts)
	}{
		{"an amount with no currency", "a + USD 0.05", "a: money", forge(`{"currency":"","minor":5}`)},
		{"an undeclared currency", "a + USD 0.05", "a: money", forge(`{"currency":"XYZ","minor":5}`)},
		{"a rate in an undeclared currency", "using(150 JPY / USD, round(a -> JPY, @half_even))", "a: money", forge(`{"base":"XYZ","quote":"JPY","rate":"150"}`)},
		{"an item of an array", "[USD 0.05, USD 0.10]", "", forge(`[{"currency":"USD","minor":5},{"currency":"XYZ","minor":10}]`)},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if err := forgedLoad(t, test.source, test.contract, test.forge); err == nil {
				t.Fatalf("Instantiate of %s with %s = nil, want a refusal", test.source, test.name)
			}
		})
	}
	if err := forgedLoad(t, "a + USD 0.05", "a: money", func(*machine.ArtifactParts) {}); err != nil {
		t.Fatalf("Instantiate of an honest artifact = %v", err)
	}
}

// Every type an instruction carries is whole: an array with no element type
// is refused when the artifact loads, not met as a nil when it is walked.
func TestLoadingRefusesATypeWithAPartMissing(t *testing.T) {
	t.Parallel()
	var partial machine.Type
	if err := json.Unmarshal([]byte(`{"kind":"array"}`), &partial); err != nil {
		t.Fatal(err)
	}
	err := forgedLoad(t, "[a]", "a: int", func(p *machine.ArtifactParts) { op(t, p, machine.OpMakeArray).Type = &partial })
	if err == nil || !strings.Contains(err.Error(), "not concrete") {
		t.Fatalf("Instantiate of an array with no element type = %v, want it refused", err)
	}
}

// A loop_collect's A says whether it takes a key, and is read as nothing
// else: one that says more is refused, as are operands it does not have.
func TestALoopCollectWithOtherOperandsIsRefused(t *testing.T) {
	t.Parallel()
	registry := forgeRegistry(t)
	for _, forge := range []struct {
		name string
		set  func(*machine.Instruction)
	}{
		{"a=2", func(in *machine.Instruction) { in.A = 2 }},
		{"a=-1", func(in *machine.Instruction) { in.A = -1 }},
		{"a=1<<30", func(in *machine.Instruction) { in.A = 1 << 30 }},
		{"b=1", func(in *machine.Instruction) { in.B = 1 }},
		{"d=-1", func(in *machine.Instruction) { in.D = -1 }},
	} {
		t.Run(forge.name, func(t *testing.T) {
			t.Parallel()
			parts := forgeSeed(t, registry, `reduce(x in xs, t = 0, t + x)`)
			at := slices.IndexFunc(parts.Instructions, func(in machine.Instruction) bool { return in.Op == machine.OpLoopCollect })
			if at < 0 {
				t.Fatal("the program has no loop_collect")
			}
			forge.set(&parts.Instructions[at])
			sealed, err := machine.SealArtifact(parts, nil)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := machine.Instantiate(sealed, registry); err == nil {
				t.Errorf("Instantiate(loop_collect with %s) = nil error, want a refusal", forge.name)
			}
		})
	}
}

// An artifact rewritten in storage or on the wire — any operand of any
// instruction set to anything, and the digest computed again — is refused or
// loaded, and a loaded one runs; neither panics.
func FuzzInstantiateNeverPanics(f *testing.F) {
	registry := forgeRegistry(f)
	sources := []string{
		`reduce(x in xs, t = 0, t + x)`,
		`len({string(x): x * 2 for x in xs})`,
		`sum([x * 2 for x in xs if x > a])`,
		`[x * 2 > 5 for x in xs]`,
		`switch(a, case 1 => b, else => len([y for x in xs for y in xs]))`,
		`let(r = {a: a, b: b}, r.a + fallback(xs[a], b))`,
	}
	seeds := make([]machine.ArtifactParts, len(sources))
	for i, source := range sources {
		seeds[i] = forgeSeed(f, registry, source)
	}
	for _, value := range []int64{-1, 0, 1, 2, 1 << 30, math.MinInt64} {
		f.Add(uint8(0), uint16(3), uint8(0), value)
	}
	f.Fuzz(func(t *testing.T, seed uint8, pc uint16, operand uint8, value int64) {
		parts := seeds[int(seed)%len(seeds)]
		parts.Instructions = slices.Clone(parts.Instructions)
		in := &parts.Instructions[int(pc)%len(parts.Instructions)]
		*[]*int{&in.A, &in.B, &in.C, &in.D}[operand%4] = int(value)
		sealed, err := machine.SealArtifact(parts, nil)
		if err != nil {
			return
		}
		runtime, err := machine.Instantiate(sealed, registry)
		if err != nil {
			return
		}
		args := map[string]any{"a": int64(1), "b": int64(2), "xs": []any{int64(1), int64(2), int64(3)}}
		if _, err := runtime.Run(t.Context(), args); err != nil && strings.Contains(err.Error(), "panicked") {
			t.Fatalf("%v: %v", parts.Instructions, err)
		}
	})
}

// forgeRegistry is the kernel with its loops and switch.
func forgeRegistry(tb testing.TB) *machine.Registry {
	tb.Helper()
	registry := machine.CoreRegistry()
	if err := registry.EnableForm(machine.SwitchForm, machine.ForForm, machine.ReduceForm); err != nil {
		tb.Fatal(err)
	}
	return registry
}

// forgeSeed is the parts of source compiled against a, b and xs.
func forgeSeed(tb testing.TB, registry *machine.Registry, source string) machine.ArtifactParts {
	tb.Helper()
	artifact, err := compile.CompileExpr(source, registry, compile.CompileOptions{Args: []compile.ArgSpec{
		{Name: "a", Type: machine.IntType}, {Name: "b", Type: machine.IntType}, {Name: "xs", Type: machine.ArrayOf(machine.IntType)},
	}})
	if err != nil {
		tb.Fatalf("%s: %v", source, err)
	}
	return machine.PartsOf(artifact)
}
