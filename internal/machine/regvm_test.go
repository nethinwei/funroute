package machine_test

import (
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/nethinwei/funroute/internal/compile"
	"github.com/nethinwei/funroute/internal/machine"
)

// A random program answers as itself with its arguments written in: the
// compiler folds every closed part of that one, each part lowered and run on
// its own, so the two runs share no register, block or fuse. The value and
// the failure are the same, word for word.

func TestRandomProgramsAnswerAsTheirFoldedSelves(t *testing.T) {
	t.Parallel()
	registry := randomRegistry(t)
	compared := 0
	for seed := range uint64(3000) {
		g := &generator{rand: rand.New(rand.NewPCG(seed, 7)), ints: []string{"a", "b"}, arrays: []string{"xs"}}
		source := g.of("int", 4)
		runtime, err := load(source, registry, randomContract)
		if err != nil {
			continue // the generator does not know every rule
		}
		for range 3 {
			args := g.args()
			want := assertAnswers(t, runtime, source, args)
			folded, err := load(written(source, args), registry, compile.CompileOptions{})
			if err != nil {
				continue // a part that fails when folded fails to compile
			}
			got := outcome(t, folded, nil)
			compared++
			if got != want {
				t.Fatalf("%s with %v gives %s; with the arguments written in, %s", source, args, want, got)
			}
		}
	}
	if compared < 2000 {
		t.Fatalf("only %d runs of the random programs were compared", compared)
	}
}

var randomContract = compile.CompileOptions{Args: []compile.ArgSpec{
	{Name: "a", Type: machine.IntType}, {Name: "b", Type: machine.IntType},
	{Name: "f", Type: machine.FloatType}, {Name: "s", Type: machine.StringType},
	{Name: "flag", Type: machine.BoolType}, {Name: "xs", Type: machine.ArrayOf(machine.IntType)},
	{Name: "fs", Type: machine.ArrayOf(machine.FloatType)},
}}

func load(source string, registry *machine.Registry, options compile.CompileOptions) (*machine.Runtime, error) {
	artifact, err := compile.CompileExpr(source, registry, options)
	if err != nil {
		return nil, err
	}
	return machine.Instantiate(artifact, registry)
}

// written is the program with its arguments bound to literals around it.
func written(source string, args map[string]any) string {
	names := make([]string, 0, len(args))
	for name := range args {
		names = append(names, name)
	}
	slices.Sort(names)
	var out strings.Builder
	out.WriteString("let(")
	for _, name := range names {
		fmt.Fprintf(&out, "%s = %s, ", name, literal(args[name]))
	}
	out.WriteString(source + ")")
	return out.String()
}

func literal(value any) string {
	switch value := value.(type) {
	case []any:
		items := make([]string, len(value))
		for i, item := range value {
			items[i] = literal(item)
		}
		return "[" + strings.Join(items, ", ") + "]"
	case int64:
		if value < -1<<62 {
			return fmt.Sprintf("(%d - 1)", value+1) // the literal of MinInt64 does not fit
		}
		return strconv.FormatInt(value, 10)
	case float64:
		return fmt.Sprintf("%.2f", value)
	case string:
		return fmt.Sprintf("%q", value)
	}
	return fmt.Sprint(value)
}

// assertAnswers runs the program and returns its answer, which is no
// internal error, and an array in memory of its own, not the frame's.
func assertAnswers(t *testing.T, runtime *machine.Runtime, source string, args map[string]any) string {
	t.Helper()
	answer := outcome(t, runtime, args)
	if strings.Contains(answer, "internal error") {
		t.Fatalf("%s with %v: %s", source, args, answer)
	}
	if value, err := runtime.Run(t.Context(), args); err == nil && !machine.NativeBacked(value) {
		t.Fatalf("%s with %v answers an array in the frame's slot", source, args)
	}
	return answer
}

// outcome is what one run gave, as text: the value's JSON or the failure.
func outcome(t *testing.T, runtime *machine.Runtime, args map[string]any) string {
	t.Helper()
	value, err := runtime.Run(t.Context(), args)
	if err != nil {
		return "error: " + err.Error()
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

// randomRegistry is the whole language and a host function that fails when
// its argument is a multiple of three, which fallback takes.
func randomRegistry(t *testing.T) *machine.Registry {
	t.Helper()
	registry := machine.CoreRegistry()
	if err := registry.EnableForm(machine.SwitchForm, machine.ForForm, machine.ReduceForm); err != nil {
		t.Fatal(err)
	}
	flaky := func(x int64) (int64, error) {
		if x%3 == 0 {
			return 0, fmt.Errorf("flaky refuses %d", x)
		}
		return x * 2, nil
	}
	if err := registry.Register(machine.FunctionSpec{Name: "host.flaky_v1", Go: flaky}); err != nil {
		t.Fatal(err)
	}
	// total sums what it is handed: an array in a frame's slot, handed over
	// by mistake, would read as empty.
	total := func(xs []int64) int64 {
		sum := int64(0)
		for _, x := range xs {
			sum += x
		}
		return sum
	}
	if err := registry.Register(machine.FunctionSpec{Name: "host.total_v1", Go: total}); err != nil {
		t.Fatal(err)
	}
	return registry
}
