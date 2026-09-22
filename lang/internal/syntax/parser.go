package syntax

import (
	"fmt"
	"strconv"
	"strings"

	"funroute/lang/internal/machine"
)

type parser struct {
	tokens []token
	index  int
	nextID int
}

// tokenize turns source into the token slice the parser walks.
func tokenize(source string) ([]token, error) {
	lex := &lexer{source: source}
	return lex.tokens()
}

// Parse reads an expression. A program is only an expression: the contract it
// runs under is the host's, and arrives through CompileOptions.
func Parse(source string) (Expr, error) {
	tokens, err := tokenize(source)
	if err != nil {
		return nil, err
	}
	p := &parser{tokens: tokens, nextID: 1}
	expr, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	if p.peek().kind != tokenEOF {
		return nil, p.errorf(p.peek(), "unexpected token %q", p.peek().text)
	}
	return expr, nil
}

func (p *parser) parseExpr() (Expr, error) {
	return p.parseBinary(1)
}

// node finishes a constructed node: the same normalisation and checks the
// importer applies, so a rule is written once in ast.go. Errors are placed at
// the node's opening token.
// enumReference splits @member and @enum.member; which enum a bare member
// belongs to is decided by the contract at compile time, not here.
func enumReference(id int, tok token) Expr {
	enum, member, qualified := strings.Cut(tok.text, ".")
	if !qualified {
		return &EnumExpr{ID: id, Pos: tok.pos, Member: enum}
	}
	return &EnumExpr{ID: id, Pos: tok.pos, Enum: enum, Member: member}
}

func (p *parser) node(at token, expr Expr) (Expr, error) {
	finished, err := finish(expr)
	if err != nil {
		return nil, p.errorf(at, "%v", err)
	}
	return finished, nil
}

// parseBinary is precedence climbing; every operator here is left associative.
func (p *parser) parseBinary(min int) (Expr, error) {
	left, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	for {
		operator := p.peek()
		spec, ok := p.infixOperator(operator)
		if !ok || spec.precedence < min {
			return left, nil
		}
		p.index++
		right, err := p.parseBinary(spec.precedence + 1)
		if err != nil {
			return nil, err
		}
		left = p.expandOperator(operator, spec, left, right)
	}
}

// infixOperator finds the operator a token starts, whether it is punctuation
// (+, %) or a word (in).
func (p *parser) infixOperator(tok token) (operatorSpec, bool) {
	if tok.kind == tokenIdentifier {
		spec, ok := keywordOperators[tok.text]
		return spec, ok
	}
	spec, ok := binaryOperators[tok.kind]
	return spec, ok
}

func (p *parser) expandOperator(operator token, spec operatorSpec, operands ...Expr) Expr {
	switch spec.expansion {
	case expandNotEqual:
		left, right := operands[0], operands[1]
		return p.pick(operator, p.call(operator, "eq", left, right), p.boolean(operator, false), p.boolean(operator, true))
	case expandAnd:
		// Short circuits, because if is lazy.
		return p.pick(operator, operands[0], operands[1], p.boolean(operator, false))
	case expandOr:
		return p.pick(operator, operands[0], p.boolean(operator, true), operands[1])
	case expandNot:
		return p.pick(operator, operands[0], p.boolean(operator, false), p.boolean(operator, true))
	default:
		return p.call(operator, spec.function, operands...)
	}
}

func (p *parser) parseUnary() (Expr, error) {
	operator := p.peek()
	spec, ok := unaryOperators[operator.kind]
	if !ok {
		primary, err := p.parsePrimary()
		if err != nil {
			return nil, err
		}
		return p.parsePostfix(primary)
	}
	p.index++
	if spec.expansion == expandNegate {
		return p.negate(operator)
	}
	operand, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	return p.expandOperator(operator, spec, operand), nil
}

// parsePostfix reads what can follow a primary: subscripts and field reads.
// xs[i] is at(xs, i) and d["k"] is at(d, "k"), so indexing adds no node — only
// a spelling; .field is the same FieldExpr a dotted name produces, which is
// why orders[0].amount and order.amount mean the same thing.
func (p *parser) parsePostfix(base Expr) (Expr, error) {
	for {
		switch p.peek().kind {
		case tokenLeftBracket:
			indexed, err := p.parseSubscript(base)
			if err != nil {
				return nil, err
			}
			base = indexed
		case tokenDot:
			field, err := p.parseFieldRead(base)
			if err != nil {
				return nil, err
			}
			base = field
		default:
			return base, nil
		}
	}
}

