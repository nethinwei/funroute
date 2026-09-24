package compile

import (
	"funroute/lang/internal/machine"
	"funroute/lang/internal/syntax"
)

// using(…, rate, …, body) types as its body. Every rate it quotes is an
// exchange rate — settled / paid, 150 JPY / USD, an argument — between any
// two currencies.

func inferUsing(node *syntax.UsingExpr, state *inferState, context inferContext) ([]inferResult, error) {
	states, err := inferRateTable(node.Table, state, context)
	if err != nil {
		return nil, err
	}
	for _, rate := range node.Quotes {
		next, err := inferQuotedRate(rate, states, context)
		if err != nil {
			return nil, err
		}
		states = next
	}
	var out []inferResult
	var dropped error
	for _, candidate := range states {
		bodies, err := inferExpr(node.Body, candidate, context)
		if err != nil {
			dropped = err
			continue
		}
		out = append(out, bodies...)
	}
	if len(out) == 0 {
		return nil, dropped
	}
	return record(node, out), nil
}

// inferRateTable types a using's @name: a member of rate_table, one of the
// named rate tables the contract declares.
func inferRateTable(table syntax.Expr, state *inferState, context inferContext) ([]*inferState, error) {
	if table == nil {
		return []*inferState{state}, nil
	}
	results, err := inferExpr(table, state, context)
	if err != nil {
		return nil, err
	}
	var out []*inferState
	for _, result := range results {
		if typ, ok := result.state.publicType(result.typ); ok && typ.Kind() == machine.EnumKind && typ.Name() == machine.RateTableEnum {
			out = append(out, result.state)
		}
	}
	if len(out) == 0 {
		return nil, syntax.Around(table, "type error: using names a rate table the contract declares, and %s is none", table.(*syntax.EnumExpr).Source())
	}
	return out, nil
}

// inferQuotedRate types one quoted rate in every candidate state, keeping the
// readings that are exchange rates.
func inferQuotedRate(rate syntax.Expr, states []*inferState, context inferContext) ([]*inferState, error) {
	var next []*inferState
	var dropped error
	for _, candidate := range states {
		results, err := inferExpr(rate, candidate, context)
		if err != nil {
			dropped = err
			continue
		}
		for _, result := range results {
			if reading, ok := asExchangeRate(result); ok {
				next = append(next, reading)
			}
		}
		if len(results) > 0 && len(next) == 0 {
			dropped = syntax.Around(rate, "type error: using quotes exchange rates, such as settled / paid or 150 JPY / USD, and this is %s", candidate.describe(results[0].typ))
		}
	}
	if len(next) == 0 {
		return nil, dropped
	}
	return next, nil
}

// asExchangeRate is result's state with its type an exchange rate between
// any two currencies, if it can be one.
func asExchangeRate(result inferResult) (*inferState, bool) {
	candidate := result.state.clone()
	rate := candidate.unitTerm(machine.FxRateKind, []string{"", ""}, func(string) typeTerm { return candidate.freshUnit(unitInfo{}) })
	return candidate, candidate.unify(rate, result.typ) == nil
}
