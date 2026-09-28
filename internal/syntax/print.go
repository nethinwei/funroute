package syntax

import (
	"bytes"
	"cmp"
	"encoding/json"
	"slices"
	"strconv"
	"strings"

	"github.com/nethinwei/funroute/internal/machine"
)

// Printing is the parser read backwards. Operators are recognised by the
// same table that expands them, so add(a, b) prints as a + b, and what is
// printed parses back to the same program: that is the property the tests
// hold every printed program to.

// operatorMatch is a node read as the operator it was expanded from.
type operatorMatch struct {
	spec     operatorSpec
	operands []Expr
}

// readingOrder is the operator table from the most specific reading down.
var readingOrder = func() []operatorSpec {
	order := slices.Clone(sourceOperators)
	slices.SortStableFunc(order, func(a, b operatorSpec) int { return cmp.Compare(b.specificity(), a.specificity()) })
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
var postfixPrecedence = sourceOperators[slices.IndexFunc(sourceOperators, func(spec operatorSpec) bool { return spec.fixity == "index" })].precedence

func inline(expr Expr, parent int) string {
	if match, ok := readOperator(expr); ok {
		return operatorSource(match, parent)
	}
	switch node := expr.(type) {
	case *VariableExpr:
		return node.Name
	case *LiteralExpr:
		return literalSource(node)
	case *EnumExpr:
		return node.Source()
	case *MoneyExpr:
		// The sign is the amount's: USD -1.70.
		return node.Currency + " " + node.Amount
	case *CurrencyExpr:
		return node.Code
	case *FxRateExpr:
		// Next to * or / it is bracketed for the reader; the parser would
		// read it as one literal either way.
		text := node.Rate + " " + node.Quote + " / " + node.Base
		if parent >= binaryOperators[opKey{kind: tokenStar}].precedence {
			return "(" + text + ")"
		}
		return text
	case *RatioExpr:
		return node.Value + node.Unit
	case *FieldExpr:
		return postfixBase(node.Value) + "." + node.Field
	case *SelectorExpr:
		return "." + node.Path
	}
	layout, ok := splitNode(expr)
	if !ok {
		panic("print: unknown node " + kindOf(expr, planOf(expr)))
	}
	return layout.inline()
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
		// Left associative: a right operand of the same precedence keeps its
		// parentheses, so a - (b - c) stays and (a - b) - c loses them; a
		// comparison keeps them on both sides, and -> on any right operand
		// that is not postfix (leftLevel, rightLevel).
		left := inline(match.operands[0], spec.leftLevel())
		right := inline(match.operands[1], spec.rightLevel())
		text = left + " " + spec.token + " " + right
	}
	if spec.precedence < parent {
		return "(" + text + ")"
	}
	return text
}

// postfixBase writes what a subscript or a field read applies to. Anything
// that ends in a number or a code is parenthesised, or the lexer would read
// the . or [ into it: (1).x is not 1.x, (@a).b is not the qualified member
// @a.b, and so for (USD 1).x, (2.9%)[0] and (150 JPY / USD).x; (.a).b is
// not the selector .a.b.
func postfixBase(expr Expr) string {
	switch expr.(type) {
	case *EnumExpr, *RatioExpr, *MoneyExpr, *FxRateExpr, *CurrencyExpr, *SelectorExpr:
		return "(" + inline(expr, 0) + ")"
	}
	if isNumber(expr) {
		return "(" + inline(expr, 0) + ")"
	}
	return inline(expr, postfixPrecedence)
}

func literalSource(literal *LiteralExpr) string {
	value := literal.Value
	switch value.Kind() {
	case machine.IntKind:
		number, _ := value.Int()
		return strconv.FormatInt(number, 10)
	case machine.FloatKind:
		return floatLiteral(literal.Decimal.String())
	case machine.StringKind:
		text, _ := value.String()
		return quote(text)
	case machine.DurationKind:
		length, _ := value.Duration()
		return durationText(length)
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

// A spliced loop's yield is the loop written after it, so the chain prints as
// the one comprehension someone wrote: [e for x in xs for y in ys].
func forClauses(node *ForExpr) []string {
	clauses := []string{"for " + loopVariables(node.KeyVariable, node.Variable) + " in " + inline(node.Source, 0)}
	if node.Where != nil {
		clauses = append(clauses, "if "+inline(node.Where, 0))
	}
	if next, ok := spliced(node); ok {
		clauses = append(clauses, forClauses(next)...)
	}
	return clauses
}

// innerLoop is the clause that yields the element: the outer ones splice.
func innerLoop(node *ForExpr) *ForExpr {
	for {
		next, ok := spliced(node)
		if !ok {
			return node
		}
		node = next
	}
}

// spliced is what the flatten flag means: the yield is the next clause,
// which it returns.
func spliced(node *ForExpr) (*ForExpr, bool) {
	next, loop := node.Yield.(*ForExpr)
	return next, node.Flatten && loop
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
