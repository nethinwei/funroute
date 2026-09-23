package syntax

import (
	"bytes"
	"encoding/json"
	"sort"
	"strconv"
	"strings"

	"funroute/lang/internal/machine"
)

// Printing is the parser read backwards. Operators are recognised by the
// same table that expands them, so add(a, b) prints as a + b, and what is
// printed parses back to the same program: that is the property the tests
// hold every printed program to.

// Inline writes expr on one line.
func Inline(expr Expr) string { return inline(expr, 0) }

// operatorMatch is a node read as the operator it was expanded from.
type operatorMatch struct {
	spec     operatorSpec
	operands []Expr
}

// readingOrder is the operator table from the most specific reading down.
var readingOrder = func() []operatorSpec {
	order := append([]operatorSpec(nil), sourceOperators...)
	sort.SliceStable(order, func(i, j int) bool { return order[i].specificity() > order[j].specificity() })
	return order
}()

func readOperator(expr Expr) (operatorMatch, bool) {
	for _, spec := range readingOrder {
		if operands, ok := spec.read(expr); ok {
			return operatorMatch{spec: spec, operands: operands}, true
		}
	}
	return operatorMatch{}, false
}

// postfixPrecedence is how tightly xs[i] and r.field bind: tighter than any
// prefix or infix operator, so what they apply to keeps its parentheses.
var postfixPrecedence = func() int {
	for _, spec := range sourceOperators {
		if spec.fixity == "index" {
			return spec.precedence
		}
	}
	panic("the operator table has no index operator")
}()

func inline(expr Expr, parent int) string {
	if match, ok := readOperator(expr); ok {
		return operatorSource(match, parent)
	}
	switch node := expr.(type) {
	case *VariableExpr:
		return node.Name
	case *LiteralExpr:
		return literalSource(node.Value)
	case *EnumExpr:
		return node.Source()
	case *FieldExpr:
		return postfixBase(node.Value) + "." + node.Field
	case *CallExpr:
		return node.Name + "(" + joinInline(node.Args) + ")"
	case *ArrayExpr:
		return "[" + joinInline(node.Items) + "]"
	default:
		return compoundSource(expr)
	}
}

func compoundSource(expr Expr) string {
	switch node := expr.(type) {
	case *DictExpr:
		return "{" + joinParts(len(node.Entries), func(i int) string {
			return quote(node.Entries[i].Key) + ": " + inline(node.Entries[i].Value, 0)
		}) + "}"
	case *RecordExpr:
		return "{" + joinParts(len(node.Fields), func(i int) string {
			return node.Fields[i].Name + ": " + inline(node.Fields[i].Value, 0)
		}) + "}"
	case *SwitchExpr:
		return switchSource(node)
	case *ForExpr:
		return comprehensionSource(node)
	case *ReduceExpr:
		return "reduce(" + reduceHead(node) + ", " + accumulatorHead(node) + ", " + inline(node.Body, 0) + ")"
	case *LetExpr:
		return "let(" + joinParts(len(node.Bindings), func(i int) string {
			return node.Bindings[i].Name + " = " + inline(node.Bindings[i].Value, 0)
		}) + ", " + inline(node.Body, 0) + ")"
	}
	panic("print: unknown node " + expr.kind())
}

func operatorSource(match operatorMatch, parent int) string {
	spec := match.spec
	var text string
	switch spec.fixity {
	case "index":
		return postfixBase(match.operands[0]) + "[" + inline(match.operands[1], 0) + "]"
	case "prefix":
		text = spec.token + inline(match.operands[0], spec.precedence)
	default:
		left := inline(match.operands[0], spec.precedence)
		// Left associative: a right operand of the same precedence keeps its
		// parentheses, so a - (b - c) stays and (a - b) - c loses them.
		right := inline(match.operands[1], spec.precedence+1)
		text = left + " " + spec.token + " " + right
	}
	if spec.precedence < parent {
		return "(" + text + ")"
	}
	return text
}

