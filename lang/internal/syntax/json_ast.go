package syntax

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"strconv"

	"funroute/lang/internal/machine"
)

// ExprJSON is the canonical exchange form of a program. Both directions are
// driven by the node plans in walk.go: a field is written or read because the
// node's struct declares it, so adding a node means declaring it and nothing
// else. Field order in the JSON is struct order, and sorted lists are sorted
// before they get here, so the same program always produces the same bytes —
// which is what the artifact digest relies on.

// ExprJSONVersion changes whenever the document shape does; a document with
// any other version is rejected rather than guessed at.
const ExprJSONVersion = 2

type exprJSONDocument struct {
	Version int             `json:"version"`
	Expr    json.RawMessage `json:"expr"`
}

// ExportExprJSON produces canonical, tagged JSON.
func ExportExprJSON(expr Expr) ([]byte, error) {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, `{"version":%d,"expr":`, ExprJSONVersion)
	if err := exportNode(&buf, expr); err != nil {
		return nil, err
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

func exportNode(buf *bytes.Buffer, expr Expr) error {
	if expr == nil {
		return fmt.Errorf("expression contains a nil node")
	}
	if literal, ok := expr.(*LiteralExpr); ok {
		return exportLiteral(buf, literal)
	}
	plan := planOf(expr)
	if plan == nil {
		return fmt.Errorf("unsupported expression node %T", expr)
	}
	fmt.Fprintf(buf, `{"node":%q`, expr.kind())
	if err := exportFields(buf, reflect.ValueOf(expr).Elem(), plan); err != nil {
		return err
	}
	buf.WriteByte('}')
	return nil
}

// Literals are scalars, so the public accessors cost nothing here: there is no
// container to copy. A float is written as text so 1.0 stays a float.
func exportLiteral(buf *bytes.Buffer, node *LiteralExpr) error {
	var encoded []byte
	var err error
	switch node.Value.Kind() {
	case machine.IntKind, machine.StringKind, machine.BoolKind:
		encoded, err = json.Marshal(node.Value.Any())
	case machine.FloatKind:
		number, _ := node.Value.Float()
		encoded, err = json.Marshal(strconv.FormatFloat(number, 'g', -1, 64))
	default:
		return fmt.Errorf("literal node has unsupported value type %s", node.Value.Type())
	}
	if err != nil {
		return err
	}
	fmt.Fprintf(buf, `{"node":%q,%q:%s}`, node.kind(), node.kind(), encoded)
	return nil
}

// exportFields writes a struct's tagged fields in declaration order, skipping
// an optional field that is absent.
func exportFields(buf *bytes.Buffer, value reflect.Value, plan *structPlan) error {
	for _, field := range plan.fields {
		current := value.Field(field.index)
		if field.optional && isAbsent(current) {
			continue
		}
		fmt.Fprintf(buf, `,%q:`, field.name)
		if err := exportField(buf, current, field); err != nil {
			return err
		}
	}
	return nil
}

func isAbsent(value reflect.Value) bool {
	switch value.Kind() {
	case reflect.Interface:
		return value.IsNil()
	case reflect.String, reflect.Slice:
		return value.Len() == 0
	default:
		return false
	}
}

func exportField(buf *bytes.Buffer, value reflect.Value, field fieldPlan) error {
	switch field.kind {
	case fieldExpr:
		return exportNode(buf, value.Interface().(Expr))
	case fieldName:
		encoded, _ := json.Marshal(value.String())
		buf.Write(encoded)
		return nil
	default:
		return exportList(buf, value, field)
	}
}

// exportList writes a []Expr as nodes, or a list of items as objects.
func exportList(buf *bytes.Buffer, list reflect.Value, field fieldPlan) error {
	buf.WriteByte('[')
	for i := 0; i < list.Len(); i++ {
		if i > 0 {
			buf.WriteByte(',')
		}
		if field.kind == fieldExprs {
			if err := exportNode(buf, list.Index(i).Interface().(Expr)); err != nil {
				return err
			}
			continue
		}
		buf.WriteByte('{')
		var item bytes.Buffer
		if err := exportFields(&item, list.Index(i), field.item); err != nil {
			return err
		}
		buf.Write(item.Bytes()[1:]) // drop the leading comma
		buf.WriteByte('}')
	}
	buf.WriteByte(']')
	return nil
}

// ImportExprJSON validates and imports a canonical expression document.
func ImportExprJSON(data []byte) (Expr, error) {
	var document exprJSONDocument
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&document); err != nil {
		return nil, fmt.Errorf("decode expression JSON: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("decode expression JSON: trailing JSON value")
		}
		return nil, fmt.Errorf("decode expression JSON: %w", err)
	}
	if document.Version != ExprJSONVersion {
		return nil, fmt.Errorf("unsupported expression JSON version %d", document.Version)
	}
	if len(document.Expr) == 0 {
		return nil, fmt.Errorf("expression JSON is missing expr")
	}
	importer := &importer{nextID: 1}
	return importer.node(document.Expr)
}

type importer struct {
	nextID int
}

// object is one decoded JSON object, with the keys still to be consumed.
type object map[string]json.RawMessage

func (o object) take(name string) (json.RawMessage, bool) {
	raw, ok := o[name]
	delete(o, name)
	return raw, ok
}

func (o object) unknown() error {
	for key := range o {
		return fmt.Errorf("unknown field %q", key)
	}
	return nil
}

func decodeObject(raw json.RawMessage) (object, error) {
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, fmt.Errorf("expression JSON contains a null node")
	}
	var fields object
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, fmt.Errorf("expression JSON node is not an object")
	}
	return fields, nil
}

