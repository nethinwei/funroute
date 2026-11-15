package golden

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"slices"
	"testing"

	"github.com/nethinwei/funroute"
	"github.com/nethinwei/funroute/extensions/std"
)

var update = flag.Bool("update", false, "write the record anew into testdata/outcomes.json and testdata/phrases.json")

// programsPerType is how many programs are written for each result type.
const programsPerType = 150

// entry is one program and what it gave: for each input, a Program's run and
// a RunValues run, "same" when the two agree.
type entry struct {
	Type     string   `json:"type"`
	Source   string   `json:"source"`
	Outcomes []string `json:"outcomes"`
}

// Every program answers as the record says it does, on every input, both
// ways it runs.
func TestTheLanguageAnswersAsItsRecord(t *testing.T) {
	got := recorded(run(t))
	if t.Failed() {
		// A type whose run failed wrote none of its programs: the record
		// would lose them, so it is neither written nor compared.
		t.FailNow()
	}
	if *update {
		write(t, got)
		return
	}
	want := read(t)
	if len(want.entries) != len(got.entries) {
		t.Fatalf("the record has %d programs, a run writes %d; go test -update writes it anew", len(want.entries), len(got.entries))
	}
	if i := firstOther(want.phrases, got.phrases); i >= 0 {
		t.Errorf("phrase P%d is %q in the record, %q in a run", i+1, at(want.phrases, i), at(got.phrases, i))
	}
	differ := 0
	for i, e := range got.entries {
		j := firstOther(want.entries[i].Outcomes, e.Outcomes)
		if e.Type == want.entries[i].Type && e.Source == want.entries[i].Source && j < 0 {
			continue
		}
		if differ++; differ <= 20 {
			t.Errorf("%s program %d: %s\n  outcome %d in the record: %s\n  in a run:               %s",
				e.Type, i, e.Source, j, at(want.entries[i].Outcomes, j), at(e.Outcomes, j))
		}
	}
	if differ > 0 {
		t.Fatalf("%d of %d programs answer other than the record says", differ, len(got.entries))
	}
}

// firstOther is the first index two lists differ at, -1 when they are one.
func firstOther(a, b []string) int {
	for i := range max(len(a), len(b)) {
		if at(a, i) != at(b, i) || i >= len(a) || i >= len(b) {
			return i
		}
	}
	return -1
}

// at is list[i], or "(none)" past its end.
func at(list []string, i int) string {
	if i < 0 || i >= len(list) {
		return "(none)"
	}
	return list[i]
}

// run writes every program and runs it, each result type through a binding
// of its own Go type, in a registry of its own: the types run side by side,
// and each writes its programs from its own seeds, so the order is the same
// however they interleave.
func run(t *testing.T) []entry {
	t.Helper()
	types := roots()
	perRoot := make([][]entry, len(types))
	t.Run("types", func(t *testing.T) {
		for _, root := range types {
			t.Run(root.typ, func(t *testing.T) {
				t.Parallel()
				perRoot[root.index] = runRoot(t, root, len(types))
			})
		}
	})
	return slices.Concat(perRoot...)
}

// runRoot writes and runs root's programs, each from a seed of its own.
func runRoot(t *testing.T, root root, types int) []entry {
	t.Helper()
	outcomes := root.bind(t, goldenRegistry(t))
	entries := make([]entry, 0, programsPerType)
	for seed := range uint64(programsPerType) {
		seed = seed*uint64(types) + uint64(root.index)
		source, results := compiling(t, seed, root.typ, outcomes)
		entries = append(entries, entry{Type: root.typ, Source: source, Outcomes: results})
	}
	return entries
}

// maxAttempts is how many programs are written for a seed before the
// generator is held to be writing what the language refuses.
const maxAttempts = 20

// compiling is the first program for seed that compiles, and its outcomes.
// A program can be refused for what it says — a closed part that fails,
// int(1.5), is a compile error — and is then written anew.
func compiling(t *testing.T, seed uint64, typ string, outcomes func(context.Context, string) ([]string, error)) (string, []string) {
	t.Helper()
	var last error
	for attempt := range uint64(maxAttempts) {
		source := newGenerator(seed, attempt).of(typ, 4)
		results, err := outcomes(t.Context(), source)
		if err == nil {
			return source, results
		}
		last = err
	}
	t.Fatalf("no program of %d written for seed %d compiles; the last: %v", maxAttempts, seed, last)
	return "", nil
}

