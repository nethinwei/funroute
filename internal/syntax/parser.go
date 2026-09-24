package syntax

import (
	"fmt"
	"strings"

	"github.com/nethinwei/funroute/internal/machine"
	"github.com/nethinwei/funroute/internal/money"
)

type parser struct {
	source string
	tokens []token
	index  int
	nextID int
	// depth is how many expressions the parser is inside, held to
	// maxNesting.
	depth int
	// roles records what the parser took each token to be, keyed by where
	// it starts. Only Lexemes asks for it; Parse leaves it nil and pays nothing.
	roles map[int]roleMark
}

// reading is one pass over a source: the lexer (which keeps the comments),
// the parser (which, when asked, keeps the role of each token) and the
// expression they made. Parse, Lexemes, SyntaxTree and FormatSource are all a
// reading and differ only in what they take from it.
type reading struct {
	lex    *lexer
	parser *parser
	tokens []token
	expr   Expr
	err    error
}

// read lexes and parses source. The lexer carries on past a bad character, so
// the parser always has tokens to read; the error is the lexer's if it had
// one, else the parser's.
func read(source string, roles bool) reading {
	lex := &lexer{source: source}
	tokens, err := lex.tokens()
	p := &parser{source: source, tokens: tokens, nextID: 1}
	if roles {
		p.roles = map[int]roleMark{}
	}
	expr, parseErr := p.program()
	if err == nil {
		err = parseErr
	}
	return reading{lex: lex, parser: p, tokens: tokens, expr: expr, err: err}
}

// Parse reads an expression. A program is only an expression: the contract it
// runs under is the host's, and arrives through CompileOptions.
func Parse(source string) (Expr, error) {
	r := read(source, false)
	if r.err != nil {
		return nil, r.err
	}
	return r.expr, nil
}

