// Package funroute is the FunRoute language: a strongly typed, pure expression
// language for payment routing, in which every program terminates.
//
// This package is the whole public surface, and its files hold nothing but
// aliases and forwards, grouped by what a host does with them: values and
// types, the registry, compiling, running, batching, binding Go types, money,
// the manifest and the errors. The implementation lives under internal/,
// which Go's internal rule keeps unimportable from outside this module, so it
// can be reorganised without breaking a host. The language server is the
// sibling package lsp; the standard library of functions is extensions/std.
//
// What a host needs is deliberately narrow:
//
//	Registry        which functions and lazy forms exist — the type authority
//	CompileOptions  the contract: arguments, their order, types, prose, result
//	Artifact        immutable bytecode plus a digest, safe to store and ship
//	Runtime         an artifact bound to a registry, ready to run
//
// The AST is not part of it, and no function here hands one out: programs are
// exchanged as ExprJSON, which is what ExprJSON is for. A host that inspects or
// builds a program works with that canonical form rather than with Go types
// that may change.
package funroute

import (
	"github.com/nethinwei/funroute/internal/compile"
	"github.com/nethinwei/funroute/internal/machine"
	"github.com/nethinwei/funroute/internal/money"
	"github.com/nethinwei/funroute/internal/syntax"
)

// Values.
type (
	Value = machine.Value
	Type  = machine.Type
	// Field is one field of a record: a name and its own type. The order of a
	// record's fields is part of its type.
	Field = machine.Field
	Kind  = machine.Kind
)

// Value constructors. Array and Dict check and pack a slice of values; a host
// that already holds a Go container uses ToValue and keeps its backing. A
// float is any float IEEE 754 has, NaN and the infinities included.
var (
	Int    = machine.Int
	Float  = machine.Float
	String = machine.String
	Bool   = machine.Bool
	Array  = machine.Array
	Record = machine.Record
	Dict   = machine.Dict
)

// ToValue wraps a Go value — a []float64 becomes an array<float> holding that
// very slice — and FromValue hands the backing back out: the host boundary is
// free of conversion. The same two functions convert an extension's arguments
// and result, so the list of supported Go types (bool, int64, float64, string,
// their slices and string-keyed maps, and [][]float64) is stated once, in
// machine.
//
// What makes this safe is a convention rather than a copy: a slice or map that
// has been given to a Value, or taken from one, is read-only from then on.
func ToValue[T any](input T) (Value, error) { return machine.ToValue(input) }

// FromValue returns the Go form of a value; for a container it is the backing
// itself, read-only. See ToValue.
func FromValue[T any](value Value) (T, error) { return machine.FromValue[T](value) }

// Types.
var (
	BoolType   = machine.BoolType
	IntType    = machine.IntType
	FloatType  = machine.FloatType
	StringType = machine.StringType
	TypeVar    = machine.TypeVar
	RecordOf   = machine.RecordOf
	FieldOf    = machine.FieldOf
	ArrayOf    = machine.ArrayOf
	DictOf     = machine.DictOf
	HandleOf   = machine.HandleOf
	EnumOf     = machine.EnumOf
	ParseType  = machine.ParseType
	// ParseTypeWith reads a type that may name one of the given aliases, so a
	// text contract can declare record{…} once and refer to it by name.
	ParseTypeWith = machine.ParseTypeWith
)

// NewHandle wraps a host value as a handle by hand. Handles are how an
// inference engine's data crosses the expression without the language knowing
// anything about it: DefineHandle names a Go type as handle<name>, NewHandle
// wraps a value, and FromValue unwraps one. A handle can only be passed along
// — never compared, indexed or inspected.
var NewHandle = machine.NewHandle

func DefineHandle[T any](registry *Registry, name string) error {
	return machine.DefineHandle[T](registry, name)
}

// The registry is the single authority on what exists and what it means, and
// Registry.Register is the one way to add a function to it. A host function is
// a FunctionSpec that names a Go function in Go, its signature read once by
// reflection — any arity, an optional leading context.Context, Go containers
// nested to any depth, a result with or without an error — and GoBatch when it
// also has a batch implementation. A function of one array may declare a
// Fold, and a call of it on a comprehension then folds as the comprehension
// runs, with no array built.
type (
	Registry     = machine.Registry
	FunctionSpec = machine.FunctionSpec
	EvalFunc     = machine.EvalFunc
	Fold         = machine.Fold
	Doc          = machine.Doc
	Example      = machine.Example
	Form         = machine.Form
)

