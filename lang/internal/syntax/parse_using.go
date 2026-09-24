package syntax

// parseUsingCall reads using(…, rate, …, body): a named rate table first if
// there is one, exchange rates, and the body last. A using converts at what
// it writes; a rate from outside comes in as fx(base, quote).
func (p *parser) parseUsingCall(name token) (Expr, error) {
	entries, err := p.usingEntries()
	if err != nil {
		return nil, err
	}
	last := entries[len(entries)-1]
	if len(entries) < 2 {
		return nil, p.errorf(name, "using needs an exchange rate and ends with its body")
	}
	node := &UsingExpr{ID: p.id(), Pos: name.pos, Body: last}
	if table, named := entries[0].(*EnumExpr); named {
		node.Table, entries = table, entries[1:]
	}
	for _, entry := range entries[:len(entries)-1] {
		if _, named := entry.(*EnumExpr); named {
			return nil, p.errorf(name, "a using names its rate table first: using(@table, quotes…, body)")
		}
		node.Quotes = append(node.Quotes, entry)
	}
	return p.node(name, node)
}

// usingEntries reads the comma separated entries of a using through the
// closing parenthesis.
func (p *parser) usingEntries() ([]Expr, error) {
	var entries []Expr
	for {
		entry, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
		if p.peek().kind == tokenRightParen {
			p.index++
			return entries, nil
		}
		if err := p.expect(tokenComma, "',' or ')'"); err != nil {
			return nil, err
		}
	}
}
