package lsp

import (
	"fmt"
	"testing"
)

// Positions count UTF-16 code units unless the client agreed to UTF-8.
func TestPositionsCountTheAgreedUnits(t *testing.T) {
	t.Parallel()
	text := `{"好😀": nope(x)}`
	for capabilities, want := range map[string]float64{`{}`: 8, `{"capabilities":{"general":{"positionEncodings":["utf-8"]}}}`: 12} {
		t.Run(capabilities, func(t *testing.T) {
			t.Parallel()
			s := newSession(t, standard(t), capabilities)
			s.open("file:///a.fr", text)
			got := s.diagnostics("file:///a.fr")
			start := got[0].(map[string]any)["range"].(map[string]any)["start"].(map[string]any)["character"]
			if start != want {
				t.Errorf("%s: the error starts at %v, want %v", capabilities, start, want)
			}
		})
	}
	for _, line := range []int{-1, -5} {
		t.Run(fmt.Sprintf("line %d", line), func(t *testing.T) {
			t.Parallel()
			if got := newDocument("x", 0, "ab\ncd").offset(Position{Line: line, Character: 1}, utf16Encoding); got != 1 {
				t.Errorf("line %d names byte %d, want 1", line, got)
			}
		})
	}
	invalid := newDocument("x", 0, "a\xffb")
	for _, encoding := range []string{utf8Encoding, utf16Encoding} {
		t.Run("invalid byte in "+encoding, func(t *testing.T) {
			t.Parallel()
			if back := invalid.offset(invalid.position(2, encoding), encoding); back != 2 {
				t.Errorf("%s: past an invalid byte, offset 2 comes back as %d", encoding, back)
			}
		})
	}
	doc := newDocument("x", 0, "a😀b\nc")
	for _, offset := range []int{0, 1, 5, 6, 7, 8} {
		t.Run(fmt.Sprintf("offset %d", offset), func(t *testing.T) {
			t.Parallel()
			if back := doc.offset(doc.position(offset, utf16Encoding), utf16Encoding); back != offset {
				t.Errorf("offset %d comes back as %d", offset, back)
			}
		})
	}
}

func at(line, character int) map[string]int {
	return map[string]int{"line": line, "character": character}
}

func position(uri string, line, character int) map[string]any {
	return map[string]any{"textDocument": map[string]string{"uri": uri}, "position": at(line, character)}
}
