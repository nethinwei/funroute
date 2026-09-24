package syntax

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/nethinwei/funroute/internal/machine"
	"github.com/nethinwei/funroute/internal/money"
)

// negate keeps -42 a literal and turns -e into sub(0, e); -USD 1.70 is
// the negation of an amount like any other operand's. The literal is only a
// number standing alone: -150 JPY / USD negates an exchange rate and -2[0]
// an item, as the grammar's unary over postfix says.
func (p *parser) negate(operator token) (Expr, error) {
	if next := p.peek(); (next.kind == tokenInt || next.kind == tokenFloat) && p.numberAlone(next) {
		p.index++
		return p.numberLiteral(next, true)
	}
	operand, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	zero := &LiteralExpr{Node: Node{ID: p.id(), Pos: operator.pos, Span: tokenSpan(operator)}, Value: machine.Int(0)}
	return p.call(operator, "sub", zero, operand), nil
}

// startsMoney reports an amount: a currency code, spaces or tabs, and a
// number whose sign, if it has one, is against its digits — USD 1.70,
// USD -1.70. The whole amount is on one line with no comment in it: USD-1,
// USD - 1, USD // x\n 1 and USD at the end of a line with -1 on the next are
// not amounts, so where an amount ends is plain to see.
func (p *parser) startsMoney(code token) bool {
	if !money.IsCurrencyCode(code.text) {
		return false
	}
	next := p.peek()
	if !onlyHorizontalSpace(p.source[code.pos+len(code.text) : next.pos]) {
		return false
	}
	if next.kind == tokenMinus {
		after := p.peekN(1)
		return (after.kind == tokenInt || after.kind == tokenFloat) && after.pos == next.pos+1
	}
	return next.kind == tokenInt || next.kind == tokenFloat
}

// onlyHorizontalSpace reports text that is spaces and tabs and nothing else,
// at least one character of it: what separates the parts of a money or an
// exchange rate literal. A newline or a comment ends the literal.
func onlyHorizontalSpace(text string) bool {
	return text != "" && horizontalGap(text)
}

// horizontalGap reports text that is spaces and tabs or nothing at all.
func horizontalGap(text string) bool {
	return strings.Trim(text, " \t") == ""
}

// moneyLiteral reads the amount after its currency code, with its sign. The
// amount keeps its decimal text; whether it fits the currency's places is
// the compiler's to say, since the places are the registry's.
func (p *parser) moneyLiteral(code token) (Expr, error) {
	p.mark(code, RoleCurrency)
	sign := ""
	if minus := p.peek(); minus.kind == tokenMinus {
		p.index++
		p.mark(minus, RoleOperator)
		sign = "-"
	}
	amount := p.peek()
	p.index++
	p.mark(amount, RoleLiteral)
	if strings.ContainsAny(amount.text, "eE") {
		return nil, p.errorf(amount, "an amount is written as a plain decimal, not with an exponent")
	}
	return p.node(code, &MoneyExpr{Node: p.at(code.pos), Currency: code.text, Amount: sign + amount.text})
}

// ratioLiteral splits 2.9% or 25bps into its value and unit.
func (p *parser) ratioLiteral(tok token) (Expr, error) {
	p.mark(tok, RoleLiteral)
	unit := "%"
	if strings.HasSuffix(tok.text, "bps") {
		unit = "bps"
	}
	value := strings.TrimSuffix(tok.text, unit)
	if next := p.peek(); unit == "%" && next.pos == tok.pos+len(tok.text) && touchingOperand(next) {
		return nil, p.errorf(next, "%s is a ratio: a %% against a number is always one; a remainder has a space before the %%, %s %% …", tok.text, value)
	}
	return p.node(tok, &RatioExpr{Node: p.at(tok.pos), Value: value, Unit: unit})
}

// touchingOperand is what, written against a ratio, shows its writer meant a
// remainder: 10%3, 10%x, 10%(x). Words the language reserves are not
// operands, so [x * 2.9%for x in xs] still reads.
func touchingOperand(next token) bool {
	switch next.kind {
	case tokenInt, tokenFloat, tokenRatio, tokenString, tokenEnum, tokenLeftParen, tokenLeftBrace, tokenBang:
		return true
	case tokenIdentifier:
		return !machine.IsReservedName(next.text)
	default:
		return false
	}
}