// program reads the one expression a source is, and nothing after it.
func (p *parser) program() (Expr, error) {
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

// enumReference splits @member and @enum.member; which enum a bare member
// belongs to is decided by the contract at compile time, not here.
func enumReference(node Node, text string) Expr {
	enum, member, qualified := strings.Cut(text, ".")
	if !qualified {
		return &EnumExpr{Node: node, Member: enum}
	}
	return &EnumExpr{Node: node, Enum: enum, Member: member}
}

// node finishes a constructed node: the same normalisation and checks the
// importer applies, so a rule is written once in ast.go. Errors are placed at
// the node's opening token.
func (p *parser) node(at token, expr Expr) (Expr, error) {
	finished, err := finish(expr)
	if err != nil {
		return nil, p.errorf(at, "%v", err)
	}
	return finished, nil
}

// parseBinary is precedence climbing. An operator is left associative
// unless the table says otherwise: see follows.
func (p *parser) parseBinary(lowest int) (Expr, error) {
	left, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	return p.climb(left, lowest)
}

// climb reads the infix operators after left, whose precedence is at least
// lowest.
func (p *parser) climb(left Expr, lowest int) (Expr, error) {
	var last *operatorSpec
	for {
		operator := p.peek()
		spec, ok := p.infixOperator(operator)
		if !ok || spec.precedence < lowest {
			return left, nil
		}
		if err := p.follows(last, spec, operator); err != nil {
			return nil, err
		}
		p.index++
		p.mark(operator, RoleOperator)
		right, err := p.rightOperand(spec)
		if err != nil {
			return nil, err
		}
		left = p.stamp(left.Extent().Start, p.expandOperator(operator, spec, left, right))
		last = &spec
	}
}

// follows refuses an operator the grammar does not let come after last at
// one level: a comparison after one of its own precedence, which would be a
// chain the language gives no meaning to, and anything tighter than -> after
// its right operand, which is a currency and nothing more.
func (p *parser) follows(last *operatorSpec, spec operatorSpec, operator token) error {
	switch {
	case last == nil:
		return nil
	case last.nonAssociative && spec.precedence == last.precedence:
		return p.errorf(operator, "comparisons do not chain: a %s b %s c needs parentheses, or && between two comparisons", last.token, spec.token)
	case last.postfixRight && spec.precedence > last.precedence:
		return p.errorf(operator, "%s takes one currency on its right: write (amount %s JPY) %s …", last.token, last.token, spec.token)
	}
	return nil
}

// rightOperand reads an infix operator's right operand: the next tighter
// level, or for -> one postfix expression — a code, a name, a field, a call,
// a parenthesised expression.
func (p *parser) rightOperand(spec operatorSpec) (Expr, error) {
	if !spec.postfixRight {
		return p.parseBinary(spec.precedence + 1)
	}
	primary, err := p.parsePrimary()
	if err != nil {
		return nil, err
	}
	return p.parsePostfix(primary)
}

// infixOperator finds the operator a token starts, whether it is punctuation
// (+, %) or a word (in).
func (p *parser) infixOperator(tok token) (operatorSpec, bool) {
	spec, ok := binaryOperators[keyOf(tok.kind, tok.text)]
	return spec, ok
}

func (p *parser) expandOperator(operator token, spec operatorSpec, operands ...Expr) Expr {
	switch spec.expansion {
	case expandNotEqual:
		left, right := operands[0], operands[1]
		equal := p.stamp(left.Extent().Start, p.call(operator, "eq", left, right))
		return p.pick(operator, equal, p.boolean(operator, false), p.boolean(operator, true))
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

// maxNesting is how deep one expression may nest: parentheses, calls,
// containers and prefix operators each count a level. A rule is a few levels
// deep; the parser, the importer and everything after them recurse over the
// tree, and a Go stack that overflows is a fatal error no recover catches —
// a million parentheses in a two-megabyte text would take the process down.
// Source and ExprJSON are held to the same limit.
const maxNesting = 1000

// parseUnary is where every nested expression passes, so it is where the
// depth is counted.
func (p *parser) parseUnary() (Expr, error) {
	if p.depth == maxNesting {
		return nil, p.errorf(p.peek(), "the expression nests deeper than %d levels", maxNesting)
	}
	p.depth++
	defer func() { p.depth-- }()
	return p.unary()
}

func (p *parser) unary() (Expr, error) {
	operator := p.peek()
	spec, ok := unaryOperators[keyOf(operator.kind, operator.text)]
	if !ok {
		primary, err := p.parsePrimary()
		if err != nil {
			return nil, err
		}
		return p.parsePostfix(primary)
	}
	p.index++
	p.mark(operator, RoleOperator)
	if spec.expansion == expandNegate {
		negated, err := p.negate(operator)
		return p.stamp(operator.pos, negated), err
	}
	operand, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	return p.stamp(operator.pos, p.expandOperator(operator, spec, operand)), nil
}

// parsePostfix reads what can follow a primary: subscripts, field reads and
// with {…}, the record with some fields replaced.
// xs[i] is at(xs, i) and d["k"] is at(d, "k"), so indexing adds no node — only
// a spelling; .field is the same FieldExpr a dotted name produces, which is
// why orders[0].amount and order.amount mean the same thing.
func (p *parser) parsePostfix(base Expr) (Expr, error) {
	start := base.Extent().Start
	for {
		var next Expr
		var err error
		switch {
		case p.peek().kind == tokenLeftBracket:
			next, err = p.parseSubscript(base)
		case p.peek().kind == tokenDot:
			next, err = p.parseFieldRead(base)
		case p.keyword("with"):
			next, err = p.parseWith(base)
		default:
			return base, nil
		}
		if err != nil {
			return nil, err
		}
		base = p.stamp(start, next)
	}
}

func (p *parser) parseSubscript(base Expr) (Expr, error) {
	bracket := p.peek()
	p.index++
	index, err := p.exprThen(tokenRightBracket, "']' after the index")
	if err != nil {
		return nil, err
	}
	return p.call(bracket, "at", base, index), nil
}

func (p *parser) parseFieldRead(base Expr) (Expr, error) {
	dot := p.peek()
	p.index++
	name := p.peek()
	if name.kind != tokenIdentifier {
		return nil, p.errorf(name, "expected a field name after '.'")
	}
	p.index++
	// The lexer keeps a name and the dots in it together, so f(x).a.b arrives
	// as one name "a.b": it is two reads, as order.a.b is.
	return p.fieldChain(base, dot, dot.pos, name.text, true)
}

// fieldChain reads the dotted field names in fields off expr, the first
// after the dot at at. Each read covers the source from expr's start through
// its name, and its errors are placed at from. A postfix read, f(x).a.b,
// points at its own dot and marks its name as it goes; the fields of a
// dotted name, order.a.b, point at the name, which is already marked.
func (p *parser) fieldChain(expr Expr, from token, at int, fields string, postfix bool) (Expr, error) {
	start := expr.Extent().Start
	for field := range strings.SplitSeq(fields, ".") {
		end := at + 1 + len(field)
		pos := from.pos
		if postfix {
			pos = at
			p.markSpan(at+1, end, RoleField)
		}
		node, err := p.node(from, &FieldExpr{Node: Node{ID: p.id(), Pos: pos, Span: Span{start, end}}, Value: expr, Field: field})
		if err != nil {
			return nil, err
		}
		at, expr = end, node
	}
	return expr, nil
}

// call and boolean build the nodes an operator expands into. What has no
// source of its own — the false in a && b — covers the operator.
func (p *parser) call(at token, name string, args ...Expr) Expr {
	return &CallExpr{Node: Node{ID: p.id(), Pos: at.pos, Span: tokenSpan(at)}, Name: name, Args: args}
}

func (p *parser) pick(at token, condition, whenTrue, whenFalse Expr) Expr {
	return p.call(at, "if", condition, whenTrue, whenFalse)
}

func (p *parser) boolean(at token, value bool) Expr {
	return &LiteralExpr{Node: Node{ID: p.id(), Pos: at.pos, Span: tokenSpan(at)}, Value: machine.Bool(value)}
}

func tokenSpan(tok token) Span { return Span{Start: tok.pos, End: tok.end} }

// stamp records that expr was read from start up to the last token consumed.
func (p *parser) stamp(start int, expr Expr) Expr {
	if expr != nil {
		expr.setExtent(Span{Start: start, End: p.lastEnd()})
	}
	return expr
}

func (p *parser) lastEnd() int {
	consumed := min(p.index, len(p.tokens))
	if consumed == 0 {
		return 0
	}
	return p.tokens[consumed-1].end
}

func (p *parser) keyword(word string) bool {
	return p.peek().kind == tokenIdentifier && p.peek().text == word
}

// takeKeyword consumes the keyword keyword() just saw.
func (p *parser) takeKeyword() {
	p.mark(p.peek(), RoleKeyword)
	p.index++
}

// mark records what the parser took tok to be.
func (p *parser) mark(tok token, role Role) { p.markSpan(tok.pos, tok.end, role) }

func (p *parser) markSpan(start, end int, role Role) {
	if p.roles != nil {
		p.roles[start] = roleMark{end: end, role: role}
	}
}

func (p *parser) expect(kind tokenKind, what string) error {
	if p.peek().kind != kind {
		return p.errorf(p.peek(), "expected %s", what)
	}
	p.index++
	return nil
}

// exprThen reads an expression and the token that has to follow it.
func (p *parser) exprThen(kind tokenKind, what string) (Expr, error) {
	expr, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	if err := p.expect(kind, what); err != nil {
		return nil, err
	}
	return expr, nil
}

// parsePrimary reads one primary and stamps it with all it was read from:
// (a + b) covers its parentheses.
func (p *parser) parsePrimary() (Expr, error) {
	start := p.peek().pos
	expr, err := p.primary()
	if err != nil {
		return nil, err
	}
	return p.stamp(start, expr), nil
}

func (p *parser) primary() (Expr, error) {
	tok := p.peek()
	switch tok.kind {
	case tokenInt, tokenFloat:
		p.index++
		if p.startsFxRate(tok) {
			return p.fxRateLiteral(tok)
		}
		return p.numberLiteral(tok, false)
	case tokenString:
		p.index++
		p.mark(tok, RoleLiteral)
		value, err := p.unquote(tok, "invalid string escape")
		if err != nil {
			return nil, err
		}
		return &LiteralExpr{Node: p.at(tok.pos), Value: machine.String(value)}, nil
	case tokenEnum:
		p.index++
		p.mark(tok, RoleEnumMember)
		return p.node(tok, enumReference(p.at(tok.pos), tok.text))
	case tokenRatio:
		p.index++
		return p.ratioLiteral(tok)
	case tokenIdentifier:
		p.index++
		return p.name(tok)
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

// name reads what a name starts: an amount (USD 1.70), true or false, a
// call, a currency (USD), or a variable and the fields read off it.
func (p *parser) name(tok token) (Expr, error) {
	if p.startsMoney(tok) {
		return p.moneyLiteral(tok)
	}
	if tok.text == "true" || tok.text == "false" {
		p.mark(tok, RoleLiteral)
		return &LiteralExpr{Node: p.at(tok.pos), Value: machine.Bool(tok.text == "true")}, nil
	}
	if p.peek().kind == tokenLeftParen {
		return p.parseCall(tok)
	}
	if money.IsCurrencyCode(tok.text) {
		p.mark(tok, RoleCurrency)
		return p.node(tok, &CurrencyExpr{Node: p.at(tok.pos), Code: tok.text})
	}
	// A dotted name that is not being called is a variable and the fields
	// read off it: order.amount is at(order).amount, never one name. A call
	// keeps its dots, because that is how a function is versioned:
	// route.score_v1(…).
	return p.variableWithFields(tok)
}

// parseGroup reads (e), which is only there to override infix precedence.
func (p *parser) parseGroup() (Expr, error) {
	p.index++
	return p.exprThen(tokenRightParen, "')'")
}

// variableWithFields splits name.field.field into a variable and the field
// accesses on it, checking each part is a usable name.
func (p *parser) variableWithFields(tok token) (Expr, error) {
	name, fields, dotted := strings.Cut(tok.text, ".")
	if money.IsCurrencyCode(name) {
		return nil, p.errorf(tok, "%s is a currency, and a currency has no fields", name)
	}
	offset, role := tok.pos, RoleVariable
	for part := range strings.SplitSeq(tok.text, ".") {
		// An empty part (a..b) is no name to mark; the field read refuses it.
		if part != "" {
			p.markSpan(offset, offset+len(part), role)
		}
		offset, role = offset+len(part)+1, RoleField
	}
	if err := validName(name, "var"); err != nil {
		return nil, p.errorf(tok, "%v", err)
	}
	end := tok.pos + len(name)
	var expr Expr = &VariableExpr{Node: Node{ID: p.id(), Pos: tok.pos, Span: Span{tok.pos, end}}, Name: name}
	if !dotted {
		return expr, nil
	}
	return p.fieldChain(expr, tok, end, fields, false)
}

// callForm is the rule that reads a form written like a call, name(…), and
// nil for a name that is not one.
func callForm(name string) func(*parser, token) (Expr, error) {
	switch name {
	case "reduce":
		return (*parser).parseReduceCall
	case "switch":
		return (*parser).parseSwitchCall
	case "let":
		return (*parser).parseLetCall
	case "using":
		return (*parser).parseUsingCall
	}
	return nil
}

func (p *parser) parseCall(name token) (Expr, error) {
	p.index++ // (
	p.mark(name, RoleFunction)
	if form := callForm(name.text); form != nil {
		p.mark(name, RoleForm)
		return form(p, name)
	}
	args, err := p.parseList(tokenRightParen)
	if err != nil {
		return nil, err
	}
	return &CallExpr{Node: p.at(name.pos), Name: name.text, Args: args}, nil
}

// parseLetCall reads let(x = e1, y = e2, body). Bindings are ordered: a later
// value may use an earlier name, and the body sees all of them. The names are
// locals, so they never show up in the program's arguments.
func (p *parser) parseLetCall(name token) (Expr, error) {
	bindings, err := p.letBindings(name)
	if err != nil {
		return nil, err
	}
	body, err := p.exprThen(tokenRightParen, "')'")
	if err != nil {
		return nil, err
	}
	return p.node(name, &LetExpr{Node: p.at(name.pos), Bindings: bindings, Body: body})
}

func (p *parser) letBindings(name token) ([]LetBinding, error) {
	var bindings []LetBinding
	for p.startsBinding() {
		local, err := p.localIdentifier()
		if err != nil {
			return nil, err
		}
		p.index++ // =
		value, err := p.exprThen(tokenComma, "',' after a let binding")
		if err != nil {
			return nil, err
		}
		bindings = append(bindings, LetBinding{Name: local, Value: value})
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
	key, value, err := p.loopVariables()
	if err != nil {
		return nil, p.errorf(name, "reduce starts with its element name: reduce(item in source, acc = init, body)")
	}
	p.takeKeyword() // loopVariables has already checked the "in"
	return p.reduceKeywordForm(name, key, value)
}

func (p *parser) reduceKeywordForm(name token, key, variable string) (Expr, error) {
	source, where, err := p.loopSource()
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
	body, err := p.exprThen(tokenRightParen, "')'")
	if err != nil {
		return nil, err
	}
	return p.node(name, &ReduceExpr{
		Node: p.at(name.pos), Source: source, Variable: variable,
		KeyVariable: key, Where: where, Accumulator: accumulator, Init: init, Body: body,
	})
}

// parseAccumulator reads "total = 0,", the shape a let binding already has.
func (p *parser) parseAccumulator() (string, Expr, error) {
	if !p.startsBinding() {
		return "", nil, p.errorf(p.peek(), "reduce needs an accumulator and its initial value: acc = init")
	}
	accumulator, err := p.localIdentifier()
	if err != nil {
		return "", nil, err
	}
	p.index++ // =
	init, err := p.exprThen(tokenComma, "',' before the reduce body")
	if err != nil {
		return "", nil, err
	}
	return accumulator, init, nil
}

func (p *parser) parseArray() (Expr, error) {
	start := p.peek()
	p.index++
	if p.peek().kind == tokenRightBracket {
		p.index++
		return &ArrayExpr{Node: p.at(start.pos)}, nil
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
	return &ArrayExpr{Node: p.at(start.pos), Items: items}, nil
}

// localIdentifier consumes one identifier and checks it can name a local.
func (p *parser) localIdentifier() (string, error) {
	tok := p.peek()
	if tok.kind != tokenIdentifier || validName(tok.text, "local") != nil {
		return "", p.errorf(tok, "expected a local variable name")
	}
	p.index++
	p.mark(tok, RoleLocal)
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
	err := p.rest(end, true, "',' or closing delimiter", func() error {
		item, err := p.parseExpr()
		items = append(items, item)
		return err
	})
	if err != nil {
		return nil, err
	}
	return items, nil
}

// rest reads the items after the first of a comma separated list, each
// with item, and consumes the closing end. trailing lets a comma come right
// before end; missing is what is expected where neither a comma nor end is.
func (p *parser) rest(end tokenKind, trailing bool, missing string, item func() error) error {
	for {
		if p.peek().kind == end {
			p.index++
			return nil
		}
		if err := p.expect(tokenComma, missing); err != nil {
			return err
		}
		if trailing && p.peek().kind == end {
			p.index++
			return nil
		}
		if err := item(); err != nil {
			return err
		}
	}
}

func (p *parser) id() int {
	id := p.nextID
	p.nextID++
	return id
}

// at is a new node's Node: the next ID, with messages pointing at pos.
func (p *parser) at(pos int) Node { return Node{ID: p.id(), Pos: pos} }

func (p *parser) peek() token { return p.peekN(0) }

func (p *parser) peekN(offset int) token {
	index := p.index + offset
	if index >= len(p.tokens) {
		return token{kind: tokenEOF}
	}
	return p.tokens[index]
}

func (p *parser) errorf(tok token, format string, args ...any) error {
	return over(tok.pos, tok.end, "syntax error: %s", fmt.Sprintf(format, args...))
}
