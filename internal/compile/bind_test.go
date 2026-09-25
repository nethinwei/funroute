package compile

import (
	"errors"
	"math"
	"testing"

	"github.com/nethinwei/funroute/internal/machine"
	"github.com/nethinwei/funroute/internal/money"
)

// A host struct on the caller's stack stays there: scalars cross with no
// allocation, which also proves Run does not make *In escape.
func TestProgramScalarsDoNotAllocate(t *testing.T) {
	program := bindProgram[scalarIn, int64](t, machine.CoreRegistry(), `if(country == "SG", amount * 2, amount)`)
	ctx := t.Context()
	allocs := testing.AllocsPerRun(1000, func() {
		in := scalarIn{Country: "SG", Amount: 1000}
		out, err := program.Run(ctx, &in)
		if err != nil || out != 2000 {
			t.Fatalf("Run(%+v) = %d, %v, want 2000", in, out, err)
		}
	})
	if allocs != 0 {
		t.Fatalf("Run allocated %v times, want 0", allocs)
	}
}

type moneyIn struct {
	Amount money.Money `funroute:"amount"`
}

// A Money field is read and the result written in place, like a scalar.
func TestProgramMoneyDoesNotAllocate(t *testing.T) {
	registry := moneyRegistry(t)
	program := bindProgram[moneyIn, money.Money](t, registry, `round(amount * 0.029, @half_even)`)
	table, _ := registry.Currencies()
	amount, _ := table.Minor("USD", 10_000)
	fee, _ := table.Minor("USD", 290)
	ctx := t.Context()
	allocs := testing.AllocsPerRun(1000, func() {
		in := moneyIn{Amount: amount}
		out, err := program.Run(ctx, &in)
		if err != nil || out != fee {
			t.Fatalf("Run(%+v) = %v, %v, want USD 2.90", in, out, err)
		}
	})
	if allocs != 0 {
		t.Fatalf("Run allocated %v times, want 0", allocs)
	}
}

type vectorIn struct {
	Features []float64 `funroute:"features"`
}