func (m *importer) node(raw json.RawMessage) (Expr, error) {
	fields, err := decodeObject(raw)
	if err != nil {
		return nil, err
	}
	var kind string
	if tag, ok := fields.take("node"); !ok || json.Unmarshal(tag, &kind) != nil {
		return nil, fmt.Errorf("expression JSON node is missing its node tag")
	}
	typ, ok := byKind[kind]
	if !ok {
		return nil, fmt.Errorf("unknown expression node %q", kind)
	}
	id := m.nextID
	m.nextID++
	if typ == reflect.TypeOf(LiteralExpr{}) {
		return importLiteral(kind, id, fields)
	}
	node := reflect.New(typ)
	if err := m.fill(node.Elem(), fields, plans[typ], kind); err != nil {
		return nil, err
	}
	node.Elem().FieldByName("ID").SetInt(int64(id))
	expr, err := finish(node.Interface().(Expr))
	if err != nil {
		return nil, fmt.Errorf("%s node: %w", kind, err)
	}
	return expr, nil
}

func importLiteral(kind string, id int, fields object) (Expr, error) {
	raw, ok := fields.take(kind)
	if !ok {
		return nil, fmt.Errorf("%s node is missing %s", kind, kind)
	}
	if err := fields.unknown(); err != nil {
		return nil, fmt.Errorf("%s node: %w", kind, err)
	}
	value, err := literalValue(kind, raw)
	if err != nil {
		return nil, err
	}
	return &LiteralExpr{ID: id, Value: value}, nil
}

func literalValue(kind string, raw json.RawMessage) (machine.Value, error) {
	var value any
	switch kind {
	case "int":
		value = new(int64)
	case "float", "string":
		value = new(string)
	default:
		value = new(bool)
	}
	if err := json.Unmarshal(raw, value); err != nil {
		return machine.Value{}, fmt.Errorf("%s node has a malformed %s", kind, kind)
	}
	switch typed := value.(type) {
	case *int64:
		return machine.Int(*typed), nil
	case *bool:
		return machine.Bool(*typed), nil
	}
	text := *value.(*string)
	if kind == "string" {
		return machine.String(text), nil
	}
	parsed, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return machine.Value{}, fmt.Errorf("invalid float %q", text)
	}
	return machine.CheckedFloat(parsed)
}

// fill sets a struct's tagged fields from a decoded object and refuses what is
// missing, unknown or malformed. It serves nodes and list items alike.
func (m *importer) fill(target reflect.Value, fields object, plan *structPlan, context string) error {
	for _, field := range plan.fields {
		raw, present := fields.take(field.name)
		if !present {
			if field.optional {
				continue
			}
			return fmt.Errorf("%s node is missing %s", context, field.name)
		}
		if err := m.setField(target.Field(field.index), raw, field); err != nil {
			return fmt.Errorf("%s %s: %w", context, field.name, err)
		}
	}
	if err := fields.unknown(); err != nil {
		return fmt.Errorf("%s node: %w", context, err)
	}
	return nil
}

func (m *importer) setField(target reflect.Value, raw json.RawMessage, field fieldPlan) error {
	switch field.kind {
	case fieldExpr:
		child, err := m.node(raw)
		if err != nil {
			return err
		}
		target.Set(reflect.ValueOf(child))
		return nil
	case fieldName:
		return setName(target, raw, field)
	default:
		return m.setList(target, raw, field)
	}
}

func setName(target reflect.Value, raw json.RawMessage, field fieldPlan) error {
	var name string
	if err := json.Unmarshal(raw, &name); err != nil {
		return fmt.Errorf("must be a string")
	}
	if name == "" && field.optional {
		return nil
	}
	if err := validName(name, field.role); err != nil {
		return err
	}
	target.SetString(name)
	return nil
}

// validName is the single rule for what may appear in each kind of name slot.
func validName(name, role string) error {
	switch role {
	case "text":
		return nil
	case "fn":
		if !machine.IsValidFunctionName(name) {
			return fmt.Errorf("invalid function name %q", name)
		}
	case "var":
		if !machine.IsValidVariableName(name) || name == "true" || name == "false" {
			return fmt.Errorf("invalid variable name %q", name)
		}
	default:
		if !machine.IsValidVariableName(name) || machine.IsReservedName(name) {
			return fmt.Errorf("invalid local variable name %q", name)
		}
	}
	return nil
}

func (m *importer) setList(target reflect.Value, raw json.RawMessage, field fieldPlan) error {
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return fmt.Errorf("must be a list")
	}
	if len(items) < field.min {
		return fmt.Errorf("needs at least %d item(s)", field.min)
	}
	list := reflect.MakeSlice(target.Type(), len(items), len(items))
	for i, item := range items {
		if err := m.setItem(list.Index(i), item, field, i); err != nil {
			return err
		}
	}
	target.Set(list)
	return nil
}

func (m *importer) setItem(slot reflect.Value, raw json.RawMessage, field fieldPlan, index int) error {
	if field.kind == fieldExprs {
		child, err := m.node(raw)
		if err != nil {
			return fmt.Errorf("item %d: %w", index, err)
		}
		slot.Set(reflect.ValueOf(child))
		return nil
	}
	fields, err := decodeObject(raw)
	if err != nil {
		return fmt.Errorf("item %d: %w", index, err)
	}
	return m.fill(slot, fields, field.item, fmt.Sprintf("%s item %d", field.name, index))
}
