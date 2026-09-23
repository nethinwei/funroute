package machine

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
)

// ArtifactVersion names the shape of everything the digest covers; an artifact
// with any other version is refused rather than reinterpreted. Nothing is
// deployed against this language yet, so a shape change edits the shape rather
// than adding a version to migrate from.
const ArtifactVersion = 1

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
	// Elem, Items and Keys carry a container: Items holds an array's elements
	// or a record's fields in the type's order, and Keys pairs with Items for
	// a dictionary. Elem is the element type an empty array or dictionary
	// would otherwise lose, and the record's own type for a record.
	Elem  *Type      `json:"elem,omitempty"`
	Items []Constant `json:"items,omitempty"`
	Keys  []string   `json:"keys,omitempty"`
}

// ConstantFromValue interns a compile-time value, containers included: a
// literal list or record that does not read an argument is built once, while
// the rule compiles, rather than on every run.
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
	case ArrayKind, DictKind, RecordKind:
		return containerConstant(value)
	default:
		return Constant{}, fmt.Errorf("bytecode constant cannot contain %s", value.Type())
	}
}

func containerConstant(value Value) (Constant, error) {
	typ := value.Type()
	out := Constant{Type: value.kind, Elem: &typ}
	switch value.kind {
	case ArrayKind:
		for i := 0; i < value.length(); i++ {
			item, err := ConstantFromValue(value.at(i))
			if err != nil {
				return Constant{}, err
			}
			out.Items = append(out.Items, item)
		}
	case DictKind:
		for _, key := range value.keys() {
			entry, _ := value.lookup(key)
			item, err := ConstantFromValue(entry)
			if err != nil {
				return Constant{}, err
			}
			out.Keys = append(out.Keys, key)
			out.Items = append(out.Items, item)
		}
	default:
		record, ok := value.box.(*recordValue)
		if !ok {
			return Constant{}, fmt.Errorf("malformed record constant")
		}
		for _, field := range record.fields {
			item, err := ConstantFromValue(field)
			if err != nil {
				return Constant{}, err
			}
			out.Items = append(out.Items, item)
		}
	}
	return out, nil
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
	case ArrayKind, DictKind, RecordKind:
		return c.container()
	default:
		return Value{}, fmt.Errorf("unknown constant type %q", c.Type)
	}
}

func (c Constant) container() (Value, error) {
	if c.Elem == nil || !c.Elem.IsConcrete() {
		return Value{}, fmt.Errorf("container constant is missing its type")
	}
	items := make([]Value, len(c.Items))
	for i, item := range c.Items {
		value, err := item.value()
		if err != nil {
			return Value{}, err
		}
		items[i] = value
	}
	switch c.Type {
	case ArrayKind:
		return Array(*c.Elem.Elem, items)
	case RecordKind:
		return Record(*c.Elem, items)
	default:
		if len(c.Keys) != len(items) {
			return Value{}, fmt.Errorf("dictionary constant has %d keys for %d values", len(c.Keys), len(items))
		}
		entries := make(map[string]Value, len(items))
		for i, key := range c.Keys {
			entries[key] = items[i]
		}
		return Dict(*c.Elem.Elem, entries)
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