func (p *parser) numberLiteral(tok token, negative bool) (Expr, error) {
	p.mark(tok, RoleLiteral)
	text := tok.text
	if negative {
		text = "-" + text
	}
	if tok.kind == tokenInt {
		value, err := strconv.ParseInt(text, 10, 64)
		if err != nil {
			return nil, p.errorf(tok, "integer is outside int64 range")
		}
		return &LiteralExpr{Node: p.at(tok.pos), Value: machine.Int(value)}, nil
	}
	literal, err := decimalLiteral(text)
	if err != nil {
		return nil, p.errorf(tok, "%v", err)
	}
	literal.Node = p.at(tok.pos)
	return literal, nil
}

// decimalLiteral is the literal a decimal written as text stands for: the
// decimal itself, and the float64 nearest it. One too large for any float64
// is refused; one a float64 only approximates is refused where it is read as
// a float.
func decimalLiteral(text string) (*LiteralExpr, error) {
	decimal, err := money.ParseDecimalLiteral(text)
	if err != nil {
		return nil, fmt.Errorf("invalid float: %w", err)
	}
	nearest, err := strconv.ParseFloat(decimal.String(), 64)
	if err != nil {
		return nil, fmt.Errorf("float %s is out of float64's range", decimal)
	}
	value, err := machine.CheckedFloat(nearest)
	if err != nil {
		return nil, err
	}
	return &LiteralExpr{Value: value, Decimal: decimal}, nil
}

// unquote reads a string token's text. Text is UTF-8: a byte that is not,
// escaped as \200, would be replaced on the way through ExprJSON, and the
// program would not read back as itself; written raw, strconv.Unquote would
// quietly replace it already. Both are refused.
func (p *parser) unquote(tok token, invalid string) (string, error) {
	if !utf8.ValidString(tok.text) {
		return "", p.errorf(tok, "string is not valid UTF-8")
	}
	value, err := strconv.Unquote(tok.text)
	if err != nil {
		return "", p.errorf(tok, "%s: %v", invalid, err)
	}
	if !utf8.ValidString(value) {
		return "", p.errorf(tok, "string is not valid UTF-8")
	}
	return value, nil
}

// startsFxRate reports an exchange rate written the way a market writes one:
// a figure, white space, a currency code, "/" and another code — 150 JPY /
// USD. That is one literal; a figure followed by anything else is a number.
func (p *parser) startsFxRate(figure token) bool {
	return p.fxRateAfter(figure, 0)
}

// fxRateAfter reports the code, slash and code of an exchange rate starting
// offset tokens on from the parser's position, right after figure.
func (p *parser) fxRateAfter(figure token, offset int) bool {
	quote, slash, base := p.peekN(offset), p.peekN(offset+1), p.peekN(offset+2)
	return quote.kind == tokenIdentifier && money.IsCurrencyCode(quote.text) && onlyHorizontalSpace(p.source[figure.end:quote.pos]) &&
		slash.kind == tokenSlash && horizontalGap(p.source[quote.end:slash.pos]) &&
		base.kind == tokenIdentifier && money.IsCurrencyCode(base.text) && horizontalGap(p.source[slash.end:base.pos])
}

// numberAlone reports a number at the parser's position that is no more than
// itself: not the figure of an exchange rate, not indexed.
func (p *parser) numberAlone(number token) bool {
	return p.peekN(1).kind != tokenLeftBracket && !p.fxRateAfter(number, 1)
}

// fxRateLiteral reads the rest of 150 JPY / USD after its figure.
func (p *parser) fxRateLiteral(figure token) (Expr, error) {
	quote, slash, base := p.peek(), p.peekN(1), p.peekN(2)
	p.index += 3
	p.mark(figure, RoleLiteral)
	p.mark(quote, RoleCurrency)
	p.mark(slash, RoleOperator)
	p.mark(base, RoleCurrency)
	if strings.ContainsAny(figure.text, "eE") {
		return nil, p.errorf(figure, "an exchange rate's figure is a plain decimal, not written with an exponent")
	}
	return p.node(figure, &FxRateExpr{Node: Node{ID: p.id(), Pos: figure.pos, Span: Span{Start: figure.pos, End: base.end}}, Rate: figure.text, Quote: quote.text, Base: base.text})
}

// Float is a decimal literal read as a float: its value when the float64 is
// the decimal as written, and otherwise the refusal, naming the float64 it
// would have been. 0.30000000000000001 would be 0.3, and a rule would say
// one thing and compute another.
func (e *LiteralExpr) Float() (machine.Value, error) {
	number, _ := e.Value.Float()
	if nearest := strconv.FormatFloat(number, 'g', -1, 64); nearest != e.Decimal.String() {
		return machine.Value{}, Around(e, "type error: float %s is not a float64 as written: the nearest one is %s", e.Decimal, nearest)
	}
	return e.Value, nil
}
