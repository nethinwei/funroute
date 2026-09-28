package syntax

import (
	"slices"

	"github.com/nethinwei/funroute/internal/kit"
)

// Survey is what one walk of a program by the scope rule finds: where each
// free variable is first read, and, by node, what the node reads from
// outside it. A compiler asks the second of every node it lays out — is its
// value fixed before the program runs? — so it is found for all of them in
// the one walk rather than by walking each node's subtree again.
type Survey struct {
	// First is where each free variable is first read, in that order, as
	// FirstReads says.
	First []*VariableExpr
	// Nodes is how many nodes there are.
	Nodes int
	// pinned is, by node ID, a node that reads from outside it a free
	// variable or a name a loop or a reduce binds; seen, a node the walk
	// visited.
	pinned, seen []bool
	// lets is, by node ID, the names a node reads from outside it that a
	// let binds; nil until one does.
	lets [][]letRead
	// forms is the forms that bind names around the node being walked,
	// innermost last, and how deep each is.
	forms []boundForm
}

// letRead is a read of a name a let binds, and how deep that let is.
type letRead struct {
	name  string
	depth int
}

type boundForm struct {
	form  Expr
	depth int
}

// notPinned is past every depth: what reads nothing that pins a node.
const notPinned = int(^uint(0) >> 1)

// SurveyOf walks root once by the scope rule, visiting every node parents
// first, as Nodes does.
func SurveyOf(root Expr, visit func(Expr)) *Survey {
	s := &Survey{}
	s.walk(root, nil, 0, visit)
	s.forms = nil
	return s
}

// walk records expr, depth deep, and answers what its subtree reads from
// outside it: the shallowest depth of a form whose name it reads — -1 for a
// free variable — among loops and reduces, and its reads of the lets' names.
func (s *Survey) walk(expr Expr, bound scope, depth int, visit func(Expr)) (int, []letRead) {
	visit(expr)
	s.Nodes++
	if variable, ok := expr.(*VariableExpr); ok {
		pinnedAt, lets := s.read(variable, bound)
		s.record(variable.ID, pinnedAt < depth, lets)
		return pinnedAt, lets
	}
	binds := planOf(expr) != nil && planOf(expr).binds
	if binds {
		s.forms = append(s.forms, boundForm{form: expr, depth: depth})
	}
	pinnedAt, lets := notPinned, []letRead(nil)
	walkChildren(expr, bound, func(child Expr, inner scope) {
		childPinned, childLets := s.walk(child, inner, depth+1, visit)
		pinnedAt, lets = min(pinnedAt, childPinned), outsideOf(lets, childLets, depth)
	})
	if binds {
		s.forms = s.forms[:len(s.forms)-1]
	}
	s.record(expr.NodeID(), pinnedAt < depth, lets)
	return pinnedAt, lets
}

// read is what one variable read pins, or the let's name it reads.
func (s *Survey) read(variable *VariableExpr, bound scope) (int, []letRead) {
	binder, local := bound.binder(variable.Name)
	if !local {
		if !slices.ContainsFunc(s.First, func(read *VariableExpr) bool { return read.Name == variable.Name }) {
			s.First = append(s.First, variable)
		}
		return -1, nil
	}
	depth := s.depthOf(binder)
	if _, let := binder.(*LetExpr); let {
		return notPinned, []letRead{{name: variable.Name, depth: depth}}
	}
	return depth, nil
}

// depthOf is how deep a form around the node being walked is.
func (s *Survey) depthOf(form Expr) int {
	for _, bound := range slices.Backward(s.forms) {
		if bound.form == form {
			return bound.depth
		}
	}
	return -1
}

// outsideOf is lets with the reads of more that are from outside a node
// depth deep, sharing more when lets has none: a let's reads pass up the
// tree unchanged until the let itself.
func outsideOf(lets, more []letRead, depth int) []letRead {
	for _, read := range more {
		if read.depth >= depth || slices.Contains(lets, read) {
			continue
		}
		if len(lets) == 0 && len(more) == 1 {
			return more
		}
		lets = append(slices.Clip(lets), read)
	}
	return lets
}

// record keeps what the walk found of node id.
func (s *Survey) record(id int, pinned bool, lets []letRead) {
	s.pinned, s.seen = kit.Reach(s.pinned, id), kit.Reach(s.seen, id)
	s.pinned[id], s.seen[id] = pinned, true
	if len(lets) > 0 {
		s.lets = kit.Reach(s.lets, id)
		s.lets[id] = lets
	}
}

// Fixed reports whether node id's value is fixed before the program runs:
// it reads from outside it no free variable and no name a loop or a reduce
// binds, and constant says each let's name it reads is. known is false for a
// node the walk did not see.
func (s *Survey) Fixed(id int, constant func(name string) bool) (fixed, known bool) {
	if s == nil || id < 0 || id >= len(s.seen) || !s.seen[id] {
		return false, false
	}
	if s.pinned[id] {
		return false, true
	}
	if id < len(s.lets) {
		for _, read := range s.lets[id] {
			if !constant(read.name) {
				return false, true
			}
		}
	}
	return true, true
}
