package syntax

import (
	"sort"

	"github.com/nethinwei/funroute/internal/machine"
)

type operatorExpansion uint8

const (
	expandCall operatorExpansion = iota
	expandNotEqual
	expandAnd
	expandOr
	expandNot
	expandNegate
	expandConvert
)

// operatorSpec is one operator. An infix operator is left associative
// unless nonAssociative says it may not follow one of its own precedence:
// the comparisons, so a < b < c and a == b == c are errors rather than
// (a < b) < c. postfixRight says its right operand is a postfix expression,
// not the next tighter level: amount -> JPY + fee is an error rather than
// amount -> (JPY + fee), and the operators tighter than it cannot follow it.
type operatorSpec struct {
	kind           tokenKind
	token          string
	fixity         string
	precedence     int
	function       string
	arity          int
	expansion      operatorExpansion
	nonAssociative bool
	postfixRight   bool
}

// sourceOperators is the one operator definition: the lexer, the parser and
// the printer all read it, so a new operator is one row here.
var sourceOperators = []operatorSpec{
	{tokenOrOr, "||", "infix", 1, "", 2, expandOr, false, false},
	{tokenAndAnd, "&&", "infix", 2, "", 2, expandAnd, false, false},
	{tokenEqEq, "==", "infix", 3, "eq", 2, expandCall, true, false},
	{tokenBangEq, "!=", "infix", 3, "", 2, expandNotEqual, true, false},
	{tokenLess, "<", "infix", 4, "lt", 2, expandCall, true, false},
	{tokenLessEq, "<=", "infix", 4, "le", 2, expandCall, true, false},
	{tokenGreater, ">", "infix", 4, "gt", 2, expandCall, true, false},
	{tokenGreaterEq, ">=", "infix", 4, "ge", 2, expandCall, true, false},
	// amount -> JPY converts at the quotes of its using. It binds looser
	// than arithmetic, so fee + amount -> JPY converts the sum, and tighter
	// than comparison; a bare currency code after it is that currency.
	{tokenArrow, "->", "infix", 5, "convert", 2, expandConvert, false, true},
	{tokenPlus, "+", "infix", 6, "add", 2, expandCall, false, false},
	{tokenMinus, "-", "infix", 6, "sub", 2, expandCall, false, false},
	{tokenStar, "*", "infix", 7, "mul", 2, expandCall, false, false},
	{tokenSlash, "/", "infix", 7, "div", 2, expandCall, false, false},
	{tokenPercent, "%", "infix", 7, "mod", 2, expandCall, false, false},
	// "in" is spelled as a word, so it is matched by text rather than by token
	// kind; "[]" is postfix and is matched by the parser where a primary ends.
	{tokenIdentifier, "in", "infix", 4, "member", 2, expandCall, true, false},
	{tokenLeftBracket, "[]", "index", 9, "at", 2, expandCall, false, false},
	{tokenBang, "!", "prefix", 8, "", 1, expandNot, false, false},
	{tokenMinus, "-", "prefix", 8, "sub", 1, expandNegate, false, false},
}

var binaryOperators = operatorMap("infix")
var unaryOperators = operatorMap("prefix")

func operatorMap(fixity string) map[tokenKind]operatorSpec {
	out := make(map[tokenKind]operatorSpec)
	for _, spec := range sourceOperators {
		if spec.fixity == fixity && !spec.spelledAsWord() {
			out[spec.kind] = spec
		}
	}
	return out
}

// keywordOperators holds the infix operators written as words. They cannot be
// indexed by token kind, because that kind is "identifier" — the same kind a
// variable has — so the parser looks them up by text.
var keywordOperators = func() map[string]operatorSpec {
	out := make(map[string]operatorSpec)
	for _, spec := range sourceOperators {
		if spec.fixity == "infix" && spec.spelledAsWord() {
			out[spec.token] = spec
		}
	}
	return out
}()

// leftLevel is the precedence an infix operator's left operand must reach
// to go without parentheses: its own, or one more when it does not chain.
func (s operatorSpec) leftLevel() int {
	if s.nonAssociative {
		return s.precedence + 1
	}
	return s.precedence
}

