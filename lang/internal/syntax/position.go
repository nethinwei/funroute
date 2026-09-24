package syntax

import (
	"errors"
	"fmt"
)

// PosError is a compile-time error that knows where in the source it happened.
// The position is a byte offset, because that is what the lexer and the parser
// have; turning it into a line and a column needs the text, which only the
// caller still holds — a program that arrived as ExprJSON has no text at all.
// So the error carries the offset and the host decides how to show it, rather
// than the compiler formatting a location nobody can use.
//
// Offset is where the message points; Span is the source it is about, which
// is more than one character for a node — `1 + "a"`, not just its "+". When
// nothing larger is known, the span is the offset itself. Only this package
// makes one, so a host reads it and cannot move it.
type PosError struct {
	pos        int
	start, end int
	message    string
	// cause is the error this one places, when it places one (AroundError).
	cause error
}

func (e *PosError) Error() string { return e.message }

// Offset is the byte offset the message points at.
func (e *PosError) Offset() int { return e.pos }

// Span is the byte range [start, end) of the source the error is about.
func (e *PosError) Span() (start, end int) { return e.start, e.end }

// Unwrap is the error AroundError placed, so its category still answers
// errors.Is.
func (e *PosError) Unwrap() error { return e.cause }

// At builds a positioned error. Every message the lexer, the parser and type
// inference produce goes through here or through Around, so a host can always
// ask where.
func At(pos int, format string, args ...any) error {
	return &PosError{pos: pos, start: pos, end: pos, message: fmt.Sprintf(format, args...)}
}

// Around builds an error about a node: it points where the node's position
// is and covers the source the node was read from.
func Around(expr Expr, format string, args ...any) error {
	extent := expr.Extent()
	return &PosError{pos: expr.Position(), start: extent.Start, end: extent.End, message: fmt.Sprintf(format, args...)}
}

// AroundError places an error that already happened — one from running a
// closed expression at compile time — on the node it came from, keeping it
// for errors.Is.
func AroundError(expr Expr, err error) error {
	extent := expr.Extent()
	return &PosError{pos: expr.Position(), start: extent.Start, end: extent.End, message: err.Error(), cause: err}
}

// over builds an error about a stretch of source: a token, or what a lexeme
// the lexer gave up on consumed.
func over(start, end int, format string, args ...any) error {
	return &PosError{pos: start, start: start, end: end, message: fmt.Sprintf(format, args...)}
}

// LineColumn finds err's position in source, counting lines from 1 and columns
// in characters. It reports false when the error carries no position.
func LineColumn(err error, source string) (line, column int, ok bool) {
	var positioned *PosError
	if !errors.As(err, &positioned) || positioned.pos < 0 {
		return 0, 0, false
	}
	pos := min(positioned.pos, len(source))
	line, column = 1, 1
	for _, char := range source[:pos] {
		if char == '\n' {
			line, column = line+1, 1
			continue
		}
		column++
	}
	return line, column, true
}
