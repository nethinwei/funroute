package lang

import "fmt"

// validateForms rejects a program that uses a special form its registry does
// not enable. It runs on the AST, so source and ExprJSON go through the same
// check and a console cannot smuggle recur in as JSON.
func validateForms(expr Expr, registry *Registry) error {
	form, missing := missingForm(expr, registry)
	if missing {
		return fmt.Errorf("%s is not enabled in this registry", string(form))
	}
	return nil
}

func missingForm(expr Expr, registry *Registry) (Form, bool) {
	switch node := expr.(type) {
	case *CallExpr:
		if node.Name == "recur" && !registry.FormEnabled(RecurForm) {
			return RecurForm, true
		}
		return firstMissingForm(registry, node.Args...)
	case *SwitchExpr:
		if !registry.FormEnabled(SwitchForm) {
			return SwitchForm, true
		}
		return firstMissingForm(registry, switchChildren(node)...)
	case *ForExpr:
		if !registry.FormEnabled(ForForm) {
			return ForForm, true
		}
		return firstMissingForm(registry, node.Source, node.Where, node.Yield)
	case *ReduceExpr:
		if !registry.FormEnabled(ReduceForm) {
			return ReduceForm, true
		}
		return firstMissingForm(registry, node.Source, node.Init, node.Body)
	case *ArrayExpr:
		return firstMissingForm(registry, node.Items...)
	case *DictExpr:
		return firstMissingForm(registry, dictValues(node)...)
	default:
		return "", false
	}
}

func switchChildren(node *SwitchExpr) []Expr {
	out := []Expr{node.Value, node.Default}
	for _, item := range node.Cases {
		out = append(out, item.Match...)
		out = append(out, item.Result)
	}
	return out
}

func dictValues(node *DictExpr) []Expr {
	out := make([]Expr, len(node.Entries))
	for i, entry := range node.Entries {
		out[i] = entry.Value
	}
	return out
}

func firstMissingForm(registry *Registry, exprs ...Expr) (Form, bool) {
	for _, expr := range exprs {
		if expr == nil {
			continue
		}
		if form, missing := missingForm(expr, registry); missing {
			return form, true
		}
	}
	return "", false
}
