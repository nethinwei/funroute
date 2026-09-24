package lang

import "funroute/lang/internal/machine"

// The currency table: the currencies a registry declares with their decimal
// places, and the rounding its money takes by default. It is also where money
// and exchange rates are made — Parse, Of and Minor for amounts, FxRate and
// Currency — so everything a host holds is in a declared currency.
type (
	// Currencies is a declared currency table, for the Go operations on
	// money that need a currency's decimal places: Parse and Format "USD
	// 1.70", Convert through an exchange rate. Registry.Currencies is the one
	// the rules use; NewCurrencies builds one from a MoneySpec.
	Currencies = machine.Currencies
	// MoneySpec is what DeclareMoney takes: the currencies and the default
	// rounding. CurrencySpec is one currency and its decimal places.
	MoneySpec    = machine.MoneySpec
	CurrencySpec = machine.CurrencySpec
	// Rounding is how a product falling between two minor units is settled.
	Rounding = machine.Rounding
	// AllocationStrategy is where Money.AllocateBy, and allocate(m, w, @s)
	// in a rule, hands the units that rounding every share down leaves.
	AllocationStrategy = machine.AllocationStrategy
)

var (
	NewCurrencies = machine.NewCurrencies
	// ParseRounding reads a mode's name.
	ParseRounding = machine.ParseRounding
	// RoundingEnumType is the enum a rounding mode is written in; a money
	// function that rounds registers a variant taking it last, and
	// round(expr, @mode) calls that one.
	RoundingEnumType = machine.RoundingEnumType
	// ParseAllocation reads a strategy's name; AllocationEnumType is the enum
	// a rule writes one in, @all_last.
	ParseAllocation    = machine.ParseAllocation
	AllocationEnumType = machine.AllocationEnumType
)

// The rounding modes, by their names in a declaration and in round(expr,
// @mode): half_even, half_up, half_down, down, up, ceiling, floor.
const (
	RoundHalfEven = machine.RoundHalfEven
	RoundHalfUp   = machine.RoundHalfUp
	RoundHalfDown = machine.RoundHalfDown
	RoundDown     = machine.RoundDown
	RoundUp       = machine.RoundUp
	RoundCeiling  = machine.RoundCeiling
	RoundFloor    = machine.RoundFloor
)

// The allocation strategies, by their names in allocate(m, w, @strategy):
// largest_remainder (the default), largest_weight, in_order, reverse_order,
// all_first, all_last.
const (
	AllocateLargestRemainder = machine.AllocateLargestRemainder
	AllocateLargestWeight    = machine.AllocateLargestWeight
	AllocateInOrder          = machine.AllocateInOrder
	AllocateReverseOrder     = machine.AllocateReverseOrder
	AllocateAllFirst         = machine.AllocateAllFirst
	AllocateAllLast          = machine.AllocateAllLast
)
