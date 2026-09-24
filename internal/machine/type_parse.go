package machine

import (
	"fmt"
	"strings"
)

// ParseType parses bool, int, float, string, array<T>, dict<T>, handle<name>,
// enum<name>{member,...}, record{name: T,...}, and the money types: ratio,
// money, currency and fxrate.
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
	case "ratio":
		return RatioType, nil
	case "money", "currency", "fxrate":
		return p.parseMoneyType(name)
	default:
		if alias, ok := p.aliases[name]; ok {
			return CloneType(alias), nil
		}
		return Type{}, fmt.Errorf("unknown type %q", name)
	}
}

// parseMoneyType reads money, currency or fxrate: each is one whole type. A
// currency belongs to a value, so nothing follows the name; money<USD> is
// refused and says so.
func (p *typeParser) parseMoneyType(name string) (Type, error) {
	p.skipSpace()
	if p.i < len(p.s) && p.s[p.i] == '<' {
		return Type{}, fmt.Errorf("%s takes no currency: a currency is the value's, so the type is %s", name, name)
	}
	return map[string]Type{"money": MoneyType, "currency": CurrencyType, "fxrate": FxRateType}[name], nil
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
		return Type{}, fmt.Errorf("enum<%s> requires a member set", named.name)
	}
	p.i++
	values, err := p.enumMembers()
	if err != nil {
		return Type{}, err
	}
	return EnumOf(named.name, values...), nil
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
	return Field{name: name, typ: typ}, nil
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
	if !IsValidFieldName(value) {
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
