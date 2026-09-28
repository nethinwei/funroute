package syntax

import (
	"slices"
	"strings"
)

// What a position in a program can see, by the rule FreeVariables follows: a
// form's binds tags, walked by walkChildren (and, for every variable read,
// eachVariable). Nothing here restates it.

// ScopeAt lists the local names visible at offset in the source root was
// parsed from: loop variables, an accumulator, let bindings before offset.
// The program's arguments are the contract's, not the program's, so they are
// not in the list.
func ScopeAt(root Expr, offset int) []string {
	var bound scope
	for current := root; ; {
		next, inner := childAt(current, bound, offset)
		if next == nil {
			break
		}
		current, bound = next, inner
	}
	names := bound.names()
	slices.Sort(names)
	return slices.Compact(names)
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

// UpdatedRecordAt finds the record update a field name is being typed into.
// A language server puts a placeholder name at the cursor so the text parses;
// this is the innermost update around offset with a field whose name ends in
// that placeholder — it ends in it because whatever was typed of the name
// comes first. It answers where the base and the whole update are, and the
// fields already written besides that one.
func UpdatedRecordAt(root Expr, offset int, placeholder string) (base, update Span, written []string, ok bool) {
	var found *RecordUpdateExpr
	for expr := range Nodes(root) {
		if node, isUpdate := expr.(*RecordUpdateExpr); isUpdate && node.Extent().HoldsCursor(offset) && typedInto(node, placeholder) {
			found = node
		}
	}
	if found == nil {
		return Span{}, Span{}, nil, false
	}
	for _, field := range found.Fields {
		if !strings.HasSuffix(field.Name, placeholder) {
			written = append(written, field.Name)
		}
	}
	return found.Base.Extent(), found.Extent(), written, true
}

// SelectedListAt is where a selector is being written, sort_by(xs, .|): the
// span of the list its call reads, the call's first argument, and of the
// call, and the fields the selector names before the one being typed, whose
// name ends in placeholder. It reports false anywhere else.
func SelectedListAt(root Expr, offset int, placeholder string) (list, call Span, path []string, ok bool) {
	for expr := range Nodes(root) {
		node, isCall := expr.(*CallExpr)
		if !isCall || !node.Extent().HoldsCursor(offset) || len(node.Args) < 2 {
			continue
		}
		for _, arg := range node.Args[1:] {
			if selector, isSelector := arg.(*SelectorExpr); isSelector && strings.HasSuffix(selector.Path, placeholder) {
				list, call, path, ok = node.Args[0].Extent(), node.Extent(), strings.Split(selector.Path, ".")[:strings.Count(selector.Path, ".")], true
			}
		}
	}
	return list, call, path, ok
}

func typedInto(node *RecordUpdateExpr, placeholder string) bool {
	return slices.ContainsFunc(node.Fields, func(field RecordFieldExpr) bool {
		return strings.HasSuffix(field.Name, placeholder)
	})
}
