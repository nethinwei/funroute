package machine

import "github.com/nethinwei/funroute/internal/money"

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

// WithPrefetched is options carrying a Batch's answers, for a test that plays
// the Batch.
func WithPrefetched(options RunOptions, prefetched map[int]Prefetched) RunOptions {
	options.prefetched = prefetched
	return options
}

// ArtifactWith is an unsealed artifact over parts, for the table tests of
// how an instruction is checked on load.
func ArtifactWith(parts ArtifactParts) *Artifact { return &Artifact{parts: parts} }

// ConstantValue reads a constant back, as Instantiate does.
func ConstantValue(c Constant) (Value, error) { return c.value() }

// StackDepth is how deep loading found the runtime's stack gets.
func StackDepth(r *Runtime) int { return r.depth }
