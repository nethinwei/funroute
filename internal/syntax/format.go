package syntax

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/nethinwei/funroute/internal/kit"
)

// Format lays a program out the way a person would: on one line while it
// fits, otherwise one part per line with the parts indented. A long chain of
// one infix operator breaks before each operator. The layout changes only
// whitespace, so the text parses back to the same program.
func Format(expr Expr) string {
	p := &printer{}
	one := p.inline(expr, 0)
	if fits("", 0, one) {
		return one
	}
	// The layout starts from the text just written.
	p.keep()
	p.written[printed{expr: expr, parent: 0}] = one
	var out strings.Builder
	p.format(&out, expr, "", 0)
	return out.String()
}

// fits reports whether one, after indent and used characters, stays within
// a line.
func fits(indent string, used int, one string) bool {
	return utf8.RuneCountInString(indent)+used+utf8.RuneCountInString(one) < maxLine
}

// maxLine is where a line is split into parts. It counts the indent, so a
// branch nested three levels deep still fits.
const maxLine = 72

// format writes expr to out, laid out from indent. used is what the line
// already holds before expr: a binding's name, a case head, a key. The whole
// line has to fit, not just expr. It writes rather than returns, so a deep
// program costs what its text is, not that again at every level.
func (p *printer) format(out *strings.Builder, expr Expr, indent string, used int) {
	one := p.inline(expr, 0)
	if fits(indent, used, one) {
		out.WriteString(one)
		return
	}
	// The layout asks again of every part: from here on what is written is
	// kept.
	p.keep()
	if match, ok := readOperator(expr); ok {
		if !p.chainSource(out, match, indent) {
			out.WriteString(one)
		}
		return
	}
	layout, ok := p.splitNode(expr)
	if !ok {
		out.WriteString(one)
		return
	}
	inner := indent + "  "
	// A switch's subject is followed by a space on one line and by the line
	// break when split; no other opening ends in a space.
	out.WriteString(strings.TrimSuffix(layout.open, " "))
	out.WriteByte('\n')
	for i, part := range layout.parts {
		if i > 0 {
			out.WriteString(layout.separator)
		}
		out.WriteString(inner)
		part.layout(p, out, inner)
	}
	out.WriteByte('\n')
	out.WriteString(indent)
	out.WriteString(layout.close)
}

// chainSource writes a chain of one infix operator broken before each
// operator, and reports false, writing nothing, for anything else.
func (p *printer) chainSource(out *strings.Builder, match operatorMatch, indent string) bool {
	if match.spec.fixity != "infix" || match.spec.nonAssociative {
		return false
	}
	parts := p.chainParts(match)
	if len(parts) < 2 {
		return false
	}
	out.WriteString(strings.Join(parts, "\n"+indent+"  "+match.spec.token+" "))
	return true
}

// chainParts is a - b - c as its operands, following the left operand while
// it is the same operator.
func (p *printer) chainParts(match operatorMatch) []string {
	spec := match.spec
	left := []string{p.inline(match.operands[0], spec.leftLevel())}
	if inner, ok := readOperator(match.operands[0]); ok && inner.spec.token == spec.token && inner.spec.fixity == spec.fixity {
		left = p.chainParts(inner)
	}
	return append(left, p.inline(match.operands[1], spec.rightLevel()))
}

// split is a node's parts between an opening and a closing: on one line
// joined by sep, or one part per line joined by separator. It is the one
// description of how a node is printed, so inline and format cannot disagree.
type split struct {
	open, close, sep, separator string
	parts                       []part
}

func (s split) inline(p *printer) string {
	// Most nodes have a few parts, and their text stays on the stack.
	var fixed [8]string
	parts := fixed[:0]
	for _, part := range s.parts {
		parts = append(parts, part.inline(p))
	}
	return s.open + strings.Join(parts, s.sep) + s.close
}

// part is one part of a split: a head — a binding's name, a key, else —
// and the subexpression after it, the head counting toward the line. A
// switch case's head is its matches; a part with no subexpression is a line
// already written, such as a loop clause.
type part struct {
	head    string
	matches []Expr
	expr    Expr
}

func (x part) inline(p *printer) string {
	head := x.lead(func(match Expr) string { return p.inline(match, 0) })
	if x.expr == nil {
		return head
	}
	return head + p.inline(x.expr, 0)
}