// The host's vector reaches the extension as the same backing array.
func TestProgramPassesVectorsThrough(t *testing.T) {
	t.Parallel()
	registry := machine.CoreRegistry()
	var received []float64
	err := registry.Register(machine.FunctionSpec{
		Name: "model.score_v1",
		Go: func(xs []float64) (float64, error) {
			received = xs
			return xs[0], nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	program := bindProgram[vectorIn, float64](t, registry, `model.score_v1(features)`)
	in := vectorIn{Features: []float64{0.25, 0.5}}
	if out, err := program.Run(t.Context(), &in); err != nil || out != 0.25 {
		t.Fatalf("Run(%+v) = %v, %v, want 0.25", in, out, err)
	}
	if &received[0] != &in.Features[0] {
		t.Fatal("the extension received a copy of the host's vector")
	}
}

type mixedIn struct {
	Amount int64     `funroute:"amount"`
	Scores []float64 `funroute:"scores"`
}

// Only what the program reads is converted — and a NaN that is read is still
// refused where it enters.
func TestProgramConvertsOnlyWhatItReads(t *testing.T) {
	t.Parallel()
	in := mixedIn{Amount: 1, Scores: []float64{math.NaN()}}
	ignores := bindProgram[mixedIn, int64](t, machine.CoreRegistry(), `amount + 1`)
	if out, err := ignores.Run(t.Context(), &in); err != nil || out != 2 {
		t.Fatalf("amount + 1 on %+v = %d, %v, want 2", in, out, err)
	}
	reads := bindProgram[mixedIn, float64](t, machine.CoreRegistry(), `scores[0]`)
	if _, err := reads.Run(t.Context(), &in); !errors.Is(err, machine.ErrContract) {
		t.Fatalf("a NaN entered the program: %v, want ErrContract", err)
	}
}

// A narrow Go result is a promise the program's value has to keep.
func TestProgramChecksNarrowResults(t *testing.T) {
	t.Parallel()
	program := bindProgram[scalarIn, int8](t, machine.CoreRegistry(), `amount`)
	in := scalarIn{Amount: 300}
	if _, err := program.Run(t.Context(), &in); !errors.Is(err, machine.ErrContract) {
		t.Fatalf("300 fit an int8: %v, want ErrContract", err)
	}
	in.Amount = -5
	if out, err := program.Run(t.Context(), &in); err != nil || out != -5 {
		t.Fatalf("Run(%+v) = %d, %v, want -5", in, out, err)
	}
}

type tensor struct{ norm float64 }

type handleIn struct {
	Embedding *tensor            `funroute:"embedding"`
	Weights   map[string][]int64 `funroute:"weights"`
}

type handleOut struct {
	Embedding *tensor            `funroute:"embedding"`
	Weights   map[string][]int64 `funroute:"weights"`
	Norm      float64            `funroute:"norm"`
}

// Handles and non-native maps take the reflective path, in both directions.
func TestProgramCarriesHandlesAndMaps(t *testing.T) {
	t.Parallel()
	registry := machine.CoreRegistry()
	if err := machine.DefineHandle[*tensor](registry, "onnx.tensor"); err != nil {
		t.Fatal(err)
	}
	err := registry.Register(machine.FunctionSpec{
		Name: "model.norm_v1",
		Go: func(x *tensor) (float64, error) {
			return x.norm, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	program := bindProgram[handleIn, handleOut](t, registry,
		`{embedding: embedding, weights: weights, norm: model.norm_v1(embedding)}`)
	in := handleIn{Embedding: &tensor{norm: 2.5}, Weights: map[string][]int64{"a": {1, 2}}}
	out, err := program.Run(t.Context(), &in)
	if err != nil {
		t.Fatal(err)
	}
	if out.Embedding != in.Embedding || out.Norm != 2.5 || len(out.Weights["a"]) != 2 || out.Weights["a"][1] != 2 {
		t.Fatalf("Run = %+v, want the same embedding, norm 2.5 and weights %v", out, in.Weights)
	}
}

func TestBindRefusesTypesWithoutAContract(t *testing.T) {
	t.Parallel()
	registry := machine.CoreRegistry()
	if _, err := Bind[int64, int64](registry); err == nil {
		t.Fatal("a non-struct bound as arguments")
	}
	if _, err := Bind[struct {
		Amount uint64 `funroute:"amount"`
	}, int64](registry); err == nil {
		t.Fatal("uint64 bound, which int cannot hold")
	}
	if _, err := Bind[scalarIn, uint64](registry); err == nil {
		t.Fatal("uint64 bound as a result")
	}
}

type code string

type codeIn struct {
	Limits map[code]int64 `funroute:"limits"`
}

// A map keyed by a named string type crosses both ways, through a Go
// function and out of a Program, without reflect refusing the key.
func TestNamedMapKeysCross(t *testing.T) {
	t.Parallel()
	registry := machine.CoreRegistry()
	err := registry.Register(machine.FunctionSpec{
		Name: "limit.of_v1",
		Go: func(limits map[code]int64) (int64, error) {
			return limits["a"], nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	in := codeIn{Limits: map[code]int64{"a": 7}}
	through := bindProgram[codeIn, int64](t, registry, `limit.of_v1(limits)`)
	if out, err := through.Run(t.Context(), &in); err != nil || out != 7 {
		t.Fatalf("limit.of_v1(limits) on %v = %d, %v, want 7", in.Limits, out, err)
	}
	back := bindProgram[codeIn, map[code]int64](t, registry, `limits`)
	if out, err := back.Run(t.Context(), &in); err != nil || out["a"] != 7 {
		t.Fatalf("limits on %v = %v, %v, want a: 7", in.Limits, out, err)
	}
}

type narrowOut struct {
	Amount int64 `funroute:"amount"`
	Small  int8  `funroute:"small"`
}

// A result that fails to decode comes back as the zero value, not half filled.
func TestFailedResultIsZero(t *testing.T) {
	t.Parallel()
	program := bindProgram[scalarIn, narrowOut](t, machine.CoreRegistry(), `{amount: amount, small: amount}`)
	in := scalarIn{Amount: 300}
	out, err := program.Run(t.Context(), &in)
	if !errors.Is(err, machine.ErrContract) || out != (narrowOut{}) {
		t.Fatalf("Run(%+v) = %+v, %v, want the zero value and ErrContract", in, out, err)
	}
	errs, failed := failures()
	requests, outs := []scalarIn{{Amount: 1}, in}, make([]narrowOut, 2)
	program.RunBatch(t.Context(), 2, func(i int) *scalarIn { return &requests[i] }, func(i int) *narrowOut { return &outs[i] }, failed)
	if len(errs) != 1 || outs[0] != (narrowOut{Amount: 1, Small: 1}) || errs[1] == nil || outs[1] != (narrowOut{}) {
		t.Fatalf("outs = %+v, errs = %v, want [{1 1} {}] and one error at 1", outs, errs)
	}
}

// A rule may take no arguments: an argument struct with no tagged fields binds,
// runs, and refuses a program that reads a name.
func TestBindTakesARuleWithoutArguments(t *testing.T) {
	t.Parallel()
	registry := machine.CoreRegistry()
	binding, err := Bind[struct{ Untagged int64 }, int64](registry)
	if err != nil {
		t.Fatal(err)
	}
	program, err := binding.Compile(`1 + 2`)
	if err != nil {
		t.Fatal(err)
	}
	if out, err := program.Run(t.Context(), &struct{ Untagged int64 }{}); err != nil || out != 3 {
		t.Fatalf("1 + 2 = %d, %v, want 3", out, err)
	}
	if _, err := binding.Compile(`amount`); !errors.Is(err, machine.ErrContract) {
		t.Fatalf("a program reading a name the binding does not take compiled: %v, want ErrContract", err)
	}
}

type scalarIn struct {
	Country string `funroute:"country"`
	Amount  int64  `funroute:"amount"`
}

func bindProgram[In, Out any](t *testing.T, registry *machine.Registry, source string) *machine.Program[In, Out] {
	t.Helper()
	binding, err := Bind[In, Out](registry)
	if err != nil {
		t.Fatal(err)
	}
	program, err := binding.Compile(source)
	if err != nil {
		t.Fatal(err)
	}
	return program
}

// failures collects what RunBatch reports, by request.
func failures() (map[int]error, func(int, error)) {
	failed := map[int]error{}
	return failed, func(i int, err error) { failed[i] = err }
}

type orderIn struct {
	Order struct {
		Amount int64   `funroute:"amount"`
		Risk   float64 `funroute:"risk"`
		Note   string  `funroute:"note"`
	} `funroute:"order"`
	Fees []int64 `funroute:"fees"`
}

type decisionOut struct {
	Channel string  `funroute:"channel"`
	Net     int64   `funroute:"net"`
	Fees    []int64 `funroute:"fees"`
}

func forRegistry(t *testing.T) *machine.Registry {
	t.Helper()
	registry := machine.CoreRegistry()
	if err := registry.EnableForm(machine.ForForm); err != nil {
		t.Fatal(err)
	}
	return registry
}

// A record argument read field by field, and a record answered, cross
// without an allocation; so does an array answered into the slice the host
// lent it, once the slice has room.
func TestProgramRecordsAndLentArraysDoNotAllocate(t *testing.T) {
	program := bindProgram[orderIn, decisionOut](t, forRegistry(t),
		`{channel: if(order.risk < 0.5, "adyen", "manual"), net: order.amount - 30, fees: [fee * 2 for fee in fees if fee > 1]}`)
	ctx := t.Context()
	var in orderIn
	in.Order.Amount, in.Order.Risk, in.Fees = 1000, 0.2, []int64{1, 2, 3}
	var out decisionOut
	run := func() {
		if err := program.RunInto(ctx, &in, &out); err != nil || out.Net != 970 || out.Channel != "adyen" || len(out.Fees) != 2 || out.Fees[1] != 6 {
			t.Fatalf("RunInto(%+v) = %+v, %v", in, out, err)
		}
	}
	run()
	first := &out.Fees[0]
	if allocs := testing.AllocsPerRun(100, run); allocs != 0 {
		t.Errorf("RunInto allocated %v times, want 0", allocs)
	}
	if &out.Fees[0] != first {
		t.Error("RunInto did not build the fees in the slice it was lent")
	}
	if allocs := testing.AllocsPerRun(100, func() { _, _ = program.Run(ctx, &in) }); allocs != 1 {
		t.Errorf("Run allocated %v times, want 1: the fees' memory", allocs)
	}
}

type feesIn struct {
	Fees []int64 `funroute:"fees"`
}

// A slice lent that is the argument's memory is not built into: the answer
// is what it would be anywhere else, and the argument is left as it was.
func TestRunIntoDoesNotBuildOverItsArguments(t *testing.T) {
	t.Parallel()
	program := bindProgram[feesIn, []int64](t, forRegistry(t), `[fee + 1 for fee in fees]`)
	fees := []int64{1, 2, 3}
	in, out := feesIn{Fees: fees}, fees[:1]
	if err := program.RunInto(t.Context(), &in, &out); err != nil {
		t.Fatal(err)
	}
	if len(out) != 3 || out[0] != 2 || out[2] != 4 || fees[0] != 1 || fees[2] != 3 {
		t.Fatalf("RunInto over its own argument = %v, argument now %v; want [2 3 4] and [1 2 3]", out, fees)
	}
	if err := program.RunInto(t.Context(), &in, nil); !errors.Is(err, machine.ErrContract) {
		t.Fatalf("RunInto into nil = %v, want ErrContract", err)
	}
}

// A binding loads what it compiles without copying it or checking its
// digest again, and loses nothing by it: the program's artifact is the one
// CompileExpr makes, byte for byte, and loads anywhere with its digest.
func TestABindingLoadsWhatItCompilesAsItIs(t *testing.T) {
	t.Parallel()
	registry := machine.CoreRegistry()
	const source = `if(country == "SG", amount * 2, amount)`
	program := bindProgram[scalarIn, int64](t, registry, source)
	binding, err := Bind[scalarIn, int64](registry)
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := CompileExpr(source, registry, binding.Options())
	if err != nil {
		t.Fatal(err)
	}
	got, err := program.Artifact().MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	want, err := compiled.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Errorf("the program's artifact:\n%s\nCompileExpr's:\n%s", got, want)
	}
	if _, err := machine.Instantiate(program.Artifact(), registry); err != nil {
		t.Errorf("the program's artifact does not load: %v", err)
	}
}
