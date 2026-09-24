package lsp

import (
	"strings"
	"testing"
)

// Semantic tokens say what each piece is: the contract's arguments are
// parameters, a local is a variable declared where it is bound.
func TestSemanticTokensNameEachPiece(t *testing.T) {
	t.Parallel()
	s := newSession(t, standard(t), `{}`)
	s.open("file:///a.fr", "let(rate = fee, rate + 1) // why")
	data := as[[]any](t, as[map[string]any](t, s.request("textDocument/semanticTokens/full", docParams("file:///a.fr")))["data"])
	var got []string
	for i := 0; i+4 < len(data); i += 5 {
		name := tokenTypes[int(as[float64](t, data[i+3]))]
		if as[float64](t, data[i+4]) == 1 {
			name += "+declaration"
		}
		got = append(got, name)
	}
	want := "keyword variable+declaration operator parameter variable operator number comment"
	if strings.Join(got, " ") != want {
		t.Errorf("tokens are %s\nwant       %s", strings.Join(got, " "), want)
	}
}

// tokenNames is the type of each semantic token of the document, in order,
// with +declaration where it is one.
func tokenNames(s *session, uri string) []string {
	s.t.Helper()
	data := as[[]any](s.t, as[map[string]any](s.t, s.request("textDocument/semanticTokens/full", docParams(uri)))["data"])
	var got []string
	for i := 0; i+4 < len(data); i += 5 {
		name := tokenTypes[int(as[float64](s.t, data[i+3]))]
		if as[float64](s.t, data[i+4]) == 1 {
			name += "+declaration"
		}
		got = append(got, name)
	}
	return got
}

// A currency code is a currency, in an amount and on its own; an amount's
// figure is a number, and the minus in front is an operator.
func TestSemanticTokensNameMoney(t *testing.T) {
	t.Parallel()
	for text, want := range map[string]string{
		"amount + USD 0.30":       "parameter operator currency number",
		"JPY -1_000":              "currency operator number",
		"money(170, USD)":         "function number currency",
		"round(amount, @half_up)": "function parameter enumMember",
		"USD + 1":                 "currency operator number",
		"150 JPY / USD":           "number currency operator currency",
	} {
		t.Run(text, func(t *testing.T) {
			t.Parallel()
			s := newSession(t, withMoney(t), `{}`)
			s.open("file:///a.fr", text)
			if got := strings.Join(tokenNames(s, "file:///a.fr"), " "); got != want {
				t.Errorf("tokens of %q are %s, want %s", text, got, want)
			}
		})
	}
}

// A ratio literal is a number with its unit, and is marked as one.
func TestSemanticTokensNameRatios(t *testing.T) {
	t.Parallel()
	s := newSession(t, withMoney(t), `{}`)
	s.open("file:///a.fr", "amount * 2.9% + 25bps")
	if got, want := strings.Join(tokenNames(s, "file:///a.fr"), " "), "parameter operator number operator number"; got != want {
		t.Errorf("tokens are %s, want %s", got, want)
	}
}
