package syntax

import (
	"fmt"
	"iter"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/nethinwei/funroute/internal/kit"
	"github.com/nethinwei/funroute/internal/machine"
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
	min      int
	sortKey  string
	item     *structPlan
}

// structPlan is a node type or a list item type. kind is a node's ExprJSON
// tag, isForm whether it is a form, and form the form a registry may turn
// off, if it is one, all from the kind tag on its Node. binds says whether anything in it, or in its list items, binds a
// name — most nodes do not, and the walk skips the bookkeeping for them. rest
// says whether one of its own fields binds "@rest", the items after it in a
// list.
type structPlan struct {
	typ    reflect.Type
	id     []int // where a node's ID is, for the importer to set
	kind   string
	isForm bool
	form   machine.Form
	fields []fieldPlan
	binds  bool
	rest   bool
	// sorts says one of its lists is kept in key order.
	sorts bool
}

var (
	plans   = map[reflect.Type]*structPlan{}
	byKind  = map[string]reflect.Type{}
	exprTyp = reflect.TypeFor[Expr]()
	nodeTyp = reflect.TypeFor[Node]()
)

func init() {
	for _, node := range nodeTypes {
		typ := reflect.TypeOf(node).Elem()
		if _, literal := node.(*LiteralExpr); literal {
			for _, kind := range literalKinds {
				byKind[kind] = typ
			}
			continue
		}
		plan := planStruct(typ)
		id, _ := typ.FieldByName("ID")
		plan.id = id.Index
		byKind[plan.kind] = typ
		plans[typ] = plan
	}
}

func planStruct(typ reflect.Type) *structPlan {
	plan := &structPlan{typ: typ}
	for i := range typ.NumField() {
		field := typ.Field(i)
		if field.Type == nodeTyp {
			flags := strings.Split(field.Tag.Get("kind"), ",")
			plan.kind, plan.isForm = flags[0], slices.Contains(flags, "form")
			if slices.Contains(flags, "optional") {
				plan.form = machine.Form(plan.kind)
			}
			continue
		}
		name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		if name == "" || name == "-" {
			continue
		}
		planned := planField(i, name, field)
		plan.fields = append(plan.fields, planned)
		plan.binds = plan.binds || len(planned.binds) > 0 || (planned.item != nil && planned.item.binds)
		plan.rest = plan.rest || slices.Contains(planned.binds, "@rest")
		plan.sorts = plan.sorts || planned.kind == fieldList && planned.sortKey != ""
	}
	return plan
}

