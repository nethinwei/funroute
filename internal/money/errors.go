package money

import (
	"errors"
	"fmt"
)

// The errors of money, which the machine reports as its own: a currency
// where another is required or one not declared, arithmetic with no answer,
// and an exchange rate not at hand.
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
)

// errDivisionByZero is every division by zero money refuses.
var errDivisionByZero = fmt.Errorf("%w: division by zero", ErrArithmetic)

// errConversion is text that reads as no amount or ratio: arithmetic with no
// answer, and so the rule's or the data's.
func errConversion(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrArithmetic, fmt.Sprintf(format, args...))
}

// Classify is one error with a class and a cause: errors.Is answers the class
// from this error itself, and Unwrap leads to the cause, so a host still finds
// its own error behind the class it was given. detail, when there is one,
// sits between the two in the text and ends in its own separator.
func Classify(class error, detail string, cause error) error {
	return &classified{class: class, detail: detail, cause: cause}
}

type classified struct {
	class  error
	detail string
	cause  error
}

func (c *classified) Error() string        { return c.class.Error() + ": " + c.detail + c.cause.Error() }
func (c *classified) Is(target error) bool { return target == c.class }
func (c *classified) Unwrap() error        { return c.cause }
