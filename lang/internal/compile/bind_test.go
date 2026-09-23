package compile

import (
	"context"
	"errors"
	"math"
	"testing"

	"funroute/lang/internal/machine"
)

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

// A host struct on the caller's stack stays there: scalars cross with no
// allocation, which also proves Run does not make *In escape.
func TestProgramScalarsDoNotAllocate(t *testing.T) {
	program := bindProgram[scalarIn, int64](t, machine.CoreRegistry(), `if(country == "SG", amount * 2, amount)`)
	ctx := context.Background()
	allocs := testing.AllocsPerRun(1000, func() {
		in := scalarIn{Country: "SG", Amount: 1000}
		out, err := program.Run(ctx, &in, machine.RunOptions{})
		if err != nil || out != 2000 {
			t.Fatalf("out = %d, err = %v", out, err)
		}
	})
	if allocs != 0 {
		t.Fatalf("Run allocated %v times", allocs)
	}
}

type vectorIn struct {
	Features []float64 `funroute:"features"`
}

// The host's vector reaches the extension as the same backing array.
func TestProgramPassesVectorsThrough(t *testing.T) {
	registry := machine.CoreRegistry()
	var received []float64
	err := machine.Logic(registry, "model.score_v1", machine.Doc{Cost: 10}, func(xs []float64) (float64, error) {
		received = xs
		return xs[0], nil
	})
	if err != nil {
		t.Fatal(err)
	}
	program := bindProgram[vectorIn, float64](t, registry, `model.score_v1(features)`)
	in := vectorIn{Features: []float64{0.25, 0.5}}
	if out, err := program.Run(context.Background(), &in, machine.RunOptions{}); err != nil || out != 0.25 {
		t.Fatalf("out = %v, err = %v", out, err)
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
	in := mixedIn{Amount: 1, Scores: []float64{math.NaN()}}
	ignores := bindProgram[mixedIn, int64](t, machine.CoreRegistry(), `amount + 1`)
	if out, err := ignores.Run(context.Background(), &in, machine.RunOptions{}); err != nil || out != 2 {
		t.Fatalf("out = %d, err = %v", out, err)
	}
	reads := bindProgram[mixedIn, float64](t, machine.CoreRegistry(), `scores[0]`)
	if _, err := reads.Run(context.Background(), &in, machine.RunOptions{}); !errors.Is(err, machine.ErrContract) {
		t.Fatalf("a NaN entered the program: %v", err)
	}
}

// A narrow Go result is a promise the program's value has to keep.
func TestProgramChecksNarrowResults(t *testing.T) {
	program := bindProgram[scalarIn, int8](t, machine.CoreRegistry(), `amount`)
	in := scalarIn{Amount: 300}
	if _, err := program.Run(context.Background(), &in, machine.RunOptions{}); !errors.Is(err, machine.ErrContract) {
		t.Fatalf("300 fit an int8: %v", err)
	}
	in.Amount = -5
	if out, err := program.Run(context.Background(), &in, machine.RunOptions{}); err != nil || out != -5 {
		t.Fatalf("out = %d, err = %v", out, err)
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
	registry := machine.CoreRegistry()
	if err := machine.DefineHandle[*tensor](registry, "onnx.tensor"); err != nil {
		t.Fatal(err)
	}
	err := machine.Logic(registry, "model.norm_v1", machine.Doc{Cost: 10}, func(x *tensor) (float64, error) {
		return x.norm, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	program := bindProgram[handleIn, handleOut](t, registry,
		`{embedding: embedding, weights: weights, norm: model.norm_v1(embedding)}`)
	in := handleIn{Embedding: &tensor{norm: 2.5}, Weights: map[string][]int64{"a": {1, 2}}}
	out, err := program.Run(context.Background(), &in, machine.RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if out.Embedding != in.Embedding || out.Norm != 2.5 || len(out.Weights["a"]) != 2 || out.Weights["a"][1] != 2 {
		t.Fatalf("out = %+v", out)
	}
}

func TestBindRefusesTypesWithoutAContract(t *testing.T) {
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

// A map keyed by a named string type crosses both ways, through a Logic
// function and out of a Program, without reflect refusing the key.
func TestNamedMapKeysCross(t *testing.T) {
	registry := machine.CoreRegistry()
	err := machine.Logic(registry, "limit.of_v1", machine.Doc{Cost: 1}, func(limits map[code]int64) (int64, error) {
		return limits["a"], nil
	})
	if err != nil {
		t.Fatal(err)
	}
	in := codeIn{Limits: map[code]int64{"a": 7}}
	through := bindProgram[codeIn, int64](t, registry, `limit.of_v1(limits)`)
	if out, err := through.Run(context.Background(), &in, machine.RunOptions{}); err != nil || out != 7 {
		t.Fatalf("out = %d, err = %v", out, err)
	}
	back := bindProgram[codeIn, map[code]int64](t, registry, `limits`)
	if out, err := back.Run(context.Background(), &in, machine.RunOptions{}); err != nil || out["a"] != 7 {
		t.Fatalf("out = %v, err = %v", out, err)
	}
}

type narrowOut struct {
	Amount int64 `funroute:"amount"`
	Small  int8  `funroute:"small"`
}

// A result that fails to decode comes back as the zero value, not half filled.
func TestFailedResultIsZero(t *testing.T) {
	program := bindProgram[scalarIn, narrowOut](t, machine.CoreRegistry(), `{amount: amount, small: amount}`)
	in := scalarIn{Amount: 300}
	out, err := program.Run(context.Background(), &in, machine.RunOptions{})
	if !errors.Is(err, machine.ErrContract) || out != (narrowOut{}) {
		t.Fatalf("out = %+v, err = %v", out, err)
	}
	errs, failed := failures()
	outs := program.RunBatch(context.Background(), []scalarIn{{Amount: 1}, in}, machine.RunOptions{}, failed)
	if len(errs) != 1 || outs[0] != (narrowOut{Amount: 1, Small: 1}) || errs[1] == nil || outs[1] != (narrowOut{}) {
		t.Fatalf("outs = %+v, errs = %v", outs, errs)
	}
}

// A rule may take no arguments: an argument struct with no tagged fields binds,
// runs, and refuses a program that reads a name.
func TestBindTakesARuleWithoutArguments(t *testing.T) {
	registry := machine.CoreRegistry()
	binding, err := Bind[struct{ Untagged int64 }, int64](registry)
	if err != nil {
		t.Fatal(err)
	}
	program, err := binding.Compile(`1 + 2`)
	if err != nil {
		t.Fatal(err)
	}
	if out, err := program.Run(context.Background(), &struct{ Untagged int64 }{}, machine.RunOptions{}); err != nil || out != 3 {
		t.Fatalf("out = %d, err = %v", out, err)
	}
	if _, err := binding.Compile(`amount`); !errors.Is(err, machine.ErrContract) {
		t.Fatalf("a program reading a name the binding does not take compiled: %v", err)
	}
}
