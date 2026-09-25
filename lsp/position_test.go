package lsp

import (
	"fmt"
	"math/rand/v2"
	"sort"
	"strconv"
	"strings"
	"testing"
	"unicode/utf16"
	"unicode/utf8"
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

// Positions found by a binary search over a line's wide characters are the
// ones a walk from the line's start finds, in both encodings, for every byte
// and every position, on texts mixing ASCII, two-, three- and four-byte
// characters, bytes that are not UTF-8 and every line terminator.
func TestPositionsAreTheWalkFromTheLinesStart(t *testing.T) {
	t.Parallel()
	pieces := []string{"a", "b", " ", "é", "银", "😀", "\xff", "\xe9\x93", "\n", "\r\n", "\r", "\ufffd"}
	random := rand.New(rand.NewPCG(3, 4))
	for trial := range 300 {
		var text strings.Builder
		for range random.IntN(40) {
			text.WriteString(pieces[random.IntN(len(pieces))])
		}
		doc := newDocument("x", 0, text.String())
		for _, encoding := range []string{utf8Encoding, utf16Encoding} {
			assertTheWalk(t, trial, doc, encoding)
		}
	}
}

// assertTheWalk compares doc's positions and offsets with the walk.
func assertTheWalk(t *testing.T, trial int, doc *document, encoding string) {
	t.Helper()
	for offset := -1; offset <= len(doc.text)+1; offset++ {
		if got, want := doc.position(offset, encoding), walkedPosition(doc, offset, encoding); got != want {
			t.Fatalf("trial %d, %q in %s: position(%d) = %+v, the walk says %+v", trial, doc.text, encoding, offset, got, want)
		}
	}
	for line := -1; line <= len(doc.lines); line++ {
		for character := -1; character <= len(doc.text)+2; character++ {
			at := Position{Line: line, Character: character}
			if got, want := doc.offset(at, encoding), walkedOffset(doc, at, encoding); got != want {
				t.Fatalf("trial %d, %q in %s: offset(%+v) = %d, the walk says %d", trial, doc.text, encoding, at, got, want)
			}
		}
	}
}

// walkedPosition and walkedOffset are how positions were found before each
// line kept its wide characters: by counting from the line's start.
func walkedPosition(d *document, offset int, encoding string) Position {
	offset = min(max(offset, 0), len(d.text))
	line := sort.Search(len(d.lines), func(i int) bool { return d.lines[i] > offset }) - 1
	start, end := d.lines[line], min(offset, d.ends[line])
	return Position{Line: line, Character: walkedUnits(d.text[start:end], encoding)}
}

func walkedOffset(d *document, position Position, encoding string) int {
	line := max(position.Line, 0)
	if line >= len(d.lines) {
		return len(d.text)
	}
	start, end := d.lines[line], d.ends[line]
	for at, counted := start, 0; at < end; {
		if counted >= position.Character {
			return at
		}
		_, size := utf8.DecodeRuneInString(d.text[at:end])
		counted += walkedUnits(d.text[at:at+size], encoding)
		at += size
	}
	return end
}

func walkedUnits(text string, encoding string) int {
	if encoding == utf8Encoding {
		return len(text)
	}
	count := 0
	for _, r := range text {
		count += utf16.RuneLen(r)
	}
	return count
}

// Every position on a line of 50000 characters, half of them wide, costs a
// logarithm of the line: the total grows with the line, not its square.
func BenchmarkPositionsOnALongLine(b *testing.B) {
	for _, n := range []int{5_000, 50_000} {
		doc := newDocument("x", 0, strings.Repeat("a银", n/2))
		b.Run(strconv.Itoa(n), func(b *testing.B) {
			for b.Loop() {
				everyPosition(doc)
			}
		})
	}
}

// everyPosition finds the position of every fourth byte of doc.
func everyPosition(doc *document) {
	for offset := 0; offset < len(doc.text); offset += 4 {
		doc.position(offset, utf16Encoding)
	}
}

// position names a character on the first line of the document at uri.
func position(uri string, character int) map[string]any {
	return map[string]any{"textDocument": map[string]string{"uri": uri}, "position": map[string]int{"line": 0, "character": character}}
}
