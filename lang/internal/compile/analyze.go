package compile

import (
	"funroute/lang/internal/machine"
	"funroute/lang/internal/syntax"
)

// Analyze is what the compiler knows about a program's text, for a tool that
// asks about a position in it: each node's span, the type inference settled
// on, and the signature a call resolved to. It takes the path CompileExpr
// takes and stops before sealing an artifact. On an error it still returns
// what was learnt before it — the facts of a program that type-checks but
// fails to fold, for instance.
func Analyze(source string, registry *machine.Registry, options CompileOptions) (*Analysis, error) {
	expr, err := syntax.Parse(source)
	if err != nil {
		return nil, compileError(err)
	}
	inferred, _, err := build(expr, registry, options)
	if inferred == nil {
		return nil, err
	}
	analysis := &Analysis{Params: inferred.Params, Result: inferred.Result}
	locals := syntax.LocalReferences(expr)
	var visit func(syntax.Expr)
	visit = func(node syntax.Expr) {
		fact := NodeFact{Span: node.Extent(), Signature: inferred.Selections[node.NodeID()]}
		if typ, ok := inferred.NodeTypes[node.NodeID()]; ok {
			fact.Type = &typ
		}
		if local, isVariable := locals[node.NodeID()]; isVariable {
			fact.Reference = referenceKind(local)
		}
		analysis.Nodes = append(analysis.Nodes, fact)
		for _, child := range syntax.Children(node) {
			visit(child)
		}
	}
	visit(expr)
	return analysis, err
}

// Analysis is a program's facts. Nodes lists every node, parents before their
// children.
type Analysis struct {
	Params []machine.Parameter
	Result machine.Type
	Nodes  []NodeFact
}

// NodeFact is one node: the source it was read from, its type, the full
// signature a call resolved to, and, for a variable, whether it names a local
// or one of the program's arguments.
type NodeFact struct {
	Span      syntax.Span
	Type      *machine.Type
	Signature string
	Reference string
}

func referenceKind(local bool) string {
	if local {
		return "local"
	}
	return "argument"
}

// At is the innermost node whose source holds offset.
func (a *Analysis) At(offset int) (NodeFact, bool) {
	var found NodeFact
	ok := false
	for _, fact := range a.Nodes {
		if fact.Span.HoldsCharacter(offset) && (!ok || fact.Span.End-fact.Span.Start <= found.Span.End-found.Span.Start) {
			found, ok = fact, true
		}
	}
	return found, ok
}
