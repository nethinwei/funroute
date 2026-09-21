package compile

import (
	"fmt"
	"funroute/lang/internal/machine"
	"funroute/lang/internal/syntax"
)

// validateForms rejects a program that uses a special form its registry does
// not enable. It runs on the AST, so source and ExprJSON go through the same
// check and a console cannot smuggle a form in as JSON.
//
// Which nodes are forms is declared on the nodes themselves (syntax.Form), and
// the walk is the generic one, so a new form needs nothing here.
func validateForms(expr syntax.Expr, registry *machine.Registry) error {
	if form, ok := syntax.FormOf(expr); ok && !registry.FormEnabled(form) {
		return fmt.Errorf("%s is not enabled in this registry", string(form))
	}
	for _, child := range syntax.Children(expr) {
		if err := validateForms(child, registry); err != nil {
			return err
		}
	}
	return nil
}
