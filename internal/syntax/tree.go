package syntax

import (
	"reflect"
)

// Tree is a program as it was written: every node with the source it covers
// and its fields, in the shape ExprJSON gives them, so an editor can show the
// structure of a text and turn an edit to the structure back into an edit to
// the text. Like ExprJSON it is read off the node plans, so no node is named
// here.
type Tree struct {
	Node string `json:"node"`
	Span Span   `json:"span"`
	// Operator is how the node is spelled when it is an operator's expansion:
	// add(a, b) written a + b.
	Operator string      `json:"operator,omitempty"`
	Fields   []TreeField `json:"fields,omitempty"`
}

// TreeField is one field of a node: a subexpression, a list of them, a list
// of items with fields of their own, or a name. A name's span is where the
// name is written, when the parser saw one there.
type TreeField struct {
	Name     string        `json:"name"`
	Nodes    []Tree        `json:"nodes,omitempty"`
	Items    [][]TreeField `json:"items,omitempty"`
	Text     string        `json:"text,omitempty"`
	TextSpan *Span         `json:"text_span,omitempty"`
	Flag     bool          `json:"flag,omitempty"`
}

// SyntaxTree parses source into its tree.
func SyntaxTree(source string) (*Tree, error) {
	r := read(source, true)
	if r.err != nil {
		return nil, r.err
	}
	tree := treeOf(r.expr, nameSpans(source, r.parser.roles))
	return &tree, nil
}

// nameSpans lists where each local or field name is written, by name, in
// source order.
func nameSpans(source string, roles map[int]roleMark) map[string][]Span {
	out := map[string][]Span{}
	for start, mark := range roles {
		if mark.role == RoleLocal || mark.role == RoleField {
			out[source[start:mark.end]] = append(out[source[start:mark.end]], Span{start, mark.end})
		}
	}
	return out
}

func treeOf(expr Expr, names map[string][]Span) Tree {
	tree := Tree{Node: expr.kind(), Span: expr.Extent()}
	if match, ok := readOperator(expr); ok {
		tree.Operator = match.spec.token
	}
	if plan := planOf(expr); plan != nil {
		own := ownSource{node: tree.Span}
		for _, child := range Children(expr) {
			own.children = append(own.children, child.Extent())
		}
		tree.Fields = fieldsOf(reflect.ValueOf(expr).Elem(), plan, own, names)
	}
	return tree
}

// ownSource is a node's source less its children's: where the names the node
// itself holds are written.
type ownSource struct {
	node     Span
	children []Span
}

func (o ownSource) holds(span Span) bool {
	if span.Start < o.node.Start || span.End > o.node.End {
		return false
	}
	for _, child := range o.children {
		if span.Start >= child.Start && span.End <= child.End {
			return false
		}
	}
	return true
}

func fieldsOf(value reflect.Value, plan *structPlan, within ownSource, names map[string][]Span) []TreeField {
	out := make([]TreeField, 0, len(plan.fields))
	for _, field := range plan.fields {
		current := value.Field(field.index)
		if field.optional && isAbsent(current) {
			continue
		}
		out = append(out, fieldOf(current, field, within, names))
	}
	return out
}

func fieldOf(value reflect.Value, field fieldPlan, within ownSource, names map[string][]Span) TreeField {
	out := TreeField{Name: field.name}
	switch field.kind {
	case fieldExpr:
		out.Nodes = []Tree{treeOf(value.Interface().(Expr), names)}
	case fieldExprs:
		for i := 0; i < value.Len(); i++ {
			out.Nodes = append(out.Nodes, treeOf(value.Index(i).Interface().(Expr), names))
		}
	case fieldList:
		for i := 0; i < value.Len(); i++ {
			out.Items = append(out.Items, fieldsOf(value.Index(i), field.item, within, names))
		}
	case fieldFlag:
		out.Flag = value.Bool()
	default:
		out.Text = value.String()
		out.TextSpan = spanWithin(names[out.Text], within)
	}
	return out
}

// spanWithin is where the name is written in the node's own source. A node
// writes a name once — its own rules refuse binding one twice — so the first
// such place is the one.
func spanWithin(spans []Span, within ownSource) *Span {
	var found *Span
	for i := range spans {
		span := spans[i]
		if within.holds(span) && (found == nil || span.Start < found.Start) {
			found = &span
		}
	}
	return found
}
