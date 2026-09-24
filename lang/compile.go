package lang

import (
	"funroute/lang/internal/compile"
	"funroute/lang/internal/machine"
	"funroute/lang/internal/syntax"
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