// rightLevel is the same for the right operand: one more than its own, so a
// right operand of equal precedence keeps its parentheses, or a postfix
// expression's.
func (s operatorSpec) rightLevel() int {
	if s.postfixRight {
		return postfixPrecedence
	}
	return s.precedence + 1
}

// spelledAsWord reports an operator whose token is a word, not punctuation.
func (s operatorSpec) spelledAsWord() bool { return s.kind == tokenIdentifier }

func lexedOperators() []struct {
	text string
	kind tokenKind
} {
	out := []struct {
		text string
		kind tokenKind
	}{{"=>", tokenFatArrow}, {"=", tokenAssign}}
	seen := map[string]bool{"=>": true, "=": true}
	for _, spec := range sourceOperators {
		// Words are lexed as identifiers and "[]" is punctuation the lexer
		// already knows; neither is a symbol to match here.
		if seen[spec.token] || spec.spelledAsWord() || spec.fixity == "index" {
			continue
		}
		seen[spec.token] = true
		out = append(out, struct {
			text string
			kind tokenKind
		}{spec.token, spec.kind})
	}
	sort.SliceStable(out, func(i, j int) bool { return len(out[i].text) > len(out[j].text) })
	return out
}

// read is expandOperator backwards: the operands of expr when it is what
// this operator expands into, as the parser builds it. A minus in front of a
// number is not read as one, because the lexer would read the two together
// as a negative number: sub(0, 1) is 0 - 1.
func (s operatorSpec) read(expr Expr) ([]Expr, bool) {
	call, ok := expr.(*CallExpr)
	if !ok {
		return nil, false
	}
	args := call.Args
	switch s.expansion {
	case expandCall, expandConvert:
		return args, call.Name == s.function && len(args) == s.arity
	case expandNegate:
		// The arity is checked before the slice: f() has no args[1:].
		if call.Name != "sub" || len(args) != 2 {
			return nil, false
		}
		return args[1:], isInt(args[0], 0) && !isNumber(args[1])
	}
	if call.Name != "if" || len(args) != 3 {
		return nil, false
	}
	switch s.expansion {
	case expandNotEqual:
		equal, isEqual := args[0].(*CallExpr)
		return equalArgs(equal), isEqual && equal.Name == "eq" && len(equal.Args) == 2 && isBool(args[1], false) && isBool(args[2], true)
	case expandAnd:
		return args[:2], isBool(args[2], false)
	case expandOr:
		return []Expr{args[0], args[2]}, isBool(args[1], true)
	default: // expandNot
		return args[:1], isBool(args[1], false) && isBool(args[2], true)
	}
}

func equalArgs(equal *CallExpr) []Expr {
	if equal == nil {
		return nil
	}
	return equal.Args
}

// specificity orders the readings of one node: sub(0, x) is both 0 - x and
// -x, and if(eq(a, b), false, true) both a != b and !(a == b). The reading
// that fixes more of the tree is the one that was written.
func (s operatorSpec) specificity() int {
	switch s.expansion {
	case expandNotEqual:
		return 3
	case expandNot:
		return 2
	case expandCall:
		return 0
	default:
		return 1
	}
}

func isBool(expr Expr, want bool) bool {
	literal, ok := expr.(*LiteralExpr)
	flag, isFlag := literal.valueOr().Bool()
	return ok && isFlag && flag == want
}

func isInt(expr Expr, want int64) bool {
	literal, ok := expr.(*LiteralExpr)
	number, isInt := literal.valueOr().Int()
	return ok && isInt && number == want
}

func isNumber(expr Expr) bool {
	literal, ok := expr.(*LiteralExpr)
	kind := literal.valueOr().Kind()
	return ok && (kind == machine.IntKind || kind == machine.FloatKind)
}

// valueOr is the literal's value, or nothing when there is no literal.
func (e *LiteralExpr) valueOr() machine.Value {
	if e == nil {
		return machine.Value{}
	}
	return e.Value
}

// Operators lists every operator as its fixity and spelling, fixity:token,
// in the parser's table order.
func Operators() []string {
	out := make([]string, len(sourceOperators))
	for i, spec := range sourceOperators {
		out[i] = spec.fixity + ":" + spec.token
	}
	return out
}
