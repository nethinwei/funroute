package lsp

import (
	"slices"
	"strings"
	"testing"
)

func TestFormattingAndHover(t *testing.T) {
	t.Parallel()
	s := newSession(t, standard(t), `{}`)
	s.notify("funroute/setContract", contract("fee:int", "x:int"))
	s.open("file:///a.fr", "let(rate=fee*2,rate+x)\n")
	edits := s.request("textDocument/formatting", docParams("file:///a.fr")).([]any)
	if len(edits) != 1 || edits[0].(map[string]any)["newText"] != "let(rate = fee * 2, rate + x)\n" {
		t.Errorf("formatting gives %v, want one edit to %q", edits, "let(rate = fee * 2, rate + x)\n")
	}
	shown := s.request("textDocument/hover", position("file:///a.fr", 0, 19)).(map[string]any)
	value := shown["contents"].(map[string]any)["value"].(string)
	if !strings.Contains(value, "rate+x: int") || !strings.Contains(value, "add(int,int)->int") {
		t.Errorf("hover on + is %q, want the type \"rate+x: int\" and the signature \"add(int,int)->int\"", value)
	}
	argument := s.request("textDocument/hover", position("file:///a.fr", 0, 10)).(map[string]any)
	if value := argument["contents"].(map[string]any)["value"].(string); !strings.Contains(value, "fee 的说明") {
		t.Errorf("hover on an argument is %q, want its contract doc \"fee 的说明\"", value)
	}
}

func TestCompletionOffersWhatThePositionCanName(t *testing.T) {
	t.Parallel()
	s := newSession(t, standard(t), `{}`)
	s.notify("funroute/setContract", map[string]any{"contract": map[string]any{"args": []map[string]string{
		{"name": "fee", "type": "int"}, {"name": "ch", "type": "enum<channel>{adyen,stripe}"},
	}}})
	s.open("file:///a.fr", "let(rate = fee, rate)")
	got := strings.Join(labels(s.request("textDocument/completion", position("file:///a.fr", 0, 17))), " ")
	for _, want := range []string{"fee", "ch", "rate", "add", "let", "switch"} {
		if !strings.Contains(" "+got+" ", " "+want+" ") {
			t.Errorf("completion lacks %s: %s", want, got)
		}
	}
	s.open("file:///b.fr", "ch == @")
	if got := labels(s.request("textDocument/completion", position("file:///b.fr", 0, 7))); strings.Join(got, ",") != "adyen,stripe" {
		t.Errorf("after @ the completion is %v, want [adyen stripe]", got)
	}
}

// Signature help works on a call still being typed, which does not parse.
func TestSignatureHelpFindsTheCallBeingTyped(t *testing.T) {
	t.Parallel()
	s := newSession(t, standard(t), `{}`)
	s.open("file:///a.fr", "if(a, [1, 2], ")
	help := s.request("textDocument/signatureHelp", position("file:///a.fr", 0, 14)).(map[string]any)
	if help["activeParameter"].(float64) != 2 || !strings.HasPrefix(help["signatures"].([]any)[0].(map[string]any)["label"].(string), "if(") {
		t.Errorf("signature help is %v, want the third parameter of if", help)
	}
}

// A record's colon or a field's dot inside an earlier argument does not
// reset the count.
func TestSignatureHelpCountsPastRecordsAndFields(t *testing.T) {
	t.Parallel()
	for text, want := range map[string]float64{"if(true, {a: 1}, ": 2, "if(r.a, ": 1, "if(true, [x for x in xs], ": 2} {
		t.Run(text, func(t *testing.T) {
			t.Parallel()
			s := newSession(t, standard(t), `{}`)
			s.open("file:///a.fr", text)
			help := s.request("textDocument/signatureHelp", position("file:///a.fr", 0, len(text))).(map[string]any)
			if help["activeParameter"].(float64) != want {
				t.Errorf("%q: the active argument is %v, want %v", text, help["activeParameter"], want)
			}
		})
	}
}

// A program being typed rarely parses, and the name being typed is rarely
// declared yet; the locals it can see are offered all the same.
func TestCompletionOffersLocalsWhileTyping(t *testing.T) {
	t.Parallel()
	cases := map[string][]string{
		"let(rate = fee, ra":               {"rate"},
		"let(rate = fee, ":                 {"rate"},
		"[x * 2 for x in xs if ":           {"x"},
		"reduce(p in ps, acc = 0, ":        {"p", "acc"},
		"let(a = 1, b = ":                  {"a"},
		"{k: v for k, v in d if ":          {"k", "v"},
		"let(a = 1, switch(case a > 1 => ": {"a"},
		"[x for x in ":                     {},
	}
	for text, want := range cases {
		t.Run(text, func(t *testing.T) {
			t.Parallel()
			s := newSession(t, standard(t), `{}`)
			s.notify("funroute/setContract", contract("fee:int"))
			s.open("file:///a.fr", text)
			got := labels(s.request("textDocument/completion", position("file:///a.fr", 0, len(text))))
			checkLocals(t, text, got, want)
		})
	}
}

// checkLocals requires every local in want among the completion got at the
// end of text; with none wanted, the loop variable x must not be offered.
func checkLocals(t *testing.T, text string, got, want []string) {
	t.Helper()
	offered := " " + strings.Join(got, " ") + " "
	for _, name := range want {
		if !strings.Contains(offered, " "+name+" ") {
			t.Errorf("%q: completion lacks the local %s: %v", text, name, got)
		}
	}
	if len(want) == 0 && strings.Contains(offered, " x ") {
		t.Errorf("%q: a loop's source cannot see its variable: %v", text, got)
	}
}

// With no contract the program's arguments are what the text reads: they are
// offered in completion and listed by funroute/arguments, in order.
func TestInferredArgumentsAreOfferedAndListed(t *testing.T) {
	t.Parallel()
	s := newSession(t, standard(t), `{}`)
	s.open("file:///a.fr", "amount * bps / 10000")
	list := s.request("funroute/arguments", docParams("file:///a.fr")).([]any)
	if len(list) != 2 || list[0].(map[string]any)["name"] != "amount" || list[1].(map[string]any)["type"] != "int" {
		t.Fatalf("the arguments are %v, want amount then bps, both int", list)
	}
	if got := labels(s.request("textDocument/completion", position("file:///a.fr", 0, 0))); !slices.Contains(got, "bps") {
		t.Errorf("completion without a contract offers %v, want bps among them", got)
	}
}

func labels(items any) []string {
	var out []string
	for _, item := range items.([]any) {
		out = append(out, item.(map[string]any)["label"].(string))
	}
	return out
}
