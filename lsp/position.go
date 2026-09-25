package lsp

import (
	"sort"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/nethinwei/funroute/internal/compile"
	"github.com/nethinwei/funroute/internal/syntax"
)

// The language counts in bytes; a client counts in lines and in characters
// of the encoding the two agreed on at initialize. UTF-16 is the protocol's
// default; UTF-8 needs no counting within a line.
//
// A character whose UTF-16 length is its UTF-8 length — ASCII, and a byte
// that is not UTF-8, which counts one either way — needs no counting in
// UTF-16 either. So each line keeps only its other characters, with how many
// units short of its bytes the line is by the end of each: a column is its
// bytes less that shortfall, found by a binary search, and a line that is all
// ASCII keeps nothing. However long a line is, a position costs a logarithm,
// not a walk from the line's start.

const (
	utf8Encoding  = "utf-8"
	utf16Encoding = "utf-16"
)

// document is one open text and where each of its lines starts and ends.
// A line ends at \n, \r\n or a lone \r, as the protocol counts them, and
// its end is where that terminator starts.
type document struct {
	uri     string
	version int
	text    string
	lines   []int
	ends    []int
	// wide is, by line, the characters whose UTF-16 length is not their
	// UTF-8 length; a line with none has no entry.
	wide map[int]*wideChars

	// What the language made of the text, worked out on first use: every
	// request on one version reads the same answer. A change is a new
	// document; a new contract clears the analysis (Server.setContract).
	lexemes     []syntax.Lexeme
	lexed       bool
	analysis    *compile.Analysis
	analysisErr error
	analyzed    bool
}

// wideChars is a line's characters of more than one byte, in order: where
// each starts and ends in the text, and short, the UTF-8 bytes less the
// UTF-16 units of the line up to the end of each.
type wideChars struct {
	start, end, short []int
}

func (w *wideChars) add(start, size int, r rune) {
	short := size - utf16.RuneLen(r)
	if n := len(w.short); n > 0 {
		short += w.short[n-1]
	}
	w.start, w.end, w.short = append(w.start, start), append(w.end, start+size), append(w.short, short)
}

// shortBefore is how many units short of its bytes the line is by the end
// of its first n wide characters.
func (w *wideChars) shortBefore(n int) int {
	if n == 0 {
		return 0
	}
	return w.short[n-1]
}

func newDocument(uri string, version int, text string) *document {
	d := &document{uri: uri, version: version, text: text, lines: []int{0}}
	for i := 0; i < len(text); i++ {
		switch c := text[i]; {
		case c >= utf8.RuneSelf:
			i += d.addWide(i) - 1
		case c == '\n' || c == '\r':
			d.ends = append(d.ends, i)
			if c == '\r' && i+1 < len(text) && text[i+1] == '\n' {
				i++
			}
			d.lines = append(d.lines, i+1)
		}
	}
	d.ends = append(d.ends, len(text))
	return d
}

// addWide records the character at byte i of the current line when it is
// one, and returns how many bytes it takes. A byte that is not UTF-8 is one
// byte and one unit, as ASCII is.
func (d *document) addWide(i int) int {
	r, size := utf8.DecodeRuneInString(d.text[i:])
	if size == 1 {
		return 1
	}
	line := len(d.lines) - 1
	if d.wide == nil {
		d.wide = map[int]*wideChars{}
	}
	if d.wide[line] == nil {
		d.wide[line] = &wideChars{}
	}
	d.wide[line].add(i, size, r)
	return size
}

// position is where byte offset falls, counted in encoding. A byte inside a
// line's terminator is at the line's end; one inside a character counts each
// byte of it before offset as a unit, as a byte that is not UTF-8 would.
func (d *document) position(offset int, encoding string) Position {
	offset = min(max(offset, 0), len(d.text))
	line := sort.SearchInts(d.lines, offset+1) - 1
	start, end := d.lines[line], min(offset, d.ends[line])
	column := end - start
	if w := d.wide[line]; w != nil && encoding != utf8Encoding {
		column -= w.shortBefore(sort.SearchInts(w.end, end+1))
	}
	return Position{Line: line, Character: column}
}

// offset is the byte a position names. A position past a line's end is its
// end, as the protocol asks; one before the first line is on the first; one
// inside a character — within a surrogate pair, or within a UTF-8 sequence —
// is the character's end.
func (d *document) offset(position Position, encoding string) int {
	line := max(position.Line, 0)
	if line >= len(d.lines) {
		return len(d.text)
	}
	start, end := d.lines[line], d.ends[line]
	column := max(position.Character, 0)
	w := d.wide[line]
	if w == nil {
		return min(start+column, end)
	}
	if encoding == utf8Encoding {
		// The first wide character that ends past the byte, if the byte is
		// inside it, moves the position to its end.
		at := start + column
		if i := sort.SearchInts(w.end, at+1); i < len(w.end) && w.start[i] < at {
			at = w.end[i]
		}
		return min(at, end)
	}
	return min(w.offsetOf(start, column), end)
}

// offsetOf is the byte of the line starting at start that column UTF-16
// units name: past every wide character that ends within column units, the
// rest of the way one byte a unit.
func (w *wideChars) offsetOf(start, column int) int {
	unitsAt := func(byteAt, short int) int { return byteAt - start - short }
	n := sort.Search(len(w.end), func(i int) bool { return unitsAt(w.end[i], w.short[i]) > column })
	at := start + column + w.shortBefore(n)
	if n < len(w.end) && unitsAt(w.start[n], w.shortBefore(n)) < column {
		at = w.end[n]
	}
	return at
}

func (d *document) rangeOf(span syntax.Span, encoding string) Range {
	return Range{Start: d.position(span.Start, encoding), End: d.position(span.End, encoding)}
}

func (d *document) whole(encoding string) Range {
	return d.rangeOf(syntax.Span{Start: 0, End: len(d.text)}, encoding)
}
