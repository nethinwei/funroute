package syntax

// parseSwitchCall accepts all three shapes. The case keyword marks where the
// branches start, which is what removes the ambiguity between a subject and a
// multi-value condition branch:
//
//	switch(subject, case "SG" => "a", case "MY", "TH" => "b", else => "c")
//	switch(case amount > 100 => "a", case risk > 0.8 => "b", else => "c")
func (p *parser) parseSwitchCall(name token) (Expr, error) {
	if p.keyword("case") {
		return p.switchBranches(name, nil)
	}
	subject, err := p.exprThen(tokenComma, "',' after the switch subject")
	if err != nil {
		return nil, err
	}
	if p.keyword("case") {
		return p.switchBranches(name, subject)
	}
	return nil, p.errorf(name, "switch branches start with \"case\": switch(subject, case m => r, else => d)")
}

// switchBranches reads "case m1, m2 => r" groups and an optional final
// "else => d". The compiler accepts the missing else only when a declared enum
// subject is covered exhaustively.
func (p *parser) switchBranches(name token, subject Expr) (Expr, error) {
	var cases []SwitchCaseExpr
	for p.keyword("case") {
		p.takeKeyword()
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
	var fallback Expr
	if p.keyword("else") {
		p.takeKeyword()
		// Every branch leads to its result with =>: "else => r" as a case
		// reads "case m => r". There is no second spelling.
		arrow := p.peek()
		if arrow.kind != tokenFatArrow {
			return nil, p.errorf(arrow, "expected '=>' after else: a switch reads switch(subject, case m => r, else => d)")
		}
		p.index++
		p.mark(arrow, RoleOperator)
		var err error
		if fallback, err = p.parseExpr(); err != nil {
			return nil, err
		}
	}
	if err := p.expect(tokenRightParen, "')'"); err != nil {
		return nil, err
	}
	return &SwitchExpr{Node: p.at(name.pos), Value: subject, Cases: cases, Default: fallback}, nil
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
	return p.parseRest(entry, tokenRightParen, false, "',' or ')'")
}
