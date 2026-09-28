package compile

import (
	"slices"
	"testing"

	"github.com/nethinwei/funroute/internal/machine"
	"github.com/nethinwei/funroute/internal/syntax"
)

// The one walk says of every node what walking the node's own subtree says:
// it reads something from outside exactly when it has free variables, the
// lets' names among them, and every call in it may be folded exactly when
// constexprOnly finds so.
func TestTheClosureAgreesWithEachSubtree(t *testing.T) {
	t.Parallel()
	registry := machine.CoreRegistry()
	if err := registry.EnableForm(machine.ForForm, machine.ReduceForm, machine.SwitchForm); err != nil {
		t.Fatal(err)
	}
	ints := machine.ArrayOf(machine.IntType)
	options := CompileOptions{Args: []ArgSpec{{Name: "xs", Type: ints}, {Name: "n", Type: machine.IntType}}}
	for _, source := range []string{
		`[x + 1 + n for x in xs]`,
		`let(a = 2, b = a * n, [a + b + x for x in xs if x > a])`,
		`let(a = n, [let(a = x, a + 1) for x in xs])`,
		`reduce(x in xs, acc = 0, acc + x * len(xs))`,
		`switch(n, case 1 => sum([x for x in xs]), else => let(k = 3, k + [y * k for y in range(4)][0]))`,
		`[[x + y for y in range(3)] for x in xs]`,
	} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			expr, err := syntax.Parse(source)
			if err != nil {
				t.Fatal(err)
			}
			inferred, compiler, err := build(expr, registry, options)
			if err != nil {
				t.Fatal(err)
			}
			for node := range syntax.Nodes(expr) {
				holdsAsItsSubtree(t, compiler.closure, node, inferred, registry)
			}
		})
	}
}

func holdsAsItsSubtree(t *testing.T, c *closure, node syntax.Expr, inferred *inference, registry *machine.Registry) {
	t.Helper()
	id, free := node.NodeID(), syntax.FreeVariables(node)
	if constexpr, known := c.callsConstexpr(id); !known || constexpr != constexprOnly(node, inferred, registry) {
		t.Errorf("node %d: callsConstexpr = %v, %v; constexprOnly = %v", id, constexpr, known, !constexpr)
	}
	reads := c.pinned[id] || len(c.lets[id]) > 0
	if reads != (len(free) > 0) {
		t.Errorf("node %d: pinned %v, lets %v; free variables %v", id, c.pinned[id], c.lets[id], free)
	}
	for _, name := range c.lets[id] {
		if !slices.Contains(free, name) {
			t.Errorf("node %d: let name %q is not free in it (%v)", id, name, free)
		}
	}
}
