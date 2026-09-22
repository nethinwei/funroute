// Package lang is the FunRoute language: a strongly typed, pure expression
// language for payment routing.
//
// This file is the whole public surface. Everything else lives under
// lang/internal, which Go's internal rule keeps unimportable from outside
// this module, so the implementation can be reorganised without breaking a
// host.
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
package lang

import (
	"funroute/lang/internal/compile"
	"funroute/lang/internal/machine"
	"funroute/lang/internal/syntax"
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
// that already holds a Go container uses ToValue and keeps its backing.
var (
	Int    = machine.Int
	String = machine.String
	Bool   = machine.Bool
	Array  = machine.Array
	Record = machine.Record
	Dict   = machine.Dict
)

// Float rejects NaN and infinities at the public boundary. Returning an error
// keeps invalid host data from entering a Runtime through RunValues.
func Float(value float64) (Value, error) { return machine.CheckedFloat(value) }

// The host boundary is free of conversion. ToValue wraps a Go value — a
// []float64 becomes an array<float> holding that very slice — and FromValue
// hands the backing back out. The same two functions convert an extension's
// arguments and result, so the list of supported Go types (bool, int64,
// float64, string, their slices and string-keyed maps, and [][]float64) is
// stated once, in machine.
//
// What makes this safe is a convention rather than a copy: a slice or map that
// has been given to a Value, or taken from one, is read-only from then on.
func ToValue[T any](input T) (Value, error) { return machine.ToValue(input) }

func FromValue[T any](value Value) (T, error) { return machine.FromValue[T](value) }

// Types.
var (
	BoolType   = machine.BoolType
	IntType    = machine.IntType
	FloatType  = machine.FloatType
	StringType = machine.StringType
	TypeVar    = machine.TypeVar
	RecordOf   = machine.RecordOf
	ArrayOf    = machine.ArrayOf
	DictOf     = machine.DictOf
	HandleOf   = machine.HandleOf
	EnumOf     = machine.EnumOf
	ParseType  = machine.ParseType
)

// Handles are how an inference engine's data crosses the expression without
// the language knowing anything about it: DefineHandle names a Go type as
// handle<name>, NewHandle wraps a value by hand, and FromValue unwraps one.
// A handle can only be passed along — never compared, indexed or inspected.
var NewHandle = machine.NewHandle

func DefineHandle[T any](registry *Registry, name string) error {
	return machine.DefineHandle[T](registry, name)
}

// The registry is the single authority on what exists and what it means.
type (
	Registry     = machine.Registry
	FunctionSpec = machine.FunctionSpec
	EvalFunc     = machine.EvalFunc
	Doc          = machine.Doc
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

// A host function is registered by its Go signature, read once by reflection:
// Logic for business logic, Model for a model that also has a batch
// implementation. Both accept any arity, an optional leading context.Context,
// and Go containers nested to any depth.
var (
	Logic = machine.Logic
	Model = machine.Model
)

// The errors a host tells apart with errors.Is.
// A compile error knows where it happened. The offset is what the lexer and
// the parser have; LineColumn turns it into what a person reads, against the
// source the host still holds — a program compiled from ExprJSON has no text,
// and then there is nothing to report.
type PositionError = syntax.PosError

var LineColumn = syntax.LineColumn

var (
	ErrCompile   = machine.ErrCompile
	ErrContract  = machine.ErrContract
	ErrFuel      = machine.ErrFuel
	ErrDeadline  = machine.ErrDeadline
	ErrExtension = machine.ErrExtension
)

// The contract — which arguments, in which order, with which types and prose,
// and what comes back — belongs to the host, not to the expression text. A
// console already stores a rule's metadata; argument types are the same kind
// of information, and a second copy inside the source would be two stores to
// drift apart. So it arrives through CompileOptions.
type ArgSpec = compile.ArgSpec

// A host never handles the AST: ParseToJSON turns text into the canonical
// document a front end renders, and the two Compile functions take text or
// that document.
var ParseToJSON = compile.ParseToJSON

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
// everything that is contract. Its fields are public because an artifact is
// meant to be stored and shipped as JSON.
type (
	Artifact       = machine.Artifact
	Parameter      = machine.Parameter
	Constant       = machine.Constant
	Instruction    = machine.Instruction
	CallReference  = machine.CallReference
	OpCode         = machine.OpCode
	CompileOptions = compile.CompileOptions
)

const ArtifactVersion = machine.ArtifactVersion

var (
	CompileExpr      = compile.CompileExpr
	CompileJSON      = compile.CompileJSON
	ValidateContract = compile.ValidateContract
)

// Running an artifact. Instantiate rejects an artifact whose registry has
// drifted; Run enforces the fuel budget.
type (
	Runtime    = machine.Runtime
	RunOptions = machine.RunOptions
)

var Instantiate = machine.Instantiate

// A Batch runs one artifact for many requests and calls each model once per
// batch: the calls the bytecode proves hoistable (arguments straight from the
// request, not in a loop, not behind a condition) go through the function's
// batch implementation before the programs run.
type (
	Batch        = machine.Batch
	BatchOptions = machine.BatchOptions
)

var NewBatch = machine.NewBatch

// The catalog is what a front end renders: every function and form a registry
// offers with its presentation metadata, and the shape of every ExprJSON node
// so the canvas builds exactly what the compiler accepts.
type (
	LanguageCatalog     = machine.LanguageCatalog
	FunctionDescriptor  = machine.FunctionDescriptor
	ValueTypeDescriptor = machine.ValueTypeDescriptor
	SourceSyntax        = machine.SourceSyntax
	SourceOperator      = machine.SourceOperatorDescriptor
	ExpressionTemplate  = machine.ExpressionTemplate
	NodeSchema          = machine.NodeSchema
	FieldSchema         = machine.FieldSchema
)

var Catalog = compile.Catalog
