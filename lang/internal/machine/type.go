package machine

import (
	"fmt"
	"strings"
)

// Kind is a runtime and compile-time type constructor.
// Kind is a small enum so values stay compact and comparisons are a byte test.
// It still serialises as the same name it always had, so artifact digests and
// stored ExprJSON are unaffected.
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
)

var kindNames = [...]string{"invalid", "bool", "int", "float", "string", "array", "dict", "var"}

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
	default:
		return t.Kind.String()
	}
}

func (t Type) IsConcrete() bool {
	if t.Kind == VarKind || t.Kind == InvalidKind {
		return false
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

// ParseType parses bool, int, float, string, array<T>, and dict<T>.
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
		p.skipSpace()
		if p.i >= len(p.s) || p.s[p.i] != '<' {
			return Type{}, fmt.Errorf("%s requires an element type", name)
		}
		p.i++
		elem, err := p.parse()
		if err != nil {
			return Type{}, err
		}
		p.skipSpace()
		if p.i >= len(p.s) || p.s[p.i] != '>' {
			return Type{}, fmt.Errorf("missing > in %s type", name)
		}
		p.i++
		if name == "array" {
			return ArrayOf(elem), nil
		}
		return DictOf(elem), nil
	default:
		return Type{}, fmt.Errorf("unknown type %q", name)
	}
}