func planField(index int, name string, field reflect.StructField) fieldPlan {
	plan := fieldPlan{
		index:    index,
		name:     name,
		optional: strings.Contains(field.Tag.Get("json"), "omitempty"),
		role:     field.Tag.Get("role"),
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
	if plan := planOf(expr); plan != nil && plan.sorts {
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
	at := slices.IndexFunc(plan.fields, func(field fieldPlan) bool { return field.name == name })
	if at < 0 {
		panic(fmt.Sprintf("sort key %q is not a field", name))
	}
	return plan.fields[at].index
}

// Children returns a node's direct subexpressions, in definition order and
// skipping absent optional ones.
func Children(expr Expr) []Expr {
	var out []Expr
	EachChild(expr, func(child Expr) { out = append(out, child) })
	return out
}

// EachChild visits a node's direct subexpressions in Children's order,
// without gathering them.
func EachChild(expr Expr, visit func(Expr)) {
	walkChildren(expr, nil, func(child Expr, _ scope) { visit(child) })
}

// Nodes is root and every node under it, parents before their children and
// children in definition order.
func Nodes(root Expr) iter.Seq[Expr] {
	return func(yield func(Expr) bool) { preorder(root, yield) }
}

func preorder(expr Expr, yield func(Expr) bool) bool {
	if !yield(expr) {
		return false
	}
	going := true
	EachChild(expr, func(child Expr) { going = going && preorder(child, yield) })
	return going
}

// scope is the locally bound names at a point in the tree, outermost
// first, each with the node that binds it; a name may be there twice, when
// an inner form binds it again.
type scope []local

// local is a bound name and the form that binds it.
type local struct {
	name string
	by   Expr
}

// binder is the form whose binding of name is visible here: the innermost.
func (s scope) binder(name string) (Expr, bool) {
	for _, bound := range slices.Backward(s) {
		if bound.name == name {
			return bound.by, true
		}
	}
	return nil, false
}

// names is the bound names, outermost first.
func (s scope) names() []string { return kit.Map(s, func(l local) string { return l.name }) }

// with is s and names, bound by by, after it, in a list of its own: s itself
// is shared by every sibling, so it is never appended to in place.
func (s scope) with(names []string, by Expr) scope {
	if len(names) == 0 {
		return s
	}
	out := slices.Grow(slices.Clip(s), len(names))
	for _, name := range names {
		out = append(out, local{name: name, by: by})
	}
	return out
}

// boundName is a name a node binds and the field it is visible in.
type boundName struct{ target, name string }

// in is s and the names binds makes visible in field, bound by by.
func (s scope) in(binds []boundName, field string, by Expr) scope {
	var names []string
	for _, bind := range binds {
		if bind.target == field {
			names = append(names, bind.name)
		}
	}
	return s.with(names, by)
}

// walkChildren visits each direct child with the names bound at it: the
// node's own binds tags, plus what earlier list items bind into later ones.
func walkChildren(expr Expr, bound scope, visit func(Expr, scope)) {
	plan := planOf(expr)
	if plan == nil {
		return
	}
	value := reflect.ValueOf(expr).Elem()
	var binds []boundName
	if plan.binds {
		binds = collectBinds(value, plan, binds)
	}
	for _, field := range plan.fields {
		inner := bound.in(binds, field.name, expr)
		if field.kind == fieldList {
			walkList(value.Field(field.index), field.item, inner, expr, visit)
			continue
		}
		visitField(value.Field(field.index), field.kind, inner, visit)
	}
}

// collectBinds records, for every field of the node, the locals visible in it:
// from the node's own name fields, and from the name fields of its list items
// (a let binding's name is visible in the let's body). "@rest" is left for
// walkList, which knows the item order.
func collectBinds(value reflect.Value, plan *structPlan, binds []boundName) []boundName {
	for _, field := range plan.fields {
		switch field.kind {
		case fieldName:
			binds = addBinds(binds, value.Field(field.index).String(), field.binds)
		case fieldList:
			list := value.Field(field.index)
			for i := range list.Len() {
				binds = collectBinds(list.Index(i), field.item, binds)
			}
		}
	}
	return binds
}

func addBinds(binds []boundName, name string, targets []string) []boundName {
	if name == "" {
		return binds
	}
	for _, target := range targets {
		if target != "@rest" {
			binds = append(binds, boundName{target: target, name: name})
		}
	}
	return binds
}

// walkList visits the expressions inside each list item. An item's names that
// bind "@rest" are visible in the items after it; what they bind into the
// parent's fields was already gathered by collectBinds. by is the node the
// list belongs to, which binds what its items do.
func walkList(list reflect.Value, plan *structPlan, bound scope, by Expr, visit func(Expr, scope)) {
	// Only items that bind "@rest" gather names, one a let binding; any other
	// list gathers none, and a zero capacity allocates nothing.
	size := 0
	if plan.rest {
		size = list.Len()
	}
	rest := make([]string, 0, size)
	for i := range list.Len() {
		item := list.Index(i)
		inner := bound.with(rest, by)
		for _, field := range plan.fields {
			visitField(item.Field(field.index), field.kind, inner, visit)
		}
		rest = append(rest, itemBinds(item, plan, "@rest")...)
	}
}

// itemBinds lists the names an item's fields bind into target.
func itemBinds(item reflect.Value, plan *structPlan, target string) []string {
	var names []string
	for _, field := range plan.fields {
		if field.kind == fieldName && slices.Contains(field.binds, target) {
			names = append(names, item.Field(field.index).String())
		}
	}
	return names
}

// visitField visits the expressions a field holds: its one, or each of its
// list. A name or a flag holds none.
func visitField(field reflect.Value, kind fieldKind, bound scope, visit func(Expr, scope)) {
	switch kind {
	case fieldExpr:
		visitExpr(field, bound, visit)
	case fieldExprs:
		for i := range field.Len() {
			visitExpr(field.Index(i), bound, visit)
		}
	}
}

func visitExpr(field reflect.Value, bound scope, visit func(Expr, scope)) {
	if field.IsNil() {
		return
	}
	visit(heldExpr(field), bound)
}

// heldExpr is the expression value holds: a new node, or a node's Expr field
// or list element. A node type always is an Expr, so only a nil field could
// fail the assertion, and no caller reaches one (absent optional fields are
// skipped first); were one to slip through, it panics as the unchecked
// assertion did.
func heldExpr(value reflect.Value) Expr {
	expr, ok := reflect.TypeAssert[Expr](value)
	if !ok {
		panic("syntax: " + value.Type().String() + " holds no expression")
	}
	return expr
}

// FreeVariables returns the free variables in the order they first appear,
// which is the argument order a program gets when its host declares no
// contract. Locals bound by let, for and reduce are not free, so a name used
// only inside a comprehension never becomes an argument.
func FreeVariables(expr Expr) []string {
	return kit.Map(FirstReads(expr), func(read *VariableExpr) string { return read.Name })
}

// FreeReads is every read of a free variable in expr, by node ID: the reads
// no let, for or reduce inside expr binds.
func FreeReads(expr Expr) map[int]bool {
	out := map[int]bool{}
	eachVariable(expr, func(variable *VariableExpr, local bool) {
		if !local {
			out[variable.ID] = true
		}
	})
	return out
}

// FirstReads is where each free variable is first read, in that order: the
// node a message about the argument points at.
func FirstReads(expr Expr) []*VariableExpr {
	seen := map[string]bool{}
	var out []*VariableExpr
	eachVariable(expr, func(variable *VariableExpr, local bool) {
		if !local && !seen[variable.Name] {
			seen[variable.Name] = true
			out = append(out, variable)
		}
	})
	return out
}

// eachVariable visits every variable read in root, in source order, saying
// whether a form binds its name there. It is the one walk of the scope rule:
// free variables, the locals the language server colours and the reads
// Lexemes marks are all this walk.
func eachVariable(root Expr, visit func(variable *VariableExpr, local bool)) {
	EachRead(root, func(variable *VariableExpr, binder Expr) { visit(variable, binder != nil) })
}

// EachRead visits every variable read in root, in source order, with the
// form whose binding of its name the read sees there: a let, a loop, a
// reduce — or nil for a read of a free variable, one of the program's
// arguments.
func EachRead(root Expr, visit func(variable *VariableExpr, binder Expr)) {
	var walk func(Expr, scope)
	walk = func(expr Expr, bound scope) {
		if variable, ok := expr.(*VariableExpr); ok {
			binder, _ := bound.binder(variable.Name)
			visit(variable, binder)
			return
		}
		walkChildren(expr, bound, walk)
	}
	walk(root, nil)
}

// literalKinds are a literal's ExprJSON tags, one per kind of value it may
// hold.
var literalKinds = []string{"int", "float", "string", "bool", "duration"}

// NodeKinds lists every node's ExprJSON tag: the literal kinds and one per
// node type.
func NodeKinds() []string {
	out := make([]string, 0, len(literalKinds)+len(nodeTypes)-1)
	out = append(out, literalKinds...)
	for _, node := range nodeTypes[1:] {
		out = append(out, planOf(node).kind)
	}
	return out
}

// FormOf reports the lazy form a node belongs to, for nodes a registry can
// switch off.
func FormOf(expr Expr) (machine.Form, bool) {
	plan := planOf(expr)
	if plan == nil || plan.form == "" {
		return "", false
	}
	return plan.form, true
}

// kindOf is a node's ExprJSON tag, given its plan: a literal's, which has
// none, is its value's kind.
func kindOf(expr Expr, plan *structPlan) string {
	if plan != nil {
		return plan.kind
	}
	literal, _ := expr.(*LiteralExpr)
	return literal.Value.Kind().String()
}