// root is a result type: the one the generator writes, and the Go type a
// Program answers it in.
type root struct {
	typ   string
	index int
	bind  func(*testing.T, *funroute.Registry) func(context.Context, string) ([]string, error)
}

func roots() []root {
	list := []root{
		rootOf[int64]("int"), rootOf[float64]("float"), rootOf[bool]("bool"), rootOf[string]("string"),
		rootOf[[]int64]("ints"), rootOf[[]float64]("floats"), rootOf[[]string]("strings"),
		rootOf[map[string]int64]("dict"), rootOf[order]("order"), rootOf[[]channel]("chans"),
	}
	for i := range list {
		list[i].index = i
	}
	return list
}

func rootOf[Out any](typ string) root {
	return root{typ: typ, bind: func(t *testing.T, registry *funroute.Registry) func(context.Context, string) ([]string, error) {
		t.Helper()
		binding, err := funroute.Bind[request, Out](registry)
		if err != nil {
			t.Fatal(err)
		}
		return func(ctx context.Context, source string) ([]string, error) {
			program, err := binding.Compile(source)
			if err != nil {
				return nil, err
			}
			return outcomesOf(ctx, program), nil
		}
	}}
}

// outcomesOf runs the program on every input, both ways.
func outcomesOf[Out any](ctx context.Context, program *funroute.Program[request, Out]) []string {
	requests := inputs()
	outcomes := make([]string, 0, 2*len(requests))
	for _, in := range requests {
		byProgram, byValues := programOutcome(ctx, program, in), valuesOutcome(ctx, program.Runtime(), in)
		if byValues == byProgram {
			byValues = "same"
		}
		outcomes = append(outcomes, byProgram, byValues)
	}
	return outcomes
}

func programOutcome[Out any](ctx context.Context, program *funroute.Program[request, Out], in *request) string {
	out, err := program.Run(ctx, in)
	if err != nil {
		return failure(err)
	}
	return encoded(out)
}

func valuesOutcome(ctx context.Context, runtime *funroute.Runtime, in *request) string {
	values, err := valuesOf(in, runtime.Args())
	if err != nil {
		return "refused " + failure(err)
	}
	value, err := runtime.RunValues(ctx, values)
	if err != nil {
		return failure(err)
	}
	return encoded(value)
}

// encoded is value's JSON, or what JSON has no form for: the value alone,
// since the wording around it is encoding/json's and changes with Go.
func encoded(value any) string {
	text, err := json.Marshal(value)
	if unsupported := (*json.UnsupportedValueError)(nil); errors.As(err, &unsupported) {
		return "unencodable: " + unsupported.Str
	}
	if err != nil {
		return "unencodable: " + err.Error()
	}
	return string(text)
}

// failure is an error's class, as funroute.ClassName names it, and its
// words.
func failure(err error) string {
	class := funroute.ClassName(err)
	if class == "" {
		class = "none"
	}
	return fmt.Sprintf("error[%s] %s", class, err)
}

// goldenRegistry is the kernel, every form, std and two host functions: one
// that fails on a multiple of three, which fallback takes, and one that sums
// what it is handed.
func goldenRegistry(t *testing.T) *funroute.Registry {
	t.Helper()
	registry := funroute.CoreRegistry()
	if err := registry.EnableForm(funroute.SwitchForm, funroute.ForForm, funroute.ReduceForm); err != nil {
		t.Fatal(err)
	}
	if err := std.Register(registry); err != nil {
		t.Fatal(err)
	}
	flaky := func(x int64) (int64, error) {
		if x%3 == 0 {
			return 0, fmt.Errorf("flaky refuses %d", x)
		}
		return x * 2, nil
	}
	total := func(xs []int64) int64 {
		sum := int64(0)
		for _, x := range xs {
			sum += x
		}
		return sum
	}
	for _, spec := range []funroute.FunctionSpec{{Name: "host.flaky_v1", Go: flaky}, {Name: "host.total_v1", Go: total}} {
		if err := registry.Register(spec); err != nil {
			t.Fatal(err)
		}
	}
	return registry
}
