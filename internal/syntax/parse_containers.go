package syntax

import (
	"slices"

	"github.com/nethinwei/funroute/internal/machine"
	"github.com/nethinwei/funroute/internal/money"
)

// Containers and comprehensions share one corner of the grammar: brackets
// build an array or map over one, and braces build a dictionary, a record, or
// a dictionary comprehension. Which one a brace starts is decided by its first
// entry, so the three live together here.

// parseBrace reads the three things a brace can start, told apart by what the
// first entry looks like:
//
//	{"k": v}                 a dictionary — string keys, one value type
//	{amount: 1200}           a record — named fields, each with its own type
//	{k: v for k, v in rates} a dictionary comprehension — keys are computed
//
// The first two differ only in the key, so the comprehension is what makes the
// lookahead necessary: its key is an expression, and only the "for" that
// follows the first entry says so.
func (p *parser) parseBrace() (Expr, error) {
	start := p.peek()
	p.index++
	if p.peek().kind == tokenRightBrace {
		p.index++
		return &DictExpr{ID: p.id(), Pos: start.pos}, nil
	}
	// The first key of a record is read as an expression, which would take a
	// reserved word for a bad variable name; it is a bad field name.
	if first := p.peek(); first.kind == tokenIdentifier && p.tokens[p.index+1].kind == tokenColon && machine.IsReservedName(first.text) {
		return nil, p.errorf(first, "invalid field name %q", first.text)
	}
	if first := p.peek(); p.fieldOnlyName(first) && p.tokens[p.index+1].kind == tokenColon {
		return p.recordNamedFirst(start, first)
	}
	key, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	if err := p.expect(tokenColon, "':' after the key"); err != nil {
		return nil, err
	}
	value, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	if p.keyword("for") {
		return p.dictComprehension(start, key, value)
	}
	return p.parseBraceLiteral(start, key, value)
}

// fieldOnlyName reports a name a field may have and a variable may not —
// if — so a brace starting with it and a colon can only be a record.
func (p *parser) fieldOnlyName(tok token) bool {
	return tok.kind == tokenIdentifier && machine.IsValidFieldName(tok.text) && !machine.IsValidVariableName(tok.text) && !money.IsCurrencyCode(tok.text)
}

// recordNamedFirst reads a record whose first field has a name no expression
// could have been read as.
func (p *parser) recordNamedFirst(start, name token) (Expr, error) {
	p.index += 2
	p.mark(name, RoleField)
	value, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	return p.recordLiteral(start, name.text, value)
}

// dictComprehension reads the tail of {key: value for item in source if cond}.
// One clause only: splicing dictionaries would have to answer what a repeated
// key means, and a nested list comprehension inside the value says it better.
func (p *parser) dictComprehension(start token, key, value Expr) (Expr, error) {
	clauses, err := p.loopClauses(start)
	if err != nil {
		return nil, err
	}
	if len(clauses) > 1 {
		return nil, p.errorf(start, "a dictionary comprehension takes one 'for' clause")
	}
	if err := p.expect(tokenRightBrace, "'}'"); err != nil {
		return nil, err
	}
	return p.nestClauses(start, clauses, key, value)
}

// loopClause is one "for x in xs if c" of a comprehension.
type loopClause struct {
	key, variable string
	source, where Expr
}

// loopClauses reads the clauses back to back, starting on a "for". Python
// spells a cartesian product this way and so does this language, because the
// alternative — flatten([[...] for ...]) — is a puzzle, not a rule.
func (p *parser) loopClauses(start token) ([]loopClause, error) {
	var clauses []loopClause
	for {
		p.takeKeyword() // for
		key, variable, err := p.loopVariables(start)
		if err != nil {
			return nil, err
		}
		p.takeKeyword() // in
		source, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		where, err := p.loopFilter()
		if err != nil {
			return nil, err
		}
		clauses = append(clauses, loopClause{key: key, variable: variable, source: source, where: where})
		if !p.keyword("for") {
			return clauses, nil
		}
	}
}

// nestClauses builds the ForExpr chain from the inside out. The innermost
// clause yields the element and carries the dictionary key if there is one;
// every clause outside it splices what the one inside yielded.
func (p *parser) nestClauses(start token, clauses []loopClause, yieldKey, yield Expr) (Expr, error) {
	node := yield
	for i, clause := range slices.Backward(clauses) {
		innermost := i == len(clauses)-1
		var key Expr
		if innermost {
			key = yieldKey
		}
		expr, err := p.node(start, &ForExpr{
			ID: p.id(), Pos: start.pos, Source: clause.source, Variable: clause.variable,
			KeyVariable: clause.key, Where: clause.where, YieldKey: key, Yield: node,
			Flatten: !innermost,
		})
		if err != nil {
			return nil, err
		}
		// Every clause is part of the one comprehension that was written.
		node = p.stamp(start.pos, expr)
	}
	return node, nil
}

