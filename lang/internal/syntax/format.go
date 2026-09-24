package syntax

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// Format lays a program out the way a person would: on one line while it
// fits, otherwise one part per line with the parts indented. A long chain of
// one infix operator breaks before each operator. The layout changes only
// whitespace, so the text parses back to the same program.
func Format(expr Expr) string { return format(expr, "", 0) }

// maxLine is where a line is split into parts. It counts the indent, so a
// branch nested three levels deep still fits.
const maxLine = 72

// used is what the line already holds before expr: a binding's name, a case
// head, a key. The whole line has to fit, not just expr.
func format(expr Expr, indent string, used int) string {
	one := inline(expr, 0)
	if utf8.RuneCountInString(indent)+used+utf8.RuneCountInString(one) < maxLine {
		return one
	}
	if match, ok := readOperator(expr); ok {
		if chain, ok := chainSource(match, indent); ok {
			return chain
		}
		return one
	}
	layout, ok := splitNode(expr)
	if !ok {
		return one
	}
	inner := indent + "  "
	parts := make([]string, len(layout.parts))
	for i, part := range layout.parts {
		parts[i] = inner + part.layout(inner)
	}
	return layout.open + "\n" + strings.Join(parts, layout.separator) + "\n" + indent + layout.close
}

func chainSource(match operatorMatch, indent string) (string, bool) {
	if match.spec.fixity != "infix" || match.spec.nonAssociative {
		return "", false
	}
	parts := chainParts(match)
	if len(parts) < 2 {
		return "", false
	}
	return strings.Join(parts, "\n"+indent+"  "+match.spec.token+" "), true
}

// chainParts is a - b - c as its operands, following the left operand while
// it is the same operator.
func chainParts(match operatorMatch) []string {
	spec := match.spec
	left := []string{inline(match.operands[0], spec.leftLevel())}
	if inner, ok := readOperator(match.operands[0]); ok && inner.spec.token == spec.token && inner.spec.fixity == spec.fixity {
		left = chainParts(inner)
	}
	return append(left, inline(match.operands[1], spec.rightLevel()))
}

// split is a node laid out one part per line between an opening and a closing.
type split struct {
	open, close, separator string
	parts                  []part
}

type part interface {
	layout(indent string) string
}

// exprPart is a subexpression on a line of its own.
type exprPart struct{ expr Expr }

func (p exprPart) layout(indent string) string { return format(p.expr, indent, 0) }

// textPart is a line already written, such as a loop clause.
type textPart string

func (p textPart) layout(string) string { return string(p) }

// headedPart is a subexpression after a head on its line — a binding's name,
// a key, else — and the head counts toward the line.
type headedPart struct {
	head string
	expr Expr
}

func (p headedPart) layout(indent string) string {
	return p.head + format(p.expr, indent, utf8.RuneCountInString(p.head))
}

// branchPart is one switch case, whose matches are part of its head.
type branchPart struct{ branch SwitchCaseExpr }

func (p branchPart) layout(indent string) string {
	matches := make([]string, len(p.branch.Match))
	for i, match := range p.branch.Match {
		matches[i] = format(match, indent, 0)
	}
	return headedPart{"case " + strings.Join(matches, ", ") + " => ", p.branch.Result}.layout(indent)
}

func listSplit(open, close string, parts []part) split {
	return split{open: open, close: close, separator: ",\n", parts: parts}
}

func exprParts(items []Expr) []part {
	parts := make([]part, len(items))
	for i, item := range items {
		parts[i] = exprPart{item}
	}
	return parts
}

func splitNode(expr Expr) (split, bool) {
	switch node := expr.(type) {
	case *CallExpr:
		return listSplit(node.Name+"(", ")", exprParts(node.Args)), true
	case *ArrayExpr:
		return listSplit("[", "]", exprParts(node.Items)), true
	case *SwitchExpr:
		return switchSplit(node), true
	case *ForExpr:
		return comprehensionSplit(node), true
	case *ReduceExpr:
		return listSplit("reduce(", ")", []part{textPart(reduceHead(node)), textPart(accumulatorHead(node)), exprPart{node.Body}}), true
	case *LetExpr:
		parts := make([]part, 0, len(node.Bindings)+1)
		for _, binding := range node.Bindings {
			parts = append(parts, headedPart{binding.Name + " = ", binding.Value})
		}
		return listSplit("let(", ")", append(parts, exprPart{node.Body})), true
	case *UsingExpr:
		var parts []part
		for _, text := range usingParts(node) {
			parts = append(parts, textPart(text))
		}
		return listSplit("using(", ")", append(parts, exprPart{node.Body})), true
	default:
		return braceSplit(expr)
	}
}

func braceSplit(expr Expr) (split, bool) {
	switch node := expr.(type) {
	case *DictExpr:
		parts := make([]part, len(node.Entries))
		for i, entry := range node.Entries {
			parts[i] = headedPart{quote(entry.Key) + ": ", entry.Value}
		}
		return listSplit("{", "}", parts), true
	case *RecordExpr:
		parts := make([]part, len(node.Fields))
		for i, field := range node.Fields {
			parts[i] = headedPart{field.Name + ": ", field.Value}
		}
		return listSplit("{", "}", parts), true
	case *RecordUpdateExpr:
		parts := make([]part, len(node.Fields))
		for i, field := range node.Fields {
			parts[i] = headedPart{field.Name + ": ", field.Value}
		}
		return listSplit(postfixBase(node.Base)+" with {", "}", parts), true
	default:
		return split{}, false
	}
}

func switchSplit(node *SwitchExpr) split {
	open := "switch("
	if node.Value != nil {
		open += inline(node.Value, 0) + ","
	}
	parts := make([]part, 0, len(node.Cases)+1)
	for _, branch := range node.Cases {
		parts = append(parts, branchPart{branch})
	}
	if node.Default != nil {
		parts = append(parts, headedPart{"else => ", node.Default})
	}
	return listSplit(open, ")", parts)
}

// A comprehension keeps its clauses one per line under what it yields.
func comprehensionSplit(node *ForExpr) split {
	inner := innerLoop(node)
	open, close := "[", "]"
	parts := []part{exprPart{inner.Yield}}
	if inner.YieldKey != nil {
		open, close = "{", "}"
		parts = []part{headedPart{inline(inner.YieldKey, 0) + ": ", inner.Yield}}
	}
	for _, clause := range forClauses(node) {
		parts = append(parts, textPart(clause))
	}
	return split{open: open, close: close, separator: "\n", parts: parts}
}

// FormatSource formats a program's text. Comments never reach the AST, so a
// comment inside the expression would be lost by reprinting it; rather than
// drop one, FormatSource refuses. The comments before the expression — a
// contract written as comments, say — and after it are kept as written, and
// so is a final newline.
func FormatSource(source string) (string, error) {
	r := read(source, false)
	if r.err != nil {
		return "", r.err
	}
	lex, expr := r.lex, r.expr
	extent := expr.Extent()
	for _, comment := range lex.comments {
		if comment.pos > extent.Start && comment.pos < extent.End {
			return "", over(comment.pos, comment.end, "cannot format a program with a comment inside the expression yet")
		}
	}
	before, after := "", ""
	if len(lex.comments) > 0 && lex.comments[0].pos < extent.Start {
		before = source[:extent.Start]
	}
	if last := len(lex.comments) - 1; last >= 0 && lex.comments[last].pos >= extent.End {
		after = strings.TrimRightFunc(source[extent.End:], unicode.IsSpace)
	}
	formatted := before + Format(expr) + after
	if strings.HasSuffix(source, "\n") {
		formatted += "\n"
	}
	return formatted, nil
}
