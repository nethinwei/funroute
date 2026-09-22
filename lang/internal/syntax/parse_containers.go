package syntax

import "strconv"

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

// dictComprehension reads the tail of {key: value for item in source if cond}.
func (p *parser) dictComprehension(start token, key, value Expr) (Expr, error) {
	p.index++ // for
	loopKey, variable, err := p.loopVariables(start)
	if err != nil {
		return nil, err
	}
	p.index++ // in
	source, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	where, err := p.loopFilter()
	if err != nil {
		return nil, err
	}
	if err := p.expect(tokenRightBrace, "'}'"); err != nil {
		return nil, err
	}
	return p.node(start, &ForExpr{
		ID: p.id(), Pos: start.pos, Source: source, Variable: variable,
		KeyVariable: loopKey, Where: where, YieldKey: key, Yield: value,
	})
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
	name, ok := key.(*VariableExpr)
	if !ok {
		return nil, p.errorf(start, "a record field is written as name: value")
	}
	return p.recordLiteral(start, name.Name, value)
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
		key, err := strconv.Unquote(keyToken.text)
		if err != nil {
			return nil, p.errorf(keyToken, "invalid dictionary key")
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
	fields := []RecordFieldExpr{{Name: firstName, Value: firstValue}}
	for {
		if done, err := p.endOfBrace(); err != nil {
			return nil, err
		} else if done {
			return p.node(start, &RecordExpr{ID: p.id(), Pos: start.pos, Fields: fields})
		}
		name := p.peek()
		if name.kind != tokenIdentifier {
			return nil, p.errorf(name, "record fields are written as name: value")
		}
		p.index++
		if err := p.expect(tokenColon, "':' after the field name"); err != nil {
			return nil, err
		}
		value, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		fields = append(fields, RecordFieldExpr{Name: name.text, Value: value})
	}
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
	where, err := p.loopFilter()
	if err != nil {
		return nil, err
	}
	if err := p.expect(tokenRightBracket, "']'"); err != nil {
		return nil, err
	}
	return p.node(start, &ForExpr{
		ID: p.id(), Pos: start.pos, Source: source,
		Variable: variable, KeyVariable: key, Where: where, Yield: yield,
	})
}

// loopFilter reads the optional "if condition" that both loop forms share:
// items the condition rejects are neither yielded nor folded.
func (p *parser) loopFilter() (Expr, error) {
	if !p.keyword("if") {
		return nil, nil
	}
	p.index++
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