// parseBraceLiteral finishes a dictionary or a record, decided by the first
// key: a string literal means a dictionary, a bare name means a record.
func (p *parser) parseBraceLiteral(start token, key, value Expr) (Expr, error) {
	if literal, ok := key.(*LiteralExpr); ok {
		text, isString := literal.Value.String()
		if !isString {
			return nil, p.errorf(start, "a dictionary key must be a string, a record field must be a name")
		}
		return p.dictLiteral(start, text, value)
	}
	// It was read as an expression before the brace said what it is: a
	// name, or a field shaped like a code, which read as a currency.
	switch name := key.(type) {
	case *VariableExpr:
		p.markSpan(name.Pos, name.Pos+len(name.Name), RoleField)
		return p.recordLiteral(start, name.Name, value)
	case *CurrencyExpr:
		p.markSpan(name.Pos, name.Pos+len(name.Code), RoleField)
		return p.recordLiteral(start, name.Code, value)
	}
	return nil, p.errorf(start, "a record field is written as name: value")
}

func (p *parser) dictLiteral(start token, firstKey string, firstValue Expr) (Expr, error) {
	entries := []DictEntryExpr{{Key: firstKey, Value: firstValue}}
	for {
		if done, err := p.endOfBrace(); err != nil {
			return nil, err
		} else if done {
			return p.node(start, &DictExpr{ID: p.id(), Pos: start.pos, Entries: entries})
		}
		keyToken := p.peek()
		if keyToken.kind != tokenString {
			return nil, p.errorf(keyToken, "dictionary keys must be strings")
		}
		p.index++
		p.mark(keyToken, RoleLiteral)
		key, err := p.unquote(keyToken, "invalid dictionary key")
		if err != nil {
			return nil, err
		}
		if err := p.expect(tokenColon, "':' after the dictionary key"); err != nil {
			return nil, err
		}
		value, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		entries = append(entries, DictEntryExpr{Key: key, Value: value})
	}
}

func (p *parser) recordLiteral(start token, firstName string, firstValue Expr) (Expr, error) {
	fields, err := p.recordFields([]RecordFieldExpr{{Name: firstName, Value: firstValue}})
	if err != nil {
		return nil, err
	}
	return p.node(start, &RecordExpr{ID: p.id(), Pos: start.pos, Fields: fields})
}

// parseWith reads what follows base in base with {name: value, …}: the
// record base with those fields replaced. It is a postfix, as .field is, and
// at least one field follows: with {} would be base itself.
func (p *parser) parseWith(base Expr) (Expr, error) {
	with := p.peek()
	p.takeKeyword()
	if err := p.expect(tokenLeftBrace, "'{' after with, as in order with {amount: 1}"); err != nil {
		return nil, err
	}
	if closing := p.peek(); closing.kind == tokenRightBrace {
		return nil, p.errorf(closing, "with {} changes nothing: name the fields to replace, as in order with {amount: 1}")
	}
	first, err := p.recordField()
	if err != nil {
		return nil, err
	}
	fields, err := p.recordFields([]RecordFieldExpr{first})
	if err != nil {
		return nil, err
	}
	return p.node(with, &RecordUpdateExpr{ID: p.id(), Pos: with.pos, Base: base, Fields: fields})
}

// recordFields reads ", name: value" entries up to the closing brace.
func (p *parser) recordFields(fields []RecordFieldExpr) ([]RecordFieldExpr, error) {
	for {
		if done, err := p.endOfBrace(); err != nil {
			return nil, err
		} else if done {
			return fields, nil
		}
		field, err := p.recordField()
		if err != nil {
			return nil, err
		}
		fields = append(fields, field)
	}
}

// recordField reads one name: value.
func (p *parser) recordField() (RecordFieldExpr, error) {
	name := p.peek()
	if name.kind != tokenIdentifier {
		return RecordFieldExpr{}, p.errorf(name, "record fields are written as name: value")
	}
	if machine.IsReservedName(name.text) {
		return RecordFieldExpr{}, p.errorf(name, "invalid field name %q", name.text)
	}
	p.index++
	p.mark(name, RoleField)
	if err := p.expect(tokenColon, "':' after the field name"); err != nil {
		return RecordFieldExpr{}, err
	}
	value, err := p.parseExpr()
	if err != nil {
		return RecordFieldExpr{}, err
	}
	return RecordFieldExpr{Name: name.text, Value: value}, nil
}

// endOfBrace consumes the separator after an entry and reports whether the
// brace closed, so a trailing comma reads the same as in a list.
func (p *parser) endOfBrace() (bool, error) {
	if p.peek().kind == tokenRightBrace {
		p.index++
		return true, nil
	}
	if err := p.expect(tokenComma, "',' between entries"); err != nil {
		return false, err
	}
	if p.peek().kind == tokenRightBrace {
		p.index++
		return true, nil
	}
	return false, nil
}

// comprehension reads [yield for item in source if condition], the same shape
// Python and Haskell use. It is sugar for a ForExpr, so ExprJSON and the canvas
// see one node either way.
func (p *parser) comprehension(start token, yield Expr) (Expr, error) {
	clauses, err := p.loopClauses(start)
	if err != nil {
		return nil, err
	}
	if err := p.expect(tokenRightBracket, "']'"); err != nil {
		return nil, err
	}
	return p.nestClauses(start, clauses, nil, yield)
}

// loopFilter reads the optional "if condition" that both loop forms share:
// items the condition rejects are neither yielded nor folded.
func (p *parser) loopFilter() (Expr, error) {
	if !p.keyword("if") {
		return nil, nil
	}
	p.takeKeyword()
	return p.parseExpr()
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
