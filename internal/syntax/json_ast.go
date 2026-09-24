package syntax

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"reflect"
	"slices"
	"strconv"

	"github.com/nethinwei/funroute/internal/machine"
)

// ExprJSON is the canonical exchange form of a program. Both directions are
// driven by the node plans in walk.go: a field is written or read because the
// node's struct declares it, so adding a node means declaring it and nothing
// else. Field order in the JSON is struct order, and sorted lists are sorted
// before they get here, so the same program always produces the same bytes —
// which is what the artifact digest relies on.

// ExprJSONVersion names the shape of the document; one with any other version
// is rejected rather than guessed at.
const ExprJSONVersion = 1

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
		return errors.New("expression contains a nil node")
	}
	if literal, ok := expr.(*LiteralExpr); ok {
		return exportLiteral(buf, literal)
	}
	plan := planOf(expr)
	if plan == nil {
		return fmt.Errorf("unsupported expression node %T", expr)
	}
	fmt.Fprintf(buf, `{"node":%q`, plan.kind)
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
		encoded, err = json.Marshal(node.Decimal.String())
	default:
		return fmt.Errorf("literal node has unsupported value type %s", node.Value.Type())
	}
	if err != nil {
		return err
	}
	kind := node.Value.Kind().String()
	fmt.Fprintf(buf, `{"node":%q,%q:%s}`, kind, kind, encoded)
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
	case reflect.Bool:
		return !value.Bool()
	default:
		return false
	}
}

func exportField(buf *bytes.Buffer, value reflect.Value, field fieldPlan) error {
	switch field.kind {
	case fieldExpr:
		return exportNode(buf, heldExpr(value))
	case fieldName:
		encoded, _ := json.Marshal(value.String())
		buf.Write(encoded)
		return nil
	case fieldFlag:
		encoded, _ := json.Marshal(value.Bool())
		buf.Write(encoded)
		return nil
	default:
		return exportList(buf, value, field)
	}
}

