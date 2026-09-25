package machine

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/nethinwei/funroute/internal/kit"
)

// ArtifactVersion names the shape of everything the digest covers; an artifact
// with any other version is refused rather than reinterpreted. Nothing is
// deployed against this language yet, so a shape change edits the shape rather
// than adding a version to migrate from.
const ArtifactVersion = 2

// OpCode is a dense enum, lowered to the register form when an artifact is
// loaded. The JSON form keeps the original mnemonics, so artifact digests do
// not move.
type OpCode uint8

const (
	OpInvalid OpCode = iota
	OpConstant
	OpLoadArg
	OpLoadLocal
	OpStoreLocal
	OpMakeArray
	OpMakeDict
	OpMakeRecord
	OpField
	OpEqual
	OpCall
	OpLoopInit
	OpLoopCollect
	OpLoopNext
	OpJumpIfFalse
	OpJump
	OpBeginFallback
	OpEndFallback
	OpLoopSpread
	OpRecordWith
	OpFxPush
	OpFxPop
	OpLoopBreak
	OpLoopFold
)

// Parameter is one argument of an artifact's contract: its name, its type
// and the host's prose for it. A host reads it from Artifact.Args.
type Parameter struct {
	name string
	typ  Type
	// doc is the host's prose for the argument (ArgSpec.Doc). It is
	// presentation only and is cleared before the digest is computed, so
	// rewording it does not invalidate an artifact that is already deployed.
	doc string
}

// cloneParameters copies each parameter's name and type, the type deep; the
// doc stays behind.
func cloneParameters(params []Parameter) []Parameter {
	return kit.Map(params, func(param Parameter) Parameter { return Parameter{name: param.name, typ: param.typ} })
}

// NewParameter is the compiler's entry: a contract is the host's input,
// and a Parameter is only what the compiler made of it.
func NewParameter(name string, typ Type, doc string) Parameter {
	return Parameter{name: name, typ: typ, doc: doc}
}

func (p Parameter) Name() string { return p.name }
func (p Parameter) Type() Type   { return p.typ }
func (p Parameter) Doc() string  { return p.doc }

// String is the parameter as a contract writes it: "amount: money<USD>".
func (p Parameter) String() string { return p.name + ": " + p.typ.String() }

type parameterJSON struct {
	Name string `json:"name"`
	Type Type   `json:"type"`
	Doc  string `json:"doc,omitempty"`
}

func (p Parameter) MarshalJSON() ([]byte, error) {
	return json.Marshal(parameterJSON{Name: p.name, Type: p.typ, Doc: p.doc})
}

func (p *Parameter) UnmarshalJSON(data []byte) error {
	var shape parameterJSON
	if err := json.Unmarshal(data, &shape); err != nil {
		return err
	}
	*p = Parameter{name: shape.Name, typ: shape.Type, doc: shape.Doc}
	return nil
}

// Constant is a value the compiler worked out, as an artifact carries it:
// its type and the value as MarshalJSON writes it. It is read back the way a
// host's JSON argument is (coerce), so a constant has no encoding of its own
// and holds every value exactly: a float in its shortest text, money in
// minor units, a ratio and an exchange rate as their exact fractions.
type Constant struct {
	Type  Type            `json:"type"`
	Value json.RawMessage `json:"value"`
}

// ConstantOf interns a compile-time value, containers included, as a
// constant of typ, the type the program gives it — for an enum member more
// than the string it holds. A literal list or record that does not read an
// argument is built once, while the rule compiles, rather than on every run.
// It reports false for a value that has no constant form: a handle, exact
// money, or a value that is not of typ.
func ConstantOf(value Value, typ Type) (Constant, bool) {
	if value.kind == HandleKind || IsExact(value) || !typ.IsConcrete() || !value.hasType(typ) {
		return Constant{}, false
	}
	data, err := json.Marshal(value)
	return Constant{Type: typ, Value: data}, err == nil
}

// value reads the constant back as a value of its type.
func (c Constant) value() (Value, error) {
	decoder := kit.NumberDecoder(c.Value)
	var input any
	if err := decoder.Decode(&input); err != nil {
		return Value{}, fmt.Errorf("constant: %w", err)
	}
	if !c.Type.IsConcrete() {
		return Value{}, errors.New("constant is missing its type")
	}
	return coerce(input, c.Type)
}