var (
	NewRegistry  = machine.NewRegistry
	CoreRegistry = machine.CoreRegistry
)

// The lazy forms a registry can enable. A registry is a console: what it
// enables is what its operators can write.
const (
	SwitchForm = machine.SwitchForm
	ForForm    = machine.ForForm
	ReduceForm = machine.ReduceForm
)

// The catalog is what a registry offers — its functions and its forms, with
// what the host and the language say about each — for a tool that lists
// them. Registry.Catalog builds it.
type (
	LanguageCatalog    = machine.LanguageCatalog
	FunctionDescriptor = machine.FunctionDescriptor
	FormDescriptor     = machine.FormDescriptor
)

// ArgSpec is one argument of a contract. The contract — which arguments, in
// which order, with which types and prose, and what comes back — belongs to
// the host, not to the expression text. A console already stores a rule's
// metadata; argument types are the same kind of information, and a second
// copy inside the source would be two stores to drift apart. So it arrives
// through CompileOptions.
type ArgSpec = compile.ArgSpec

// ParseToJSON turns text into the canonical document a front end renders. A
// host never handles the AST: the two Compile functions take text or that
// document.
var ParseToJSON = compile.ParseToJSON

// Format lays a program's text out the way the language prints it, keeping
// the comments around the expression; it refuses a comment inside one rather
// than drop it. The result parses to the same program.
var Format = syntax.FormatSource

// RenderWithContract writes the contract as comments above the expression, for
// a rule that leaves the console — a ticket, an RFC, a chat. Comments are not
// syntax, so the text parses to the same program and re-parsing does not carry
// them back: the host record stays the authority.
var (
	RenderWithContract   = compile.RenderWithContract
	ContractFromArtifact = compile.ContractFromArtifact
)

// ExprJSONVersion is the version of the canonical JSON form. A front end that
// produces documents must match it.
const ExprJSONVersion = syntax.ExprJSONVersion

// Compilation produces an artifact: immutable bytecode with a digest over
// everything that is contract. A host stores and ships it as JSON and reads
// its contract through Args, Result and Digest; its bytecode is the
// machine's, and Instantiate checks every part of it again.
type (
	Artifact       = machine.Artifact
	Parameter      = machine.Parameter
	CompileOptions = compile.CompileOptions
)

const ArtifactVersion = machine.ArtifactVersion

var (
	CompileExpr      = compile.CompileExpr
	CompileJSON      = compile.CompileJSON
	ValidateContract = compile.ValidateContract
)

// TextContract is a contract written down as data — types as text, record
// types named once — and Options is its one reading into CompileOptions.
type (
	TextContract = compile.TextContract
	TextArg      = compile.TextArg
	TextResult   = compile.TextResult
)

// Runtime runs an artifact. Instantiate rejects an artifact whose registry
// has drifted; a run stops at the deadline of the context it is given,
// before a host's call or within about a millisecond of a loop.
type Runtime = machine.Runtime

var Instantiate = machine.Instantiate

// DecodeArgs reads a program's arguments from a JSON object, keeping every
// digit of an integer. A CLI, a language server and a console decode alike
// through it.
var DecodeArgs = machine.DecodeArgs

// A Batch runs one artifact for many requests and calls each model once per
// batch: the calls the bytecode proves hoistable (arguments straight from the
// request, not in a loop, not behind a condition) go through the function's
// batch implementation before the programs run.
type (
	Batch        = machine.Batch
	BatchOptions = machine.BatchOptions
)

var NewBatch = machine.NewBatch