// exportList writes a []Expr as nodes, or a list of items as objects.
func exportList(buf *bytes.Buffer, list reflect.Value, field fieldPlan) error {
	buf.WriteByte('[')
	for i := range list.Len() {
		if i > 0 {
			buf.WriteByte(',')
		}
		if field.kind == fieldExprs {
			if err := exportNode(buf, heldExpr(list.Index(i))); err != nil {
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

// ImportExprJSON validates and imports a canonical expression document. The
// document is decoded once, into a tree of maps, slices and scalars, and the
// importer walks that tree: a node never re-reads the bytes under it, so a
// deep document costs what its size does.
func ImportExprJSON(data []byte) (Expr, error) {
	document, err := decodeDocument(data)
	if err != nil {
		return nil, err
	}
	if document.version != ExprJSONVersion {
		return nil, fmt.Errorf("unsupported expression JSON version %d", document.version)
	}
	if !document.hasExpr {
		return nil, errors.New("expression JSON is missing expr")
	}
	importer := &importer{nextID: 1}
	return importer.node(document.expr)
}

// exprJSONDocument is the {version, expr} document, decoded.
type exprJSONDocument struct {
	version int
	expr    any
	hasExpr bool
}

// decodeDocument reads the {version, expr} document, one JSON value and
// nothing after it. Its keys are matched exactly, as a node's are:
// encoding/json matches a struct's fields regardless of case, and would let
// "VERSION" through. Numbers stay their text (UseNumber), so an int literal
// is read exactly, never through a float64.
func decodeDocument(data []byte) (exprJSONDocument, error) {
	var document exprJSONDocument
	var fields map[string]any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&fields); err != nil {
		return document, fmt.Errorf("decode expression JSON: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return document, errors.New("decode expression JSON: trailing JSON value")
		}
		return document, fmt.Errorf("decode expression JSON: %w", err)
	}
	for _, key := range slices.Sorted(maps.Keys(fields)) {
		if key != "version" && key != "expr" {
			return document, fmt.Errorf("decode expression JSON: unknown field %q", key)
		}
	}
	if version, ok := fields["version"]; ok {
		number, isNumber := version.(json.Number)
		parsed, err := strconv.Atoi(string(number))
		if !isNumber || err != nil {
			return document, errors.New("decode expression JSON: version must be an integer")
		}
		document.version = parsed
	}
	document.expr, document.hasExpr = fields["expr"]
	return document, nil
}

type importer struct {
	nextID int
	depth  int // how many nodes the importer is inside, held to maxNesting
}

// object is one decoded JSON object, with the keys still to be consumed.
type object map[string]any

func (o object) take(name string) (any, bool) {
	value, ok := o[name]
	delete(o, name)
	return value, ok
}

func (o object) unknown() error {
	for key := range o {
		return fmt.Errorf("unknown field %q", key)
	}
	return nil
}

func decodeObject(value any) (object, error) {
	if value == nil {
		return nil, errors.New("expression JSON contains a null node")
	}
	fields, ok := value.(map[string]any)
	if !ok {
		return nil, errors.New("expression JSON node is not an object")
	}
	return fields, nil
}

func (m *importer) node(value any) (Expr, error) {
	if m.depth == maxNesting {
		return nil, fmt.Errorf("expression JSON nests deeper than %d levels", maxNesting)
	}
	m.depth++
	defer func() { m.depth-- }()
	fields, err := decodeObject(value)
	if err != nil {
		return nil, err
	}
	tag, _ := fields.take("node")
	kind, ok := tag.(string)
	if !ok {
		return nil, errors.New("expression JSON node is missing its node tag")
	}
	typ, ok := byKind[kind]
	if !ok {
		return nil, fmt.Errorf("unknown expression node %q", kind)
	}
	id := m.nextID
	m.nextID++
	if typ == reflect.TypeFor[LiteralExpr]() {
		return importLiteral(kind, id, fields)
	}
	node := reflect.New(typ)
	plan := plans[typ]
	if err := m.fill(node.Elem(), fields, plan, kind); err != nil {
		return nil, err
	}
	node.Elem().FieldByIndex(plan.id).SetInt(int64(id))
	expr, err := finish(heldExpr(node))
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
	literal, err := literalValue(kind, raw)
	if err != nil {
		return nil, err
	}
	literal.ID = id
	return literal, nil
}

// literalValue reads a literal: an int is a JSON number that is an int64
// exactly, a float is the decimal a JSON string writes, a string is a JSON
// string and a bool a bool.
func literalValue(kind string, raw any) (*LiteralExpr, error) {
	malformed := fmt.Errorf("%s node has a malformed %s", kind, kind)
	switch kind {
	case "int":
		number, ok := raw.(json.Number)
		parsed, err := strconv.ParseInt(string(number), 10, 64)
		if !ok || err != nil {
			return nil, malformed
		}
		return &LiteralExpr{Value: machine.Int(parsed)}, nil
	case "bool":
		flag, ok := raw.(bool)
		if !ok {
			return nil, malformed
		}
		return &LiteralExpr{Value: machine.Bool(flag)}, nil
	}
	text, ok := raw.(string)
	switch {
	case !ok:
		return nil, malformed
	case kind == "string":
		return &LiteralExpr{Value: machine.String(text)}, nil
	}
	return decimalLiteral(text)
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

func (m *importer) setField(target reflect.Value, raw any, field fieldPlan) error {
	switch field.kind {
	case fieldExpr:
		return m.setExpr(target, raw)
	case fieldName:
		return setName(target, raw, field)
	case fieldFlag:
		return setFlag(target, raw, field)
	default:
		return m.setList(target, raw, field)
	}
}

// setFlag reads a node's boolean mode. It has no children and binds nothing,
// so this is the whole of what the walk does with it.
func setFlag(target reflect.Value, raw any, field fieldPlan) error {
	flag, ok := raw.(bool)
	if !ok {
		return fmt.Errorf("field %q must be a boolean", field.name)
	}
	target.SetBool(flag)
	return nil
}

func setName(target reflect.Value, raw any, field fieldPlan) error {
	name, ok := raw.(string)
	if !ok {
		return errors.New("must be a string")
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
		// A call named let or switch would print as that form, which is a
		// different program: the names the syntax has taken are nobody's.
		if !machine.IsValidFunctionName(name) || machine.IsReservedName(name) {
			return fmt.Errorf("invalid function name %q", name)
		}
	case "var":
		if !machine.IsValidVariableName(name) || machine.IsReservedName(name) {
			return fmt.Errorf("invalid variable name %q", name)
		}
	default:
		if !machine.IsValidVariableName(name) || machine.IsReservedName(name) {
			return fmt.Errorf("invalid local variable name %q", name)
		}
	}
	return nil
}

func (m *importer) setList(target reflect.Value, raw any, field fieldPlan) error {
	items, ok := raw.([]any)
	if !ok {
		return errors.New("must be a list")
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

func (m *importer) setItem(slot reflect.Value, raw any, field fieldPlan, index int) error {
	if field.kind == fieldExprs {
		if err := m.setExpr(slot, raw); err != nil {
			return fmt.Errorf("item %d: %w", index, err)
		}
		return nil
	}
	fields, err := decodeObject(raw)
	if err != nil {
		return fmt.Errorf("item %d: %w", index, err)
	}
	return m.fill(slot, fields, field.item, fmt.Sprintf("%s item %d", field.name, index))
}

// setExpr imports the node raw into slot, an Expr field or list element.
func (m *importer) setExpr(slot reflect.Value, raw any) error {
	child, err := m.node(raw)
	if err != nil {
		return err
	}
	slot.Set(reflect.ValueOf(child))
	return nil
}
