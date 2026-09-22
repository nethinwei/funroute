package syntax

import (
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"funroute/lang/internal/machine"
)

// The walker reads the node definitions in ast.go once, through reflection,
// and turns them into plans. Exporting, importing, scope-aware traversal and
// the front-end schema are all written against the plans, so a node is
// described exactly once — in its struct — and nothing here names a node.
//
// Reflection runs at compile time only, never while a program executes.

type fieldKind uint8

const (
	fieldExpr  fieldKind = iota // Expr
	fieldExprs                  // []Expr
	fieldName                   // string
	fieldList                   // []struct with its own plan
	fieldFlag                   // bool: a mode of the node, never a child
)

// fieldPlan is one tagged struct field.
type fieldPlan struct {
	index    int
	name     string
	kind     fieldKind
	optional bool
	role     string
	binds    []string
	deflt    string
	min      int
	sortKey  string
	item     *structPlan
}

// structPlan is a node type or a list item type. binds says whether anything
// in it, or in its list items, binds a name — most nodes do not, and the walk
// skips the bookkeeping for them.
type structPlan struct {
	typ    reflect.Type
	fields []fieldPlan
	binds  bool
}

var (
	plans   = map[reflect.Type]*structPlan{}
	byKind  = map[string]reflect.Type{}
	exprTyp = reflect.TypeOf((*Expr)(nil)).Elem()
)

func init() {
	for _, node := range nodeTypes {
		typ := reflect.TypeOf(node).Elem()
		if _, literal := node.(*LiteralExpr); literal {
			for _, kind := range []string{"int", "float", "string", "bool"} {
				byKind[kind] = typ
			}
			continue
		}
		byKind[node.kind()] = typ
		plans[typ] = planStruct(typ)
	}
}

func planStruct(typ reflect.Type) *structPlan {
	plan := &structPlan{typ: typ}
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		if name == "" || name == "-" {
			continue
		}
		planned := planField(i, name, field)
		plan.fields = append(plan.fields, planned)
		plan.binds = plan.binds || len(planned.binds) > 0 || (planned.item != nil && planned.item.binds)
	}
	return plan
}

func planField(index int, name string, field reflect.StructField) fieldPlan {
	plan := fieldPlan{
		index:    index,
		name:     name,
		optional: strings.Contains(field.Tag.Get("json"), "omitempty"),
		role:     field.Tag.Get("role"),
		deflt:    field.Tag.Get("default"),
		sortKey:  field.Tag.Get("sort"),
	}
	if binds := field.Tag.Get("binds"); binds != "" {
		plan.binds = strings.Split(binds, ",")
	}
	plan.min, _ = strconv.Atoi(field.Tag.Get("min"))
	switch {
	case field.Type == exprTyp:
		plan.kind = fieldExpr
	case field.Type.Kind() == reflect.Slice && field.Type.Elem() == exprTyp:
		plan.kind = fieldExprs
	case field.Type.Kind() == reflect.String:
		plan.kind = fieldName
	case field.Type.Kind() == reflect.Bool:
		plan.kind = fieldFlag
	case field.Type.Kind() == reflect.Slice:
		plan.kind = fieldList
		plan.item = planStruct(field.Type.Elem())
	default:
		panic(fmt.Sprintf("node field %s has unsupported type %s", name, field.Type))
	}
	return plan
}

// planOf returns the plan for a node, or nil for a literal.
func planOf(expr Expr) *structPlan {
	return plans[reflect.TypeOf(expr).Elem()]
}

// finish normalises a freshly built node and applies its own rules. The parser
// and the importer both call it, so source and JSON obey the same checks.
func finish(expr Expr) (Expr, error) {
	if plan := planOf(expr); plan != nil {
		sortLists(reflect.ValueOf(expr).Elem(), plan)
	}
	if node, ok := expr.(checker); ok {
		if err := node.check(); err != nil {
			return nil, err
		}
	}
	return expr, nil
}

// sortLists puts a list tagged sort:"key" in key order, which is what makes
// the canonical JSON — and the argument order — independent of how a program
// was written.
func sortLists(value reflect.Value, plan *structPlan) {
	for _, field := range plan.fields {
		if field.kind != fieldList || field.sortKey == "" {
			continue
		}
		list := value.Field(field.index)
		key := keyField(field.item, field.sortKey)
		sort.SliceStable(list.Interface(), func(i, j int) bool {
			return list.Index(i).Field(key).String() < list.Index(j).Field(key).String()
		})
	}
}

func keyField(plan *structPlan, name string) int {
	for _, field := range plan.fields {
		if field.name == name {
			return field.index
		}
	}
	panic(fmt.Sprintf("sort key %q is not a field", name))
}

// Children returns a node's direct subexpressions, in definition order and
// skipping absent optional ones.
func Children(expr Expr) []Expr {
	var out []Expr
	walkChildren(expr, nil, func(child Expr, _ scope) { out = append(out, child) })
	return out
}

// scope is the set of locally bound names at a point in the tree.
type scope map[string]bool

func (s scope) with(names []string) scope {
	if len(names) == 0 {
		return s
	}
	inner := make(scope, len(s)+len(names))
	for name := range s {
		inner[name] = true
	}
	for _, name := range names {
		inner[name] = true
	}
	return inner
}

