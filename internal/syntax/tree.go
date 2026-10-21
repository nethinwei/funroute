package syntax

import (
	"cmp"
	"reflect"
	"slices"
	"strings"
)

// Tree is a program as it was written: every node with the source it covers
// and its fields, in the shape ExprJSON gives them, so an editor can show the
// structure of a text and turn an edit to the structure back into an edit to
// the text. Like ExprJSON it is read off the node plans; the one node told
// apart is a comprehension's next clause, which covers its parent's source
// (ownedBy).
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

// nameSpans lists where each local or field name is written, by name, in
// source order, so spanWithin finds a node's places without reading the
// others.
func nameSpans(source string, roles map[int]roleMark) map[string][]Span {
	out := map[string][]Span{}
	for start, mark := range roles {
		if mark.role == RoleLocal || mark.role == RoleField {
			out[source[start:mark.end]] = append(out[source[start:mark.end]], Span{start, mark.end})
		}
	}
	for _, spans := range out {
		slices.SortFunc(spans, func(a, b Span) int { return cmp.Compare(a.Start, b.Start) })
	}
	return out
}

func treeOf(expr Expr, names map[string][]Span) Tree {
	plan := planOf(expr)
	tree := Tree{Node: kindOf(expr, plan), Span: expr.Extent()}
	// An operator's operands are what was written around it: a != b holds a
	// and b, not the == its expansion negates. Only they are read, so a chain
	// of operators is read once, not once per reading of its expansion.
	match, operator := readOperator(expr)
	if operator {
		tree.Operator = match.spec.token
	}
	if plan != nil {
		tree.Form = plan.isForm
		own := ownSource{node: tree.Span}
		EachChild(expr, func(child Expr) { own.children = append(own.children, ownedBy(expr, child)...) })
		tree.Fields = fieldsOf(reflect.ValueOf(expr).Elem(), plan, own, names, match.operands)
	}
	if selector, ok := expr.(*SelectorExpr); ok && len(tree.Fields) == 1 {
		tree.Fields[0].TextSpan = pathSpan(selector, names)
	}
	return tree
}

// pathSpan is where a selector's path is written: one lexeme, however many
// names it reads, .fee as much as .customer.fee, from the first name after
// its dot, which blanks may part from it, . fee.
func pathSpan(selector *SelectorExpr, names map[string][]Span) *Span {
	first, _, _ := strings.Cut(selector.Path, ".")
	spans := names[first]
	at, _ := slices.BinarySearchFunc(spans, selector.Position()+1, func(span Span, start int) int { return cmp.Compare(span.Start, start) })
	if at == len(spans) {
		return nil
	}
	return &Span{Start: spans[at].Start, End: spans[at].Start + len(selector.Path)}
}

func operandTrees(operands []Expr, names map[string][]Span) []Tree {
	out := make([]Tree, len(operands))
	for i, operand := range operands {
		out[i] = treeOf(operand, names)
	}
	return out
}

// ownedBy is the source child holds within expr: its extent, but of a
// comprehension's next clause only its own for on and the yield written
// before all of them, so that the clause's names stay its parent's.
func ownedBy(expr, child Expr) []Span {
	if !continues(expr, child) {
		return []Span{child.Extent()}
	}
	// continues holds child to a comprehension, and import a flattened one
	// to a yield that is the next.
	innermost, _ := child.(*ForExpr)
	for innermost.Flatten {
		next, ok := innermost.Yield.(*ForExpr)
		if !ok {
			break
		}
		innermost = next
	}
	return []Span{{Start: child.Position(), End: child.Extent().End}, innermost.Yield.Extent()}
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

// fieldsOf reads a node's fields; operands, when the node is an operator's
// expansion, stand for its args.
func fieldsOf(value reflect.Value, plan *structPlan, within ownSource, names map[string][]Span, operands []Expr) []TreeField {
	out := make([]TreeField, 0, len(plan.fields))
	for _, field := range plan.fields {
		current := value.Field(field.index)
		if field.optional && isAbsent(current) {
			continue
		}
		if operands != nil && field.name == "args" {
			out = append(out, TreeField{Name: field.name, Nodes: operandTrees(operands, names)})
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
		visitField(value, field.kind, nil, func(child Expr, _ scope) {
			// An operator's expansion writes in operands of its own — the
			// false of &&, the zero of a minus — which cover no source.
			if span := child.Extent(); span.Start < span.End {
				out.Nodes = append(out.Nodes, treeOf(child, names))
			}
		})
	case fieldList:
		for i := range value.Len() {
			out.Items = append(out.Items, fieldsOf(value.Index(i), field.item, within, names, nil))
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
	from, _ := slices.BinarySearchFunc(spans, within.node.Start, func(span Span, start int) int { return cmp.Compare(span.Start, start) })
	for i := from; i < len(spans) && spans[i].Start < within.node.End; i++ {
		if span := spans[i]; within.holds(span) {
			return &span
		}
	}
	return nil
}
