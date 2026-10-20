package compile

import (
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
