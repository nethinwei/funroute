package machine_test

import (
	"errors"
	"testing"

	"github.com/nethinwei/funroute/internal/compile"
	"github.com/nethinwei/funroute/internal/machine"
)

// runKernel runs source with one argument x of x's own type through the core
// registry.
func runKernel(t *testing.T, source string, x machine.Value) (machine.Value, error) {
	t.Helper()
	registry := machine.CoreRegistry()
	artifact, err := compile.CompileExpr(source, registry, compile.CompileOptions{Args: []compile.ArgSpec{{Name: "x", Type: x.Type()}}})
	if err != nil {
		t.Fatalf("CompileExpr(%q) error = %v", source, err)
	}
	runtime, err := machine.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	return runtime.RunValues(t.Context(), []machine.Value{x}, machine.RunOptions{})
}

// int(float) takes a whole float inside int64 and nothing else; 2^63 is
// outside, although math.MaxInt64 converts to it.
func TestIntOfAFloatStopsBelowTwoToTheSixtyThree(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		x    float64
		fits bool
	}{{0x1p63, false}, {-0x1p63, true}, {0x1p62, true}, {2.5, false}} {
		got, err := runKernel(t, "int(x)", machine.Float(test.x))
		if fits := err == nil; fits != test.fits {
			t.Errorf("int(%v) = %v, %v, want fits %v", test.x, got.Any(), err, test.fits)
		}
	}
}

// A conversion with no answer is ErrArithmetic, the rule's or the data's, as
// a division by zero is: text that is no number, a fraction taken as an int,
// an int past what a float holds exactly.
func TestConversionsWithNoAnswerAreArithmetic(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		source string
		x      machine.Value
	}{
		{"int(x)", machine.String("seven")},
		{"float(x)", machine.String("seven")},
		{"int(x)", machine.Float(2.5)},
		{"float(x)", machine.Int(1<<53 + 1)},
	} {
		if got, err := runKernel(t, test.source, test.x); !errors.Is(err, machine.ErrArithmetic) {
			t.Errorf("%s with x = %v: %v, %v, want ErrArithmetic", test.source, test.x.Any(), got.Any(), err)
		}
	}
}
