package syntax

import (
	"fmt"
	"funroute/lang/internal/machine"
	"strconv"
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

// Infix operators are pure source sugar: they desugar into calls, so the AST,
// ExprJSON and the canvas never see them. Every one is left associative.
var precedences = map[tokenKind]int{
	tokenOrOr:      1,
	tokenAndAnd:    2,
	tokenEqEq:      3,
	tokenBangEq:    3,
	tokenLess:      4,
	tokenLessEq:    4,
	tokenGreater:   4,
	tokenGreaterEq: 4,
	tokenPlus:      5,
	tokenMinus:     5,
	tokenStar:      6,
	tokenSlash:     6,
}

// binaryFunctions maps an operator to the kernel function it becomes. The
// operators missing here (&&, ||, !=) are derived forms that expand into `if`
// instead; see desugarBinary.
var binaryFunctions = map[tokenKind]string{
	tokenEqEq:      "eq",
	tokenLess:      "lt",
	tokenLessEq:    "le",
	tokenGreater:   "gt",
	tokenGreaterEq: "ge",
	tokenPlus:      "add",
	tokenMinus:     "sub",
	tokenStar:      "mul",
	tokenSlash:     "div",
}

// Parse reads an expression. A program is only an expression: the contract it
// runs under is the host's, and arrives through CompileOptions.
func Parse(source string) (Expr, error) {
	tokens, err := tokenize(source)
	if err != nil {
		return nil, err
	}
	p := &parser{tokens: tokens, nextID: 1}
	if p.peek().kind == tokenIdentifier && p.peek().text == "expr" && p.peekN(1).kind == tokenArrow {
		return nil, p.errorf(p.peek(), "the expr-> prefix is not part of the syntax; write the expression directly")
	}
	if p.peek().kind == tokenAt {
		return nil, p.errorf(p.peek(), "@arg/@let/@ret headers were removed; declare arguments through the host's contract and write bindings as let(name = value, body)")
	}
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
		precedence, ok := precedences[operator.kind]
		if !ok || precedence < min {
			return left, nil
		}
		p.index++
		right, err := p.parseBinary(precedence + 1)
		if err != nil {
			return nil, err
		}
		left = p.desugarBinary(operator, left, right)
	}
}

func (p *parser) desugarBinary(operator token, left, right Expr) Expr {
	switch operator.kind {
	case tokenBangEq:
		return p.pick(operator, p.call(operator, "eq", left, right), p.boolean(operator, false), p.boolean(operator, true))
	case tokenAndAnd:
		// Short circuits, because if is lazy.
		return p.pick(operator, left, right, p.boolean(operator, false))
	case tokenOrOr:
		return p.pick(operator, left, p.boolean(operator, true), right)
	default:
		return p.call(operator, binaryFunctions[operator.kind], left, right)
	}
}

func (p *parser) parseUnary() (Expr, error) {
	operator := p.peek()
	switch operator.kind {
	case tokenMinus:
		p.index++
		return p.negate(operator)
	case tokenBang:
		p.index++
		operand, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return p.pick(operator, operand, p.boolean(operator, false), p.boolean(operator, true)), nil
	default:
		return p.parsePrimary()
	}
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
	case tokenIdentifier:
		p.index++
		if tok.text == "true" || tok.text == "false" {
			return &LiteralExpr{ID: p.id(), Pos: tok.pos, Value: machine.Bool(tok.text == "true")}, nil
		}
		if p.peek().kind == tokenAt {
			// "@" used to be a decorative version suffix on function names. It
			// carried no meaning, so it was freed up for annotations.
			return nil, p.errorf(p.peek(), "'@' is an annotation marker, not a name suffix; write %s_v1 instead of %s@1", tok.text, tok.text)
		}
		if p.peek().kind == tokenLeftParen {
			return p.parseCall(tok)
		}
		return &VariableExpr{ID: p.id(), Pos: tok.pos, Name: tok.text}, nil
	case tokenLeftParen:
		// Grouping, so infix precedence can be overridden.
		p.index++
		inner, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		if err := p.expect(tokenRightParen, "')'"); err != nil {
			return nil, err
		}
		return inner, nil
	case tokenLeftBracket:
		return p.parseArray()
	case tokenLeftBrace:
		return p.parseDict()
	default:
		return nil, p.errorf(tok, "expected an expression")
	}
}

