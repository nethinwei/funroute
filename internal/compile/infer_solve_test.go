package compile

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/nethinwei/funroute/internal/machine"
	"github.com/nethinwei/funroute/internal/syntax"
)

// A call whose every candidate fails says why the last one did, where it
// did, and not that it is ambiguous among none.
func TestACallWithNoCandidateLeftSaysWhy(t *testing.T) {
	t.Parallel()
	_, err := CompileExpr("min(xs).x", machine.CoreRegistry(), CompileOptions{})
	var positioned *syntax.PosError
	if !errors.As(err, &positioned) || strings.Contains(err.Error(), "ambiguous") || !strings.Contains(err.Error(), `"x" is read off`) {
		t.Fatalf("CompileExpr(min(xs).x) error = %v, want the field read refused, with its position", err)
	}
}

// A candidate's cost is the same number on every platform: two types left
// open, or two thousand literals read as amounts, are past what a 32-bit int
// holds, and wrapping would rank the candidates otherwise there.
func TestOverloadsRankAlikeOnEveryPlatform(t *testing.T) {
	t.Parallel()
	artifact, err := CompileExpr("at(xs, i)", machine.CoreRegistry(), CompileOptions{})
	if err != nil {
		t.Fatalf("at(xs, i) with no contract: %v", err)
	}
	if got := artifact.Result().String(); got != "string" {
		t.Errorf("at(xs, i) with no contract answers %s, want string", got)
	}
	zeros := "sum([" + strings.Repeat("0, ", 2047) + "0])"
	artifact, err = CompileExpr(zeros, moneyRegistry(t), CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got := artifact.Result().String(); got != "int" {
		t.Errorf("the sum of 2048 zeros under money answers %s, want int", got)
	}
}

// A trial leaves the variables as it found them: the ones it made are gone,
// and the ones made before it keep what they were, whatever the trial bound
// them to.
func TestATrialDropsTheVariablesItMade(t *testing.T) {
	t.Parallel()
	s := newInferState()
	before := s.fresh()
	s.endTrial(func() (int, int) {
		trail, vars := s.trial()
		made := s.fresh()
		s.bind(before.id, made)
		s.bind(made.id, scalarTerm(machine.IntKind))
		return trail, vars
	}())
	if len(s.vars) != before.id+1 {
		t.Fatalf("after a trial that made a variable, len(vars) = %d, want %d", len(s.vars), before.id+1)
	}
	if slot := s.vars[before.id]; slot.bound {
		t.Errorf("variable %d made before the trial is still bound to %s, want it open", before.id, s.describe(slot.term))
	}
	if s.probing != 0 {
		t.Errorf("after the trial, probing = %d, want 0", s.probing)
	}
}

// A narrowing that fails leaves no choice queued, and none marked as if it
// were: what queued them is about to be taken back.
func TestAFailedNarrowingLeavesNothingQueued(t *testing.T) {
	t.Parallel()
	s := newInferState()
	abs := machine.OverloadsOf(machine.CoreRegistry(), "abs")
	text := s.fresh()
	s.bind(text.id, scalarTerm(machine.StringKind))
	failing := &choice{node: &syntax.CallExpr{Name: "abs"}, args: []typeTerm{text}, result: s.fresh(), open: abs, queued: true}
	waiting := &choice{node: &syntax.CallExpr{Name: "abs"}, args: []typeTerm{s.fresh()}, result: s.fresh(), open: abs, queued: true}
	s.queue = append(s.queue, failing, waiting)
	if err := s.propagate(); err == nil {
		t.Fatal("propagate narrowed abs(string) without an error, want no overload")
	}
	if len(s.queue) != 0 || waiting.queued {
		t.Errorf("after the failure the queue holds %d choices and the one behind is queued = %v, want none and false", len(s.queue), waiting.queued)
	}
}

// A candidate decide takes and whose consequences fail is dropped, and leaves
// nothing behind: not its bindings, not the variables it made, not a choice
// queued.
func TestADroppedCandidateLeavesNoVariables(t *testing.T) {
	t.Parallel()
	registry := machine.NewRegistry()
	for _, param := range []machine.Type{machine.IntType, machine.FloatType} {
		typeT := machine.TypeVar("T")
		spec := machine.FunctionSpec{Name: "t.first_v1", Params: []machine.Type{param, machine.ArrayOf(typeT)}, Result: typeT,
			Eval: func(context.Context, []machine.Value) (machine.Value, error) { return machine.Value{}, nil }}
		if err := registry.Register(spec); err != nil {
			t.Fatal(err)
		}
	}
	s := newInferState()
	arg := s.fresh()
	c := &choice{node: &syntax.CallExpr{Name: "t.first_v1"}, args: []typeTerm{arg, s.fresh()}, result: s.fresh()}
	if err := s.offer(c, machine.OverloadsOf(registry, "t.first_v1")); err != nil || len(c.open) != 2 {
		t.Fatalf("offer(t.first_v1(x, xs)) = %v with %d candidates open, want no error and 2", err, len(c.open))
	}
	// The int candidate is the cheaper, and what follows from it fails.
	if err := s.waitFor(func() (bool, error) {
		switch s.deref(arg).kind {
		case machine.VarKind:
			return false, nil
		case machine.IntKind:
			return false, errors.New("not an int")
		}
		return true, nil
	}, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	vars := len(s.vars)
	if decided, err := s.decide(); !decided || err != nil {
		t.Fatalf("decide() = %v, %v, want true, nil", decided, err)
	}
	if len(c.open) != 1 || c.open[0].Params[0].Kind() != machine.FloatKind || s.deref(arg).kind != machine.VarKind {
		t.Errorf("after the int candidate failed, %d candidates are open and the argument is %s, want the float one and open", len(c.open), s.describe(arg))
	}
	if len(s.vars) != vars || len(s.queue) != 0 {
		t.Errorf("after the dropped candidate, len(vars) = %d and len(queue) = %d, want %d and 0", len(s.vars), len(s.queue), vars)
	}
}
