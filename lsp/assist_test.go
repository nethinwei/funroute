package lsp

import (
	"slices"
	"strings"
	"testing"

	"github.com/nethinwei/funroute/internal/machine"
	"github.com/nethinwei/funroute/internal/money"
)

func TestFormattingAndHover(t *testing.T) {
	t.Parallel()
	s := newSession(t, standard(t), `{}`)
	s.notify("github.com/nethinwei/funroute/setContract", contract("fee:int", "x:int"))
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
	s.notify("github.com/nethinwei/funroute/setContract", map[string]any{"contract": map[string]any{"args": []map[string]string{
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
			s.notify("github.com/nethinwei/funroute/setContract", contract("fee:int"))
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
	list := s.request("github.com/nethinwei/funroute/arguments", docParams("file:///a.fr")).([]any)
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

// withMoney is standard with two currencies declared, rounding half up.
func withMoney(t *testing.T) *machine.Registry {
	t.Helper()
	registry := standard(t)
	err := registry.DeclareMoney(money.MoneySpec{Currencies: []money.CurrencySpec{
		{Code: "USD", Digits: 2}, {Code: "JPY", Digits: 0},
	}})
	if err != nil {
		t.Fatal(err)
	}
	return registry
}

// After @ the rounding modes are offered; the declared currencies are
// offered as names, written bare; and a money literal's hover says what it
// is in minor units.
func TestMoneyIsCompletedAndExplained(t *testing.T) {
	t.Parallel()
	s := newSession(t, withMoney(t), `{}`)
	s.notify("github.com/nethinwei/funroute/setContract", contract("amount:money"))
	s.open("file:///a.fr", "round(amount * 2.9% + USD 1.70, @")
	got := " " + strings.Join(labels(s.request("textDocument/completion", position("file:///a.fr", 0, 33))), " ") + " "
	for _, want := range []string{"half_up", "half_even"} {
		if !strings.Contains(got, " "+want+" ") {
			t.Errorf("after @ the completion lacks %s: %s", want, got)
		}
	}
	if strings.Contains(got, " USD ") {
		t.Errorf("after @ a currency is offered, but currencies are no enum: %s", got)
	}
	s.open("file:///c.fr", "amount -> ")
	names := " " + strings.Join(labels(s.request("textDocument/completion", position("file:///c.fr", 0, 10))), " ") + " "
	for _, want := range []string{"USD", "JPY"} {
		if !strings.Contains(names, " "+want+" ") {
			t.Errorf("the completion lacks the currency %s: %s", want, names)
		}
	}
	s.open("file:///b.fr", "amount + USD 1.70")
	shown := s.request("textDocument/hover", position("file:///b.fr", 0, 10)).(map[string]any)
	if value := shown["contents"].(map[string]any)["value"].(string); !strings.Contains(value, "money") || !strings.Contains(value, "最小单位 170") {
		t.Errorf("hover on USD 1.70 is %q, want its type and 最小单位 170", value)
	}
}

// withKWD is standard with a two-place, a zero-place and a three-place
// currency, rounding half even.
func withKWD(t *testing.T) *machine.Registry {
	t.Helper()
	registry := standard(t)
	err := registry.DeclareMoney(money.MoneySpec{Currencies: []money.CurrencySpec{
		{Code: "USD", Digits: 2}, {Code: "JPY", Digits: 0}, {Code: "KWD", Digits: 3},
	}})
	if err != nil {
		t.Fatal(err)
	}
	return registry
}

// A member that a contract enum shares with another enum — here a rounding
// mode — is offered the way it has to be written, qualified by each of its
// enums; the rest are offered bare, each with the enum it belongs to. A
// member shaped like a code is an enum member after @, not a currency.
func TestCompletionQualifiesMembersTheContractShares(t *testing.T) {
	t.Parallel()
	s := newSession(t, withKWD(t), `{}`)
	s.notify("github.com/nethinwei/funroute/setContract", map[string]any{"contract": map[string]any{"args": []map[string]string{
		{"name": "ccy", "type": "enum<ccy>{USD,EUR}"}, {"name": "mode", "type": "enum<mode>{half_up,fast}"},
	}}})
	s.open("file:///a.fr", "ccy == @")
	items := s.request("textDocument/completion", position("file:///a.fr", 0, 8)).([]any)
	details := map[string]string{}
	for _, item := range items {
		entry := item.(map[string]any)
		details[entry["label"].(string)] = entry["detail"].(string)
	}
	want := map[string]string{
		"EUR": "enum<ccy>", "USD": "enum<ccy>",
		"fast": "enum<mode>", "mode.half_up": "enum<mode>", "rounding.half_up": "enum<rounding>", "half_even": "enum<rounding>", "floor": "enum<rounding>",
		"all_last": "enum<allocation>", "largest_remainder": "enum<allocation>",
	}
	for label, detail := range want {
		if details[label] != detail {
			t.Errorf("after @ %q has detail %q, want %q (offered %v)", label, details[label], detail, details)
		}
	}
	for _, bare := range []string{"half_up", "JPY"} {
		if _, offered := details[bare]; offered {
			t.Errorf("after @ the shared member %q is offered bare: %v", bare, details)
		}
	}
	if len(details) != 17 {
		t.Errorf("after @ %d members are offered, want 17 (2 ccy, 2 mode, 7 roundings, 6 allocations): %v", len(details), details)
	}
}

// A registry without money has no currency or rounding to offer.
func TestCompletionOffersNoCurrencyWithoutMoney(t *testing.T) {
	t.Parallel()
	s := newSession(t, standard(t), `{}`)
	s.notify("github.com/nethinwei/funroute/setContract", map[string]any{"contract": map[string]any{"args": []map[string]string{{"name": "ch", "type": "enum<channel>{adyen,stripe}"}}}})
	s.open("file:///a.fr", "ch == @")
	if got := labels(s.request("textDocument/completion", position("file:///a.fr", 0, 7))); strings.Join(got, ",") != "adyen,stripe" {
		t.Errorf("after @ without money the completion is %v, want [adyen stripe]", got)
	}
}

// The hover on an amount says what it is in minor units, by the places the
// registry gives its currency — with its sign — and covers the literal.
func TestHoverSaysAnAmountInMinorUnits(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		text, typ, note string
		character       int
		start, end      float64
	}{
		{"USD 1.70", "USD 1.70: money", "最小单位 170（USD 保留 2 位小数）", 1, 0, 8},
		{"USD -1.70", "USD -1.70: money", "最小单位 -170（USD 保留 2 位小数）", 0, 0, 9},
		{"USD -1.70", "USD -1.70: money", "最小单位 -170（USD 保留 2 位小数）", 6, 0, 9},
		{"JPY 1000", "JPY 1000: money", "最小单位 1000（JPY 保留 0 位小数）", 5, 0, 8},
		{"KWD 1.234", "KWD 1.234: money", "最小单位 1234（KWD 保留 3 位小数）", 0, 0, 9},
		{"k + KWD -0.001", "KWD -0.001: money", "最小单位 -1（KWD 保留 3 位小数）", 6, 4, 14},
		{"USD 0", "USD 0: money", "最小单位 0（USD 保留 2 位小数）", 4, 0, 5},
	} {
		t.Run(test.text, func(t *testing.T) {
			t.Parallel()
			s := newSession(t, withKWD(t), `{}`)
			s.notify("github.com/nethinwei/funroute/setContract", contract("k:money"))
			s.open("file:///a.fr", test.text)
			shown := s.request("textDocument/hover", position("file:///a.fr", 0, test.character)).(map[string]any)
			value := shown["contents"].(map[string]any)["value"].(string)
			if !strings.Contains(value, test.typ) || !strings.Contains(value, test.note) {
				t.Errorf("hover on %q at %d = %q, want %q and %q", test.text, test.character, value, test.typ, test.note)
			}
			checkRange(t, test.text, shown["range"], 0, test.start, 0, test.end)
		})
	}
}

// A ratio and a money value that is not a literal have a type but no minor
// units to tell.
func TestHoverOnlyExplainsAmountLiterals(t *testing.T) {
	t.Parallel()
	s := newSession(t, withKWD(t), `{}`)
	s.notify("github.com/nethinwei/funroute/setContract", contract("amount:money"))
	for text, character := range map[string]int{"round(amount * 2.9%, @half_even)": 10, "amount": 2, "money(170, USD)": 1} {
		s.open("file:///a.fr", text)
		shown, _ := s.request("textDocument/hover", position("file:///a.fr", 0, character)).(map[string]any)
		value, _ := shown["contents"].(map[string]any)["value"].(string)
		if value == "" || strings.Contains(value, "位小数）") {
			t.Errorf("hover on %q at %d = %q, want a type and no minor units", text, character, value)
		}
	}
}

// An amount written with separators or more space than one is the same
// literal, and its hover says the same minor units.
func TestHoverExplainsAmountsHoweverSpaced(t *testing.T) {
	t.Parallel()
	for text, note := range map[string]string{"JPY 1_000": "最小单位 1000", "USD  1.70": "最小单位 170", "USD\t1.70": "最小单位 170"} {
		s := newSession(t, withKWD(t), `{}`)
		s.open("file:///a.fr", text)
		shown := s.request("textDocument/hover", position("file:///a.fr", 0, 1)).(map[string]any)
		if value := shown["contents"].(map[string]any)["value"].(string); !strings.Contains(value, note) {
			t.Errorf("hover on %q = %q, want %q", text, value, note)
		}
	}
}

// A function's hover and its completion show its examples, a form's
// completion its own, each a call with the value it gives.
func TestHoverAndCompletionShowExamples(t *testing.T) {
	t.Parallel()
	s := newSession(t, standard(t), `{}`)
	s.open("file:///a.fr", "len([1]) + sw")
	shown := s.request("textDocument/hover", position("file:///a.fr", 0, 1)).(map[string]any)
	if value := shown["contents"].(map[string]any)["value"].(string); !strings.Contains(value, "len([1, 2, 3])  // 3") {
		t.Errorf("hover on len = %q, want its examples", value)
	}
	documented := map[string]string{}
	for _, item := range s.request("textDocument/completion", position("file:///a.fr", 0, 13)).([]any) {
		entry := item.(map[string]any)
		if doc, ok := entry["documentation"].(map[string]any); ok {
			documented[entry["label"].(string)] = doc["value"].(string)
		}
	}
	for label, want := range map[string]string{"len": `len("日本円")  // 3`, "switch": `else => "many")  // "few"`} {
		if !strings.Contains(documented[label], want) {
			t.Errorf("completion of %s documents %q, want it to contain %q", label, documented[label], want)
		}
	}
}