func (p *parser) parseCall(name token) (Expr, error) {
	p.index++ // (
	if name.text == "for" {
		return nil, p.errorf(name, "for(...) is not part of the syntax; write a comprehension: [result for item in source if condition]")
	}
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

// parseReduceCall accepts the keyword form
//
//	reduce(item in source, acc from init, body)
//
// and the positional form reduce(source, item, acc, init, body).
func (p *parser) parseReduceCall(name token) (Expr, error) {
	if p.startsKeywordReduce() {
		key, value, err := p.loopVariables(name)
		if err != nil {
			return nil, err
		}
		p.index++ // in
		return p.reduceKeywordForm(name, key, value)
	}
	args, err := p.parseList(tokenRightParen)
	if err != nil {
		return nil, err
	}
	return p.reduceExpr(name, args)
}

// startsKeywordReduce peeks for "v in" or "k, v in", which distinguishes the
// keyword form from the positional one.
func (p *parser) startsKeywordReduce() bool {
	if p.peek().kind != tokenIdentifier {
		return false
	}
	if p.peekN(1).kind == tokenIdentifier && p.peekN(1).text == "in" {
		return true
	}
	return p.peekN(1).kind == tokenComma && p.peekN(2).kind == tokenIdentifier &&
		p.peekN(3).kind == tokenIdentifier && p.peekN(3).text == "in"
}

func (p *parser) reduceKeywordForm(name token, key, variable string) (Expr, error) {
	source, err := p.parseExpr()
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
		KeyVariable: key, Accumulator: accumulator, Init: init, Body: body,
	})
}

// parseAccumulator reads "total from 0".
func (p *parser) parseAccumulator() (string, Expr, error) {
	named, err := p.parseExpr()
	if err != nil {
		return "", nil, err
	}
	variable, ok := named.(*VariableExpr)
	if !ok {
		return "", nil, p.errorf(p.peek(), "reduce accumulator must be a local variable name")
	}
	if !p.keyword("from") {
		return "", nil, p.errorf(p.peek(), "expected 'from' after the accumulator name")
	}
	p.index++
	init, err := p.parseExpr()
	if err != nil {
		return "", nil, err
	}
	return variable.Name, init, nil
}

// parseSwitchCall accepts all three shapes. The case keyword marks where the
// branches start, which is what removes the ambiguity between a subject and a
// multi-value condition branch:
//
//	switch(subject, case "SG" => "a", case "MY", "TH" => "b", else "c")
//	switch(case amount > 100 => "a", case risk > 0.8 => "b", else "c")
//	switch(subject, "SG", "a", "MY", "b", "c")               positional
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
	second, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	rest, err := p.parseRest(second, tokenRightParen)
	if err != nil {
		return nil, err
	}
	return p.positionalSwitch(name, append([]Expr{subject}, rest...))
}

// switchBranches reads "case m1, m2 => r" groups and the final "else d".
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
		if err := p.expect(tokenComma, "',' after a switch branch"); err != nil {
			return nil, err
		}
	}
	if len(cases) == 0 {
		return nil, p.errorf(name, "switch needs at least one case")
	}
	if !p.keyword("else") {
		return nil, p.errorf(p.peek(), "switch needs a final 'else' result")
	}
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

func (p *parser) positionalSwitch(name token, args []Expr) (Expr, error) {
	if len(args) < 4 || len(args)%2 != 0 {
		return nil, p.errorf(name, "switch expects value, one or more match/result pairs, and a default result")
	}
	cases := make([]SwitchCaseExpr, 0, (len(args)-2)/2)
	for i := 1; i < len(args)-1; i += 2 {
		cases = append(cases, SwitchCaseExpr{Match: []Expr{args[i]}, Result: args[i+1]})
	}
	return &SwitchExpr{ID: p.id(), Pos: name.pos, Value: args[0], Cases: cases, Default: args[len(args)-1]}, nil
}

