package compile

import (
	"github.com/nethinwei/funroute/internal/machine"
	"github.com/nethinwei/funroute/internal/syntax"
)

// using(rate, …, body) types as its body. Every quote is an exchange rate —
// implied(settled, paid), 150 JPY / USD, an argument — or an array of them,
// between any two currencies.

func inferUsing(node *syntax.UsingExpr, state *inferState, context inferContext) ([]inferResult, error) {
	states := []*inferState{state}
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
			dropped = syntax.Around(rate, "type error: using quotes exchange rates — an fxrate such as 150 JPY / USD or implied(settled, paid), or an array<fxrate> — and this is %s", candidate.describe(results[0].typ))
		}
	}
	if len(next) == 0 {
		return nil, dropped
	}
	return next, nil
}

// asExchangeRate is result's state with its type an exchange rate, or an
// array of them, if it can be one.
func asExchangeRate(result inferResult) (*inferState, bool) {
	candidate := result.state.clone()
	if candidate.unify(scalarTerm(machine.FxRateKind), result.typ) == nil {
		return candidate, true
	}
	candidate = result.state.clone()
	return candidate, candidate.unify(containerTerm(machine.ArrayKind, scalarTerm(machine.FxRateKind)), result.typ) == nil
}
