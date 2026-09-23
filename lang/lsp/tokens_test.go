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
	data := s.request("textDocument/semanticTokens/full", docParams("file:///a.fr")).(map[string]any)["data"].([]any)
	var got []string
	for i := 0; i+4 < len(data); i += 5 {
		name := tokenTypes[int(data[i+3].(float64))]
		if data[i+4].(float64) == 1 {
			name += "+declaration"
		}
		got = append(got, name)
	}
	want := "keyword variable+declaration operator parameter variable operator number comment"
	if strings.Join(got, " ") != want {
		t.Errorf("tokens are %s\nwant       %s", strings.Join(got, " "), want)
	}
}