// A contract can also be two Go types. Bind reads it off them once: the
// `funroute:"name"` fields of In are the arguments, in declaration order —
// which is the ABI — and Out is the result. The Binding compiles and loads
// only programs of that contract, and a Program runs on the types directly:
//
//	binding, err := funroute.Bind[RouteIn, Decision](registry)
//	program, err := binding.Compile(source)
//	decision, err := program.Run(ctx, &request)
//
// No name is looked up and nothing is reflected per call. Scalars cost no
// allocation, a slice or map is wrapped rather than copied, and an argument
// the program does not read is not converted at all. A container in the
// result is the program's backing and is read-only.
//
// Load takes an artifact compiled elsewhere, matched by name: it may declare
// fewer arguments, in another order, and records with fewer fields, as a
// console that declares only what a rule reads does; a Go string carries an
// enum. Program.RunBatch runs requests the host already holds, sharing each
// model call, reading and writing them wherever they live; one index names a
// request, its result and its failure. Program.Batch does the same for
// requests from many goroutines.
type (
	Binding[In, Out any]      = compile.Binding[In, Out]
	Program[In, Out any]      = machine.Program[In, Out]
	ProgramBatch[In, Out any] = machine.ProgramBatch[In, Out]
)

func Bind[In, Out any](registry *Registry) (*Binding[In, Out], error) {
	return compile.Bind[In, Out](registry)
}

// Money. A registry that calls DeclareMoney gets four more kinds of value:
// money in a currency's minor unit, an exact ratio, an exchange rate between
// two currencies, and a currency itself. Money and ordinary numbers meet
// only in the operators and a handful of functions; a registry that declares
// nothing compiles exactly what it did before. The exchange rates a rule
// converts at are values like any other — written in the rule, or given as
// arguments, a []FxRate among them — and amount -> JPY converts only inside
// a using that names them.
type (
	// Money is an amount in a currency's minor unit, the Go form of money.
	// Only a currency table makes one (Currencies.Parse, Of, Minor); the zero
	// value is the currency-less zero. []Money is the backing of
	// array<money>, handed over without a copy.
	Money = money.Money
	// ExactMoney is money between minor units, exactly: what a step inside
	// round(…) makes before the round rounds it once. Money.Exact makes one,
	// its methods compute as a rule's steps do, Currencies.ConvertExact
	// converts one, and Round gives Money back. The zero value is the
	// currency-less zero.
	ExactMoney = money.ExactMoney
	// Ratio is an exact ratio, made by ParseRatio, Percent or BasisPoints; the
	// zero value is 0. It only works on money and on other ratios, never
	// rounds against another ratio, and crosses JSON as its exact text: a
	// decimal, or p/q where it has no finite decimal.
	Ratio = money.Ratio
	// FxRate is an exact exchange rate, the Go form of fxrate: made by
	// Currencies.FxRate or Implied, passed to a rule as an argument, and
	// returned from one. From a currency to itself it is 1.
	// Currencies.Convert converts at it; a rule takes it, or a []FxRate, as
	// an argument, hands it to using, and converts with ->.
	FxRate = money.FxRate
	// Currency is a declared currency as a value, made by
	// Currencies.Currency.
	Currency = money.Currency
)

// Money and Ratio carry the language's arithmetic as methods — Add, Sub,
// Cmp, MulRatio, Allocate and the rest — and the kernel's
// operators are those methods, so a host computing beside a rule gets the
// rule's answer. They return an error where the language would: currencies
// that do not match (ErrCurrency), an int64 overflow, a division by zero.
var (
	// Percent("2.9") is 2.9%, BasisPoints("25") is 0.25%, exactly.
	Percent     = money.Percent
	BasisPoints = money.BasisPoints
	// AverageMoney and MedianMoney are std's avg and median: amounts in one
	// currency, rounded once by mode.
	AverageMoney = money.AverageMoney
	MedianMoney  = money.MedianMoney
)

// The money types. Each is one type: a currency is the value's, never its
// type's, as Go's Money is one struct.
var (
	RatioType    = machine.RatioType
	MoneyType    = machine.MoneyType
	CurrencyType = machine.CurrencyType
	FxRateType   = machine.FxRateType
)

// A money value becomes a Value through ToValue, like any Go value.
var (
	// ParseRatio reads a decimal ratio exactly.
	ParseRatio = money.ParseRatio
)

