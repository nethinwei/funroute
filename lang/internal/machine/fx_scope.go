package machine

import (
	"context"
	"fmt"
	"maps"
	"math/big"
	"slices"
)

// A using runs part of a program with exchange rates of the rule's own.
// OpFxPush builds the rate table that part converts through and hands it to
// the run the way RunOptions.Rates is handed in, as the context's rates, so
// convert reads it with nothing else to know; OpFxPop gives the old context
// back. The table is the quotes alone — the run's own rates and any using
// outside take no part — or, under @name, that named table with the quotes
// laid over it, each pair both ways, later ones over earlier.

func validateFxPush(in Instruction, artifact *Artifact, fail failFunc) error {
	switch {
	case len(in.Keys) > 1:
		return fail("fx_push names one rate table at most")
	case in.A != 0 || in.C != 0 || in.B < 0:
		return fail("fx_push takes B quotes and nothing else, not A=%d B=%d C=%d", in.A, in.B, in.C)
	case len(in.Keys) == 1 && !slices.Contains(artifact.parts.RateTables, in.Keys[0]):
		return fail("fx_push names rate table %q, which the contract does not declare", in.Keys[0])
	case len(in.Keys) == 0 && in.B == 0:
		return fail("fx_push needs quotes")
	}
	return nil
}

// pushScope opens a using: its exchange rates are on the stack. A using of constant quotes alone has its table built at load
// (constantScopes), which it takes when the quotes are those constants.
func (f *frame) pushScope(pc int, in Instruction) error {
	quotes, err := f.popN(in.B)
	if err != nil {
		return err
	}
	table := f.runtime.money.table
	if table == nil {
		return fmt.Errorf("using needs a registry that declares money")
	}
	if scope, ok := f.runtime.fxScopes[pc]; ok && scope.holds(quotes) {
		run, _ := f.ctx.Value(ratesKey{}).(runRates)
		f.scopes = append(f.scopes, f.ctx)
		f.ctx = context.WithValue(f.ctx, ratesKey{}, runRates{table: table, graph: scope.graph, names: run.names, tables: run.tables})
		return nil
	}
	rates := make([]FxRate, len(quotes))
	for i, quote := range quotes {
		rate, ok := quote.FxRate()
		if !ok {
			return fmt.Errorf("a using quote is %s, not an exchange rate", quote.Type())
		}
		rates[i] = rate
	}
	run, _ := f.ctx.Value(ratesKey{}).(runRates)
	base := newRateGraph()
	if len(in.Keys) == 1 {
		base = run.named(in.Keys[0])
	}
	f.scopes = append(f.scopes, f.ctx)
	graph := base.using(rates)
	f.ctx = context.WithValue(f.ctx, ratesKey{}, runRates{table: table, graph: graph, names: run.names, tables: run.tables})
	return nil
}

// constantScope is the table of a using whose quotes are constants and which
// names no table, built once at load.
type constantScope struct {
	quotes []Value
	graph  *rateGraph
}

// holds reports quotes that are the scope's constants: a jump into the middle
// of the code before a push would have left others on the stack.
func (s constantScope) holds(quotes []Value) bool {
	for i, quote := range quotes {
		if quote.box != s.quotes[i].box {
			return false
		}
	}
	return len(quotes) == len(s.quotes)
}

// constantScopes builds, for each using of constant quotes and no named
// table, the table it converts through, by the
// program counter of its fx_push. Nothing about it depends on the run, and a
// run that took it would otherwise copy and fill a table each time.
func constantScopes(artifact *Artifact, constants []Value) map[int]constantScope {
	var scopes map[int]constantScope
	instructions := artifact.parts.Instructions
	for pc, in := range instructions {
		if in.Op != OpFxPush || len(in.Keys) != 0 || pc < in.B {
			continue
		}
		scope, ok := constantScopeAt(instructions[pc-in.B:pc], constants)
		if !ok {
			continue
		}
		if scopes == nil {
			scopes = map[int]constantScope{}
		}
		scopes[pc] = scope
	}
	return scopes
}

// constantScopeAt is the scope the instructions push, if every one of them
// loads a constant exchange rate.
func constantScopeAt(loads []Instruction, constants []Value) (constantScope, bool) {
	scope := constantScope{quotes: make([]Value, len(loads))}
	rates := make([]FxRate, len(loads))
	for i, load := range loads {
		if load.Op != OpConstant || load.A < 0 || load.A >= len(constants) {
			return constantScope{}, false
		}
		rate, ok := constants[load.A].FxRate()
		if !ok {
			return constantScope{}, false
		}
		scope.quotes[i], rates[i] = constants[load.A], rate
	}
	scope.graph = newRateGraph().using(rates)
	return scope, true
}

// popScope closes the innermost using.
func (f *frame) popScope() error {
	last := len(f.scopes) - 1
	if last < 0 {
		return fmt.Errorf("fx_pop without a using")
	}
	f.ctx = f.scopes[last]
	f.scopes[last] = nil
	f.scopes = f.scopes[:last]
	return nil
}

// using is g with the quotes over it, each pair both ways, later ones over
// earlier. A rate from a currency to itself is the identity and changes
// nothing.
func (g *rateGraph) using(quotes []FxRate) *rateGraph {
	next := g.copied()
	for _, quote := range quotes {
		if !quote.identity() {
			next.setBoth(quote.base, quote.quote, quote.rate)
		}
	}
	return next
}

// setBoth sets base→quote as quoted and its inverse the other way.
func (g *rateGraph) setBoth(base, quote string, rate *big.Rat) {
	g.set(base, quote, rateEdge{rate: rate, quoted: true})
	g.set(quote, base, rateEdge{rate: new(big.Rat).Inv(rate)})
}

// copied is a copy of g's edges to change, with no conversions worked out.
func (g *rateGraph) copied() *rateGraph {
	next := &rateGraph{edges: make(map[string]map[string]rateEdge, len(g.edges)+2), factors: map[[2]string]conversion{}}
	for code, neighbours := range g.edges {
		copied := make(map[string]rateEdge, len(neighbours)+1)
		maps.Copy(copied, neighbours)
		next.edges[code] = copied
	}
	return next
}
