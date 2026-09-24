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

// A fallback's handler starts from the state at begin_fallback, which is
// what a failure cuts the run back to. A candidate that changes what was
// there before it failed would hand the handler something else under the
// same types, so the candidate leaves it alone: each of these loaded before
// and ran with a value of the wrong type.
func TestLoadingHoldsAFallbacksCandidateToWhatItBeganWith(t *testing.T) {
	t.Parallel()
	one := machine.Constant{Type: machine.IntType, Value: json.RawMessage(`1`)}
	word := machine.Constant{Type: machine.StringType, Value: json.RawMessage(`"s"`)}
	code := func(ops ...machine.Instruction) []machine.Instruction { return ops }
	i := func(op machine.OpCode, a int) machine.Instruction { return machine.Instruction{Op: op, A: a} }
	for _, test := range []struct {
		name string
		code []machine.Instruction
		want string
	}{
		{"it takes a value from under it", code(
			i(machine.OpConstant, 0), i(machine.OpBeginFallback, 11), i(machine.OpStoreLocal, 0),
			i(machine.OpConstant, 1), i(machine.OpLoadArg, 0), i(machine.OpLoadArg, 0), machine.Instruction{Op: machine.OpEqual},
			i(machine.OpStoreLocal, 1), i(machine.OpStoreLocal, 2), i(machine.OpLoadLocal, 0), i(machine.OpEndFallback, 0),
		), "takes a value from under it"},
		{"it rebinds a local the handler reads", code(
			i(machine.OpLoadArg, 0), i(machine.OpStoreLocal, 0), i(machine.OpBeginFallback, 8),
			i(machine.OpConstant, 1), i(machine.OpStoreLocal, 0), i(machine.OpLoadArg, 0), i(machine.OpEndFallback, 0),
			i(machine.OpJump, 9), i(machine.OpLoadLocal, 0),
		), "rebinds local 0"},
		{"it closes a using it is inside", code(
			i(machine.OpLoadArg, 1), machine.Instruction{Op: machine.OpFxPush, B: 1}, i(machine.OpBeginFallback, 7),
			i(machine.OpFxPop, 0), i(machine.OpLoadArg, 0), i(machine.OpEndFallback, 0), i(machine.OpJump, 9),
			i(machine.OpLoadArg, 0), i(machine.OpFxPop, 0),
		), "fx_pop without a using"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := forgedLoad(t, "a", "a: int; r: fxrate", func(p *machine.ArtifactParts) {
				p.Instructions, p.Constants, p.Locals = test.code, []machine.Constant{one, word}, 3
			})
			if err == nil || !strings.Contains(err.Error(), "invalid bytecode") || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Instantiate of a candidate where %s = %v, want the bytecode refused saying %q", test.name, err, test.want)
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
