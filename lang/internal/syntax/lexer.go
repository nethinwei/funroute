package syntax

import (
	"fmt"
	"strings"
	"unicode"
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
	tokenAssign
	// tokenEnum is @member or @enum.member: an enum member reference. The "@"
	// keeps it apart from a string and keeps "." out of expression syntax.
	tokenEnum
)

// operatorTokens comes from the parser's sourceOperators table and is scanned
// longest-first, so "<=" wins over "<" and "=>" over "=".
var operatorTokens = lexedOperators()

type token struct {
	kind tokenKind
	text string
	pos  int
}

type lexer struct {
	source string
	pos    int
}

func (l *lexer) tokens() ([]token, error) {
	var out []token
	for {
		tok, err := l.next()
		if err != nil {
			return nil, err
		}
		out = append(out, tok)
		if tok.kind == tokenEOF {
			return out, nil
		}
	}
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
}

// skipSpace also eats // line comments. Comments are lexical only: they never
// reach the AST, so they do not survive an ExprJSON round trip.
func (l *lexer) skipSpace() {
	for l.pos < len(l.source) {
		if unicode.IsSpace(rune(l.source[l.pos])) {
			l.pos++
			continue
		}
		if strings.HasPrefix(l.source[l.pos:], "//") {
			for l.pos < len(l.source) && l.source[l.pos] != '\n' {
				l.pos++
			}
			continue
		}
		return
	}
}

func (l *lexer) next() (token, error) {
	l.skipSpace()
	if l.pos >= len(l.source) {
		return token{kind: tokenEOF, pos: l.pos}, nil
	}
	start := l.pos
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
		return token{}, fmt.Errorf("syntax error at byte %d: unexpected %q", start, ch)
	}
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
		return "", fmt.Errorf("syntax error at byte %d: @ must be followed by an enum member name", start)
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
			return token{}, fmt.Errorf("syntax error at byte %d: float requires digits after '.'", start)
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
			return token{}, fmt.Errorf("syntax error at byte %d: exponent requires digits", start)
		}
		l.digits()
	}
	// 1_000_000 reads as a million; the separator never reaches strconv.
	text := strings.ReplaceAll(l.source[start:l.pos], "_", "")
	return token{kind: kind, text: text, pos: start}, nil
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
	return token{}, fmt.Errorf("syntax error at byte %d: unterminated string", start)
}
