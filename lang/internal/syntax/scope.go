package syntax

import "sort"

// What a position in a program can see, by the rule FreeVariables follows: a
// form's binds tags, walked by walkChildren (and, for every variable read,
// eachVariable). Nothing here restates it.

// ScopeAt lists the local names visible at offset in the source root was
// parsed from: loop variables, an accumulator, let bindings before offset.
// The program's arguments are the contract's, not the program's, so they are
// not in the list.
func ScopeAt(root Expr, offset int) []string {
	bound := scope{}
	for current := root; ; {
		next, inner := childAt(current, bound, offset)
		if next == nil {
			break
		}
		current, bound = next, inner
	}
	names := make([]string, 0, len(bound))
	for name := range bound {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// childAt is the child of expr whose source holds offset, with the names
// bound there; nil when no child does.
func childAt(expr Expr, bound scope, offset int) (Expr, scope) {
	var found Expr
	var inner scope
	walkChildren(expr, bound, func(child Expr, names scope) {
		if found == nil && child.Extent().HoldsCursor(offset) {
			found, inner = child, names
		}
	})
	return found, inner
}

// LocalReferences reports, for each variable reference by node ID, whether a
// form binds the name there. The others are the program's arguments.
func LocalReferences(root Expr) map[int]bool {
	out := map[int]bool{}
	eachVariable(root, func(variable *VariableExpr, local bool) { out[variable.ID] = local })
	return out
}
