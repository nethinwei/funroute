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

type operatorSpec struct {
	kind          tokenKind
	token         string
	fixity        string
	associativity string
	precedence    int
	function      string
	form          string
	operands      []string
	expansion     operatorExpansion
}

// sourceOperators is the one operator definition used by the lexer, parser,
// catalog and browser formatter. Adding syntax here cannot leave the JS SDK
// with stale precedence or a different canonical expansion.
var sourceOperators = []operatorSpec{
	{tokenOrOr, "||", "infix", "left", 1, "", "or", []string{"left", "right"}, expandOr},
	{tokenAndAnd, "&&", "infix", "left", 2, "", "and", []string{"left", "right"}, expandAnd},
	{tokenEqEq, "==", "infix", "left", 3, "eq", "", []string{"left", "right"}, expandCall},
	{tokenBangEq, "!=", "infix", "left", 3, "", "", []string{"left", "right"}, expandNotEqual},
	{tokenLess, "<", "infix", "left", 4, "lt", "", []string{"left", "right"}, expandCall},
	{tokenLessEq, "<=", "infix", "left", 4, "le", "", []string{"left", "right"}, expandCall},
	{tokenGreater, ">", "infix", "left", 4, "gt", "", []string{"left", "right"}, expandCall},
	{tokenGreaterEq, ">=", "infix", "left", 4, "ge", "", []string{"left", "right"}, expandCall},
	{tokenPlus, "+", "infix", "left", 5, "add", "", []string{"left", "right"}, expandCall},
	{tokenMinus, "-", "infix", "left", 5, "sub", "", []string{"left", "right"}, expandCall},
	{tokenStar, "*", "infix", "left", 6, "mul", "", []string{"left", "right"}, expandCall},
	{tokenSlash, "/", "infix", "left", 6, "div", "", []string{"left", "right"}, expandCall},
	{tokenPercent, "%", "infix", "left", 6, "mod", "", []string{"left", "right"}, expandCall},
	// "in" is spelled as a word, so it is matched by text rather than by token
	// kind; "[]" is postfix and is matched by the parser where a primary ends.
	{tokenIdentifier, "in", "infix", "left", 4, "member", "", []string{"left", "right"}, expandCall},
	{tokenLeftBracket, "[]", "index", "left", 8, "at", "", []string{"left", "right"}, expandCall},
	{tokenBang, "!", "prefix", "", 7, "", "not", []string{"operand"}, expandNot},
	{tokenMinus, "-", "prefix", "", 7, "sub", "", []string{"operand"}, expandNegate},
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

func SourceSyntax() machine.SourceSyntax {
	operators := make([]machine.SourceOperatorDescriptor, len(sourceOperators))
	for i, spec := range sourceOperators {
		operators[i] = machine.SourceOperatorDescriptor{
			Token: spec.token, Fixity: spec.fixity, Associativity: spec.associativity,
			Precedence: spec.precedence, Form: spec.form,
			Operands: append([]string(nil), spec.operands...), Template: spec.template(),
		}
	}
	return machine.SourceSyntax{
		ExprJSONVersion: ExprJSONVersion, VariableNamePattern: `[A-Za-z_][A-Za-z0-9_]*`,
		Keywords: []string{"case", "else", "in", "for"}, Operators: operators,
	}
}

func (s operatorSpec) template() machine.ExpressionTemplate {
	left, right := placeholder("left"), placeholder("right")
	switch s.expansion {
	case expandCall:
		return callTemplate(s.function, left, right)
	case expandNotEqual:
		return pickTemplate(callTemplate("eq", left, right), boolTemplate(false), boolTemplate(true))
	case expandAnd:
		return pickTemplate(left, right, boolTemplate(false))
	case expandOr:
		return pickTemplate(left, boolTemplate(true), right)
	case expandNot:
		return pickTemplate(placeholder("operand"), boolTemplate(false), boolTemplate(true))
	case expandNegate:
		return callTemplate("sub", intTemplate(0), placeholder("operand"))
	default:
		return machine.ExpressionTemplate{}
	}
}

func placeholder(name string) machine.ExpressionTemplate {
	return machine.ExpressionTemplate{Placeholder: name}
}

func callTemplate(name string, args ...machine.ExpressionTemplate) machine.ExpressionTemplate {
	return machine.ExpressionTemplate{Node: "call", Name: name, Args: args}
}

func pickTemplate(condition, whenTrue, whenFalse machine.ExpressionTemplate) machine.ExpressionTemplate {
	return callTemplate("if", condition, whenTrue, whenFalse)
}

func boolTemplate(value bool) machine.ExpressionTemplate {
	return machine.ExpressionTemplate{Node: "bool", Bool: &value}
}

func intTemplate(value int64) machine.ExpressionTemplate {
	return machine.ExpressionTemplate{Node: "int", Int: &value}
}
