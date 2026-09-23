package machine

import (
	"fmt"
	"slices"
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
	// EnumKind is a named, closed set of string values declared by the host
	// contract. Runtime values stay ordinary strings; the type carries the
	// allowed set for boundary checks and switch exhaustiveness.
	EnumKind
	// RecordKind is a fixed set of named fields, each with its own type. The
	// field order is part of the type: values store their fields in it, so a
	// field access compiles to an index rather than a name lookup.
	RecordKind
)

var kindNames = [...]string{"invalid", "bool", "int", "float", "string", "array", "dict", "var", "handle", "enum", "record"}

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
	Kind   Kind     `json:"kind"`
	Elem   *Type    `json:"elem,omitempty"`
	Name   string   `json:"name,omitempty"`
	Values []string `json:"values,omitempty"`
	// Fields is a record's fields, in order. The order is the type: it decides
	// where a field sits in the value and which index a field access compiles
	// to, so two records with the same fields in a different order are two
	// types.
	Fields []Field `json:"fields,omitempty"`
}

// Field is one field of a record.
type Field struct {
	Name string `json:"name"`
	Type Type   `json:"type"`
}

var (
	BoolType   = Type{Kind: BoolKind}
	IntType    = Type{Kind: IntKind}
	FloatType  = Type{Kind: FloatKind}
	StringType = Type{Kind: StringKind}
)

// AnyEnumType is the wildcard an extension writes when it accepts a member of
// any enum. Enums come from the contract, so a signature cannot name one.
var AnyEnumType = Type{Kind: EnumKind}

// IsAnyEnum reports whether t is that wildcard rather than a declared enum.
func IsAnyEnum(t Type) bool { return t.Kind == EnumKind && t.Name == "" && len(t.Values) == 0 }

func ArrayOf(elem Type) Type { return Type{Kind: ArrayKind, Elem: new(elem)} }
func DictOf(elem Type) Type  { return Type{Kind: DictKind, Elem: new(elem)} }

// RecordOf builds a record type from fields in the order they are given.
func RecordOf(fields ...Field) Type {
	return Type{Kind: RecordKind, Fields: append([]Field(nil), fields...)}
}

func TypeVar(name string) Type {
	return Type{Kind: VarKind, Name: name}
}

// HandleOf is the type of an opaque host value. The name is the host's; two
// handles with different names never unify.
func HandleOf(name string) Type { return Type{Kind: HandleKind, Name: name} }

// EnumOf constructs a canonical named string enum. Declaration order has no
// semantic meaning, so members are sorted and duplicates collapsed.
func EnumOf(name string, values ...string) Type {
	members := append([]string(nil), values...)
	slices.Sort(members)
	members = slices.Compact(members)
	return Type{Kind: EnumKind, Name: name, Values: members}
}

// CloneType deep-copies a type, so a caller that stores one cannot reach into
// the element type of another.
func CloneType(t Type) Type {
	out := t
	if t.Elem != nil {
		elem := CloneType(*t.Elem)
		out.Elem = &elem
	}
	out.Values = append([]string(nil), t.Values...)
	if t.Fields != nil {
		out.Fields = make([]Field, len(t.Fields))
		for i, field := range t.Fields {
			out.Fields[i] = Field{Name: field.Name, Type: CloneType(field.Type)}
		}
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
	case RecordKind:
		return "record{" + strings.Join(t.fieldTexts(false), ", ") + "}"
	case HandleKind:
		return fmt.Sprintf("handle<%s>", t.Name)
	case EnumKind:
		if t.Name == "" {
			return "enum"
		}
		return fmt.Sprintf("enum<%s>{%s}", t.Name, strings.Join(t.Values, ","))
	default:
		return t.Kind.String()
	}
}

func (t Type) fieldTexts(summary bool) []string {
	out := make([]string, len(t.Fields))
	for i, field := range t.Fields {
		if summary {
			out[i] = field.Name + ": " + field.Type.Summary()
			continue
		}
		out[i] = field.Name + ": " + field.Type.String()
	}
	return out
}

// enumSummaryLimit is how many members an error message spells out. A contract
// may declare hundreds (every country, every currency); a person reading the
// error needs the enum's name and a sample, not the whole set.
const enumSummaryLimit = 6

