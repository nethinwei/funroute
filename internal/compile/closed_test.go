package compile

import (
	"testing"

	"github.com/nethinwei/funroute/internal/machine"
	"github.com/nethinwei/funroute/internal/syntax"
)

// Each node's answer, found from its children's and kept, is what
// constexprOnly finds walking the node's own subtree.
func TestTheConstexprMemoAgreesWithEachSubtree(t *testing.T) {
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
		`reduce(x in xs, acc = 0, acc + x * len(xs))`,
		`switch(n, case 1 => sum([x for x in xs]), else => let(k = 3, k + [y * k for y in range(4)][0]))`,
	} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			memoAgrees(t, registry, options, source)
		})
	}
}

func memoAgrees(t *testing.T, registry *machine.Registry, options CompileOptions, source string) {
	t.Helper()
	expr, err := syntax.Parse(source)
	if err != nil {
		t.Fatal(err)
	}
	inferred, _, err := build(expr, registry, options)
	if err != nil {
		t.Fatal(err)
	}
	memo := &constexprMemo{}
	for node := range syntax.Nodes(expr) {
		if got, want := memo.of(node, inferred, registry), constexprOnly(node, inferred, registry); got != want {
			t.Errorf("node %d of %s: memo says %v, constexprOnly %v", node.NodeID(), source, got, want)
		}
	}
}
