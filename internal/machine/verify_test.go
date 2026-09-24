package machine_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/nethinwei/funroute/internal/machine"
)

// op finds the first instruction with the opcode, for a forgery to change.
func op(t *testing.T, p *machine.ArtifactParts, code machine.OpCode) *machine.Instruction {
	t.Helper()
	for i := range p.Instructions {
		if p.Instructions[i].Op == code {
			return &p.Instructions[i]
		}
	}
	t.Fatalf("no %s in %v", code, p.Instructions)
	return nil
}

// Loading types every path through the bytecode, so an artifact whose
// instructions do not type — sealed again, with a digest that proves nothing
// of who sealed it — is refused before it runs.
func TestLoadingRefusesBytecodeThatDoesNotType(t *testing.T) {
	t.Parallel()
	text := machine.StringType
	for _, test := range []struct {
		name, source, contract string
		forge                  func(*testing.T, *machine.ArtifactParts)
	}{
		{"a call handed the wrong type", "a + 1", "a: int", func(_ *testing.T, p *machine.ArtifactParts) {
			p.Constants[0] = machine.Constant{Type: text, Value: json.RawMessage(`"x"`)}
		}},
		{"an item of another type", "[a, 1]", "a: int", func(_ *testing.T, p *machine.ArtifactParts) {
			p.Constants[0] = machine.Constant{Type: text, Value: json.RawMessage(`"x"`)}
		}},
		{"a result the contract does not declare", "a + 1", "a: int", func(_ *testing.T, p *machine.ArtifactParts) { p.Result = text }},
		{"a call that says it returns something else", "a + 1", "a: int", func(t *testing.T, p *machine.ArtifactParts) {
			op(t, p, machine.OpCall).Type = &text
		}},
		{"a local read before it is bound", "let(x = a + 1, x * x)", "a: int", func(t *testing.T, p *machine.ArtifactParts) {
			op(t, p, machine.OpStoreLocal).Op = machine.OpEqual
		}},
		{"a loop collecting another type", "[x + 1 for x in xs]", "xs: array<int>", func(t *testing.T, p *machine.ArtifactParts) {
			op(t, p, machine.OpLoopCollect).Type = &text
		}},
		{"a jump to a different stack", "if(f, a, a + 1)", "f: bool; a: int", func(t *testing.T, p *machine.ArtifactParts) {
			op(t, p, machine.OpJump).A = 0
		}},
		{"a value the stack does not hold", "a + 1", "a: int", func(t *testing.T, p *machine.ArtifactParts) {
			p.Instructions = p.Instructions[1:]
		}},
		{"a field of another type", "order.amount + 1", "order: record{amount: int}", func(t *testing.T, p *machine.ArtifactParts) {
			op(t, p, machine.OpField).Type = &text
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := forgedLoad(t, test.source, test.contract, func(p *machine.ArtifactParts) { test.forge(t, p) })
			if err == nil || !strings.Contains(err.Error(), "invalid bytecode") {
				t.Fatalf("Instantiate of %s with %s = %v, want the bytecode refused", test.source, test.name, err)
			}
		})
	}
}

// The walk that types the bytecode also finds how deep the stack gets, which
// is what a frame reserves: an honest artifact loads and runs within it.
func TestLoadingFindsTheStacksDepth(t *testing.T) {
	t.Parallel()
	for source, want := range map[string]int{"a": 1, "[a, a + 1, a * 2]": 4, "if(a > 1, [a, a], [])": 2} {
		if err := forgedLoad(t, source, "a: int", func(*machine.ArtifactParts) {}); err != nil {
			t.Fatalf("Instantiate(%s) = %v", source, err)
		}
		if got := machine.StackDepth(loaded(t, source)); got != want {
			t.Errorf("the stack of %s gets %d deep, want %d", source, got, want)
		}
	}
}

// loaded compiles source with an int argument a and loads it.
func loaded(t *testing.T, source string) *machine.Runtime {
	t.Helper()
	registry := fxRegistry(t)
	artifact, err := compileMoney(t, registry, source, "a: int", "")
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := machine.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	return runtime
}
