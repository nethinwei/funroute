package machine

import (
	"fmt"
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
