package machine_test

import (
	"testing"

	"github.com/nethinwei/funroute/internal/compile"
	"github.com/nethinwei/funroute/internal/machine"
)

// Every function of the library is the kernel's: a registry with nothing
// but the kernel has every one of them, and each is a kernel function.
func TestTheLibraryIsTheKernels(t *testing.T) {
	t.Parallel()
	registry := machine.CoreRegistry()
	for _, name := range machine.LibraryNames() {
		overloads := registry.Overloads(name)
		if len(overloads) == 0 {
			t.Errorf("the kernel has no %s", name)
		}
		for _, function := range overloads {
			if !function.IsBuiltin() {
				t.Errorf("%s is not a kernel function", function.Key())
			}
		}
	}
}

// libRun runs source on the kernel and every form, and is its answer's Go
// value.
func libRun(t *testing.T, source string, args map[string]any, specs ...compile.ArgSpec) (any, error) {
	t.Helper()
	registry := machine.CoreRegistry()
	if err := registry.EnableForm(machine.SwitchForm, machine.ForForm, machine.ReduceForm); err != nil {
		t.Fatal(err)
	}
	artifact, err := compile.CompileExpr(source, registry, compile.CompileOptions{Args: specs})
	if err != nil {
		return nil, err
	}
	runtime, err := machine.Instantiate(artifact, registry)
	if err != nil {
		return nil, err
	}
	value, err := runtime.Run(t.Context(), args)
	if err != nil {
		return nil, err
	}
	return value.Any(), nil
}
