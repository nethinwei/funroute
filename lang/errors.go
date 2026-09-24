package lang

import (
	"funroute/lang/internal/machine"
	"funroute/lang/internal/syntax"
)

// PositionError is a compile error that knows where it happened. The offset
// is what the lexer and the parser have; LineColumn turns it into what a
// person reads, against the source the host still holds — a program compiled
// from ExprJSON has no text, and then there is nothing to report.
type PositionError = syntax.PosError

// LineColumn finds a PositionError's place in source: lines from 1, columns
// in characters from 1. It reports false when the error carries no position.
var LineColumn = syntax.LineColumn

// The errors a host tells apart with errors.Is.
var (
	ErrCompile   = machine.ErrCompile
	ErrContract  = machine.ErrContract
	ErrFuel      = machine.ErrFuel
	ErrDeadline  = machine.ErrDeadline
	ErrExtension = machine.ErrExtension
	// ErrCurrency is money meeting money of another currency where one is
	// required, or a currency the registry did not declare.
	ErrCurrency = machine.ErrCurrency
	// ErrNoRate is a conversion with no rate table, or a table with no rate
	// between the two currencies either way. It is data not at hand, so fallback takes it.
	ErrNoRate = machine.ErrNoRate
	// ErrArithmetic is arithmetic with no answer: an overflow, a division by
	// zero, a float that is not finite, an exchange rate that is not
	// positive. fallback does not take it.
	ErrArithmetic = machine.ErrArithmetic
	// ErrUnavailable is an ErrExtension from a function this registry knows
	// only by its signature, from a Manifest.
	ErrUnavailable = machine.ErrUnavailable
)
