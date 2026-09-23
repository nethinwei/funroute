package lsp

import (
	"fmt"
	"strings"
	"testing"
)

// A diagnostic covers what it is about, and a read of an argument the
// contract does not declare points at the read.
func TestDiagnosticsFollowTheText(t *testing.T) {
	t.Parallel()
	s := newSession(t, standard(t), `{}`)
	s.notify("funroute/setContract", contract("fee:int", "x:int"))
	s.open("file:///a.fr", "let(rate = fee * 2, rate + x)")
	if got := s.diagnostics("file:///a.fr"); len(got) != 0 {
		t.Fatalf("a correct program has diagnostics %v, want none", got)
	}
	s.notify("textDocument/didChange", map[string]any{
		"textDocument":   map[string]any{"uri": "file:///a.fr", "version": 2},
		"contentChanges": []map[string]string{{"text": "let(rate = fee * 2,\n  rate + y)"}},
	})
	got := s.diagnostics("file:///a.fr")
	if len(got) != 1 {
		t.Fatalf("diagnostics after reading y = %v, want one", got)
	}
	diagnostic := got[0].(map[string]any)
	want := map[string]any{"start": map[string]any{"line": 1.0, "character": 9.0}, "end": map[string]any{"line": 1.0, "character": 10.0}}
	if fmt.Sprint(diagnostic["range"]) != fmt.Sprint(want) || !strings.Contains(diagnostic["message"].(string), `"y"`) {
		t.Errorf("the undeclared read is reported as %v, want range %v and a message naming \"y\"", diagnostic, want)
	}
	s.notify("funroute/setContract", map[string]any{"contract": map[string]any{"args": []map[string]string{{"name": "fee", "type": "nope"}}}})
	if got := s.diagnostics("file:///a.fr"); len(got) != 1 || !strings.Contains(fmt.Sprint(got), "argument") {
		t.Errorf("a broken contract is reported as %v, want one diagnostic about the argument", got)
	}
}

func contract(args ...string) map[string]any {
	var declared []map[string]string
	for _, arg := range args {
		name, typ, _ := strings.Cut(arg, ":")
		declared = append(declared, map[string]string{"name": name, "type": typ, "doc": name + " 的说明"})
	}
	return map[string]any{"contract": map[string]any{"args": declared}}
}
