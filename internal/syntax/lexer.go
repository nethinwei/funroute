package syntax

import (
	"errors"
	"strings"
	"unicode/utf8"
)

type tokenKind uint8

const (
	tokenEOF tokenKind = iota
	tokenIdentifier
	tokenInt
	tokenFloat
	tokenString
	tokenLeftParen
	tokenRightParen
	tokenLeftBracket
	tokenRightBracket
	tokenLeftBrace
	tokenRightBrace
	tokenComma
	tokenColon
	tokenPlus
	tokenMinus
	tokenStar
	tokenSlash
	tokenPercent
	tokenDot
	tokenLess
	tokenLessEq
	tokenGreater
	tokenGreaterEq
	tokenEqEq
	tokenBangEq
	tokenAndAnd
	tokenOrOr
	tokenBang
	tokenFatArrow
	tokenArrow
	tokenAssign
	// tokenEnum is @member or @enum.member: an enum member reference. The "@"
	// keeps it apart from a string and keeps "." out of expression syntax.
	tokenEnum
	// tokenRatio is a number written with a ratio's unit, 2.9% or 25bps; its
	// text keeps the unit.
	tokenRatio
	// tokenInvalid is text the lexer could not read. It is produced only so
	// the lexing can go on past it; the error that goes with it is reported.
	tokenInvalid
	// tokenComment is a // line comment. It never reaches the parser.
	tokenComment
)

// operatorTokens comes from the parser's sourceOperators table and is scanned
// longest-first, so "<=" wins over "<" and "=>" over "=".
var operatorTokens = lexedOperators()

// token is one lexeme: its kind, its text as the parser reads it (a number
// without its _ separators, an enum reference without its @) and the bytes of
// source it covers, from pos up to end.
type token struct {
	kind tokenKind
	text string
	pos  int
	end  int
}

type lexer struct {
	source   string
	pos      int
	comments []token
}

// tokens lexes the whole source. A lexeme it cannot read becomes an invalid
// token and the lexing goes on, so everything after it is still known; the
// first such error is the one returned.
func (l *lexer) tokens() ([]token, error) {
	var out []token
	var first error
	for {
		tok, err := l.next()
		if err != nil && first == nil {
			first = err
		}
		out = append(out, tok)
		if tok.kind == tokenEOF {
			return out, first
		}
	}
}

// isSpace is ASCII whitespace. The source is read byte by byte, and a byte
// above 0x7F is part of a multi-byte character, never a space on its own —
// unicode.IsSpace would take a lone 0x85 or 0xA0 for one.
func isSpace(ch byte) bool {
	switch ch {
	case ' ', '\t', '\n', '\r', '\v', '\f':
		return true
	}
	return false
}

var singleCharTokens = map[byte]tokenKind{
	'(': tokenLeftParen,
	')': tokenRightParen,
	'[': tokenLeftBracket,
	']': tokenRightBracket,
	'{': tokenLeftBrace,
	'}': tokenRightBrace,
	',': tokenComma,
	':': tokenColon,
	// A dot inside a name belongs to the name (route.score_v1, order.amount);
	// this one follows something that is not a name: orders[0].amount.
	'.': tokenDot,
}

// skipSpace also eats // line comments. Comments are lexical only: they never
// reach the AST, so they do not survive an ExprJSON round trip. They are kept
// aside, because what the source says is not only what the parser reads.
func (l *lexer) skipSpace() {
	for l.pos < len(l.source) {
		if isSpace(l.source[l.pos]) {
			l.pos++
			continue
		}
		if strings.HasPrefix(l.source[l.pos:], "//") {
			start := l.pos
			for l.pos < len(l.source) && l.source[l.pos] != '\n' {
				l.pos++
			}
			l.comments = append(l.comments, token{kind: tokenComment, text: l.source[start:l.pos], pos: start, end: l.pos})
			continue
		}
		return
	}
}

func (l *lexer) next() (token, error) {
	l.skipSpace()
	if l.pos >= len(l.source) {
		return token{kind: tokenEOF, pos: l.pos, end: l.pos}, nil
	}
	start := l.pos
	tok, err := l.lexeme(start)
	if err != nil {
		bad := l.invalid(start)
		if positioned, ok := errors.AsType[*PosError](err); ok {
			positioned.end = bad.end
		}
		return bad, err
	}
	tok.end = l.pos
	return tok, nil
}

func (l *lexer) lexeme(start int) (token, error) {
	ch := l.source[l.pos]
	if kind, ok := singleCharTokens[ch]; ok {
		l.pos++
		return token{kind: kind, text: string(ch), pos: start}, nil
	}
	if operator, ok := l.operator(start); ok {
		return operator, nil
	}
	switch {
	case ch == '"':
		return l.stringToken()
	case ch >= '0' && ch <= '9':
		return l.number()
	case ch == '@':
		return l.enumMember(start)
	case isIdentifierStart(ch):
		return l.identifier(start)
	default:
		return token{}, unexpectedCharacter(l.source, start)
	}
}

// unexpectedCharacter names what stopped the lexer: the character when the
// bytes there are one, the byte when they are not UTF-8.
func unexpectedCharacter(source string, start int) error {
	r, size := utf8.DecodeRuneInString(source[start:])
	if r == utf8.RuneError && size <= 1 {
		return At(start, "syntax error: unexpected byte 0x%02x", source[start])
	}
	return At(start, "syntax error: unexpected %q", r)
}

