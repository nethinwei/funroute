package syntax

import (
	"reflect"
	"strings"
)

// The names an expanded selector binds. No source can write them, so they
// never meet a name of the program's; nested expansions shadow their own.
const (
	selectedItems = "$items"
	selectedItem  = "$item"
)

// ExpandSelectors is root with every call that has selectors among its
// arguments written out as the comprehensions they stand for:
// sort_by(xs, .fee) is sort_by(xs, [e.fee for e in xs]), and
// f(g(y), .fee) is let(t = g(y), f(t, [e.fee for e in t])), so the first
// argument runs once. A subtree with no selector in it is root's own, not a
// copy; the nodes it makes are numbered after root's and cover the selector
// they read, so an error about one points at it. A selector anywhere else is
// an error.
func ExpandSelectors(root Expr) (Expr, error) {
	next := 0
	for node := range Nodes(root) {
		next = max(next, node.NodeID())
	}
	x := &expander{next: next + 1}
	return x.expand(root, false)
}

type expander struct{ next int }

// expand is expr written out; argument says it is a call's argument after
// the first, the one place a selector may be.
func (x *expander) expand(expr Expr, argument bool) (Expr, error) {
	switch node := expr.(type) {
	case *SelectorExpr:
		if !argument {
			return nil, Around(node, "the selector .%s must be an argument after a call's first, the list it reads", node.Path)
		}
		return node, nil
	case *CallExpr:
		return x.call(node)
	}
	return rebuilt(expr, func(child Expr) (Expr, error) { return x.expand(child, false) })
}

func (x *expander) call(node *CallExpr) (Expr, error) {
	args := make([]Expr, len(node.Args))
	selects, changed := false, false
	for i, arg := range node.Args {
		next, err := x.expand(arg, i > 0)
		if err != nil {
			return nil, err
		}
		_, selector := next.(*SelectorExpr)
		args[i], selects, changed = next, selects || selector, changed || next != arg
	}
	if !selects {
		if !changed {
			return node, nil
		}
		return &CallExpr{Node: node.Node, Name: node.Name, Args: args}, nil
	}
	list, named := args[0].(*VariableExpr)
	name := selectedItems
	if named {
		name = list.Name
	}
	for i, arg := range args[1:] {
		if selector, ok := arg.(*SelectorExpr); ok {
			args[i+1] = x.keys(name, selector)
		}
	}
	if named {
		return &CallExpr{Node: node.Node, Name: node.Name, Args: args}, nil
	}
	first := args[0]
	args[0] = x.variable(name, first)
	call := &CallExpr{Node: node.Node, Name: node.Name, Args: args}
	return &LetExpr{Node: x.node(node), Bindings: []LetBinding{{Name: name, Value: first}}, Body: call}, nil
}

// keys is [e.path for e in list]: the selector's own node, so what is known
// about the keys is known about the selector that wrote them.
func (x *expander) keys(list string, selector *SelectorExpr) Expr {
	key := x.variable(selectedItem, selector)
	for field := range strings.SplitSeq(selector.Path, ".") {
		key = &FieldExpr{Node: x.node(selector), Value: key, Field: field}
	}
	return &ForExpr{Node: selector.Node, Source: x.variable(list, selector), Variable: selectedItem, Yield: key}
}

func (x *expander) variable(name string, at Expr) Expr {
	return &VariableExpr{Node: x.node(at), Name: name}
}

// node is a new node's Node, placed where at is.
func (x *expander) node(at Expr) Node {
	x.next++
	return Node{ID: x.next - 1, Pos: at.Position(), Span: at.Extent()}
}

// rebuilt is expr with change made of each of its children: expr itself
// when change keeps them all, else a copy sharing the ones it kept.
func rebuilt(expr Expr, change func(Expr) (Expr, error)) (Expr, error) {
	plan := planOf(expr)
	if plan == nil {
		return expr, nil
	}
	copied := reflect.New(plan.typ).Elem()
	copied.Set(reflect.ValueOf(expr).Elem())
	changed, err := rebuildFields(copied, plan, change)
	if err != nil || !changed {
		return expr, err
	}
	return heldExpr(copied.Addr()), nil
}

func rebuildFields(value reflect.Value, plan *structPlan, change func(Expr) (Expr, error)) (bool, error) {
	changed := false
	for _, field := range plan.fields {
		var moved bool
		var err error
		switch field.kind {
		case fieldExpr:
			moved, err = rebuildExpr(value.Field(field.index), change)
		case fieldExprs, fieldList:
			moved, err = rebuildList(value.Field(field.index), field, change)
		}
		if err != nil {
			return false, err
		}
		changed = changed || moved
	}
	return changed, nil
}

func rebuildExpr(target reflect.Value, change func(Expr) (Expr, error)) (bool, error) {
	if target.IsNil() {
		return false, nil
	}
	child := heldExpr(target)
	next, err := change(child)
	if err != nil || next == child {
		return false, err
	}
	target.Set(reflect.ValueOf(next))
	return true, nil
}

// rebuildList changes a list's items in a copy of it, so the list it was
// is left as it is.
func rebuildList(list reflect.Value, field fieldPlan, change func(Expr) (Expr, error)) (bool, error) {
	copied := reflect.MakeSlice(list.Type(), list.Len(), list.Len())
	reflect.Copy(copied, list)
	changed := false
	for i := range copied.Len() {
		var moved bool
		var err error
		if field.kind == fieldExprs {
			moved, err = rebuildExpr(copied.Index(i), change)
		} else {
			moved, err = rebuildFields(copied.Index(i), field.item, change)
		}
		if err != nil {
			return false, err
		}
		changed = changed || moved
	}
	if changed {
		list.Set(copied)
	}
	return changed, nil
}
