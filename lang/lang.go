// Package lang is the FunRoute language: a strongly typed, pure expression
// language for payment routing.
//
// This package is the whole public surface, and its files hold nothing but
// aliases and forwards, grouped by what a host does with them: values and
// types here, then the registry, compiling, running, batching, binding Go
// types, the manifest and the errors. Everything else lives under
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

import "funroute/lang/internal/machine"

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
