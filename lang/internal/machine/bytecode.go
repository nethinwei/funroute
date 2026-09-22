package machine

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
)

// ArtifactVersion changes whenever anything the digest covers changes shape;
// an artifact with any other version is refused rather than reinterpreted.
const ArtifactVersion = 2

// OpCode is a dense enum so the interpreter dispatches through a jump table.
// The JSON form keeps the original mnemonics, so artifact digests do not move.
type OpCode uint8

const (
	OpInvalid OpCode = iota
	OpConstant
	OpLoadArg
	OpLoadLocal
	OpStoreLocal
	OpMakeArray
	OpMakeDict
	OpEqual
	OpCall
	OpLoopInit
	OpLoopCollect
	OpLoopNext
	OpJumpIfFalse
	OpJump
	OpBeginFallback
	OpEndFallback
)

type Parameter struct {
	Name string `json:"name"`
	Type Type   `json:"type"`
	// Doc is the host's prose for the argument (ArgSpec.Doc). It is
	// presentation only and is cleared before the digest is computed, so
	// rewording it does not invalidate an artifact that is already deployed.
	Doc string `json:"doc,omitempty"`
}

type Constant struct {
	Type   Kind    `json:"type"`
	Int    *int64  `json:"int,omitempty"`
	Float  *string `json:"float,omitempty"`
	String *string `json:"string,omitempty"`
	Bool   *bool   `json:"bool,omitempty"`
}

// ConstantFromValue interns a compile-time value. Containers have no constant
// form, so they report an error and the compiler emits the work instead.
func ConstantFromValue(value Value) (Constant, error) {
	switch value.kind {
	case IntKind:
		v := value.i
		return Constant{Type: IntKind, Int: &v}, nil
	case FloatKind:
		v := strconv.FormatFloat(value.f, 'g', -1, 64)
		return Constant{Type: FloatKind, Float: &v}, nil
	case StringKind:
		v := value.s
		return Constant{Type: StringKind, String: &v}, nil
	case BoolKind:
		v := value.b
		return Constant{Type: BoolKind, Bool: &v}, nil
	default:
		return Constant{}, fmt.Errorf("bytecode constant cannot contain %s", value.Type())
	}
}

func (c Constant) value() (Value, error) {
	switch c.Type {
	case IntKind:
		if c.Int == nil {
			return Value{}, fmt.Errorf("int constant is missing int")
		}
		return Int(*c.Int), nil
	case FloatKind:
		if c.Float == nil {
			return Value{}, fmt.Errorf("float constant is missing float")
		}
		parsed, err := strconv.ParseFloat(*c.Float, 64)
		if err != nil {
			return Value{}, fmt.Errorf("invalid float constant %q", *c.Float)
		}
		return CheckedFloat(parsed)
	case StringKind:
		if c.String == nil {
			return Value{}, fmt.Errorf("string constant is missing string")
		}
		return String(*c.String), nil
	case BoolKind:
		if c.Bool == nil {
			return Value{}, fmt.Errorf("bool constant is missing bool")
		}
		return Bool(*c.Bool), nil
	default:
		return Value{}, fmt.Errorf("unknown constant type %q", c.Type)
	}
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

type Artifact struct {
	Version  int             `json:"version"`
	Digest   string          `json:"digest"`
	ExprJSON json.RawMessage `json:"expr_json"`
	Args     []Parameter     `json:"args"`
	Result   Type            `json:"result"`
	// ResultDoc is the host's prose for the result. Like Parameter.Doc it is
	// presentation only and is cleared before the digest is computed.
	ResultDoc string          `json:"result_doc,omitempty"`
	Constants []Constant      `json:"constants,omitempty"`
	Calls     []CallReference `json:"calls,omitempty"`
	Locals    int             `json:"locals,omitempty"`
	// MaxStack is the deepest the operand stack gets, computed while
	// compiling. The frame reserves it once, so pushing never has to check.
	MaxStack     int           `json:"max_stack"`
	Instructions []Instruction `json:"instructions"`
}

func (a *Artifact) MarshalIndent() ([]byte, error) {
	return json.MarshalIndent(a, "", "  ")
}

// ArtifactDigest is the artifact's identity: a sha256 over everything that
// is contract. Prose is scrubbed first, so rewording a doc string does not
// invalidate a deployed artifact.
func ArtifactDigest(artifact *Artifact) (string, error) {
	copyArtifact := *artifact
	copyArtifact.Digest = ""
	// Prose is not part of the contract: digest names and types only. The
	// ExprJSON needs no scrubbing, because it holds the expression alone.
	copyArtifact.Args = make([]Parameter, len(artifact.Args))
	for i, param := range artifact.Args {
		param.Doc = ""
		copyArtifact.Args[i] = param
	}
	copyArtifact.ResultDoc = ""
	encoded, err := json.Marshal(copyArtifact)
	if err != nil {
		return "", fmt.Errorf("encode artifact digest: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}
