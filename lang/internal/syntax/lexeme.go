package syntax

import "sort"

// Lexemes says what every piece of a source is in this language: its lexical
// class, from the lexer, and its role, from the parser. Nothing here decides
// either one — the lexer and the parser already do while they read a program;
// this keeps what they decided instead of throwing it away. How a piece is
// shown is for whoever asks.

// Class is what the lexer read.
type Class string

const (
	ClassIdentifier  Class = "identifier"
	ClassNumber      Class = "number"
	ClassString      Class = "string"
	ClassEnum        Class = "enum"
	ClassOperator    Class = "operator"
	ClassPunctuation Class = "punctuation"
	ClassComment     Class = "comment"
	ClassInvalid     Class = "invalid"
)

// Role is what the parser took a piece to be. Punctuation, comments and text
// that could not be read have none: their class says all there is.
type Role string

const (
	RoleNone       Role = ""
	RoleKeyword    Role = "keyword"     // case, else, for, in, if of a loop
	RoleOperator   Role = "operator"    // an infix or prefix operator, the word in included
	RoleVariable   Role = "variable"    // a name read as a value, in a program that did not parse
	RoleArgument   Role = "argument"    // a name read as a value that is one of the program's arguments
	RoleLocalRead  Role = "local_read"  // a name read as a value that a form binds
	RoleLocal      Role = "local"       // a name where a form binds it: let, a loop, an accumulator
	RoleFunction   Role = "function"    // the name of a call
	RoleForm       Role = "form"        // let, reduce, switch: a call-shaped form
	RoleField      Role = "field"       // a record field, read or written
	RoleLiteral    Role = "literal"     // a number, a string, true or false
	RoleEnumMember Role = "enum_member" // @member or @enum.member
)

type roleMark struct {
	end  int
	role Role
}

// Lexeme is one piece of source, from Start up to End (byte offsets). The
// pieces cover the source except its whitespace, in order.
type Lexeme struct {
	Start int   `json:"start"`
	End   int   `json:"end"`
	Class Class `json:"class"`
	Role  Role  `json:"role,omitempty"`
}

// Lexemes reads source the way Parse does and reports every piece of it. A
// source that does not parse still gets all its pieces, and the roles of the
// part the parser got through; the error is the one Parse returns.
func Lexemes(source string) ([]Lexeme, error) {
	r := read(source, true)
	if r.err == nil {
		readsOf(r.expr, r.parser.roles)
	}
	all := append(r.tokens[:len(r.tokens)-1:len(r.tokens)-1], r.lex.comments...)
	sort.Slice(all, func(i, j int) bool { return all[i].pos < all[j].pos })
	var out []Lexeme
	for _, tok := range all {
		out = append(out, pieces(tok, r.parser.roles)...)
	}
	return out, r.err
}

// readsOf tells, for a program that parsed, which names read are locals and
// which are its arguments, by the scope rule FreeVariables follows. The mark
// to refine is the one the parser left on the name itself, at Pos — the span
// of a parenthesised name starts at its parenthesis — and a read the parser
// marked nothing for gets no mark here either.
func readsOf(root Expr, roles map[int]roleMark) {
	eachVariable(root, func(variable *VariableExpr, local bool) {
		mark, ok := roles[variable.Pos]
		if !ok {
			return
		}
		mark.role = RoleArgument
		if local {
			mark.role = RoleLocalRead
		}
		roles[variable.Pos] = mark
	})
}

// pieces is the one lexeme a token is, or, for a dotted name the parser split
// into a variable and the fields read off it, each part and the dots between.
func pieces(tok token, roles map[int]roleMark) []Lexeme {
	class := classOf(tok.kind)
	if mark, ok := roles[tok.pos]; !ok || mark.end >= tok.end {
		return []Lexeme{{Start: tok.pos, End: tok.end, Class: class, Role: mark.role}}
	}
	var out []Lexeme
	for at := tok.pos; at < tok.end; {
		mark, ok := roles[at]
		if !ok {
			out = append(out, Lexeme{Start: at, End: at + 1, Class: ClassPunctuation})
			at++
			continue
		}
		// A mark always covers at least one byte ahead of it, so the walk
		// through the token always moves on.
		end := min(max(mark.end, at+1), tok.end)
		out = append(out, Lexeme{Start: at, End: end, Class: class, Role: mark.role})
		at = end
	}
	return out
}

func classOf(kind tokenKind) Class {
	switch kind {
	case tokenIdentifier:
		return ClassIdentifier
	case tokenInt, tokenFloat:
		return ClassNumber
	case tokenString:
		return ClassString
	case tokenEnum:
		return ClassEnum
	case tokenComment:
		return ClassComment
	case tokenInvalid:
		return ClassInvalid
	}
	if _, punctuation := punctuationKinds[kind]; punctuation {
		return ClassPunctuation
	}
	return ClassOperator
}

var punctuationKinds = func() map[tokenKind]bool {
	out := map[tokenKind]bool{}
	for _, kind := range singleCharTokens {
		out[kind] = true
	}
	return out
}()