func (p *parser) parseSubscript(base Expr) (Expr, error) {
	bracket := p.peek()
	p.index++
	index, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	if err := p.expect(tokenRightBracket, "']' after the index"); err != nil {
		return nil, err
	}
	return p.call(bracket, "at", base, index), nil
}

func (p *parser) parseFieldRead(base Expr) (Expr, error) {
	dot := p.peek()
	p.index++
	name := p.peek()
	if name.kind != tokenIdentifier || strings.Contains(name.text, ".") {
		return nil, p.errorf(name, "expected a field name after '.'")
	}
	p.index++
	if err := validName(name.text, "text"); err != nil {
		return nil, p.errorf(name, "%v", err)
	}
	return p.node(dot, &FieldExpr{ID: p.id(), Pos: dot.pos, Value: base, Field: name.text})
}

// negate keeps -42 a literal and turns -e into sub(0, e).
func (p *parser) negate(operator token) (Expr, error) {
	if next := p.peek(); next.kind == tokenInt || next.kind == tokenFloat {
		p.index++
		return p.numberLiteral(next, true)
	}
	operand, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	zero := &LiteralExpr{ID: p.id(), Pos: operator.pos, Value: machine.Int(0)}
	return p.call(operator, "sub", zero, operand), nil
}

func (p *parser) numberLiteral(tok token, negative bool) (Expr, error) {
	text := tok.text
	if negative {
		text = "-" + text
	}
	if tok.kind == tokenInt {
		value, err := strconv.ParseInt(text, 10, 64)
		if err != nil {
			return nil, p.errorf(tok, "integer is outside int64 range")
		}
		return &LiteralExpr{ID: p.id(), Pos: tok.pos, Value: machine.Int(value)}, nil
	}
	value, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return nil, p.errorf(tok, "invalid float")
	}
	checked, err := machine.CheckedFloat(value)
	if err != nil {
		return nil, p.errorf(tok, "%v", err)
	}
	return &LiteralExpr{ID: p.id(), Pos: tok.pos, Value: checked}, nil
}

func (p *parser) call(at token, name string, args ...Expr) Expr {
	return &CallExpr{ID: p.id(), Pos: at.pos, Name: name, Args: args}
}

func (p *parser) pick(at token, condition, whenTrue, whenFalse Expr) Expr {
	return p.call(at, "if", condition, whenTrue, whenFalse)
}

func (p *parser) boolean(at token, value bool) Expr {
	return &LiteralExpr{ID: p.id(), Pos: at.pos, Value: machine.Bool(value)}
}

func (p *parser) keyword(word string) bool {
	return p.peek().kind == tokenIdentifier && p.peek().text == word
}

func (p *parser) expect(kind tokenKind, what string) error {
	if p.peek().kind != kind {
		return p.errorf(p.peek(), "expected %s", what)
	}
	p.index++
	return nil
}

func (p *parser) parsePrimary() (Expr, error) {
	tok := p.peek()
	switch tok.kind {
	case tokenInt, tokenFloat:
		p.index++
		return p.numberLiteral(tok, false)
	case tokenString:
		p.index++
		value, err := strconv.Unquote(tok.text)
		if err != nil {
			return nil, p.errorf(tok, "invalid string escape: %v", err)
		}
		return &LiteralExpr{ID: p.id(), Pos: tok.pos, Value: machine.String(value)}, nil
	case tokenEnum:
		p.index++
		return p.node(tok, enumReference(p.id(), tok))
	case tokenIdentifier:
		p.index++
		if tok.text == "true" || tok.text == "false" {
			return &LiteralExpr{ID: p.id(), Pos: tok.pos, Value: machine.Bool(tok.text == "true")}, nil
		}
		if p.peek().kind == tokenLeftParen {
			return p.parseCall(tok)
		}
		// A dotted name that is not being called is a variable and the fields
		// read off it: order.amount is at(order).amount, never one name. A
		// call keeps its dots, because that is how a function is versioned:
		// route.score_v1(…).
		return p.variableWithFields(tok)
	case tokenLeftParen:
		return p.parseGroup()
	case tokenLeftBracket:
		return p.parseArray()
	case tokenLeftBrace:
		return p.parseBrace()
	default:
		return nil, p.errorf(tok, "expected an expression")
	}
}

// parseGroup reads (e), which is only there to override infix precedence.
func (p *parser) parseGroup() (Expr, error) {
	p.index++
	inner, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	if err := p.expect(tokenRightParen, "')'"); err != nil {
		return nil, err
	}
	return inner, nil
}