// invalid covers what a failed lexeme consumed, at least one character, so
// the lexer always moves forward.
func (l *lexer) invalid(start int) token {
	if l.pos <= start {
		_, size := utf8.DecodeRuneInString(l.source[start:])
		l.pos = start + size
	}
	return token{kind: tokenInvalid, text: l.source[start:l.pos], pos: start, end: l.pos}
}

func (l *lexer) operator(start int) (token, bool) {
	rest := l.source[l.pos:]
	for _, operator := range operatorTokens {
		if strings.HasPrefix(rest, operator.text) {
			l.pos += len(operator.text)
			return token{kind: operator.kind, text: operator.text, pos: start}, true
		}
	}
	return token{}, false
}

// enumMember scans "@member" or "@enum.member" as one lexeme, so the dot never
// reaches expression syntax and stays available for field access later.
func (l *lexer) enumMember(start int) (token, error) {
	l.pos++
	name, err := l.memberName(start)
	if err != nil {
		return token{}, err
	}
	if l.pos < len(l.source) && l.source[l.pos] == '.' {
		l.pos++
		qualified, err := l.memberName(start)
		if err != nil {
			return token{}, err
		}
		name += "." + qualified
	}
	return token{kind: tokenEnum, text: name, pos: start}, nil
}

func (l *lexer) memberName(start int) (string, error) {
	if l.pos >= len(l.source) || !isIdentifierStart(l.source[l.pos]) {
		return "", At(start, "syntax error: @ must be followed by an enum member name")
	}
	from := l.pos
	l.pos++
	for l.pos < len(l.source) && isIdentifierPart(l.source[l.pos]) {
		l.pos++
	}
	return l.source[from:l.pos], nil
}

func (l *lexer) identifier(start int) (token, error) {
	l.pos++
	for l.pos < len(l.source) && isIdentifierPart(l.source[l.pos]) {
		l.pos++
	}
	return token{kind: tokenIdentifier, text: l.source[start:l.pos], pos: start}, nil
}

func isIdentifierStart(ch byte) bool {
	return ch == '_' || (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z')
}

func isIdentifierPart(ch byte) bool {
	return isIdentifierStart(ch) || (ch >= '0' && ch <= '9') || ch == '.'
}

func (l *lexer) number() (token, error) {
	start := l.pos
	l.digits()
	kind := tokenInt
	if l.pos < len(l.source) && l.source[l.pos] == '.' {
		kind = tokenFloat
		l.pos++
		if !l.startsDigit() {
			return token{}, At(start, "syntax error: float requires digits after '.'")
		}
		l.digits()
	}
	if l.pos < len(l.source) && (l.source[l.pos] == 'e' || l.source[l.pos] == 'E') {
		kind = tokenFloat
		l.pos++
		if l.pos < len(l.source) && (l.source[l.pos] == '+' || l.source[l.pos] == '-') {
			l.pos++
		}
		if !l.startsDigit() {
			return token{}, At(start, "syntax error: exponent requires digits")
		}
		l.digits()
		// A % or bps against a number is a ratio's unit, this one's too: and
		// a ratio is written plainly, never with an exponent.
		if unit := l.ratioUnit(); unit != "" {
			return token{}, At(start, "syntax error: %s is a ratio with an exponent: a ratio is a plain decimal, such as 1000%%", l.source[start:l.pos])
		}
		// 1_000_000 reads as a million; the separator never reaches strconv.
		return token{kind: kind, text: strings.ReplaceAll(l.source[start:l.pos], "_", ""), pos: start}, nil
	}
	text := strings.ReplaceAll(l.source[start:l.pos], "_", "")
	if unit := l.ratioUnit(); unit != "" {
		return token{kind: tokenRatio, text: text + unit, pos: start}, nil
	}
	return token{kind: kind, text: text, pos: start}, nil
}

// ratioUnit reads the unit a ratio is written with right after its number:
// bps, or a % touching the number. There is no other reading — a % against a
// number is always a ratio, so 10%3 is a ratio followed by a stray number and
// 10%-3 is 10% - 3; the remainder is written with space before the %.
func (l *lexer) ratioUnit() string {
	rest := l.source[l.pos:]
	if strings.HasPrefix(rest, "bps") && (len(rest) == 3 || !isIdentifierPart(rest[3])) {
		l.pos += 3
		return "bps"
	}
	if strings.HasPrefix(rest, "%") {
		l.pos++
		return "%"
	}
	return ""
}

func (l *lexer) startsDigit() bool {
	return l.pos < len(l.source) && l.source[l.pos] >= '0' && l.source[l.pos] <= '9'
}

// digits consumes digits and the _ separators between them.
func (l *lexer) digits() {
	for l.pos < len(l.source) {
		ch := l.source[l.pos]
		if (ch >= '0' && ch <= '9') || (ch == '_' && l.startsDigitAt(l.pos+1)) {
			l.pos++
			continue
		}
		return
	}
}

func (l *lexer) startsDigitAt(index int) bool {
	return index < len(l.source) && l.source[index] >= '0' && l.source[index] <= '9'
}

func (l *lexer) stringToken() (token, error) {
	start := l.pos
	l.pos++
	escaped := false
	for l.pos < len(l.source) {
		ch := l.source[l.pos]
		l.pos++
		if escaped {
			escaped = false
			continue
		}
		if ch == '\\' {
			escaped = true
			continue
		}
		if ch == '"' {
			return token{kind: tokenString, text: l.source[start:l.pos], pos: start}, nil
		}
	}
	return token{}, At(start, "syntax error: unterminated string")
}