// Instruction operands: A is a jump target or an index, B a local slot or an
// argument count, C the accumulator slot of a fold loop (NoAccumulator for a
// mapping loop), D the key slot of a dictionary walk (NoKey otherwise).
type Instruction struct {
	Op   OpCode   `json:"op"`
	A    int      `json:"a,omitempty"`
	B    int      `json:"b,omitempty"`
	C    int      `json:"c,omitempty"`
	D    int      `json:"d,omitempty"`
	Keys []string `json:"keys,omitempty"`
	Type *Type    `json:"type,omitempty"`
}

type CallReference struct {
	Name      string `json:"name"`
	Signature string `json:"signature"`
	Cost      uint64 `json:"cost"`
}

// ArtifactParts is what an artifact is made of, as the machine and the
// compiler handle it; its JSON is the artifact's. It is internal: a host holds
// an Artifact, which only SealArtifact makes and only JSON reads back.
type ArtifactParts struct {
	Version  int             `json:"version"`
	Digest   string          `json:"digest"`
	ExprJSON json.RawMessage `json:"expr_json"`
	Args     []Parameter     `json:"args"`
	Result   Type            `json:"result"`
	// ResultDoc is the host's prose for the result. Like Parameter.Doc it is
	// presentation only and is cleared before the digest is computed.
	ResultDoc    string          `json:"result_doc,omitempty"`
	Constants    []Constant      `json:"constants,omitempty"`
	Calls        []CallReference `json:"calls,omitempty"`
	Locals       int             `json:"locals,omitempty"`
	Instructions []Instruction   `json:"instructions"`
	// Money is the money feature the artifact was compiled against, present
	// only when the program uses a money type: a program without money
	// digests exactly as it did before the feature existed.
	Money *MoneyStamp `json:"money,omitempty"`
}

// Artifact is a compiled program, frozen: its bytecode, its contract and the
// digest over both. A host gets one from a compile, stores and loads it as
// JSON, and reads its contract through the methods; nothing else can change
// it, and Instantiate checks every part again anyway.
type Artifact struct{ parts ArtifactParts }

// SealArtifact freezes parts into an artifact: the version, the money stamp
// when the program uses money, and the digest over all of it. A nil registry
// keeps the stamp the parts carry, for a tool resealing parts it took apart.
func SealArtifact(parts ArtifactParts, registry *Registry) (*Artifact, error) {
	parts.Version = ArtifactVersion
	if registry != nil {
		parts.Money = nil
	}
	artifact := &Artifact{parts: parts}
	if table := registry.currencies(); table != nil && ArtifactUsesMoney(artifact) {
		codes, err := moneyCodes(artifact)
		if err != nil {
			return nil, err
		}
		stamp := stampFor(table, codes)
		artifact.parts.Money = &stamp
	}
	digest, err := ArtifactDigest(artifact)
	if err != nil {
		return nil, err
	}
	artifact.parts.Digest = digest
	return artifact, nil
}

// PartsOf is an artifact's parts, a copy, for the compiler and the tests
// that look at bytecode; a host has the accessors.
func PartsOf(a *Artifact) ArtifactParts { return a.parts }

func (a *Artifact) Version() int      { return a.parts.Version }
func (a *Artifact) Digest() string    { return a.parts.Digest }
func (a *Artifact) Result() Type      { return a.parts.Result }
func (a *Artifact) ResultDoc() string { return a.parts.ResultDoc }

// Args is the contract's arguments in ABI order; a copy.
func (a *Artifact) Args() []Parameter { return slices.Clone(a.parts.Args) }

// InstructionCount is how many instructions the program compiled to.
func (a *Artifact) InstructionCount() int { return len(a.parts.Instructions) }

func (a *Artifact) MarshalJSON() ([]byte, error) { return json.Marshal(a.parts) }

func (a *Artifact) UnmarshalJSON(data []byte) error { return json.Unmarshal(data, &a.parts) }

func (a *Artifact) MarshalIndent() ([]byte, error) {
	return json.MarshalIndent(a.parts, "", "  ")
}

// ArtifactDigest is the artifact's identity: a sha256 over everything that
// is contract. Prose is scrubbed first, so rewording a doc string does not
// invalidate a deployed artifact.
func ArtifactDigest(artifact *Artifact) (string, error) {
	copyArtifact := artifact.parts
	copyArtifact.Digest = ""
	// Prose is not part of the contract: digest names and types only. The
	// ExprJSON needs no scrubbing, because it holds the expression alone.
	copyArtifact.Args = cloneParameters(artifact.parts.Args)
	copyArtifact.ResultDoc = ""
	encoded, err := json.Marshal(copyArtifact)
	if err != nil {
		return "", fmt.Errorf("encode artifact digest: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}
