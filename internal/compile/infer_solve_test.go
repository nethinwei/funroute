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
