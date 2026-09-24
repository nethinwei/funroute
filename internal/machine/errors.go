package machine

import (
	"errors"
	"fmt"

	"github.com/nethinwei/funroute/internal/money"
)

// The errors a host tells apart. A caller uses errors.Is and never parses a
// message: malformed source, a contract violation, a rule that reached its
// fuel limit, a request that ran out of time, an extension that failed, two
// currencies meeting where one is required, and arithmetic with no answer
// are seven different things to report or monitor. fallback catches only the
// deadline and the extension, never a program's own limits, authoring errors,
// currency mismatches or arithmetic failures.
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
)

// errDivisionByZero is every division by zero the machine refuses.
var errDivisionByZero = fmt.Errorf("%w: division by zero", ErrArithmetic)

// errConversion is a conversion with no answer — text that is not a number, a
// float with a fraction taken as an int, an int a float cannot hold, a figure
// with more places than a ratio keeps: arithmetic with no answer, like a
// division by zero, and so the rule's or the data's.
func errConversion(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrArithmetic, fmt.Sprintf(format, args...))
}

// overflowIn is an integer result past int64 in the named operation.
func overflowIn(operation string) error {
	return fmt.Errorf("%w: integer overflow in %s", ErrArithmetic, operation)
}
