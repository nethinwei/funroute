package machine

import (
	"fmt"
	"strings"
)

// Kind is a runtime and compile-time type constructor: a small enum so values
// stay compact and comparisons are a byte test, serialised by name.
type Kind uint8

const (
	InvalidKind Kind = iota
	BoolKind
	IntKind
	FloatKind
	StringKind
	ArrayKind
	DictKind
	VarKind
	// HandleKind is an opaque host value: an engine's tensor, a session, a
	// prepared statement. The language can pass one along and nothing else.
	HandleKind
)

var kindNames = [...]string{"invalid", "bool", "int", "float", "string", "array", "dict", "var", "handle"}

func (k Kind) String() string {
	if int(k) < len(kindNames) {
		return kindNames[k]
	}
	return kindNames[InvalidKind]
}

func (k Kind) MarshalText() ([]byte, error) { return []byte(k.String()), nil }

func (k *Kind) UnmarshalText(text []byte) error {
	for i, name := range kindNames {
		if name == string(text) {
			*k = Kind(i)
			return nil
		}
	}
	return fmt.Errorf("unknown kind %q", text)
}

// Type is both a concrete type and, in function signatures, a named type
// variable. Dictionaries always have string keys, so Elem is the value type.
// Name is the variable's name, or a handle's: handle<onnx.tensor> and
// handle<onnx.session> are different types the language knows nothing about
// beyond their names.
type Type struct {
	Kind Kind   `json:"kind"`
	Elem *Type  `json:"elem,omitempty"`
	Name string `json:"name,omitempty"`
}

var (
	BoolType   = Type{Kind: BoolKind}
	IntType    = Type{Kind: IntKind}
	FloatType  = Type{Kind: FloatKind}
	StringType = Type{Kind: StringKind}
)

func ArrayOf(elem Type) Type { return Type{Kind: ArrayKind, Elem: typePtr(elem)} }
func DictOf(elem Type) Type  { return Type{Kind: DictKind, Elem: typePtr(elem)} }
func TypeVar(name string) Type {
	return Type{Kind: VarKind, Name: name}
}

// HandleOf is the type of an opaque host value. The name is the host's; two
// handles with different names never unify.
func HandleOf(name string) Type { return Type{Kind: HandleKind, Name: name} }

func typePtr(t Type) *Type { return &t }

// Clone deep-copies a type, so a caller that stores one cannot reach into the
// element type of another.
func CloneType(t Type) Type {
	out := t
	if t.Elem != nil {
		elem := CloneType(*t.Elem)
		out.Elem = &elem
	}
	return out
}

func (t Type) String() string {
	switch t.Kind {
	case ArrayKind, DictKind:
		if t.Elem == nil {
			return t.Kind.String() + "<?>"
		}
		return fmt.Sprintf("%s<%s>", t.Kind, t.Elem.String())
	case VarKind:
		if t.Name == "" {
			return "?"
		}
		return t.Name
	case HandleKind:
		return fmt.Sprintf("handle<%s>", t.Name)
	default:
		return t.Kind.String()
	}
}

func (t Type) IsConcrete() bool {
	if t.Kind == VarKind || t.Kind == InvalidKind {
		return false
	}
	if t.Kind == HandleKind {
		return t.Name != ""
	}
	if t.Kind == ArrayKind || t.Kind == DictKind {
		return t.Elem != nil && t.Elem.IsConcrete()
	}
	return true
}

func (t Type) Equal(other Type) bool {
	if t.Kind != other.Kind || t.Name != other.Name {
		return false
	}
	if t.Elem == nil || other.Elem == nil {
		return t.Elem == nil && other.Elem == nil
	}
	return t.Elem.Equal(*other.Elem)
}

// ParseType parses bool, int, float, string, array<T>, dict<T> and
// handle<name>.
func ParseType(input string) (Type, error) {
	p := &typeParser{s: strings.TrimSpace(input)}
	t, err := p.parse()
	if err != nil {
		return Type{}, err
	}
	p.skipSpace()
	if p.i != len(p.s) {
		return Type{}, fmt.Errorf("unexpected type suffix %q", p.s[p.i:])
	}
	if !t.IsConcrete() {
		return Type{}, fmt.Errorf("argument type must be concrete: %s", t)
	}
	return t, nil
}

type typeParser struct {
	s string
	i int
}

func (p *typeParser) skipSpace() {
	for p.i < len(p.s) && (p.s[p.i] == ' ' || p.s[p.i] == '\t' || p.s[p.i] == '\n') {
		p.i++
	}
}

func (p *typeParser) parse() (Type, error) {
	p.skipSpace()
	start := p.i
	for p.i < len(p.s) && ((p.s[p.i] >= 'a' && p.s[p.i] <= 'z') || p.s[p.i] == '_') {
		p.i++
	}
	name := p.s[start:p.i]
	switch name {
	case "bool":
		return BoolType, nil
	case "int":
		return IntType, nil
	case "float":
		return FloatType, nil
	case "string":
		return StringType, nil
	case "array", "dict":
		elem, err := p.angled(name, p.parse)
		if err != nil {
			return Type{}, err
		}
		if name == "array" {
			return ArrayOf(elem), nil
		}
		return DictOf(elem), nil
	case "handle":
		return p.angled(name, p.handleName)
	default:
		return Type{}, fmt.Errorf("unknown type %q", name)
	}
}

// angled reads "<" inner ">" after a type constructor's name.
func (p *typeParser) angled(name string, inner func() (Type, error)) (Type, error) {
	p.skipSpace()
	if p.i >= len(p.s) || p.s[p.i] != '<' {
		return Type{}, fmt.Errorf("%s requires an element type", name)
	}
	p.i++
	t, err := inner()
	if err != nil {
		return Type{}, err
	}
	p.skipSpace()
	if p.i >= len(p.s) || p.s[p.i] != '>' {
		return Type{}, fmt.Errorf("missing > in %s type", name)
	}
	p.i++
	return t, nil
}

// handleName reads the host's name for a handle, which has the shape of a
// function name so it can carry a namespace and a version: onnx.tensor_v2.
func (p *typeParser) handleName() (Type, error) {
	p.skipSpace()
	start := p.i
	for p.i < len(p.s) && (p.s[p.i] == '.' || p.s[p.i] == '_' || isAlnum(p.s[p.i])) {
		p.i++
	}
	name := p.s[start:p.i]
	if !IsValidFunctionName(name) {
		return Type{}, fmt.Errorf("invalid handle name %q", name)
	}
	return HandleOf(name), nil
}

func isAlnum(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}
