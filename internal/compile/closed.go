package compile

import (
	"math"
	"slices"

	"github.com/nethinwei/funroute/internal/machine"
	"github.com/nethinwei/funroute/internal/syntax"
)

// Whether a node is fixed at compile time is asked of every node the
// compiler lays out, so it is worked out for all of them in one walk rather
// than by walking each node's subtree again: that made compiling a deep
// loop body quadratic in its depth. What cannot be known before compiling —
// whether a let's binding folded — is left as the names to ask about.

// closure is what the walk found of the nodes, by ID.
type closure struct {
	// pinned says a node reads, from outside it, one of the program's
	// arguments or a name a loop or a reduce binds: never fixed at compile
	// time.
	pinned []bool
	// lets is the names a node reads, from outside it, that a let binds: it
	// is fixed exactly when each of those folded.
	lets [][]string
	// constexpr says every call in a node may run while compiling, and it
	// opens no using.
	constexpr []bool
	// known says the walk saw a node.
	known []bool
}

// letRead is a read of a name a let binds, and how deep the let is.
type letRead struct {
	name  string
	depth int
}

// closureWalk is the walk: the binder each read sees, and each node's depth.
type closureWalk struct {
	out      *closure
	binders  map[int]syntax.Expr
	depths   map[syntax.Expr]int
	inferred *inference
	registry *machine.Registry
}

// closureOf walks root once.
func closureOf(root syntax.Expr, inferred *inference, registry *machine.Registry) *closure {
	w := &closureWalk{out: &closure{}, binders: map[int]syntax.Expr{}, depths: map[syntax.Expr]int{}, inferred: inferred, registry: registry}
	syntax.EachRead(root, func(variable *syntax.VariableExpr, binder syntax.Expr) { w.binders[variable.ID] = binder })
	w.visit(root, 0)
	return w.out
}

// visit records expr, at depth, and answers what its subtree reads from
// outside it: the shallowest depth of an argument or a loop's name read (-1
// for an argument, and past every depth for none), the lets' names read,
// and whether every call in it may run while compiling.
func (w *closureWalk) visit(expr syntax.Expr, depth int) (int, []letRead, bool) {
	w.depths[expr] = depth
	pinnedAt, lets, constexpr := w.own(expr)
	syntax.EachChild(expr, func(child syntax.Expr) {
		childPinned, childLets, childConstexpr := w.visit(child, depth+1)
		pinnedAt, constexpr = min(pinnedAt, childPinned), constexpr && childConstexpr
		for _, read := range childLets {
			if read.depth < depth && !slices.Contains(lets, read) {
				lets = append(lets, read)
			}
		}
	})
	w.record(expr.NodeID(), pinnedAt < depth, lets, constexpr)
	return pinnedAt, lets, constexpr
}

// own is what expr itself reads and calls, before its children.
func (w *closureWalk) own(expr syntax.Expr) (int, []letRead, bool) {
	switch node := expr.(type) {
	case *syntax.VariableExpr:
		binder := w.binders[node.ID]
		switch binder.(type) {
		case nil:
			return -1, nil, true
		case *syntax.LetExpr:
			return math.MaxInt, []letRead{{name: node.Name, depth: w.depths[binder]}}, true
		}
		return w.depths[binder], nil, true
	case *syntax.UsingExpr:
		// A using is only there for the conversions in it, which read the run.
		return math.MaxInt, nil, false
	case *syntax.CallExpr:
		function, found := w.registry.Resolve(w.inferred.Selections[node.ID])
		return math.MaxInt, nil, found && function.IsConstexpr()
	}
	return math.MaxInt, nil, true
}

// record keeps what the walk found of node id.
func (w *closureWalk) record(id int, pinned bool, lets []letRead, constexpr bool) {
	c := w.out
	if id >= len(c.known) {
		grow := id + 1 - len(c.known)
		c.pinned, c.lets = append(c.pinned, make([]bool, grow)...), append(c.lets, make([][]string, grow)...)
		c.constexpr, c.known = append(c.constexpr, make([]bool, grow)...), append(c.known, make([]bool, grow)...)
	}
	c.pinned[id], c.constexpr[id], c.known[id] = pinned, constexpr, true
	for _, read := range lets {
		c.lets[id] = append(c.lets[id], read.name)
	}
}

// fixed reports whether node id is fixed at compile time given which names
// folded, and whether the walk knows it.
func (c *closure) fixed(id int, folded func(string) bool) (fixed, known bool) {
	if c == nil || id < 0 || id >= len(c.known) || !c.known[id] {
		return false, false
	}
	return !c.pinned[id] && !slices.ContainsFunc(c.lets[id], func(name string) bool { return !folded(name) }), true
}

// callsConstexpr reports whether every call in node id may run while
// compiling, and whether the walk knows it.
func (c *closure) callsConstexpr(id int) (constexpr, known bool) {
	if c == nil || id < 0 || id >= len(c.known) || !c.known[id] {
		return false, false
	}
	return c.constexpr[id], true
}
