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
	// type's Name is its unit: a currency code (money<USD>), a contract's
	// currency variable (money<c>), or empty when the currency is only known
	// at run time (money<?>). The value always carries its currency.
	MoneyKind
	// RateKind is a pure ratio in fixed point, 1e-10 per unit: a fee rate, a
	// markup, a share.
	RateKind
	// FxRateKind is an exchange rate: one unit of Values[0] buys this many of
	// Values[1]. Either side may be a code, a variable or unknown (""). A rule
	// makes one and hands it to using; it has no arithmetic.
	FxRateKind
	// CurrencyKind is a currency itself, drawn from the registry's declared
	// set. Its Name is the unit it stands for, like money's.
	CurrencyKind
)

var kindNames = [...]string{"invalid", "bool", "int", "float", "string", "array", "dict", "var", "handle", "enum", "record",
	"money", "rate", "fxrate", "currency"}

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

// RateType is the one rate type: a ratio has no unit to tell rates apart.
var RateType = Type{kind: RateKind}

// MoneyOf is money in a unit: a currency code, a currency variable, or ""
// for a currency only known at run time.
func MoneyOf(unit string) Type { return Type{kind: MoneyKind, name: unit} }

// CurrencyOf is the type of a currency value standing for unit.
func CurrencyOf(unit string) Type { return Type{kind: CurrencyKind, name: unit} }

// FxRateOf is the rate that turns base into quote.
func FxRateOf(base, quote string) Type { return Type{kind: FxRateKind, values: []string{base, quote}} }

// IsUnitKind reports the kinds that carry a currency unit.
func IsUnitKind(kind Kind) bool {
	return kind == MoneyKind || kind == CurrencyKind || kind == FxRateKind
}

// IsMoneyKind reports every kind the money feature adds, rate included: a
// registry that declares no currencies admits none of them.
func IsMoneyKind(kind Kind) bool { return IsUnitKind(kind) || kind == RateKind }

// IsCurrencyCode reports whether name has the shape of a currency code: an
// upper-case letter, then two to seven upper-case letters or digits. It is
// the one rule; the type parser, the source parser and the registry use it.
func IsCurrencyCode(name string) bool {
	if len(name) < 3 || len(name) > 8 || name[0] < 'A' || name[0] > 'Z' {
		return false
	}
	for i := 1; i < len(name); i++ {
		if (name[i] < 'A' || name[i] > 'Z') && (name[i] < '0' || name[i] > '9') {
			return false
		}
	}
	return true
}

// IsUnitVariable reports a currency variable: a lower-case name, so a code a
// registry adds later can never collide with one.
func IsUnitVariable(name string) bool {
	return name != "" && name[0] >= 'a' && name[0] <= 'z' && IsValidVariableName(name)
}

// isUnit accepts the three spellings of a unit.
func isUnit(name string) bool { return name == "" || IsCurrencyCode(name) || IsUnitVariable(name) }

// Units lists the units a type states, in order: money's and currency's
// Name, an exchange rate's two sides.
func (t Type) Units() []string {
	switch t.kind {
	case MoneyKind, CurrencyKind:
		return []string{t.name}
	case FxRateKind:
		return t.values
	default:
		return nil
	}
}

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

// CloneType deep-copies a type, so a caller that stores one cannot reach into
// the element type of another.
func CloneType(t Type) Type {
	out := t
	if t.elem != nil {
		elem := CloneType(*t.elem)
		out.elem = &elem
	}
	out.values = append([]string(nil), t.values...)
	if t.fields != nil {
		out.fields = make([]Field, len(t.fields))
		for i, field := range t.fields {
			out.fields[i] = Field{name: field.name, typ: CloneType(field.typ)}
		}
	}
	return out
}

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
		return "record{" + strings.Join(t.fieldTexts(false), ", ") + "}"
	case HandleKind:
		return fmt.Sprintf("handle<%s>", t.name)
	case EnumKind:
		if t.name == "" {
			return "enum"
		}
		return fmt.Sprintf("enum<%s>{%s}", t.name, strings.Join(t.values, ","))
	case MoneyKind, CurrencyKind, FxRateKind:
		return t.unitString()
	default:
		return t.kind.String()
	}
}

// unitString spells a unit-carrying type: money<?>, money<USD>,
// fxrate<?,USD>. An unknown unit is written ?, never left out.
func (t Type) unitString() string {
	units := t.Units()
	spelled := make([]string, len(units))
	for i, unit := range units {
		spelled[i] = unit
		if unit == "" {
			spelled[i] = "?"
		}
	}
	return t.kind.String() + "<" + strings.Join(spelled, ",") + ">"
}

func (t Type) fieldTexts(summary bool) []string {
	out := make([]string, len(t.fields))
	for i, field := range t.fields {
		if summary {
			out[i] = field.name + ": " + field.typ.Summary()
			continue
		}
		out[i] = field.name + ": " + field.typ.String()
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
		return "record{" + strings.Join(t.fieldTexts(true), ", ") + "}"
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
	found := false
	_ = WalkTypes(t, func(inner Type) error {
		found = found || inner.kind == kind
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
	return t.unitsAreWellFormed()
}

// unitsAreWellFormed checks a unit-carrying type's units, and that nothing
// else carries one. Any mix of codes, variables and unknowns is a type.
func (t Type) unitsAreWellFormed() bool {
	switch t.kind {
	case MoneyKind, CurrencyKind:
		return isUnit(t.name) && len(t.values) == 0
	case FxRateKind:
		return t.name == "" && len(t.values) == 2 && isUnit(t.values[0]) && isUnit(t.values[1])
	default:
		return t.name == "" && len(t.values) == 0
	}
}

// SameShape is Equal with the currency units left out. The machine compares
// values by it: which currency a value is in is the currency checks' question,
// and a record or array built under money<c> still holds money.
func SameShape(a, b Type) bool {
	if a.kind != b.kind {
		return false
	}
	if !IsUnitKind(a.kind) && (a.name != b.name || !slices.Equal(a.values, b.values)) {
		return false
	}
	if len(a.fields) != len(b.fields) {
		return false
	}
	for i, field := range a.fields {
		if field.name != b.fields[i].name || !SameShape(field.typ, b.fields[i].typ) {
			return false
		}
	}
	if a.elem == nil || b.elem == nil {
		return a.elem == nil && b.elem == nil
	}
	return SameShape(*a.elem, *b.elem)
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
