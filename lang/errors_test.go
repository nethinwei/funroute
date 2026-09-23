package lang_test

import (
	"errors"
	"testing"

	"funroute/lang"
)

// A compile error carries where it happened; the text stays with the host, so
// turning the offset into a line and a column is a call the host makes.
func TestHostLocatesACompileError(t *testing.T) {
	t.Parallel()
	source := "amount\n  + \"x\""
	_, err := lang.CompileExpr(source, lang.CoreRegistry(), lang.CompileOptions{
		Args: []lang.ArgSpec{{Name: "amount", Type: lang.IntType}},
	})
	if err == nil {
		t.Fatalf("CompileExpr(%q) error = nil, want a type error: a string cannot be added to an int", source)
	}
	if _, ok := errors.AsType[*lang.PositionError](err); !ok {
		t.Fatalf("CompileExpr(%q) error = %v, want a *PositionError", source, err)
	}
	line, column, ok := lang.LineColumn(err, source)
	if !ok || line != 2 || column != 3 {
		t.Fatalf("LineColumn = %d:%d (ok=%v), want 2:3", line, column, ok)
	}
	if !errors.Is(err, lang.ErrCompile) {
		t.Fatalf("error = %v, want ErrCompile: a positioned error is still a compile error", err)
	}
}
