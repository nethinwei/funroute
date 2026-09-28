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
	Operator string `json:"operator,omitempty"`
	// Form says the node is one of the language's forms: it branches, binds
	// names or scopes what is inside it.
	Form   bool        `json:"form,omitempty"`
	Fields []TreeField `json:"fields,omitempty"`
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

// nameSpans lists where each local or field name is written, by name, in no
// particular order: spanWithin takes the first place by offset.
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
	plan := planOf(expr)
	tree := Tree{Node: kindOf(expr, plan), Span: expr.Extent()}
	if match, ok := readOperator(expr); ok {
		tree.Operator = match.spec.token
	}
	if plan != nil {
		tree.Form = plan.isForm
		own := ownSource{node: tree.Span}
		EachChild(expr, func(child Expr) { own.children = append(own.children, child.Extent()) })
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
	case fieldExpr, fieldExprs:
		visitField(value, field.kind, nil, func(child Expr, _ scope) { out.Nodes = append(out.Nodes, treeOf(child, names)) })
	case fieldList:
		for i := range value.Len() {
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
