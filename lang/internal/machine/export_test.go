package machine

// ValidateInstruction is validateInstruction, for vm_test.go's table test of
// how a single instruction is checked on load.
var ValidateInstruction = validateInstruction

// The machine's own money constructors, for the external tests that build
// values without a currency table: they test the machine, not the boundary.
var (
	NewMoney = newMoney
	NewRate  = newRate
)

// RateScaled is a rate's fixed-point units.
func RateScaled(r Rate) int64 { return r.scaled }

// NewCurrency is a currency value's Go form without a table.
func NewCurrency(code string) Currency { return Currency{code: code} }

// WithPrefetched is options carrying a Batch's answers, for a test that plays
// the Batch.
func WithPrefetched(options RunOptions, prefetched map[int]Prefetched) RunOptions {
	options.prefetched = prefetched
	return options
}

// ArtifactWith is an unsealed artifact over parts, for the table tests of
// how an instruction is checked on load.
func ArtifactWith(parts ArtifactParts) *Artifact { return &Artifact{parts: parts} }

// ScopeHolds reports whether pushed are the constant quotes a using's table
// was built from at load.
func ScopeHolds(quotes, pushed []Value) bool { return constantScope{quotes: quotes}.holds(pushed) }

// MulDivRound is the one fixed-point product, for the tests outside that
// check a host function against it.
var MulDivRound = mulDivRound
