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
// Pos is where the message points; Start and End are the source it is about,
// which is more than one character for a node — `1 + "a"`, not just its "+".
// When nothing larger is known, the range is Pos itself.
type PosError struct {
	Pos        int
	Start, End int
	Message    string
}

func (e *PosError) Error() string { return e.Message }

// At builds a positioned error. Every message the lexer, the parser and type
// inference produce goes through here or through Around, so a host can always
// ask where.
func At(pos int, format string, args ...any) error {
	return &PosError{Pos: pos, Start: pos, End: pos, Message: fmt.Sprintf(format, args...)}
}

// Around builds an error about a node: it points where the node's position
// is and covers the source the node was read from.
func Around(expr Expr, format string, args ...any) error {
	extent := expr.Extent()
	return &PosError{Pos: expr.Position(), Start: extent.Start, End: extent.End, Message: fmt.Sprintf(format, args...)}
}

// over builds an error about a stretch of source: a token, or what a lexeme
// the lexer gave up on consumed.
func over(start, end int, format string, args ...any) error {
	return &PosError{Pos: start, Start: start, End: end, Message: fmt.Sprintf(format, args...)}
}

// LineColumn finds err's position in source, counting lines from 1 and columns
// in characters. It reports false when the error carries no position.
func LineColumn(err error, source string) (line, column int, ok bool) {
	var positioned *PosError
	if !errors.As(err, &positioned) || positioned.Pos < 0 {
		return 0, 0, false
	}
	pos := min(positioned.Pos, len(source))
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