// layout writes the part laid out from indent.
func (x part) layout(p *printer, out *strings.Builder, indent string) {
	head := x.lead(func(match Expr) string {
		var text strings.Builder
		p.format(&text, match, indent, 0)
		return text.String()
	})
	out.WriteString(head)
	if x.expr != nil {
		p.format(out, x.expr, indent, utf8.RuneCountInString(head))
	}
}

// lead is the head, or a switch case's, written with its matches.
func (x part) lead(write func(Expr) string) string {
	if x.matches == nil {
		return x.head
	}
	return "case " + strings.Join(kit.Map(x.matches, write), ", ") + " => "
}

func listSplit(opening, closing string, parts []part) split {
	return split{open: opening, close: closing, sep: ", ", separator: ",\n", parts: parts}
}

func exprParts(items []Expr) []part {
	return kit.Map(items, func(item Expr) part { return part{expr: item} })
}

// fieldParts is a record's fields, each name: value.
func fieldParts(fields []RecordFieldExpr) []part {
	return kit.Map(fields, func(field RecordFieldExpr) part { return part{head: field.Name + ": ", expr: field.Value} })
}

// splitNode is how a node that has parts is printed; a leaf has none.
func (p *printer) splitNode(expr Expr) (split, bool) {
	switch node := expr.(type) {
	case *CallExpr:
		return listSplit(node.Name+"(", ")", exprParts(node.Args)), true
	case *ArrayExpr:
		return listSplit("[", "]", exprParts(node.Items)), true
	case *SwitchExpr:
		return p.switchSplit(node), true
	case *ForExpr:
		return p.comprehensionSplit(node), true
	case *ReduceExpr:
		return listSplit("reduce(", ")", []part{{head: p.reduceHead(node)}, {head: p.accumulatorHead(node)}, {expr: node.Body}}), true
	case *LetExpr:
		parts := make([]part, 0, len(node.Bindings)+1)
		for _, binding := range node.Bindings {
			parts = append(parts, part{head: binding.Name + " = ", expr: binding.Value})
		}
		return listSplit("let(", ")", append(parts, part{expr: node.Body})), true
	case *UsingExpr:
		// A rate stays on its line, however long.
		parts := make([]part, 0, len(node.Quotes)+1)
		for _, rate := range node.Quotes {
			parts = append(parts, part{head: p.inline(rate, 0)})
		}
		return listSplit("using(", ")", append(parts, part{expr: node.Body})), true
	default:
		return p.braceSplit(expr)
	}
}

func (p *printer) braceSplit(expr Expr) (split, bool) {
	switch node := expr.(type) {
	case *DictExpr:
		return listSplit("{", "}", kit.Map(node.Entries, func(entry DictEntryExpr) part {
			return part{head: quote(entry.Key) + ": ", expr: entry.Value}
		})), true
	case *RecordExpr:
		return listSplit("{", "}", fieldParts(node.Fields)), true
	case *RecordUpdateExpr:
		return listSplit(p.postfixBase(node.Base)+" with {", "}", fieldParts(node.Fields)), true
	default:
		return split{}, false
	}
}

func (p *printer) switchSplit(node *SwitchExpr) split {
	open := "switch("
	if node.Value != nil {
		open += p.inline(node.Value, 0) + ", "
	}
	parts := make([]part, 0, len(node.Cases)+1)
	for _, branch := range node.Cases {
		parts = append(parts, part{matches: branch.Match, expr: branch.Result})
	}
	if node.Default != nil {
		parts = append(parts, part{head: "else => ", expr: node.Default})
	}
	return listSplit(open, ")", parts)
}

// A comprehension keeps its clauses one per line under what it yields. One
// with a key builds a dictionary and is written in braces; without one it
// builds an array and is written in brackets.
func (p *printer) comprehensionSplit(node *ForExpr) split {
	inner := innerLoop(node)
	opening, closing := "[", "]"
	clauses := p.forClauses(node)
	parts := make([]part, 1, 1+len(clauses))
	parts[0] = part{expr: inner.Yield}
	if inner.YieldKey != nil {
		opening, closing = "{", "}"
		parts[0] = part{head: p.inline(inner.YieldKey, 0) + ": ", expr: inner.Yield}
	}
	for _, clause := range clauses {
		parts = append(parts, part{head: clause})
	}
	return split{open: opening, close: closing, sep: " ", separator: "\n", parts: parts}
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
