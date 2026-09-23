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
	// ParseTypeWith reads a type that may name one of the given aliases, so a
	// text contract can declare record{…} once and refer to it by name.
	ParseTypeWith = machine.ParseTypeWith
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
	// ErrUnavailable is an ErrExtension from a function this registry knows
	// only by its signature, from a Manifest.
	ErrUnavailable = machine.ErrUnavailable
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

// A contract can also be two Go types. Bind reads it off them once: the
// `funroute:"name"` fields of In are the arguments, in declaration order —
// which is the ABI — and Out is the result. The Binding compiles and loads
// only programs of that contract, and a Program runs on the types directly:
//
//	binding, err := lang.Bind[RouteIn, Decision](registry)
//	program, err := binding.Compile(source)
//	decision, err := program.Run(ctx, &request, lang.RunOptions{})
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
// model call; RunBatchInto writes the results into the host's own objects and
// RunBatchFunc reaches requests and results wherever they live. One index
// names a request, its result and its failure in all three. Program.Batch
// does the same for requests from many goroutines.
type (
	Binding[In, Out any]      = compile.Binding[In, Out]
	Program[In, Out any]      = machine.Program[In, Out]
	ProgramBatch[In, Out any] = machine.ProgramBatch[In, Out]
)

func Bind[In, Out any](registry *Registry) (*Binding[In, Out], error) {
	return compile.Bind[In, Out](registry)
}

// DecodeArgs reads a program's arguments from a JSON object, keeping every
// digit of an integer; DefaultFuel is the budget to give a run when nobody
// named one. A CLI, a language server and a console decode alike through it.
var DecodeArgs = machine.DecodeArgs

const DefaultFuel = machine.DefaultFuel

// A Batch runs one artifact for many requests and calls each model once per
// batch: the calls the bytecode proves hoistable (arguments straight from the
// request, not in a loop, not behind a condition) go through the function's
// batch implementation before the programs run.
type (
	Batch        = machine.Batch
	BatchOptions = machine.BatchOptions
)

var NewBatch = machine.NewBatch

// The catalog is what a registry offers — its functions and its forms, with
// what the host and the language say about each — for a tool that lists
// them. Registry.Catalog builds it.
type (
	LanguageCatalog    = machine.LanguageCatalog
	FunctionDescriptor = machine.FunctionDescriptor
	FormDescriptor     = machine.FormDescriptor
)

// A Manifest is a registry without its implementations, for a tool that has
// to type-check, explain and complete a program where the host's functions
// cannot run — a language server, an editor in the browser. Registry.Manifest
// writes one; Apply adds what it describes to a registry that holds the
// kernel and the standard library, each missing function as its signature
// alone, and TrackUnavailable says which of those a run called.
type (
	Manifest         = machine.Manifest
	ManifestFunction = machine.ManifestFunction
)

const ManifestVersion = machine.ManifestVersion

var TrackUnavailable = machine.TrackUnavailable

// TextContract is a contract written down as data — types as text, record
// types named once — and Options is its one reading into CompileOptions.
type (
	TextContract = compile.TextContract
	TextArg      = compile.TextArg
	TextResult   = compile.TextResult
)