// postfixBase writes what an index or a field read applies to. A number is
// parenthesised too, since 1.x would lex as the start of a float.
func postfixBase(expr Expr) string {
	if isNumber(expr) {
		return "(" + inline(expr, 0) + ")"
	}
	return inline(expr, postfixPrecedence)
}

func literalSource(value machine.Value) string {
	switch value.Kind() {
	case machine.IntKind:
		number, _ := value.Int()
		return strconv.FormatInt(number, 10)
	case machine.FloatKind:
		number, _ := value.Float()
		return floatLiteral(strconv.FormatFloat(number, 'g', -1, 64))
	case machine.StringKind:
		text, _ := value.String()
		return quote(text)
	default:
		flag, _ := value.Bool()
		return strconv.FormatBool(flag)
	}
}

// floatLiteral keeps a float looking like one: 2 would read back as an int.
func floatLiteral(text string) string {
	if strings.ContainsAny(text, ".eE") {
		return text
	}
	return text + ".0"
}

// quote writes a string with JSON escapes, which the lexer reads back.
func quote(text string) string {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(text)
	return strings.TrimSuffix(buf.String(), "\n")
}

func joinInline(items []Expr) string {
	return joinParts(len(items), func(i int) string { return inline(items[i], 0) })
}

func joinParts(count int, part func(int) string) string {
	parts := make([]string, count)
	for i := range parts {
		parts[i] = part(i)
	}
	return strings.Join(parts, ", ")
}

func switchSource(node *SwitchExpr) string {
	head := ""
	if node.Value != nil {
		head = inline(node.Value, 0) + ", "
	}
	branches := joinParts(len(node.Cases), func(i int) string {
		return "case " + joinInline(node.Cases[i].Match) + " => " + inline(node.Cases[i].Result, 0)
	})
	if node.Default == nil {
		return "switch(" + head + branches + ")"
	}
	return "switch(" + head + branches + ", else " + inline(node.Default, 0) + ")"
}

// A comprehension with a key builds a dictionary and is written in braces;
// without one it builds an array and is written in brackets.
func comprehensionSource(node *ForExpr) string {
	clauses := strings.Join(forClauses(node), " ")
	inner := innerLoop(node)
	if inner.YieldKey == nil {
		return "[" + inline(inner.Yield, 0) + " " + clauses + "]"
	}
	return "{" + inline(inner.YieldKey, 0) + ": " + inline(inner.Yield, 0) + " " + clauses + "}"
}

// A spliced loop's yield is the loop written after it, so the chain prints as
// the one comprehension someone wrote: [e for x in xs for y in ys].
func forClauses(node *ForExpr) []string {
	clauses := []string{"for " + loopVariables(node.KeyVariable, node.Variable) + " in " + inline(node.Source, 0)}
	if node.Where != nil {
		clauses = append(clauses, "if "+inline(node.Where, 0))
	}
	if spliced(node) {
		clauses = append(clauses, forClauses(node.Yield.(*ForExpr))...)
	}
	return clauses
}

// innerLoop is the clause that yields the element: the outer ones splice.
func innerLoop(node *ForExpr) *ForExpr {
	for spliced(node) {
		node = node.Yield.(*ForExpr)
	}
	return node
}

// spliced is what the flatten flag means: the yield is the next clause.
func spliced(node *ForExpr) bool {
	_, loop := node.Yield.(*ForExpr)
	return node.Flatten && loop
}

func loopVariables(key, value string) string {
	if key == "" {
		return value
	}
	return key + ", " + value
}

func reduceHead(node *ReduceExpr) string {
	head := loopVariables(node.KeyVariable, node.Variable) + " in " + inline(node.Source, 0)
	if node.Where == nil {
		return head
	}
	return head + " if " + inline(node.Where, 0)
}

func accumulatorHead(node *ReduceExpr) string {
	return node.Accumulator + " = " + inline(node.Init, 0)
}
