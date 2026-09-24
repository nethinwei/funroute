package money

import (
	"errors"
	"fmt"
)

// The errors of money, which the machine reports as its own: a currency
// where another is required or one not declared, arithmetic with no answer,
// data with no answer, and an exchange rate not at hand.
var (
	// ErrCurrency is money meeting money of another currency where one is
	// required, or a currency the table does not declare.
	ErrCurrency = errors.New("currency mismatch")
	// ErrArithmetic is arithmetic with no answer: an overflow, a division by
	// zero, an exchange rate that is not positive, a ratio past int64.
	ErrArithmetic = errors.New("arithmetic failed")
	// ErrNoFxRate is a conversion the rates at hand cannot make: the using
	// has no quote between the two currencies, either way. It is data not
	// yet at hand, like a failed extension, so fallback takes it.
	ErrNoFxRate = errors.New("no exchange rate")
	// ErrDomain is data an operation has no answer for: the average of no
	// amounts, an index past the end, a key a dictionary does not have.
	ErrDomain = errors.New("no answer for the data")
)

// errDivisionByZero is every division by zero money refuses.
var errDivisionByZero = fmt.Errorf("%w: division by zero", ErrArithmetic)
