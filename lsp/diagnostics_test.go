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
	s.notify("github.com/nethinwei/funroute/setContract", contract("fee:int", "x:int"))
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
	s.notify("github.com/nethinwei/funroute/setContract", map[string]any{"contract": map[string]any{"args": []map[string]string{{"name": "fee", "type": "nope"}}}})
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

// checkRange fails unless got is the LSP range from line:start to line:end.
func checkRange(t *testing.T, text string, got any, startLine, start, endLine, end float64) {
	t.Helper()
	want := map[string]any{
		"start": map[string]any{"line": startLine, "character": start},
		"end":   map[string]any{"line": endLine, "character": end},
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("%q: range = %v, want %v", text, got, want)
	}
}

// A money mistake is reported over what it is about — two currencies over
// the whole sum, an amount with too many places over the literal, an
// undeclared currency over its amount — with a message that says what to do.
func TestMoneyDiagnosticsCoverWhatTheyAreAbout(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, text, message string
		args                []string
		want                [4]float64
	}{
		{"two currencies", "USD 1 +\n JPY 2", "currency mismatch: USD and JPY", nil, [4]float64{0, 0, 1, 6}},
		{"too many places", "let(a = 1, a +\n  USD 1.234)", "at most 2 fit", nil, [4]float64{1, 2, 1, 11}},
		{"undeclared currency", "x + EUR 1", `"EUR" is not declared`, []string{"x:int"}, [4]float64{0, 4, 0, 9}},
		{"overflow", "USD 99999999999999999.99", "overflows int64", nil, [4]float64{0, 0, 0, 24}},
		{"an int for money", "amount + 5", "money(n, currency(同币种金额))", []string{"amount:money"}, [4]float64{0, 0, 0, 10}},
		{"a float for a rate", "amount * risk", "ratio(\"0.029\")", []string{"amount:money", "risk:float"}, [4]float64{0, 0, 0, 13}},
		{"money divided", "amount / 3", "allocate(m, n)", []string{"amount:money"}, [4]float64{0, 0, 0, 10}},
		{"nothing rounds", "round(USD 1 + USD 2, @half_up)", "nothing inside round", nil, [4]float64{0, 0, 0, 30}},
		{"unknown mode", "round(amount * 2.9%, @sideways)", "@sideways", []string{"amount:money"}, [4]float64{0, 21, 0, 30}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			s := newSession(t, withMoney(t), `{}`)
			s.notify("github.com/nethinwei/funroute/setContract", contract(test.args...))
			s.open("file:///a.fr", test.text)
			checkOneDiagnostic(t, s, test.text, test.message, test.want)
		})
	}
}

// Money in a registry that declares none is an error over the literal; a
// contract that names a money type is refused as a whole.
func TestMoneyWithoutADeclarationIsReported(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		text string
		want [4]float64
	}{
		{"x + USD 1.70", [4]float64{0, 4, 0, 12}},
		{"x * 2.9%", [4]float64{0, 4, 0, 8}},
		{"x * 25bps", [4]float64{0, 4, 0, 9}},
		{"let(a = 1,\n  USD -1)", [4]float64{1, 2, 1, 8}},
	} {
		t.Run(test.text, func(t *testing.T) {
			t.Parallel()
			s := newSession(t, standard(t), `{}`)
			s.notify("github.com/nethinwei/funroute/setContract", contract("x:int"))
			s.open("file:///a.fr", test.text)
			checkOneDiagnostic(t, s, test.text, "declares no money", test.want)
		})
	}
	s := newSession(t, standard(t), `{}`)
	s.notify("github.com/nethinwei/funroute/setContract", contract("x:money"))
	s.open("file:///b.fr", "x")
	if got := s.diagnostics("file:///b.fr"); len(got) != 1 || !strings.Contains(fmt.Sprint(got), "declares money") {
		t.Errorf("a money contract without money declared is reported as %v, want one diagnostic saying money is not declared", got)
	}
}

// checkOneDiagnostic fails unless exactly one diagnostic is published for the
// document, containing message, over the range want.
func checkOneDiagnostic(t *testing.T, s *session, text, message string, want [4]float64) {
	t.Helper()
	got := s.diagnostics("file:///a.fr")
	if len(got) != 1 {
		t.Fatalf("%q: diagnostics = %v, want one", text, got)
	}
	diagnostic := got[0].(map[string]any)
	if !strings.Contains(diagnostic["message"].(string), message) {
		t.Errorf("%q: message = %q, want it to contain %q", text, diagnostic["message"], message)
	}
	checkRange(t, text, diagnostic["range"], want[0], want[1], want[2], want[3])
}
