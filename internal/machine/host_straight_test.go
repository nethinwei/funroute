package machine_test

import (
	"context"
	"math"
	"testing"

	"github.com/nethinwei/funroute/internal/compile"
	"github.com/nethinwei/funroute/internal/machine"
)

type straightIn struct {
	A    int64   `funroute:"a"`
	B    int64   `funroute:"b"`
	X    float64 `funroute:"x"`
	Name string  `funroute:"name"`
	Flag bool    `funroute:"flag"`
	Xs   []int64 `funroute:"xs"`
}

// straightCase is a rule and whether it runs straight.
type straightCase struct {
	source   string
	straight bool
}

// A straight program answers as the same program does by the ordinary
// run: the value and the failure.
func TestAStraightProgramAnswersAsTheOrdinaryRun(t *testing.T) {
	t.Parallel()
	registry := straightRegistry(t)
	inputs := []*straightIn{
		{A: 7, B: 3, X: 2.5, Name: "adyen", Flag: true},
		{A: math.MaxInt64, B: 1, X: -1, Name: "", Flag: false},
		{A: 4, B: 0, X: 0, Name: "sg"},
	}
	assertStraight[int64](t, registry, inputs, []straightCase{
		{`a + b`, true}, {`a / b`, true}, {`if(flag, a * b, a - b)`, true}, {`len(name) + a`, true},
		{`len(xs) + a`, true}, {`xs[1] + a`, true}, {`len([x for x in xs])`, false},
		{`h.twice_v1(a) + b`, true}, {`h.boom_v1(a) + b`, true},
	})
	assertStraight[float64](t, registry, inputs, []straightCase{{`x * 2.0 + float(a)`, true}, {`x / float(b)`, true}})
	assertStraight[string](t, registry, inputs, []straightCase{{`if(flag, name, "none")`, true}, {`name + string(a)`, true}})
	assertStraight[bool](t, registry, inputs, []straightCase{{`a > b && flag || name == "sg"`, true}})
}

// straightRegistry is the kernel with loops and two host functions: one
// that doubles, and one that panics on 4.
func straightRegistry(t *testing.T) *machine.Registry {
	t.Helper()
	registry := machine.CoreRegistry()
	if err := registry.EnableForm(machine.ForForm); err != nil {
		t.Fatal(err)
	}
	for _, spec := range []machine.FunctionSpec{
		{Name: "h.twice_v1", Go: func(a int64) int64 { return 2 * a }},
		{Name: "h.boom_v1", Go: func(a int64) int64 {
			if a == 4 {
				panic("four")
			}
			return a
		}},
	} {
		if err := registry.Register(spec); err != nil {
			t.Fatal(err)
		}
	}
	return registry
}

func assertStraight[Out any](t *testing.T, registry *machine.Registry, inputs []*straightIn, cases []straightCase) {
	t.Helper()
	binding, err := compile.Bind[straightIn, Out](registry)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		program, err := binding.Compile(tc.source)
		if err != nil {
			t.Fatalf("%s: %v", tc.source, err)
		}
		if got := machine.Straight(program); got != tc.straight {
			t.Errorf("%s: straight %v, want %v", tc.source, got, tc.straight)
		}
		for _, in := range inputs {
			assertStraightRun(t, tc.source, program, in)
		}
	}
}

func assertStraightRun[Out any](t *testing.T, source string, program *machine.Program[straightIn, Out], in *straightIn) {
	t.Helper()
	args := []machine.Value{machine.Int(in.A), machine.Int(in.B), machine.Float(in.X), machine.String(in.Name), machine.Bool(in.Flag), must(machine.ToValue(in.Xs))}
	got, gotErr := program.Run(t.Context(), in)
	want, wantErr := program.Runtime().RunValues(t.Context(), args)
	if (gotErr == nil) != (wantErr == nil) || gotErr != nil && gotErr.Error() != wantErr.Error() {
		t.Errorf("%s on %+v: fails with %v; by the ordinary run %v", source, *in, gotErr, wantErr)
	} else if gotErr == nil && !machine.Identical(must(machine.ToValue(got)), want) {
		t.Errorf("%s on %+v: %v; by the ordinary run %v", source, *in, got, want.Any())
	}
	if machine.IdleFrameHoldsPointers(program.Runtime()) {
		t.Errorf("%s: the idle frame still points at the run; want nothing", source)
	}
}

// A float that is not finite runs straight as it runs the ordinary way — a
// NaN in is a NaN out — and the rule runs straight with no allocation.
func TestAStraightProgramIsHeldAsTheOrdinaryRun(t *testing.T) {
	binding, err := compile.Bind[straightIn, float64](machine.CoreRegistry())
	if err != nil {
		t.Fatal(err)
	}
	straight, err := binding.Compile(`x + 1.0`)
	if err != nil {
		t.Fatal(err)
	}
	ordinary, err := binding.Compile(`x + float(len(xs))`)
	if err != nil {
		t.Fatal(err)
	}
	got, gotErr := straight.Run(t.Context(), &straightIn{X: math.NaN()})
	want, wantErr := ordinary.Run(t.Context(), &straightIn{X: math.NaN()})
	if gotErr != nil || wantErr != nil || !math.IsNaN(got) || !math.IsNaN(want) {
		t.Errorf("straight: %v, %v; ordinary: %v, %v; want NaN from both", got, gotErr, want, wantErr)
	}
	in, ctx := &straightIn{X: 2}, t.Context()
	if allocs := testing.AllocsPerRun(100, func() {
		if _, err := straight.Run(ctx, in); err != nil {
			t.Fatal(err)
		}
	}); allocs != 0 {
		t.Errorf("a straight run allocated %v times, want 0", allocs)
	}
}

// A host's function called from a straight program sees the request's
// context as in any run: a cancelled request fails before the call, as the
// ordinary run fails.
func TestAStraightProgramHonoursTheDeadline(t *testing.T) {
	t.Parallel()
	binding, err := compile.Bind[straightIn, int64](straightRegistry(t))
	if err != nil {
		t.Fatal(err)
	}
	program, err := binding.Compile(`h.twice_v1(a) + b`)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	in := &straightIn{A: 1, B: 2}
	args := []machine.Value{machine.Int(1), machine.Int(2), machine.Float(0), machine.String(""), machine.Bool(false), must(machine.ToValue([]int64(nil)))}
	_, got := program.Run(ctx, in)
	_, want := program.Runtime().RunValues(ctx, args)
	if !machine.Straight(program) || got == nil || want == nil || got.Error() != want.Error() {
		t.Errorf("straight %v: %v; ordinary: %v; want the same failure", machine.Straight(program), got, want)
	}
}
