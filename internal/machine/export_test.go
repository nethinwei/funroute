package machine

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/nethinwei/funroute/internal/money"
)

// ValidateInstruction is validateInstruction, for vm_test.go's table test of
// how a single instruction is checked on load.
var ValidateInstruction = validateInstruction

// NewMoney is money without a currency table, for the external tests that
// build values: they test the machine, not the boundary.
var NewMoney = money.Make

// NewRatio is num/den, which the test knows fits.
func NewRatio(num, den int64) money.Ratio {
	r, err := money.RatioOf(num, den)
	if err != nil {
		panic(err)
	}
	return r
}

// NewCurrency is a currency value's Go form without a table.
func NewCurrency(code string) money.Currency { return money.CurrencyOf(code) }

// WithPrefetched is options carrying a Batch's answers for the runtime's
// calls, by the call instructions' program counters, for a test that plays
// the Batch.
func WithPrefetched(r *Runtime, options RunOptions, byPC map[int]Prefetched) RunOptions {
	options.prefetched = make([]Prefetched, len(r.reg.calls))
	for pc, answer := range byPC {
		answer.ready = true
		options.prefetched[r.callAt(pc)] = answer
	}
	return options
}

// ArtifactWith is an unsealed artifact over parts, for the table tests of
// how an instruction is checked on load.
func ArtifactWith(parts ArtifactParts) *Artifact { return &Artifact{parts: parts} }

// ConstantValue reads a constant back, as Instantiate does.
func ConstantValue(c Constant) (Value, error) { return c.value() }

// StackDepth is how deep loading found the runtime's stack gets.
func StackDepth(r *Runtime) int { return r.depth }

// RegisterForm lists the runtime's register operations, one a line, for a
// failure message.
func RegisterForm(r *Runtime) string {
	var out strings.Builder
	for pc, in := range r.reg.code {
		fmt.Fprintf(&out, "%d\t%s\t%d %d %d\t(from %d)\n", pc, in.op, in.a, in.b, in.c, r.reg.origins[pc])
	}
	return out.String()
}

// NativeBacked reports a value every container of which holds its own
// backing, not a pointer into a frame's slot: what a host may ever see.
func NativeBacked(v Value) bool {
	switch box := v.box.(type) {
	case *[]bool, *[]int64, *[]float64, *[]string:
		return false
	case *recordValue:
		return !slices.ContainsFunc(box.fields, func(field Value) bool { return !NativeBacked(field) })
	case *nestedArray:
		return !slices.ContainsFunc(box.items, func(item Value) bool { return !NativeBacked(item) })
	}
	return true
}

// FlowFacts is what the value-flow analysis found of a runtime's program,
// each producer written as its opcode at its pc: "loop_init@1".
type FlowFacts struct {
	Arena     []string
	Dest      map[string]int
	Answer    string
	FieldOnly []bool
}

// Flow is the value-flow analysis of the runtime's program.
func Flow(r *Runtime) FlowFacts {
	found := flowOf(&r.artifact.parts, r.functions)
	name := func(pc int) string { return fmt.Sprintf("%s@%d", r.artifact.parts.Instructions[pc].Op, pc) }
	facts := FlowFacts{Dest: map[string]int{}, FieldOnly: found.fieldOnly}
	for _, pc := range slices.Sorted(maps.Keys(found.arena)) {
		facts.Arena = append(facts.Arena, name(pc))
	}
	for pc, dest := range found.dest {
		facts.Dest[name(pc)] = dest
	}
	if found.answer >= 0 {
		facts.Answer = name(found.answer)
	}
	return facts
}

// WithoutVectors has the runtime run every loop body as it is, for a test
// that holds the vector to the body's answer. It must come before the first
// run.
func WithoutVectors(r *Runtime) {
	for i := range r.reg.loops {
		r.reg.loops[i].vec = nil
	}
}

// Vectors is how many of the runtime's loops the vector runs.
func Vectors(r *Runtime) int {
	n := 0
	for _, loop := range r.reg.loops {
		if loop.vec != nil {
			n++
		}
	}
	return n
}
