package lang

import (
	"encoding/json"
	"fmt"
	"strconv"
)

const ArtifactVersion = 2

// OpCode is a dense enum so the interpreter dispatches through a jump table.
// The JSON form keeps the original mnemonics, so artifact digests do not move.
type OpCode uint8

const (
	OpInvalid OpCode = iota
	OpConstant
	OpLoadArg
	OpLoadLocal
	OpMakeArray
	OpMakeDict
	OpEqual
	OpCall
	OpRecur
	OpLoopInit
	OpLoopCollect
	OpLoopNext
	OpJumpIfFalse
	OpJump
)

var opNames = [...]string{
	"invalid", "const", "load_arg", "load_local", "make_array", "make_dict",
	"equal", "call", "recur", "loop_init", "loop_collect", "loop_next",
	"jump_if_false", "jump",
}

func (o OpCode) String() string {
	if int(o) < len(opNames) {
		return opNames[o]
	}
	return opNames[OpInvalid]
}

func (o OpCode) MarshalText() ([]byte, error) { return []byte(o.String()), nil }

func (o *OpCode) UnmarshalText(text []byte) error {
	for i, name := range opNames {
		if name == string(text) {
			*o = OpCode(i)
			return nil
		}
	}
	return fmt.Errorf("unknown opcode %q", text)
}

type Parameter struct {
	Name string `json:"name"`
	Type Type   `json:"type"`
}

type Constant struct {
	Type   Kind    `json:"type"`
	Int    *int64  `json:"int,omitempty"`
	Float  *string `json:"float,omitempty"`
	String *string `json:"string,omitempty"`
	Bool   *bool   `json:"bool,omitempty"`
}

func constantFromValue(value Value) (Constant, error) {
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
		return checkedFloat(parsed)
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
// argument count, C the accumulator slot of a fold loop (-1 for a mapping loop)
// or, on OpRecur, tailCall when the recursion is in tail position.
type Instruction struct {
	Op   OpCode   `json:"op"`
	A    int      `json:"a,omitempty"`
	B    int      `json:"b,omitempty"`
	C    int      `json:"c,omitempty"`
	Keys []string `json:"keys,omitempty"`
	Type *Type    `json:"type,omitempty"`
}

type CallReference struct {
	Name      string `json:"name"`
	Signature string `json:"signature"`
	Cost      uint64 `json:"cost"`
}

type Artifact struct {
	Version      int             `json:"version"`
	Digest       string          `json:"digest"`
	ExprJSON     json.RawMessage `json:"expr_json"`
	Args         []Parameter     `json:"args"`
	Result       Type            `json:"result"`
	Constants    []Constant      `json:"constants,omitempty"`
	Calls        []CallReference `json:"calls,omitempty"`
	Locals       int             `json:"locals,omitempty"`
	Instructions []Instruction   `json:"instructions"`
}

func (a *Artifact) MarshalIndent() ([]byte, error) {
	return json.MarshalIndent(a, "", "  ")
}