// variableWithFields splits name.field.field into a variable and the field
// accesses on it, checking each part is a usable name.
func (p *parser) variableWithFields(tok token) (Expr, error) {
	parts := strings.Split(tok.text, ".")
	if err := validName(parts[0], "var"); err != nil {
		return nil, p.errorf(tok, "%v", err)
	}
	var expr Expr = &VariableExpr{ID: p.id(), Pos: tok.pos, Name: parts[0]}
	for _, field := range parts[1:] {
		if err := validName(field, "text"); err != nil {
			return nil, p.errorf(tok, "%v", err)
		}
		node, err := p.node(tok, &FieldExpr{ID: p.id(), Pos: tok.pos, Value: expr, Field: field})
		if err != nil {
			return nil, err
		}
		expr = node
	}
	return expr, nil
}

func (p *parser) parseCall(name token) (Expr, error) {
	p.index++ // (
	if name.text == "reduce" {
		return p.parseReduceCall(name)
	}
	if name.text == "switch" {
		return p.parseSwitchCall(name)
	}
	if name.text == "let" {
		return p.parseLetCall(name)
	}
	args, err := p.parseList(tokenRightParen)
	if err != nil {
		return nil, err
	}
	return &CallExpr{ID: p.id(), Pos: name.pos, Name: name.text, Args: args}, nil
}

// parseLetCall reads let(x = e1, y = e2, body). Bindings are ordered: a later
// value may use an earlier name, and the body sees all of them. The names are
// locals, so they never show up in the program's arguments.
func (p *parser) parseLetCall(name token) (Expr, error) {
	bindings, err := p.letBindings(name)
	if err != nil {
		return nil, err
	}
	body, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	if err := p.expect(tokenRightParen, "')'"); err != nil {
		return nil, err
	}
	return p.node(name, &LetExpr{ID: p.id(), Pos: name.pos, Bindings: bindings, Body: body})
}

func (p *parser) letBindings(name token) ([]LetBinding, error) {
	var bindings []LetBinding
	for p.startsBinding() {
		local, err := p.localIdentifier()
		if err != nil {
			return nil, err
		}
		p.index++ // =
		value, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		bindings = append(bindings, LetBinding{Name: local, Value: value})
		if err := p.expect(tokenComma, "',' after a let binding"); err != nil {
			return nil, err
		}
	}
	if len(bindings) == 0 {
		return nil, p.errorf(name, "let needs at least one binding, as in let(x = e, body)")
	}
	return bindings, nil
}

func (p *parser) startsBinding() bool {
	return p.peek().kind == tokenIdentifier && p.peekN(1).kind == tokenAssign
}

// parseReduceCall reads the one form there is:
//
//	reduce(item in source, acc = init, body)
//	reduce(key, item in source if condition, acc = init, body)
//
// The local names are part of the syntax, so they cannot be mistaken for
// expressions, and there is nothing to disambiguate.
func (p *parser) parseReduceCall(name token) (Expr, error) {
	key, value, err := p.loopVariables(name)
	if err != nil {
		return nil, p.errorf(name, "reduce starts with its element name: reduce(item in source, acc = init, body)")
	}
	p.index++ // loopVariables has already checked the "in"
	return p.reduceKeywordForm(name, key, value)
}

func (p *parser) reduceKeywordForm(name token, key, variable string) (Expr, error) {
	source, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	where, err := p.loopFilter()
	if err != nil {
		return nil, err
	}
	if err := p.expect(tokenComma, "',' before the accumulator"); err != nil {
		return nil, err
	}
	accumulator, init, err := p.parseAccumulator()
	if err != nil {
		return nil, err
	}
	if err := p.expect(tokenComma, "',' before the reduce body"); err != nil {
		return nil, err
	}
	body, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	if err := p.expect(tokenRightParen, "')'"); err != nil {
		return nil, err
	}
	return p.node(name, &ReduceExpr{
		ID: p.id(), Pos: name.pos, Source: source, Variable: variable,
		KeyVariable: key, Where: where, Accumulator: accumulator, Init: init, Body: body,
	})
}

// parseAccumulator reads "total = 0", the shape a let binding already has.
func (p *parser) parseAccumulator() (string, Expr, error) {
	if !p.startsBinding() {
		return "", nil, p.errorf(p.peek(), "reduce needs an accumulator and its initial value: acc = init")
	}
	accumulator, err := p.localIdentifier()
	if err != nil {
		return "", nil, err
	}
	p.index++ // =
	init, err := p.parseExpr()
	if err != nil {
		return "", nil, err
	}
	return accumulator, init, nil
}

