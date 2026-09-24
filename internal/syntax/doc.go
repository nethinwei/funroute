// Package syntax is how a FunRoute program is written: the lexer, the parser
// and its AST, ExprJSON, the printer and formatter, and the facts a language
// server shows — lexemes, scopes and the syntax tree. It depends only on
// machine, for names and literal values.
//
// Each node is described once, by its struct in ast.go: its tags say what its
// ExprJSON fields are and which names it binds where, and the walker reads
// them, so importing, exporting, scoping and the tree follow from that one
// description rather than from code written per node.
package syntax
