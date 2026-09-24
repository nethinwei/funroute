package lsp

import (
	"github.com/nethinwei/funroute/internal/syntax"
)

// Semantic tokens are the lexer's and the parser's reading of each piece of
// source, in the protocol's vocabulary. The client decides how each looks.

// The index of each type in tokenTypes, as a token carries it.
const (
	tokenKeyword = iota
	tokenOperator
	tokenVariable
	tokenParameter
	tokenFunction
	tokenProperty
	tokenNumber
	tokenString
	tokenEnumMember
	tokenComment
	tokenCurrency
)

var tokenTypes = []string{
	tokenKeyword: "keyword", tokenOperator: "operator", tokenVariable: "variable", tokenParameter: "parameter",
	tokenFunction: "function", tokenProperty: "property", tokenNumber: "number", tokenString: "string",
	tokenEnumMember: "enumMember", tokenComment: "comment", tokenCurrency: "currency",
}

var tokenLegend = map[string][]string{"tokenTypes": tokenTypes, "tokenModifiers": {"declaration"}}

// roleTypes is the protocol's type for each role. A program's arguments are
// its parameters; a local where it is bound is a variable's declaration.
var roleTypes = map[syntax.Role]int{
	syntax.RoleKeyword: tokenKeyword, syntax.RoleForm: tokenKeyword, syntax.RoleOperator: tokenOperator,
	syntax.RoleVariable: tokenVariable, syntax.RoleLocalRead: tokenVariable, syntax.RoleLocal: tokenVariable,
	syntax.RoleArgument: tokenParameter, syntax.RoleFunction: tokenFunction, syntax.RoleField: tokenProperty,
	syntax.RoleEnumMember: tokenEnumMember, syntax.RoleCurrency: tokenCurrency,
}

// literalTypes names a literal by what the lexer read: true and false are
// words.
var literalTypes = map[syntax.Class]int{
	syntax.ClassNumber: tokenNumber, syntax.ClassString: tokenString, syntax.ClassIdentifier: tokenKeyword,
}

// tokenOf is a lexeme's type index and modifiers, or false for one the
// protocol has no type for: punctuation, and text the lexer could not read.
func tokenOf(lexeme syntax.Lexeme) (int, int, bool) {
	typ, ok := roleTypes[lexeme.Role]
	switch {
	case lexeme.Role == syntax.RoleLiteral:
		typ, ok = literalTypes[lexeme.Class]
	case lexeme.Class == syntax.ClassComment:
		typ, ok = tokenComment, true
	case lexeme.Role == syntax.RoleNone && lexeme.Class == syntax.ClassOperator:
		typ, ok = tokenOperator, true
	}
	if !ok {
		return 0, 0, false
	}
	modifiers := 0
	if lexeme.Role == syntax.RoleLocal {
		modifiers = 1
	}
	return typ, modifiers, true
}

func (s *Server) semanticTokens(doc *document) (any, error) {
	lexemes := lexemesOf(doc)
	data := []int{}
	previous := Position{}
	for _, lexeme := range lexemes {
		typ, modifiers, ok := tokenOf(lexeme)
		start, end := doc.position(lexeme.Start, s.encoding), doc.position(lexeme.End, s.encoding)
		if !ok || start.Line != end.Line {
			continue
		}
		delta := start.Character
		if start.Line == previous.Line {
			delta -= previous.Character
		}
		data = append(data, start.Line-previous.Line, delta, end.Character-start.Character, typ, modifiers)
		previous = start
	}
	return map[string][]int{"data": data}, nil
}
