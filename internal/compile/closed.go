package compile

import (
	"github.com/nethinwei/funroute/internal/kit"
	"github.com/nethinwei/funroute/internal/machine"
	"github.com/nethinwei/funroute/internal/syntax"
)

// constexprMemo is, by node ID, whether every call in a node may run while
// compiling and it opens no using: constexprOnly, answered for a node from
// its children's answers and kept, so asking it of every node the compiler
// lays out walks each node once. It is found only for the nodes folding
// asks about — most read an argument and are never asked.
type constexprMemo struct {
	// answers is 0 for a node not yet asked, 1 for yes and 2 for no.
	answers []uint8
}

func (m *constexprMemo) of(expr syntax.Expr, inferred *inference, registry *machine.Registry) bool {
	id := expr.NodeID()
	if id < 0 {
		return constexprOnly(expr, inferred, registry)
	}
	if id < len(m.answers) && m.answers[id] != 0 {
		return m.answers[id] == 1
	}
	constexpr := ownConstexpr(expr, inferred, registry)
	syntax.EachChild(expr, func(child syntax.Expr) {
		constexpr = m.of(child, inferred, registry) && constexpr
	})
	m.answers = kit.Reach(m.answers, id)
	m.answers[id] = 2
	if constexpr {
		m.answers[id] = 1
	}
	return constexpr
}

// ownConstexpr is constexprOnly of expr itself, not its children.
func ownConstexpr(expr syntax.Expr, inferred *inference, registry *machine.Registry) bool {
	switch node := expr.(type) {
	case *syntax.UsingExpr:
		// A using is only there for the conversions in it, which read the run.
		return false
	case *syntax.CallExpr:
		function, found := registry.Resolve(inferred.Selections[node.ID])
		return found && function.IsConstexpr()
	}
	return true
}