// Summary is String for a message. String stays complete because ParseType has
// to read it back; Summary is only ever shown.
func (t Type) Summary() string {
	if t.Kind == RecordKind {
		return "record{" + strings.Join(t.fieldTexts(true), ", ") + "}"
	}
	if t.Elem != nil {
		return fmt.Sprintf("%s<%s>", t.Kind, t.Elem.Summary())
	}
	if t.Kind != EnumKind || t.Name == "" || len(t.Values) <= enumSummaryLimit {
		return t.String()
	}
	return fmt.Sprintf("enum<%s>{%s… 共 %d 个}", t.Name, strings.Join(t.Values[:enumSummaryLimit], ","), len(t.Values))
}

// WalkTypes visits t and every type inside it: a container's element and a
// record's fields. It exists because "is there an X anywhere in this type" was
// answered by four hand-written recursions, and the two that predated records
// only followed Elem — so an enum inside a record field was invisible to the
// contract. A question about a type's contents asks this, not its own switch.
func WalkTypes(t Type, visit func(Type) error) error {
	if err := visit(t); err != nil {
		return err
	}
	if t.Elem != nil {
		if err := WalkTypes(*t.Elem, visit); err != nil {
			return err
		}
	}
	for _, field := range t.Fields {
		if err := WalkTypes(field.Type, visit); err != nil {
			return err
		}
	}
	return nil
}

// TypeContains reports whether kind appears anywhere in t, fields included.
func TypeContains(t Type, kind Kind) bool {
	found := false
	_ = WalkTypes(t, func(inner Type) error {
		found = found || inner.Kind == kind
		return nil
	})
	return found
}

func (t Type) IsConcrete() bool {
	if t.Kind == VarKind || t.Kind == InvalidKind {
		return false
	}
	if t.Kind == HandleKind {
		return t.Name != ""
	}
	if t.Kind == EnumKind {
		if !IsValidFunctionName(t.Name) || len(t.Values) == 0 {
			return false
		}
		seen := make(map[string]bool, len(t.Values))
		for _, value := range t.Values {
			if seen[value] {
				return false
			}
			seen[value] = true
		}
		return true
	}
	if t.Kind == ArrayKind || t.Kind == DictKind {
		return t.Elem != nil && t.Elem.IsConcrete()
	}
	if t.Kind == RecordKind {
		return t.fieldsAreConcrete()
	}
	return true
}

func (t Type) fieldsAreConcrete() bool {
	if len(t.Fields) == 0 {
		return false
	}
	seen := make(map[string]bool, len(t.Fields))
	for _, field := range t.Fields {
		if seen[field.Name] || !IsValidVariableName(field.Name) || !field.Type.IsConcrete() {
			return false
		}
		seen[field.Name] = true
	}
	return true
}

// FieldIndex is where a field sits in a record, or -1. The compiler resolves a
// field access with it, so nothing looks a name up at run time.
func (t Type) FieldIndex(name string) int {
	for i, field := range t.Fields {
		if field.Name == name {
			return i
		}
	}
	return -1
}

func (t Type) Equal(other Type) bool {
	if t.Kind != other.Kind || t.Name != other.Name || !slices.Equal(t.Values, other.Values) {
		return false
	}
	if len(t.Fields) != len(other.Fields) {
		return false
	}
	for i, field := range t.Fields {
		if field.Name != other.Fields[i].Name || !field.Type.Equal(other.Fields[i].Type) {
			return false
		}
	}
	if t.Elem == nil || other.Elem == nil {
		return t.Elem == nil && other.Elem == nil
	}
	return t.Elem.Equal(*other.Elem)
}

// ParseType parses bool, int, float, string, array<T>, dict<T>, handle<name>,
// and enum<name>{"member",...}.
func ParseType(input string) (Type, error) {
	return ParseTypeWith(input, nil)
}

// ParseTypeWith parses a type that may name one of the given aliases. A text
// contract otherwise repeats a record's fields for every argument that has
// that shape; a Go host just reuses the Type it built. Aliases do not nest:
// one may not be written in terms of another, which keeps this a spelling
// convenience rather than a second type system.
func ParseTypeWith(input string, aliases map[string]Type) (Type, error) {
	p := &typeParser{s: strings.TrimSpace(input), aliases: aliases}
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
	s       string
	i       int
	aliases map[string]Type
}

func (p *typeParser) skipSpace() {
	for p.i < len(p.s) && (p.s[p.i] == ' ' || p.s[p.i] == '\t' || p.s[p.i] == '\n') {
		p.i++
	}
}

