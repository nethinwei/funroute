package machine

import (
	"context"
	"fmt"
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

// RunPrefetched is RunValues with a Batch's answers for the runtime's calls,
// by the call instructions' program counters, for a test that plays the
// Batch.
func RunPrefetched(ctx context.Context, r *Runtime, args []Value, byPC map[int]Prefetched) (Value, error) {
	prefetched := make([]Prefetched, len(r.reg.calls))
	for pc, answer := range byPC {
		answer.ready = true
		prefetched[r.callAt(pc)] = answer
	}
	return r.runValues(ctx, args, prefetched)
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
	ViewOnly  []bool
	// ItemsInPlace is how many loops load their items into the frame's own
	// record.
	ItemsInPlace int
}

// Flow is the value-flow analysis of the runtime's program.
func Flow(r *Runtime) FlowFacts {
	found := flowOf(&r.artifact.parts, r.functions)
	name := func(pc int) string { return fmt.Sprintf("%s@%d", r.artifact.parts.Instructions[pc].Op, pc) }
	facts := FlowFacts{Dest: map[string]int{}, FieldOnly: found.fieldOnly, ViewOnly: found.viewOnly}
	for pc, at := range found.at {
		if at.arena {
			facts.Arena = append(facts.Arena, name(pc))
		}
		if at.builds {
			facts.Dest[name(pc)] = at.dest
		}
		if at.itemsInPlace {
			facts.ItemsInPlace++
		}
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

// IdleFrameHoldsPointers reports whether the frame r keeps between runs
// still points at anything from the last run. The pool gives back the frame
// the goroutine's last run put there — unless a collection emptied it, and
// then there is nothing left to point at anything.
func IdleFrameHoldsPointers(r *Runtime) bool {
	f, ok := r.frames.Get().(*frame)
	if !ok {
		return false
	}
	defer r.frames.Put(f)
	for _, v := range f.regs[f.argBase:] {
		if v.s != "" || v.box != nil {
			return true
		}
	}
	for i := range f.items {
		if slices.ContainsFunc(f.items[i].fields, func(v Value) bool { return v.s != "" || v.box != nil }) {
			return true
		}
	}
	for i := range f.records {
		if slices.ContainsFunc(f.records[i].fields, func(v Value) bool { return v.s != "" || v.box != nil }) {
			return true
		}
	}
	if slices.ContainsFunc(f.views, func(slot arenaSlot) bool {
		return slot.bools != nil || slot.ints != nil || slot.floats != nil || slot.strings != nil
	}) {
		return true
	}
	return slices.ContainsFunc(f.recordViews, func(view recordsView) bool { return view.data != nil || view.plan != nil })
}

// Promotions is how many fields of record arguments r's program reads out of
// registers of their own (lower_promote.go).
func Promotions(r *Runtime) int { return len(r.reg.promotions) }

// Straight reports whether p runs the shorter way (host_straight.go).
func Straight[In, Out any](p *Program[In, Out]) bool { return p.straight }

// Identical reports two values that are one value, NaN included
// (identical).
var Identical = identical

// LibraryNames are the names of the library's functions (lib.go), each once.
func LibraryNames() []string {
	var names []string
	for _, spec := range librarySpecs() {
		if !slices.Contains(names, spec.Name) {
			names = append(names, spec.Name)
		}
	}
	return names
}

// ConstantBothWays is a constant read the quick way, when it is a scalar
// the quick way reads, and the decoder's way.
func ConstantBothWays(c Constant) (quick Value, ok bool, decoded Value, err error) {
	quick, ok = c.scalar()
	decoded, err = c.decoded()
	return quick, ok, decoded, err
}
