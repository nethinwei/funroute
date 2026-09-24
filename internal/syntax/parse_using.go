package syntax

// parseUsingCall reads using(rate, …, body): exchange rates, each an fxrate
// or an array<fxrate>, and the body last. A using converts at what it names;
// a rate from an outer using comes in as fx(base, quote).
func (p *parser) parseUsingCall(name token) (Expr, error) {
	entries, err := p.usingEntries()
	if err != nil {
		return nil, err
	}
	last := entries[len(entries)-1]
	if len(entries) < 2 {
		return nil, p.errorf(name, "using needs an exchange rate and ends with its body")
	}
	node := &UsingExpr{Node: p.at(name.pos), Body: last, Quotes: entries[:len(entries)-1]}
	return p.node(name, node)
}

// usingEntries reads the comma separated entries of a using through the
// closing parenthesis. Unlike a call's arguments they take no trailing comma.
func (p *parser) usingEntries() ([]Expr, error) {
	entry, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	entries := []Expr{entry}
	err = p.rest(tokenRightParen, false, "',' or ')'", func() error {
		entry, err := p.parseExpr()
		entries = append(entries, entry)
		return err
	})
	if err != nil {
		return nil, err
	}
	return entries, nil
}
