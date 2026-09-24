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
			start := as[map[string]any](t, as[map[string]any](t, as[map[string]any](t, got[0])["range"])["start"])["character"]
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

// A line ends at \n, \r\n or a lone \r, as the protocol counts them, and a
// position past a line's end is its end — before the terminator, all of it.
func TestLinesEndWhereTheProtocolSays(t *testing.T) {
	t.Parallel()
	for text, want := range map[string]struct {
		past     int
		position Position
	}{
		"a\r\nb": {past: 1, position: Position{Line: 1, Character: 0}},
		"a\rb":   {past: 1, position: Position{Line: 1, Character: 0}},
		"a\nb":   {past: 1, position: Position{Line: 1, Character: 0}},
	} {
		t.Run(fmt.Sprintf("%q", text), func(t *testing.T) {
			t.Parallel()
			doc := newDocument("x", 0, text)
			if got := doc.offset(Position{Line: 0, Character: 99}, utf16Encoding); got != want.past {
				t.Errorf("past the end of line 0 names byte %d, want %d", got, want.past)
			}
			if got := doc.position(len(text)-1, utf16Encoding); got != want.position {
				t.Errorf("b is at %+v, want %+v", got, want.position)
			}
		})
	}
	if got := newDocument("x", 0, "a\r\nb").position(2, utf16Encoding); got != (Position{Line: 0, Character: 1}) {
		t.Errorf("the \\n of a \\r\\n is at %+v, want the end of line 0", got)
	}
}

// position names a character on the first line of the document at uri.
func position(uri string, character int) map[string]any {
	return map[string]any{"textDocument": map[string]string{"uri": uri}, "position": map[string]int{"line": 0, "character": character}}
}
