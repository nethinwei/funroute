package syntax

import (
	"funroute/lang/internal/machine"
	"sort"
)

type operatorExpansion uint8

const (
	expandCall operatorExpansion = iota
	expandNotEqual
	expandAnd
	expandOr
	expandNot
	expandNegate
)

// operatorSpec is one operator. Every infix operator is left associative —
// the parser climbs precedence that way and the printer parenthesises a right
// operand of equal precedence — so there is no column for it.
type operatorSpec struct {
	kind       tokenKind
	token      string
	fixity     string
	precedence int
	function   string
	arity      int
	expansion  operatorExpansion
}

// sourceOperators is the one operator definition: the lexer, the parser and
// the printer all read it, so a new operator is one row here.
var sourceOperators = []operatorSpec{
	{tokenOrOr, "||", "infix", 1, "", 2, expandOr},
	{tokenAndAnd, "&&", "infix", 2, "", 2, expandAnd},
	{tokenEqEq, "==", "infix", 3, "eq", 2, expandCall},
	{tokenBangEq, "!=", "infix", 3, "", 2, expandNotEqual},
	{tokenLess, "<", "infix", 4, "lt", 2, expandCall},
	{tokenLessEq, "<=", "infix", 4, "le", 2, expandCall},
	{tokenGreater, ">", "infix", 4, "gt", 2, expandCall},
	{tokenGreaterEq, ">=", "infix", 4, "ge", 2, expandCall},
	{tokenPlus, "+", "infix", 5, "add", 2, expandCall},
	{tokenMinus, "-", "infix", 5, "sub", 2, expandCall},
	{tokenStar, "*", "infix", 6, "mul", 2, expandCall},
	{tokenSlash, "/", "infix", 6, "div", 2, expandCall},
	{tokenPercent, "%", "infix", 6, "mod", 2, expandCall},
	// "in" is spelled as a word, so it is matched by text rather than by token
	// kind; "[]" is postfix and is matched by the parser where a primary ends.
	{tokenIdentifier, "in", "infix", 4, "member", 2, expandCall},
	{tokenLeftBracket, "[]", "index", 8, "at", 2, expandCall},
	{tokenBang, "!", "prefix", 7, "", 1, expandNot},
	{tokenMinus, "-", "prefix", 7, "sub", 1, expandNegate},
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
	case expandCall:
		return args, call.Name == s.function && len(args) == s.arity
	case expandNegate:
		return args[1:], call.Name == "sub" && len(args) == 2 && isInt(args[0], 0) && !isNumber(args[1])
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