func (p *typeParser) parse() (Type, error) {
	p.skipSpace()
	start := p.i
	for p.i < len(p.s) && isTypeNameChar(p.s[p.i]) {
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
	case "enum":
		return p.parseEnum()
	case "record":
		return p.parseRecord()
	default:
		if alias, ok := p.aliases[name]; ok {
			return CloneType(alias), nil
		}
		return Type{}, fmt.Errorf("unknown type %q", name)
	}
}

func isTypeNameChar(ch byte) bool {
	return ch == '_' || (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9')
}

func (p *typeParser) parseEnum() (Type, error) {
	named, err := p.angled("enum", p.handleName)
	if err != nil {
		return Type{}, err
	}
	p.skipSpace()
	if p.i >= len(p.s) || p.s[p.i] != '{' {
		return Type{}, fmt.Errorf("enum<%s> requires a member set", named.Name)
	}
	p.i++
	values, err := p.enumMembers()
	if err != nil {
		return Type{}, err
	}
	return EnumOf(named.Name, values...), nil
}

// parseRecord reads record{name: type, other: type}.
func (p *typeParser) parseRecord() (Type, error) {
	p.skipSpace()
	if p.i >= len(p.s) || p.s[p.i] != '{' {
		return Type{}, fmt.Errorf("record requires a field set")
	}
	p.i++
	var fields []Field
	for {
		p.skipSpace()
		if p.i < len(p.s) && p.s[p.i] == '}' {
			p.i++
			if len(fields) == 0 {
				return Type{}, fmt.Errorf("record field set cannot be empty")
			}
			return RecordOf(fields...), nil
		}
		field, err := p.recordField()
		if err != nil {
			return Type{}, err
		}
		fields = append(fields, field)
		p.skipSpace()
		if p.i >= len(p.s) || (p.s[p.i] != ',' && p.s[p.i] != '}') {
			return Type{}, fmt.Errorf("expected ',' or '}' in record field set")
		}
		if p.s[p.i] == ',' {
			p.i++
		}
	}
}

func (p *typeParser) recordField() (Field, error) {
	name, err := p.fieldName()
	if err != nil {
		return Field{}, err
	}
	p.skipSpace()
	if p.i >= len(p.s) || p.s[p.i] != ':' {
		return Field{}, fmt.Errorf("record field %q needs a type after ':'", name)
	}
	p.i++
	typ, err := p.parse()
	if err != nil {
		return Field{}, err
	}
	return Field{Name: name, Type: typ}, nil
}

func (p *typeParser) fieldName() (string, error) {
	p.skipSpace()
	start := p.i
	for p.i < len(p.s) && isMemberChar(p.s[p.i]) {
		p.i++
	}
	name := p.s[start:p.i]
	if !IsValidFieldName(name) {
		return "", fmt.Errorf("invalid record field name %q", name)
	}
	return name, nil
}

func (p *typeParser) enumMembers() ([]string, error) {
	var values []string
	seen := map[string]bool{}
	for {
		p.skipSpace()
		if p.i < len(p.s) && p.s[p.i] == '}' {
			p.i++
			if len(values) == 0 {
				return nil, fmt.Errorf("enum member set cannot be empty")
			}
			return values, nil
		}
		value, err := p.member()
		if err != nil {
			return nil, err
		}
		if seen[value] {
			return nil, fmt.Errorf("duplicate enum member %q", value)
		}
		seen[value] = true
		values = append(values, value)
		p.skipSpace()
		if p.i >= len(p.s) || (p.s[p.i] != ',' && p.s[p.i] != '}') {
			return nil, fmt.Errorf("expected ',' or '}' in enum member set")
		}
		if p.s[p.i] == ',' {
			p.i++
		}
	}
}

// member reads one enum member. Members are identifiers, not quoted strings:
// a member is a name in the source (@adyen), and the runtime value is that
// same name, so the contract spells it the way an expression does.
func (p *typeParser) member() (string, error) {
	p.skipSpace()
	if p.i < len(p.s) && p.s[p.i] == '"' {
		return "", fmt.Errorf("enum members are identifiers: write enum<name>{adyen,stripe}")
	}
	start := p.i
	for p.i < len(p.s) && isMemberChar(p.s[p.i]) {
		p.i++
	}
	value := p.s[start:p.i]
	if !IsValidVariableName(value) {
		return "", fmt.Errorf("invalid enum member %q", value)
	}
	if IsReservedName(value) {
		return "", fmt.Errorf("enum member %q is a reserved name", value)
	}
	return value, nil
}

func isMemberChar(ch byte) bool {
	return ch == '_' || (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9')
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
