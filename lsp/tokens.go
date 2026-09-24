package lsp

import (
	"encoding/json"
	"slices"

	"github.com/nethinwei/funroute/internal/syntax"
)

// Semantic tokens are the lexer's and the parser's reading of each piece of
// source, in the protocol's vocabulary. The client decides how each looks.

var tokenTypes = []string{"keyword", "operator", "variable", "parameter", "function", "property", "number", "string", "enumMember", "comment", "currency"}

var tokenLegend = map[string][]string{"tokenTypes": tokenTypes, "tokenModifiers": {"declaration"}}

// roleTypes is the protocol's name for each role. A program's arguments are
// its parameters; a local where it is bound is a variable's declaration.
var roleTypes = map[syntax.Role]string{
	syntax.RoleKeyword: "keyword", syntax.RoleForm: "keyword", syntax.RoleOperator: "operator",
	syntax.RoleVariable: "variable", syntax.RoleLocalRead: "variable", syntax.RoleLocal: "variable",
	syntax.RoleArgument: "parameter", syntax.RoleFunction: "function", syntax.RoleField: "property",
	syntax.RoleEnumMember: "enumMember", syntax.RoleCurrency: "currency",
}

// literalTypes names a literal by what the lexer read: true and false are
// words.
var literalTypes = map[syntax.Class]string{
	syntax.ClassNumber: "number", syntax.ClassString: "string", syntax.ClassIdentifier: "keyword",
}

// tokenOf is a lexeme's type index and modifiers, or false for one the
// protocol has no type for: punctuation, and text the lexer could not read.
func tokenOf(lexeme syntax.Lexeme) (int, int, bool) {
	name, ok := roleTypes[lexeme.Role]
	switch {
	case lexeme.Role == syntax.RoleLiteral:
		name, ok = literalTypes[lexeme.Class]
	case lexeme.Class == syntax.ClassComment:
		name, ok = "comment", true
	case lexeme.Role == syntax.RoleNone && lexeme.Class == syntax.ClassOperator:
		name, ok = "operator", true
	}
	if !ok {
		return 0, 0, false
	}
	modifiers := 0
	if lexeme.Role == syntax.RoleLocal {
		modifiers = 1
	}
	if i := slices.Index(tokenTypes, name); i >= 0 {
		return i, modifiers, true
	}
	return 0, 0, false
}

func (s *Server) semanticTokens(params json.RawMessage) (any, error) {
	doc, err := s.documentOf(params)
	if err != nil {
		return nil, err
	}
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
