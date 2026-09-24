package machine

import (
	"errors"
	"fmt"

	"github.com/nethinwei/funroute/internal/money"
)

// The errors a host tells apart. A caller uses errors.Is and never parses a
// message: malformed source, a contract violation, a rule that reached its
// fuel limit, a request that ran out of time, an extension that failed, two
// currencies meeting where one is required, arithmetic with no answer and
// data with no answer are different things to report or monitor. fallback
// catches only data not at hand — the deadline, the extension, the missing
// rate — never a program's own limits, authoring errors, currency
// mismatches, or an arithmetic or a data failure.
var (
	ErrCompile   = errors.New("expression compilation failed")
	ErrContract  = errors.New("runtime contract failed")
	ErrFuel      = errors.New("execution fuel exhausted")
	ErrDeadline  = errors.New("deadline exceeded")
	ErrExtension = errors.New("extension failed")
	// ErrCurrency is money meeting money of another currency where one is
	// required, or a currency the registry did not declare. When the
	// arguments disagree with each other it is also ErrContract. It is
	// money's own error.
	ErrCurrency = money.ErrCurrency
	// ErrArithmetic is arithmetic with no answer: an overflow, a division by
	// zero, a float that is not finite, an exchange rate that is not
	// positive. It is the rule's or the data's, wherever it happens — in the
	// kernel, in a host function, in a Go method — and when an argument
	// brings it, it is also ErrContract. Money's arithmetic reports it too.
	ErrArithmetic = money.ErrArithmetic
	// ErrNoFxRate is a conversion whose using has no quote of the pair:
	// data not at hand, which fallback takes.
	ErrNoFxRate = money.ErrNoFxRate
	// ErrDomain is data an operation has no answer for: an index past the
	// end, a key the dictionary does not have, the first of an empty array,
	// two arrays that were to line up and do not, a key a comprehension
	// makes twice. Like arithmetic with no answer it is the rule's or the
	// data's, wherever it happens, and fallback does not take it.
	ErrDomain = money.ErrDomain
)

// errorClass is one class an error can have: its sentinel, the name a
// language service reports it by, and whether fallback moves past it.
type errorClass struct {
	err      error
	name     string
	fallback bool
}

// errorClasses is every class, the most specific first. An error is its most
// specific class: that names it, and decides whether fallback takes it —
// data not yet at hand is taken, the rule's or the data's own error never.
var errorClasses = []errorClass{
	{ErrUnavailable, "unavailable", true},
	{ErrNoFxRate, "nofxrate", true},
	{ErrCurrency, "currency", false},
	{ErrArithmetic, "arithmetic", false},
	{ErrDomain, "domain", false},
	{ErrContract, "contract", false},
	{ErrCompile, "compile", false},
	{ErrFuel, "fuel", false},
	{ErrDeadline, "deadline", true},
	{ErrExtension, "extension", true},
}

// classOf is err's most specific class, and false for an error with none.
func classOf(err error) (errorClass, bool) {
	for _, class := range errorClasses {
		if errors.Is(err, class.err) {
			return class, true
		}
	}
	return errorClass{}, false
}

// ClassName is the name of err's most specific class, such as "currency",
// and "" for an error that has none.
func ClassName(err error) string {
	class, _ := classOf(err)
	return class.name
}

// errDivisionByZero is every division by zero the machine refuses.
var errDivisionByZero = fmt.Errorf("%w: division by zero", ErrArithmetic)

// overflowIn is an integer result past int64 in the named operation.
func overflowIn(operation string) error {
	return fmt.Errorf("%w: integer overflow in %s", ErrArithmetic, operation)
}