// walkChildren visits each direct child with the names bound at it: the
// node's own binds tags, plus what earlier list items bind into later ones.
func walkChildren(expr Expr, bound scope, visit func(Expr, scope)) {
	plan := planOf(expr)
	if plan == nil {
		return
	}
	value := reflect.ValueOf(expr).Elem()
	var binds map[string][]string
	if plan.binds {
		binds = map[string][]string{}
		collectBinds(value, plan, binds)
	}
	for _, field := range plan.fields {
		inner := bound.with(binds[field.name])
		switch field.kind {
		case fieldExpr:
			visitExpr(value.Field(field.index), inner, visit)
		case fieldExprs:
			visitExprs(value.Field(field.index), inner, visit)
		case fieldList:
			walkList(value.Field(field.index), field.item, inner, visit)
		}
	}
}

// collectBinds records, for every field of the node, the locals visible in it:
// from the node's own name fields, and from the name fields of its list items
// (a let binding's name is visible in the let's body). "@rest" is left for
// walkList, which knows the item order.
func collectBinds(value reflect.Value, plan *structPlan, binds map[string][]string) {
	for _, field := range plan.fields {
		switch field.kind {
		case fieldName:
			addBinds(binds, value.Field(field.index).String(), field.binds)
		case fieldList:
			list := value.Field(field.index)
			for i := 0; i < list.Len(); i++ {
				collectBinds(list.Index(i), field.item, binds)
			}
		}
	}
}

func addBinds(binds map[string][]string, name string, targets []string) {
	if name == "" {
		return
	}
	for _, target := range targets {
		if target != "@rest" {
			binds[target] = append(binds[target], name)
		}
	}
}

// walkList visits the expressions inside each list item. An item's names that
// bind "@rest" are visible in the items after it; what they bind into the
// parent's fields was already gathered by collectBinds.
func walkList(list reflect.Value, plan *structPlan, bound scope, visit func(Expr, scope)) {
	var rest []string
	for i := 0; i < list.Len(); i++ {
		item := list.Index(i)
		inner := bound.with(rest)
		for _, field := range plan.fields {
			switch field.kind {
			case fieldExpr:
				visitExpr(item.Field(field.index), inner, visit)
			case fieldExprs:
				visitExprs(item.Field(field.index), inner, visit)
			}
		}
		rest = append(rest, itemBinds(item, plan, "@rest")...)
	}
}

// itemBinds lists the names an item's fields bind into target.
func itemBinds(item reflect.Value, plan *structPlan, target string) []string {
	var names []string
	for _, field := range plan.fields {
		if field.kind == fieldName && contains(field.binds, target) {
			names = append(names, item.Field(field.index).String())
		}
	}
	return names
}

func contains(list []string, want string) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}

func visitExpr(field reflect.Value, bound scope, visit func(Expr, scope)) {
	if field.IsNil() {
		return
	}
	visit(field.Interface().(Expr), bound)
}

func visitExprs(field reflect.Value, bound scope, visit func(Expr, scope)) {
	for i := 0; i < field.Len(); i++ {
		visitExpr(field.Index(i), bound, visit)
	}
}

// FreeVariables returns the free variables in the order they first appear,
// which is the argument order a program gets when its host declares no
// contract. Locals bound by let, for and reduce are not free, so a name used
// only inside a comprehension never becomes an argument.
func FreeVariables(expr Expr) []string {
	collector := &variableCollector{seen: map[string]bool{}}
	collector.visit(expr, scope{})
	return collector.out
}

type variableCollector struct {
	seen map[string]bool
	out  []string
}

func (c *variableCollector) visit(current Expr, bound scope) {
	if variable, ok := current.(*VariableExpr); ok {
		if !bound[variable.Name] && !c.seen[variable.Name] {
			c.seen[variable.Name] = true
			c.out = append(c.out, variable.Name)
		}
		return
	}
	walkChildren(current, bound, c.visit)
}

// NodeSchemas describes every node for a front end: which fields it has, which
// hold expressions, names or lists, and what a fresh one looks like. It is
// derived from the same plans that import and export use, so the canvas and
// the compiler cannot disagree about a node's shape.
func NodeSchemas() []machine.NodeSchema {
	out := make([]machine.NodeSchema, 0, len(nodeTypes)+3)
	for _, kind := range []string{"int", "float", "string", "bool"} {
		out = append(out, machine.NodeSchema{Node: kind, Fields: []machine.FieldSchema{{Name: kind, Kind: kind}}})
	}
	for _, node := range nodeTypes[1:] {
		schema := machine.NodeSchema{Node: node.kind(), Fields: fieldSchemas(planOf(node))}
		if form, ok := node.(former); ok {
			schema.Form = string(form.Form())
		}
		out = append(out, schema)
	}
	return out
}

func fieldSchemas(plan *structPlan) []machine.FieldSchema {
	out := make([]machine.FieldSchema, len(plan.fields))
	for i, field := range plan.fields {
		out[i] = machine.FieldSchema{
			Name: field.name, Kind: fieldKindNames[field.kind], Optional: field.optional,
			Role: field.role, Binds: append([]string(nil), field.binds...),
			Default: field.deflt, Min: field.min,
		}
		if field.kind == fieldName && field.role == "text" {
			out[i].Kind = "text"
		}
		if field.item != nil {
			out[i].Fields = fieldSchemas(field.item)
		}
	}
	return out
}

var fieldKindNames = [...]string{
	fieldExpr: "expr", fieldExprs: "exprs", fieldName: "name", fieldList: "list", fieldFlag: "bool",
}

// FormOf reports the lazy form a node belongs to, for nodes a registry can
// switch off.
func FormOf(expr Expr) (machine.Form, bool) {
	node, ok := expr.(former)
	if !ok {
		return "", false
	}
	return node.Form(), true
}