// parseSwitchCall accepts all three shapes. The case keyword marks where the
// branches start, which is what removes the ambiguity between a subject and a
// multi-value condition branch:
//
//	switch(subject, case "SG" => "a", case "MY", "TH" => "b", else "c")
//	switch(case amount > 100 => "a", case risk > 0.8 => "b", else "c")
func (p *parser) parseSwitchCall(name token) (Expr, error) {
	if p.keyword("case") {
		return p.switchBranches(name, nil)
	}
	subject, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	if err := p.expect(tokenComma, "',' after the switch subject"); err != nil {
		return nil, err
	}
	if p.keyword("case") {
		return p.switchBranches(name, subject)
	}
	return nil, p.errorf(name, "switch branches start with \"case\": switch(subject, case m => r, else d)")
}

// switchBranches reads "case m1, m2 => r" groups and an optional final
// "else d". The compiler accepts the missing else only when a declared enum
// subject is covered exhaustively.
func (p *parser) switchBranches(name token, subject Expr) (Expr, error) {
	var cases []SwitchCaseExpr
	for p.keyword("case") {
		p.index++
		matches, err := p.caseMatches()
		if err != nil {
			return nil, err
		}
		result, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		cases = append(cases, SwitchCaseExpr{Match: matches, Result: result})
		if p.peek().kind == tokenRightParen {
			break
		}
		if err := p.expect(tokenComma, "',' after a switch branch"); err != nil {
			return nil, err
		}
	}
	if len(cases) == 0 {
		return nil, p.errorf(name, "switch needs at least one case")
	}
	if p.keyword("else") {
		p.index++
		fallback, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		if err := p.expect(tokenRightParen, "')'"); err != nil {
			return nil, err
		}
		return &SwitchExpr{ID: p.id(), Pos: name.pos, Value: subject, Cases: cases, Default: fallback}, nil
	}
	if err := p.expect(tokenRightParen, "')'"); err != nil {
		return nil, err
	}
	return &SwitchExpr{ID: p.id(), Pos: name.pos, Value: subject, Cases: cases}, nil
}

// caseMatches reads "m1, m2, m3 =>"; any of them selects the branch.
func (p *parser) caseMatches() ([]Expr, error) {
	var matches []Expr
	for {
		match, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		matches = append(matches, match)
		if p.peek().kind == tokenFatArrow {
			p.index++
			return matches, nil
		}
		if err := p.expect(tokenComma, "',' or '=>' inside a case"); err != nil {
			return nil, err
		}
	}
}

func (p *parser) parseArray() (Expr, error) {
	start := p.peek()
	p.index++
	if p.peek().kind == tokenRightBracket {
		p.index++
		return &ArrayExpr{ID: p.id(), Pos: start.pos}, nil
	}
	first, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	if p.keyword("for") {
		return p.comprehension(start, first)
	}
	items, err := p.parseRest(first, tokenRightBracket)
	if err != nil {
		return nil, err
	}
	return &ArrayExpr{ID: p.id(), Pos: start.pos, Items: items}, nil
}

// localIdentifier consumes one identifier and checks it can name a local.
func (p *parser) localIdentifier() (string, error) {
	tok := p.peek()
	if tok.kind != tokenIdentifier || validName(tok.text, "local") != nil {
		return "", p.errorf(tok, "expected a local variable name")
	}
	p.index++
	return tok.text, nil
}

func (p *parser) parseList(end tokenKind) ([]Expr, error) {
	if p.peek().kind == end {
		p.index++
		return nil, nil
	}
	first, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	return p.parseRest(first, end)
}

// parseRest continues a comma separated list whose first item is already
// parsed. A trailing comma before the closing delimiter is allowed.
func (p *parser) parseRest(first Expr, end tokenKind) ([]Expr, error) {
	items := []Expr{first}
	for {
		if p.peek().kind == end {
			p.index++
			return items, nil
		}
		if p.peek().kind != tokenComma {
			return nil, p.errorf(p.peek(), "expected ',' or closing delimiter")
		}
		p.index++
		if p.peek().kind == end {
			p.index++
			return items, nil
		}
		item, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
}

func (p *parser) id() int {
	id := p.nextID
	p.nextID++
	return id
}

func (p *parser) peek() token { return p.peekN(0) }

func (p *parser) peekN(offset int) token {
	index := p.index + offset
	if index >= len(p.tokens) {
		return token{kind: tokenEOF}
	}
	return p.tokens[index]
}

func (p *parser) errorf(tok token, format string, args ...any) error {
	return At(tok.pos, "syntax error: %s", fmt.Sprintf(format, args...))
}
