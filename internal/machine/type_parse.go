package machine

import (
	"errors"
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

// namedTypes are the types written as a bare name. Money, currency and fxrate
// among them refuse a currency parameter: a currency is the value's.
var namedTypes = map[string]Type{
	"bool": BoolType, "int": IntType, "float": FloatType, "string": StringType, "ratio": RatioType,
	"money": MoneyType, "currency": CurrencyType, "fxrate": FxRateType,
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

// peek skips space and reports whether the next byte is ch.
func (p *typeParser) peek(ch byte) bool {
	p.skipSpace()
	return p.i < len(p.s) && p.s[p.i] == ch
}

// eat consumes ch after any space and reports whether it was there.
func (p *typeParser) eat(ch byte) bool {
	if !p.peek(ch) {
		return false
	}
	p.i++
	return true
}

// word skips space and reads the longest run of bytes that ok accepts.
func (p *typeParser) word(ok func(byte) bool) string {
	p.skipSpace()
	start := p.i
	for p.i < len(p.s) && ok(p.s[p.i]) {
		p.i++
	}
	return p.s[start:p.i]
}

// list reads "item, item, ...}" after the opening brace; what names the items
// in the errors ("record field", "enum member"). A trailing comma is allowed.
func (p *typeParser) list(what string, item func() error) error {
	for n := 0; ; n++ {
		if p.eat('}') {
			if n == 0 {
				return fmt.Errorf("%s set cannot be empty", what)
			}
			return nil
		}
		if err := item(); err != nil {
			return err
		}
		if !p.peek(',') && !p.peek('}') {
			return fmt.Errorf("expected ',' or '}' in %s set", what)
		}
		p.eat(',')
	}
}

func (p *typeParser) parse() (Type, error) {
	name := p.word(isNameChar)
	if t, ok := namedTypes[name]; ok {
		if (t.kind == MoneyKind || t.kind == CurrencyKind || t.kind == FxRateKind) && p.peek('<') {
			return Type{}, fmt.Errorf("%s takes no currency: a currency is the value's, so the type is %s", name, name)
		}
		return t, nil
	}
	switch name {
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
	}
	if alias, ok := p.aliases[name]; ok {
		return alias, nil
	}
	return Type{}, fmt.Errorf("unknown type %q", name)
}

func isNameChar(ch byte) bool {
	return ch == '_' || (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9')
}

func (p *typeParser) parseEnum() (Type, error) {
	named, err := p.angled("enum", p.handleName)
	if err != nil {
		return Type{}, err
	}
	if !p.eat('{') {
		return Type{}, fmt.Errorf("enum<%s> requires a member set", named.name)
	}
	var values []string
	seen := map[string]bool{}
	err = p.list("enum member", func() error {
		value, err := p.member()
		if err != nil {
			return err
		}
		if seen[value] {
			return fmt.Errorf("duplicate enum member %q", value)
		}
		seen[value] = true
		values = append(values, value)
		return nil
	})
	if err != nil {
		return Type{}, err
	}
	return EnumOf(named.name, values...), nil
}

// parseRecord reads record{name: type, other: type}.
func (p *typeParser) parseRecord() (Type, error) {
	if !p.eat('{') {
		return Type{}, errors.New("record requires a field set")
	}
	var fields []Field
	err := p.list("record field", func() error {
		field, err := p.recordField()
		fields = append(fields, field)
		return err
	})
	if err != nil {
		return Type{}, err
	}
	return RecordOf(fields...), nil
}

func (p *typeParser) recordField() (Field, error) {
	name := p.word(isNameChar)
	if !IsValidFieldName(name) {
		return Field{}, fmt.Errorf("invalid record field name %q", name)
	}
	if !p.eat(':') {
		return Field{}, fmt.Errorf("record field %q needs a type after ':'", name)
	}
	typ, err := p.parse()
	if err != nil {
		return Field{}, err
	}
	return Field{name: name, typ: typ}, nil
}

// member reads one enum member. Members are identifiers, not quoted strings:
// a member is a name in the source (@adyen), and the runtime value is that
// same name, so the contract spells it the way an expression does.
func (p *typeParser) member() (string, error) {
	if p.peek('"') {
		return "", errors.New("enum members are identifiers: write enum<name>{adyen,stripe}")
	}
	value := p.word(isNameChar)
	if !IsValidFieldName(value) {
		return "", fmt.Errorf("invalid enum member %q", value)
	}
	if IsReservedName(value) {
		return "", fmt.Errorf("enum member %q is a reserved name", value)
	}
	return value, nil
}

// angled reads "<" inner ">" after a type constructor's name.
func (p *typeParser) angled(name string, inner func() (Type, error)) (Type, error) {
	if !p.eat('<') {
		return Type{}, fmt.Errorf("%s requires an element type", name)
	}
	t, err := inner()
	if err != nil {
		return Type{}, err
	}
	if !p.eat('>') {
		return Type{}, fmt.Errorf("missing > in %s type", name)
	}
	return t, nil
}

// handleName reads the host's name for a handle, which has the shape of a
// function name so it can carry a namespace and a version: onnx.tensor_v2.
func (p *typeParser) handleName() (Type, error) {
	name := p.word(func(ch byte) bool { return ch == '.' || isNameChar(ch) })
	if !IsValidFunctionName(name) {
		return Type{}, fmt.Errorf("invalid handle name %q", name)
	}
	return HandleOf(name), nil
}
