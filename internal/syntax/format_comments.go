package syntax

import (
	"strings"
	"unicode/utf8"

	"github.com/nethinwei/funroute/internal/money"
)

// Internal comments belong to token boundaries, including ones the AST
// erases (parentheses, commas and expanded operators). Keep those tokens and
// their significant line breaks, and lay out the enclosing lists around them.
// Each pass visits the tokens once; no subtree is repeatedly reprinted.
func formatComments(source string, tokens, comments []token, extent Span) string {
	items := commentTokens(tokens, comments, extent)
	commasBeforeComments(items)
	p := commentPrinter{source: source, items: items, split: commentSplits(source, items)}
	for i, item := range items {
		p.write(i, item)
	}
	return p.out.String()
}

// commentTokens merges the lexer's two ordered streams, within the expression.
func commentTokens(tokens, comments []token, extent Span) []token {
	items := make([]token, 0, len(tokens)+len(comments))
	at := 0
	for _, tok := range tokens {
		for at < len(comments) && comments[at].pos < tok.pos {
			comment := comments[at]
			if comment.pos >= extent.Start && comment.pos < extent.End {
				items = append(items, comment)
			}
			at++
		}
		if tok.pos >= extent.Start && tok.pos < extent.End && tok.kind != tokenEOF {
			items = append(items, tok)
		}
	}
	return items
}

// A separator after a line comment moves before it: f(a // why\n, b)
// becomes f(a, // why\n b). Comment order and code-token order stay intact.
func commasBeforeComments(items []token) {
	for i := 0; i < len(items); i++ {
		if items[i].kind != tokenComment {
			continue
		}
		end := i + 1
		for end < len(items) && items[end].kind == tokenComment {
			end++
		}
		if end < len(items) && items[end].kind == tokenComma {
			comma := items[end]
			copy(items[i+1:end+1], items[i:end])
			items[i] = comma
		}
		i = end
	}
}

// commentSplits marks both ends of lists that need more than one line.
// A split child makes its parent split too; short, uncommented children stay
// inline. Existing newlines remain significant (USD\n-1 is not USD -1).
func commentSplits(source string, items []token) []bool {
	split := make([]bool, len(items))
	widths := make([]int, len(items))
	var stack []int
	width := 0
	for i, tok := range items {
		width += utf8.RuneCountInString(tok.text) + 1
		if len(stack) > 0 && (tok.kind == tokenComment || strings.Contains(commentGap(source, items, i), "\n")) {
			split[stack[len(stack)-1]] = true
		}
		switch {
		case commentOpen(tok.kind):
			stack = append(stack, i)
			widths[i] = width
		case commentClose(tok.kind):
			open := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			split[open] = split[open] || width-widths[open] > maxLine
			split[i] = split[open]
			if split[i] && len(stack) > 0 {
				split[stack[len(stack)-1]] = true
			}
		}
	}
	return split
}

func commentOpen(kind tokenKind) bool {
	return kind == tokenLeftParen || kind == tokenLeftBracket || kind == tokenLeftBrace
}

func commentClose(kind tokenKind) bool {
	return kind == tokenRightParen || kind == tokenRightBracket || kind == tokenRightBrace
}

// A comma moved before a comment has a later source position; there is no
// gap to copy in that direction. All other gaps keep their source newlines.
func commentGap(source string, items []token, i int) string {
	if i == 0 {
		return ""
	}
	if items[i].kind == tokenComma && i+1 < len(items) && items[i+1].pos < items[i].pos {
		return ""
	}
	previous := i - 1
	if items[i].pos < items[previous].end {
		previous-- // the comma moved ahead of this comment
	}
	if previous < 0 || items[i].pos < items[previous].end {
		return ""
	}
	return source[items[previous].end:items[i].pos]
}

type commentPrinter struct {
	source  string
	items   []token
	split   []bool
	out     strings.Builder
	lists   []bool
	depth   int
	column  int
	newline bool
	prev    token
	prefix  bool
}

func (p *commentPrinter) write(i int, tok token) {
	gap := commentGap(p.source, p.items, i)
	if tok.kind == tokenComment {
		p.comment(tok, gap)
		return
	}
	if commentClose(tok.kind) {
		p.lists = p.lists[:len(p.lists)-1]
		if p.split[i] {
			p.depth--
			p.line()
		}
	}
	if p.newline || strings.Contains(gap, "\n") {
		p.line()
	}
	if p.column > 0 && commentSpace(p.prev, tok, p.prefix) {
		p.text(" ")
	}
	p.text(p.source[tok.pos:tok.end])
	p.prefix = p.isPrefix(i, tok)
	p.prev = tok
	p.newline = tok.kind == tokenComma && len(p.lists) > 0 && p.lists[len(p.lists)-1]
	if commentOpen(tok.kind) {
		p.lists = append(p.lists, p.split[i])
		if p.split[i] {
			p.depth++
			p.line()
		}
	}
}

func (p *commentPrinter) comment(tok token, gap string) {
	if strings.Contains(gap, "\n") {
		p.line()
	}
	if p.column > 0 {
		p.text(" ")
	}
	p.text(strings.TrimRight(tok.text, " \t\r"))
	p.line()
}

func (p *commentPrinter) text(text string) {
	if p.column == 0 {
		p.out.WriteString(strings.Repeat("  ", p.depth))
		p.column = p.depth * 2
	}
	p.out.WriteString(text)
	p.column += utf8.RuneCountInString(text)
}

func (p *commentPrinter) line() {
	if p.column > 0 {
		p.out.WriteByte('\n')
		p.column = 0
	}
	p.newline = false
}

func (p *commentPrinter) isPrefix(i int, tok token) bool {
	if tok.kind == tokenBang {
		return true
	}
	if tok.kind != tokenMinus {
		return false
	}
	if !commentOperand(p.prev) {
		return true
	}
	// The minus of USD -1 must touch its digits; the subtraction USD - 1
	// must keep the space. Looking at the source distinguishes the two.
	return p.prev.kind == tokenIdentifier && money.IsCurrencyCode(p.prev.text) &&
		onlyHorizontalSpace(p.source[p.prev.end:tok.pos]) && i+1 < len(p.items) &&
		p.items[i+1].pos == tok.end && (p.items[i+1].kind == tokenInt || p.items[i+1].kind == tokenFloat)
}

func commentOperand(tok token) bool {
	switch tok.kind {
	case tokenIdentifier:
		return tok.text != "for" && tok.text != "in" && tok.text != "if" && tok.text != "case" && tok.text != "else" && tok.text != "with"
	case tokenInt, tokenFloat, tokenString, tokenEnum, tokenRatio, tokenDuration, tokenRightParen, tokenRightBracket, tokenRightBrace:
		return true
	default:
		return false
	}
}

func commentSpace(prev, next token, prefix bool) bool {
	if commentClose(next.kind) || next.kind == tokenComma || next.kind == tokenColon {
		return false
	}
	if commentOpen(prev.kind) || prev.kind == tokenDot || prefix {
		return false
	}
	if next.kind == tokenLeftParen && prev.kind == tokenIdentifier {
		return prev.text == "in" || prev.text == "for" || prev.text == "case" || prev.text == "else" || prev.text == "with"
	}
	if next.kind == tokenLeftBracket && commentOperand(prev) {
		return false
	}
	if next.kind == tokenDot && commentClose(prev.kind) {
		return false
	}
	return true
}
