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
	if p.keyword("else") {
		p.takeKeyword()
		// Every branch leads to its result with =>: "else => r" as a case
		// reads "case m => r". There is no second spelling.
		if arrow := p.peek(); arrow.kind == tokenFatArrow {
			p.index++
			p.mark(arrow, RoleOperator)
		} else {
			return nil, p.errorf(arrow, "expected '=>' after else: a switch reads switch(subject, case m => r, else => d)")
		}
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
