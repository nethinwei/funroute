package machine

import (
	"encoding/json"
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
	// MoneyKind is an amount of one currency, counted in its minor unit. The
	// currency is the value's, not the type's: money is one type, as Go's
	// Money is one struct, and the rules on currencies are checked where an
	// operation meets them.
	MoneyKind
	// RatioKind is a pure ratio, exact: a fee ratio, a markup, a share.
	RatioKind
	// FxRateKind is an exchange rate between two currencies, which are the
	// value's, as money's is.
	FxRateKind
	// CurrencyKind is a currency itself, drawn from the registry's declared
	// set: USD and EUR are its values.
	CurrencyKind
)

var kindNames = [...]string{"invalid", "bool", "int", "float", "string", "array", "dict", "var", "handle", "enum", "record",
	"money", "ratio", "fxrate", "currency"}

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
	kind   Kind
	elem   *Type
	name   string
	values []string
	// fields is a record's fields, in order. The order is the type: it decides
	// where a field sits in the value and which index a field access compiles
	// to, so two records with the same fields in a different order are two
	// types.
	fields []Field
}

// Field is one field of a record, made by FieldOf.
type Field struct {
	name string
	typ  Type
}

// Kind is what kind of type this is.
func (t Type) Kind() Kind { return t.kind }

// Name is a handle's or an enum's name, a type variable's, or money's and a
// currency's unit.
func (t Type) Name() string { return t.name }

// Elem is an array's or a dictionary's element type.
func (t Type) Elem() (Type, bool) {
	if t.elem == nil {
		return Type{}, false
	}
	return *t.elem, true
}

// Values is an enum's members, or an exchange rate's two units; a copy.
func (t Type) Values() []string { return slices.Clone(t.values) }

// Fields is a record's fields in their order; a copy.
func (t Type) Fields() []Field { return slices.Clone(t.fields) }

// FieldOf is a record field, for RecordOf.
func FieldOf(name string, typ Type) Field { return Field{name: name, typ: typ} }

// Name is the field's name.
func (f Field) Name() string { return f.name }

// Type is the field's type.
func (f Field) Type() Type { return f.typ }

// ScalarType is a type of a kind that has no element and no fields, from its
// parts: the compiler's way back from what it inferred. A host writes the
// constructors (IntType, EnumOf, HandleOf, ...) instead.
func ScalarType(kind Kind, name string, values []string) Type {
	return Type{kind: kind, name: name, values: slices.Clone(values)}
}

// A type's JSON is part of every artifact's digest; these keep the shape it
// had when its fields were public, key for key.
type typeJSON struct {
	Kind   Kind     `json:"kind"`
	Elem   *Type    `json:"elem,omitempty"`
	Name   string   `json:"name,omitempty"`
	Values []string `json:"values,omitempty"`
	Fields []Field  `json:"fields,omitempty"`
}

type fieldJSON struct {
	Name string `json:"name"`
	Type Type   `json:"type"`
}

func (t Type) MarshalJSON() ([]byte, error) {
	return json.Marshal(typeJSON{Kind: t.kind, Elem: t.elem, Name: t.name, Values: t.values, Fields: t.fields})
}

func (t *Type) UnmarshalJSON(data []byte) error {
	var shape typeJSON
	if err := json.Unmarshal(data, &shape); err != nil {
		return err
	}
	*t = Type{kind: shape.Kind, elem: shape.Elem, name: shape.Name, values: shape.Values, fields: shape.Fields}
	return nil
}

func (f Field) MarshalJSON() ([]byte, error) {
	return json.Marshal(fieldJSON{Name: f.name, Type: f.typ})
}

func (f *Field) UnmarshalJSON(data []byte) error {
	var shape fieldJSON
	if err := json.Unmarshal(data, &shape); err != nil {
		return err
	}
	*f = Field{name: shape.Name, typ: shape.Type}
	return nil
}

var (
	BoolType   = Type{kind: BoolKind}
	IntType    = Type{kind: IntKind}
	FloatType  = Type{kind: FloatKind}
	StringType = Type{kind: StringKind}
)

// RatioType is the one ratio type: a ratio has no unit to tell ratios apart.
var RatioType = Type{kind: RatioKind}

// MoneyType, CurrencyType and FxRateType are the money types. Each is one
// type: the currency is a value's, never its type's.
var (
	MoneyType    = Type{kind: MoneyKind}
	CurrencyType = Type{kind: CurrencyKind}
	FxRateType   = Type{kind: FxRateKind}
)

// IsUnitKind reports the kinds whose values carry a currency.
func IsUnitKind(kind Kind) bool {
	return kind == MoneyKind || kind == CurrencyKind || kind == FxRateKind
}

// IsMoneyKind reports every kind the money feature adds, ratio included: a
// registry that declares no currencies admits none of them.
func IsMoneyKind(kind Kind) bool { return IsUnitKind(kind) || kind == RatioKind }

// AnyEnumType is the wildcard an extension writes when it accepts a member of
// any enum. Enums come from the contract, so a signature cannot name one.
var AnyEnumType = Type{kind: EnumKind}

// IsAnyEnum reports whether t is that wildcard rather than a declared enum.
func IsAnyEnum(t Type) bool { return t.kind == EnumKind && t.name == "" && len(t.values) == 0 }

func ArrayOf(elem Type) Type { return Type{kind: ArrayKind, elem: new(elem)} }
func DictOf(elem Type) Type  { return Type{kind: DictKind, elem: new(elem)} }

