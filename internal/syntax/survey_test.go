package syntax

import (
	"slices"
	"testing"
)

// What the one walk says of each node is what walking the node's own
// subtree says: with no let's name taken as fixed, a node is fixed exactly
// when it has no free variables; with every one taken as fixed, exactly when
// each read from outside it sees a let.
func TestTheSurveyAgreesWithEachSubtree(t *testing.T) {
	t.Parallel()
	for _, source := range []string{
		`[x + 1 + n for x in xs]`,
		`let(a = 2, b = a * n, [a + b + x for x in xs if x > a])`,
		`let(a = n, [let(a = x, a + 1) for x in xs])`,
		`reduce(x in xs, acc = 0, acc + x * len(xs))`,
		`let(k = 3, t = k * 4, switch(n, case 1 => k, else => t + [y * k for y in ys][0]))`,
		`[[x + y for y in range(3)] for x in xs]`,
	} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			expr := mustParse(t, source)
			survey := SurveyOf(expr, func(Expr) {})
			binders := map[*VariableExpr]Expr{}
			EachRead(expr, func(variable *VariableExpr, binder Expr) { binders[variable] = binder })
			for node := range Nodes(expr) {
				surveysAsItsSubtree(t, survey, node, binders)
			}
		})
	}
}

func surveysAsItsSubtree(t *testing.T, survey *Survey, node Expr, binders map[*VariableExpr]Expr) {
	t.Helper()
	inside := slices.Collect(Nodes(node))
	onlyLets := true
	for _, part := range inside {
		variable, ok := part.(*VariableExpr)
		if binder := binders[variable]; ok && !slices.Contains(inside, binder) {
			_, let := binder.(*LetExpr)
			onlyLets = onlyLets && let
		}
	}
	none, _ := survey.Fixed(node.NodeID(), func(string) bool { return false })
	every, known := survey.Fixed(node.NodeID(), func(string) bool { return true })
	if !known || none != (len(FreeVariables(node)) == 0) || every != onlyLets {
		t.Errorf("node %d: fixed %v with no let fixed, %v with every one (known %v); free %v, only lets %v",
			node.NodeID(), none, every, known, FreeVariables(node), onlyLets)
	}
}
