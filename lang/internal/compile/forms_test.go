package compile

import (
	"strings"
	"testing"

	"funroute/lang/internal/machine"
	"funroute/lang/internal/syntax"
)

func TestDisabledFormsAreRejectedWhenTheyArriveAsExprJSON(t *testing.T) {
	t.Parallel()
	expr, err := syntax.Parse(`[reduce(p in row, t = 0, add(t,p)) for row in rows]`)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := syntax.ExportExprJSON(expr)
	if err != nil {
		t.Fatal(err)
	}
	imported, err := syntax.ImportExprJSON(encoded)
	if err != nil {
		t.Fatal(err)
	}
	switchAndFor := machine.CoreRegistry()
	if err := switchAndFor.EnableForm(machine.SwitchForm, machine.ForForm); err != nil {
		t.Fatal(err)
	}
	if _, err := CompileAST(imported, switchAndFor, CompileOptions{}); err == nil ||
		!strings.Contains(err.Error(), "reduce is not enabled") {
		t.Fatalf("CompileAST bypassed the registry forms: %v, want reduce is not enabled", err)
	}
	if _, err := CompileAST(imported, machine.CoreRegistry(), CompileOptions{}); err == nil ||
		!strings.Contains(err.Error(), "for is not enabled") {
		t.Fatalf("CompileAST accepted a disabled form: %v, want for is not enabled", err)
	}
}
