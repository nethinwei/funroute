package machine

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
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
	OpCurrencyCheck
	OpFxPush
	OpFxPop
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
	case MoneyKind, RateKind, FxRateKind, CurrencyKind:
		return moneyConstant(value), nil
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
	case MoneyKind, RateKind, FxRateKind, CurrencyKind:
		return c.moneyValue()
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
		return Array(*c.Elem.elem, items)
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
		return Dict(*c.Elem.elem, entries)
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
	ResultDoc string          `json:"result_doc,omitempty"`
	Constants []Constant      `json:"constants,omitempty"`
	Calls     []CallReference `json:"calls,omitempty"`
	Locals    int             `json:"locals,omitempty"`
	// MaxStack is the deepest the operand stack gets, computed while
	// compiling. The frame reserves it once, so pushing never has to check.
	MaxStack     int           `json:"max_stack"`
	Instructions []Instruction `json:"instructions"`
	// Money is the money feature the artifact was compiled against, present
	// only when the program uses a money type: a program without money
	// digests exactly as it did before the feature existed.
	Money *MoneyStamp `json:"money,omitempty"`
	// RateTables are the named rate tables the contract declares, in its
	// order: what using(@name, …) may name and RunOptions.RateTables fills.
	RateTables []string `json:"rate_tables,omitempty"`
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
		stamp := table.stampFor(moneyCodes(artifact))
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

// RateTables is the named rate tables the contract declares; a copy.
func (a *Artifact) RateTables() []string { return slices.Clone(a.parts.RateTables) }

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
	copyArtifact.Args = make([]Parameter, len(artifact.parts.Args))
	for i, param := range artifact.parts.Args {
		param.doc = ""
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
