package lsp

import (
	"sort"
	"unicode/utf16"
	"unicode/utf8"

	"funroute/lang/internal/compile"
	"funroute/lang/internal/syntax"
)

// The language counts in bytes; a client counts in lines and in characters
// of the encoding the two agreed on at initialize. UTF-16 is the protocol's
// default; UTF-8 needs no counting within a line.

const (
	utf8Encoding  = "utf-8"
	utf16Encoding = "utf-16"
)

// document is one open text and where each of its lines starts.
type document struct {
	uri     string
	version int
	text    string
	lines   []int

	// What the language made of the text, worked out on first use: every
	// request on one version reads the same answer. A change is a new
	// document; a new contract clears the analysis (Server.setContract).
	lexemes     []syntax.Lexeme
	lexed       bool
	analysis    *compile.Analysis
	analysisErr error
	analyzed    bool
}

func newDocument(uri string, version int, text string) *document {
	lines := []int{0}
	for i := 0; i < len(text); i++ {
		if text[i] == '\n' {
			lines = append(lines, i+1)
		}
	}
	return &document{uri: uri, version: version, text: text, lines: lines}
}

// position is where byte offset falls, counted in encoding.
func (d *document) position(offset int, encoding string) Position {
	offset = min(max(offset, 0), len(d.text))
	line := sort.Search(len(d.lines), func(i int) bool { return d.lines[i] > offset }) - 1
	start := d.lines[line]
	return Position{Line: line, Character: units(d.text[start:offset], encoding)}
}

// offset is the byte a position names. A position past a line's end is its
// end, as the protocol asks; one before the first line is on the first. Each
// step counts the bytes of one character in the encoding, the same count
// position makes, so the two agree even on bytes that are not UTF-8.
func (d *document) offset(position Position, encoding string) int {
	line := max(position.Line, 0)
	if line >= len(d.lines) {
		return len(d.text)
	}
	start, end := d.lines[line], len(d.text)
	if line+1 < len(d.lines) {
		end = d.lines[line+1] - 1
	}
	for at, counted := start, 0; at < end; {
		if counted >= position.Character {
			return at
		}
		_, size := utf8.DecodeRuneInString(d.text[at:end])
		counted += units(d.text[at:at+size], encoding)
		at += size
	}
	return end
}

func (d *document) rangeOf(span syntax.Span, encoding string) Range {
	return Range{Start: d.position(span.Start, encoding), End: d.position(span.End, encoding)}
}

func (d *document) whole(encoding string) Range {
	return d.rangeOf(syntax.Span{Start: 0, End: len(d.text)}, encoding)
}

// units is how long text is in the encoding's code units.
func units(text string, encoding string) int {
	if encoding == utf8Encoding {
		return len(text)
	}
	count := 0
	for _, r := range text {
		count += utf16.RuneLen(r)
	}
	return count
}
