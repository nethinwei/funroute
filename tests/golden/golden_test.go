package golden

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/nethinwei/funroute"
	"github.com/nethinwei/funroute/extensions/std"
)

var update = flag.Bool("update", false, "write the outcomes anew into testdata/outcomes.jsonl")

const recordPath = "testdata/outcomes.jsonl"

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
	entries := run(t)
	if *update {
		write(t, entries)
		return
	}
	recorded := read(t)
	if len(recorded) != len(entries) {
		t.Fatalf("the record has %d programs, a run writes %d; go test -update writes it anew", len(recorded), len(entries))
	}
	differ := 0
	for i, got := range entries {
		if want := recorded[i]; !sameEntry(got, want) {
			differ++
			if differ <= 20 {
				t.Errorf("%s program %d: %s\n  record: %v\n  run:    %v", got.Type, i, got.Source, want.Outcomes, got.Outcomes)
			}
		}
	}
	if differ > 0 {
		t.Fatalf("%d of %d programs answer other than the record says", differ, len(entries))
	}
}

func sameEntry(a, b entry) bool {
	if a.Type != b.Type || a.Source != b.Source || len(a.Outcomes) != len(b.Outcomes) {
		return false
	}
	for i := range a.Outcomes {
		if a.Outcomes[i] != b.Outcomes[i] {
			return false
		}
	}
	return true
}

// run writes every program and runs it, each result type through a binding
// of its own Go type.
func run(t *testing.T) []entry {
	t.Helper()
	registry := goldenRegistry(t)
	types := roots()
	entries := make([]entry, 0, len(types)*programsPerType)
	for _, root := range types {
		outcomes := root.bind(t, registry)
		for seed := range uint64(programsPerType) {
			seed = seed*uint64(len(types)) + uint64(root.index)
			source, results := compiling(t, seed, root.typ, outcomes)
			entries = append(entries, entry{Type: root.typ, Source: source, Outcomes: results})
		}
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

func encoded(value any) string {
	text, err := json.Marshal(value)
	if err != nil {
		return "unencodable: " + err.Error()
	}
	return string(text)
}

// classes are the failure classes a host tells apart, the most specific
// first.
var classes = []struct {
	name string
	err  error
}{
	{"unavailable", funroute.ErrUnavailable}, {"compile", funroute.ErrCompile}, {"contract", funroute.ErrContract},
	{"deadline", funroute.ErrDeadline}, {"currency", funroute.ErrCurrency}, {"fx", funroute.ErrNoFxRate},
	{"arithmetic", funroute.ErrArithmetic}, {"domain", funroute.ErrDomain}, {"extension", funroute.ErrExtension},
}

// failure is an error's class and its words.
func failure(err error) string {
	for _, class := range classes {
		if errors.Is(err, class.err) {
			return fmt.Sprintf("error[%s] %s", class.name, err)
		}
	}
	return "error[none] " + err.Error()
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

func write(t *testing.T, entries []entry) {
	t.Helper()
	var out bytes.Buffer
	for _, e := range entries {
		line, err := json.Marshal(e)
		if err != nil {
			t.Fatal(err)
		}
		out.Write(line)
		out.WriteByte('\n')
	}
	if err := os.MkdirAll("testdata", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(recordPath, out.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T) []entry {
	t.Helper()
	data, err := os.ReadFile(recordPath)
	if err != nil {
		t.Fatalf("%v; go test -update writes the record", err)
	}
	var entries []entry
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(nil, 1<<24)
	for scanner.Scan() {
		var e entry
		if err := json.Unmarshal(scanner.Bytes(), &e); err != nil {
			t.Fatal(err)
		}
		entries = append(entries, e)
	}
	if strings.TrimSpace(string(data)) != "" && len(entries) == 0 {
		t.Fatal("the record holds no programs")
	}
	return entries
}