// RecordOf builds a record type from fields in the order they are given.
func RecordOf(fields ...Field) Type {
	return Type{kind: RecordKind, fields: append([]Field(nil), fields...)}
}

func TypeVar(name string) Type {
	return Type{kind: VarKind, name: name}
}

// HandleOf is the type of an opaque host value. The name is the host's; two
// handles with different names never unify.
func HandleOf(name string) Type { return Type{kind: HandleKind, name: name} }

// EnumOf constructs a canonical named string enum. Declaration order has no
// semantic meaning, so members are sorted and duplicates collapsed.
func EnumOf(name string, values ...string) Type {
	members := append([]string(nil), values...)
	slices.Sort(members)
	members = slices.Compact(members)
	return Type{kind: EnumKind, name: name, values: members}
}

// cloneTypes is a copy of types that is never nil. A Type is immutable, so
// only the slice holding them needs copying.
func cloneTypes(types []Type) []Type { return append([]Type{}, types...) }

func (t Type) String() string {
	switch t.kind {
	case ArrayKind, DictKind:
		if t.elem == nil {
			return t.kind.String() + "<?>"
		}
		return fmt.Sprintf("%s<%s>", t.kind, t.elem.String())
	case VarKind:
		if t.name == "" {
			return "?"
		}
		return t.name
	case RecordKind:
		return "record{" + strings.Join(t.fieldTexts(Type.String), ", ") + "}"
	case HandleKind:
		return fmt.Sprintf("handle<%s>", t.name)
	case EnumKind:
		if t.name == "" {
			return "enum"
		}
		return fmt.Sprintf("enum<%s>{%s}", t.name, strings.Join(t.values, ","))
	default:
		return t.kind.String()
	}
}

// fieldTexts is each field as "name: type", the type written by text.
func (t Type) fieldTexts(text func(Type) string) []string {
	out := make([]string, len(t.fields))
	for i, field := range t.fields {
		out[i] = field.name + ": " + text(field.typ)
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
	if t.kind == RecordKind {
		return "record{" + strings.Join(t.fieldTexts(Type.Summary), ", ") + "}"
	}
	if t.elem != nil {
		return fmt.Sprintf("%s<%s>", t.kind, t.elem.Summary())
	}
	if t.kind != EnumKind || t.name == "" || len(t.values) <= enumSummaryLimit {
		return t.String()
	}
	return fmt.Sprintf("enum<%s>{%s… 共 %d 个}", t.name, strings.Join(t.values[:enumSummaryLimit], ","), len(t.values))
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
	if t.elem != nil {
		if err := WalkTypes(*t.elem, visit); err != nil {
			return err
		}
	}
	for _, field := range t.fields {
		if err := WalkTypes(field.typ, visit); err != nil {
			return err
		}
	}
	return nil
}

// TypeContains reports whether kind appears anywhere in t, fields included.
func TypeContains(t Type, kind Kind) bool {
	return typeHas(t, func(inner Kind) bool { return inner == kind })
}

// typeHas reports whether t or any type inside it is of a kind has accepts.
func typeHas(t Type, has func(Kind) bool) bool {
	found := false
	_ = WalkTypes(t, func(inner Type) error {
		found = found || has(inner.kind)
		return nil
	})
	return found
}

func (t Type) IsConcrete() bool {
	// A type read from JSON can name any kind and carry any part; only the
	// parts its kind has belong to it.
	if int(t.kind) >= len(kindNames) || t.kind == VarKind || t.kind == InvalidKind {
		return false
	}
	if (t.elem != nil) != (t.kind == ArrayKind || t.kind == DictKind) || (len(t.fields) > 0 && t.kind != RecordKind) {
		return false
	}
	if t.kind == HandleKind {
		return t.name != ""
	}
	if t.kind == EnumKind {
		if !IsValidFunctionName(t.name) || len(t.values) == 0 {
			return false
		}
		seen := make(map[string]bool, len(t.values))
		for _, value := range t.values {
			if seen[value] {
				return false
			}
			seen[value] = true
		}
		return true
	}
	if t.kind == ArrayKind || t.kind == DictKind {
		return t.elem != nil && t.elem.IsConcrete()
	}
	if t.kind == RecordKind {
		return t.fieldsAreConcrete()
	}
	return t.name == "" && len(t.values) == 0
}

func (t Type) fieldsAreConcrete() bool {
	if len(t.fields) == 0 {
		return false
	}
	seen := make(map[string]bool, len(t.fields))
	for _, field := range t.fields {
		if seen[field.name] || !IsValidFieldName(field.name) || !field.typ.IsConcrete() {
			return false
		}
		seen[field.name] = true
	}
	return true
}

// FieldIndex is where a field sits in a record, or -1. The compiler resolves a
// field access with it, so nothing looks a name up at run time.
func (t Type) FieldIndex(name string) int {
	for i, field := range t.fields {
		if field.name == name {
			return i
		}
	}
	return -1
}

func (t Type) Equal(other Type) bool {
	if t.kind != other.kind || t.name != other.name || !slices.Equal(t.values, other.values) {
		return false
	}
	if len(t.fields) != len(other.fields) {
		return false
	}
	for i, field := range t.fields {
		if field.name != other.fields[i].name || !field.typ.Equal(other.fields[i].typ) {
			return false
		}
	}
	if t.elem == nil || other.elem == nil {
		return t.elem == nil && other.elem == nil
	}
	return t.elem.Equal(*other.elem)
}
