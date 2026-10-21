package compile

import (
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/nethinwei/funroute/internal/machine"
	"github.com/nethinwei/funroute/internal/syntax"
)

// An argument a function wants fixed when the rule is compiled is not found
// by calling a host's function that is not constexpr: a clock or an engine
// is neither fixed nor called while compiling.
func TestConstantArgumentsDoNotCallTheHost(t *testing.T) {
	t.Parallel()
	registry := machine.CoreRegistry()
	var calls atomic.Int64
	pattern := func() string { calls.Add(1); return "^4" }
	if err := registry.Register(machine.FunctionSpec{Name: "t.pattern_v1", Go: pattern}); err != nil {
		t.Fatal(err)
	}
	source := `matches(s, t.pattern_v1())`
	_, err := CompileExpr(source, registry, CompileOptions{Args: []ArgSpec{{Name: "s", Type: machine.StringType}}})
	if err == nil || !strings.Contains(err.Error(), "fixed when the rule is compiled") {
		t.Errorf("CompileExpr(%s) error = %v, want the argument not fixed when the rule is compiled", source, err)
	}
	if got := calls.Load(); got != 0 {
		t.Errorf("CompileExpr(%s) called the host %d times, want 0", source, got)
	}
}

// An argument that would fold, but not within the folding budget, is not
// known when the rule is compiled: it is refused as one that reads an
// argument is, not left for the run to find a bad pattern in.
func TestAConstantArgumentPastTheFoldingBudgetIsRefused(t *testing.T) {
	t.Parallel()
	registry := machine.CoreRegistry()
	if err := registry.EnableForm(machine.ForForm); err != nil {
		t.Fatal(err)
	}
	source := `matches(s, first([x for x in ["("] for y in range(100) for z in range(200)]))`
	_, err := CompileExpr(source, registry, CompileOptions{Args: []ArgSpec{{Name: "s", Type: machine.StringType}}})
	if err == nil || !strings.Contains(err.Error(), "fixed when the rule is compiled") {
		t.Errorf("CompileExpr(%s) error = %v, want the argument not fixed when the rule is compiled", source, err)
	}
}

// An argument fixed when the rule is compiled is checked whether or not the
// call around it folds: a closed call's bad pattern is the same compile
// error, in the same place, as one that reads an argument.
func TestAConstantArgumentIsCheckedWhereTheCallFolds(t *testing.T) {
	t.Parallel()
	options := CompileOptions{Args: []ArgSpec{{Name: "s", Type: machine.StringType}}}
	for _, source := range []string{`matches("abc", "(")`, `matches(s, "(")`} {
		_, err := CompileExpr(source, machine.CoreRegistry(), options)
		positioned, placed := errors.AsType[*syntax.PosError](err)
		if !errors.Is(err, machine.ErrCompile) || !placed || !strings.Contains(err.Error(), `invalid pattern "("`) {
			t.Fatalf("CompileExpr(%s) error = %v, want ErrCompile on the pattern", source, err)
		}
		if start, end := positioned.Span(); source[start:end] != `"("` {
			t.Errorf("CompileExpr(%s) error covers %q, want the pattern", source, source[start:end])
		}
	}
}