func (p *parser) reduceExpr(name token, args []Expr) (Expr, error) {
	if len(args) != 5 {
		return nil, p.errorf(name, "reduce expects source, item variable, accumulator variable, initial value, and body")
	}
	variable, err := p.localName(name, args[1], "reduce item")
	if err != nil {
		return nil, err
	}
	accumulator, err := p.localName(name, args[2], "reduce accumulator")
	if err != nil {
		return nil, err
	}
	return p.node(name, &ReduceExpr{
		ID: p.id(), Pos: name.pos, Source: args[0],
		Variable: variable, Accumulator: accumulator, Init: args[3], Body: args[4],
	})
}

// localName validates a positional argument that names a locally bound
// variable rather than an expression.
func (p *parser) localName(name token, arg Expr, what string) (string, error) {
	variable, ok := arg.(*VariableExpr)
	if !ok {
		return "", p.errorf(name, "%s must be a local variable name", what)
	}
	if variable.Name == "true" || variable.Name == "false" || variable.Name == "recur" {
		return "", p.errorf(name, "invalid local variable %q", variable.Name)
	}
	return variable.Name, nil
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

// comprehension reads [yield for item in source if condition], the same shape
// Python and Haskell use. It produces the same node the positional form used
// to, so ExprJSON and the canvas are unchanged.
func (p *parser) comprehension(start token, yield Expr) (Expr, error) {
	p.index++ // for
	key, variable, err := p.loopVariables(start)
	if err != nil {
		return nil, err
	}
	p.index++ // in
	source, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	var where Expr
	if p.keyword("if") {
		p.index++
		if where, err = p.parseExpr(); err != nil {
			return nil, err
		}
	}
	if err := p.expect(tokenRightBracket, "']'"); err != nil {
		return nil, err
	}
	return p.node(start, &ForExpr{
		ID: p.id(), Pos: start.pos, Source: source,
		Variable: variable, KeyVariable: key, Where: where, Yield: yield,
	})
}

// loopVariables reads "v" or "k, v" and leaves the parser on the in keyword.
// Two variables mean a dictionary walk: the key comes first, like Python's
// `for k, v in d.items()`.
func (p *parser) loopVariables(at token) (key string, value string, err error) {
	first, err := p.localIdentifier()
	if err != nil {
		return "", "", err
	}
	if p.peek().kind != tokenComma {
		if !p.keyword("in") {
			return "", "", p.errorf(p.peek(), "expected 'in' after the loop variable")
		}
		return "", first, nil
	}
	p.index++
	second, err := p.localIdentifier()
	if err != nil {
		return "", "", err
	}
	if !p.keyword("in") {
		return "", "", p.errorf(p.peek(), "expected 'in' after the loop variables")
	}
	return first, second, nil
}

// localIdentifier consumes one identifier and checks it can name a local.
func (p *parser) localIdentifier() (string, error) {
	tok := p.peek()
	if tok.kind != tokenIdentifier || machine.IsReservedName(tok.text) {
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

func (p *parser) parseDict() (Expr, error) {
	start := p.peek()
	p.index++
	var entries []DictEntryExpr
	if p.peek().kind == tokenRightBrace {
		p.index++
		return &DictExpr{ID: p.id(), Pos: start.pos}, nil
	}
	for {
		keyToken := p.peek()
		if keyToken.kind != tokenString {
			return nil, p.errorf(keyToken, "dictionary keys must be strings")
		}
		p.index++
		key, err := strconv.Unquote(keyToken.text)
		if err != nil {
			return nil, p.errorf(keyToken, "invalid dictionary key")
		}
		if p.peek().kind != tokenColon {
			return nil, p.errorf(p.peek(), "expected ':' after dictionary key")
		}
		p.index++
		value, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		entries = append(entries, DictEntryExpr{Key: key, Value: value})
		if p.peek().kind == tokenRightBrace {
			p.index++
			return p.node(start, &DictExpr{ID: p.id(), Pos: start.pos, Entries: entries})
		}
		if p.peek().kind != tokenComma {
			return nil, p.errorf(p.peek(), "expected ',' or '}'")
		}
		p.index++
		if p.peek().kind == tokenRightBrace {
			p.index++
			return p.node(start, &DictExpr{ID: p.id(), Pos: start.pos, Entries: entries})
		}
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
	return fmt.Errorf("syntax error at byte %d: %s", tok.pos, fmt.Sprintf(format, args...))
}