// The currency table: the currencies a registry declares with their decimal
// places. It is also where money and exchange rates are made — Parse, Of and Minor for amounts, FxRate and
// Currency — so everything a host holds is in a declared currency.
type (
	// Currencies is a declared currency table, for the Go operations on
	// money that need a currency's decimal places: Parse and Format "USD
	// 1.70", Convert through an exchange rate. Registry.Currencies is the one
	// the rules use; NewCurrencies builds one from a MoneySpec.
	Currencies = money.Currencies
	// MoneySpec is what DeclareMoney takes: the currencies. There is no
	// default rounding: a rule writes every rounding it does. CurrencySpec is
	// one currency and its decimal places.
	MoneySpec    = money.MoneySpec
	CurrencySpec = money.CurrencySpec
	// Rounding is how a product falling between two minor units is settled.
	Rounding = money.Rounding
	// AllocationStrategy is where Money.AllocateBy, and allocate(m, w, @s)
	// in a rule, hands the units that rounding every share down leaves.
	AllocationStrategy = money.AllocationStrategy
)

var (
	NewCurrencies = money.NewCurrencies
	// ParseRounding reads a mode's name.
	ParseRounding = money.ParseRounding
	// RoundingEnumType is the enum a rounding mode is written in: the last
	// parameter of a money function that rounds where it is called, as in
	// mul(amount, 2.9%, @up), and of round(expr, @mode).
	RoundingEnumType = machine.RoundingEnumType
	// ParseAllocation reads a strategy's name; AllocationEnumType is the enum
	// a rule writes one in, @all_last.
	ParseAllocation    = money.ParseAllocation
	AllocationEnumType = machine.AllocationEnumType
)

// The rounding modes, by their names in a declaration and in round(expr,
// @mode): half_even, half_up, half_down, down, up, ceiling, floor.
const (
	RoundHalfEven = money.RoundHalfEven
	RoundHalfUp   = money.RoundHalfUp
	RoundHalfDown = money.RoundHalfDown
	RoundDown     = money.RoundDown
	RoundUp       = money.RoundUp
	RoundCeiling  = money.RoundCeiling
	RoundFloor    = money.RoundFloor
)

// The allocation strategies, by their names in allocate(m, w, @strategy):
// largest_remainder (the default), largest_weight, in_order, reverse_order,
// all_first, all_last.
const (
	AllocateLargestRemainder = money.AllocateLargestRemainder
	AllocateLargestWeight    = money.AllocateLargestWeight
	AllocateInOrder          = money.AllocateInOrder
	AllocateReverseOrder     = money.AllocateReverseOrder
	AllocateAllFirst         = money.AllocateAllFirst
	AllocateAllLast          = money.AllocateAllLast
)

// A Manifest is a registry without its implementations, for a tool that has
// to type-check, explain and complete a program where the host's functions
// cannot run — a language server, an editor in the browser. Registry.Manifest
// writes one; Apply adds what it describes to a registry that holds the
// kernel and the standard library, each missing function as its signature
// alone, and TrackUnavailable says which of those a run called.
type Manifest = machine.Manifest

const ManifestVersion = machine.ManifestVersion

var TrackUnavailable = machine.TrackUnavailable

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
	ErrDeadline  = machine.ErrDeadline
	ErrExtension = machine.ErrExtension
	// ErrCurrency is money meeting money of another currency where one is
	// required, or a currency the registry did not declare.
	ErrCurrency = machine.ErrCurrency
	// ErrNoFxRate is a conversion whose using has no quote between the two
	// currencies, either way. It is data not at hand, so fallback takes it.
	ErrNoFxRate = machine.ErrNoFxRate
	// ErrArithmetic is arithmetic with no answer: an int overflow, an int
	// division by zero, an exchange rate that is not positive; a float has
	// an answer for everything, as IEEE 754 has it. fallback does not take
	// it.
	ErrArithmetic = machine.ErrArithmetic
	// ErrDomain is data an operation has no answer for: an index past the
	// end, a missing key, the first of an empty array, two arrays that were
	// to line up and do not. fallback does not take it.
	ErrDomain = machine.ErrDomain
	// ErrUnavailable is an ErrExtension from a function this registry knows
	// only by its signature, from a Manifest.
	ErrUnavailable = machine.ErrUnavailable
)
