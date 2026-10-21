package lsp

import (
	"fmt"
	"slices"

	"github.com/nethinwei/funroute/internal/compile"
	"github.com/nethinwei/funroute/internal/kit"
	"github.com/nethinwei/funroute/internal/machine"
	"github.com/nethinwei/funroute/internal/syntax"
)

// updateFields is what can be named at a field of a record update being
// written, order with {|}: the fields of the base's record type not written
// there yet, in the record's order. It reports false anywhere else, and when
// the base's type cannot be told, so completion goes on as it would have.
//
// Both facts come from the language: the parser says the cursor is at a field
// name of an update, and inference says what the base is — asked about the
// program with the update replaced by its base, which is the one thing it
// does not need to see finished.
func (s *Server) updateFields(doc *document, offset int) ([]completionItem, bool) {
	if !inUpdate(doc, offset) {
		return nil, false
	}
	return s.fieldsAt(doc, offset, placeholder+": 0", func(tree syntax.Expr) (base, around syntax.Span, record reach, written []string, ok bool) {
		base, around, written, ok = syntax.UpdatedRecordAt(tree, offset, placeholder)
		return base, around, recordOf, written, ok
	})
}

// inUpdate reports a cursor inside the braces of an update, order with {|:
// the innermost brace still open before it follows a with. Only there is the
// text parsed to look for the update's fields.
func inUpdate(doc *document, offset int) bool {
	lexemes, depth := lexemesOf(doc), 0
	for i, lexeme := range slices.Backward(lexemes) {
		if lexeme.End > offset || lexeme.Class != syntax.ClassPunctuation {
			continue
		}
		switch doc.text[lexeme.Start:lexeme.End] {
		case "}":
			depth++
		case "{":
			if depth == 0 {
				return followsWith(doc, lexemes[:i])
			}
			depth--
		}
	}
	return false
}

// followsWith reports that the last of lexemes, past comments, is a with.
func followsWith(doc *document, lexemes []syntax.Lexeme) bool {
	for _, lexeme := range slices.Backward(lexemes) {
		if lexeme.Class != syntax.ClassComment {
			return lexeme.Class == syntax.ClassIdentifier && doc.text[lexeme.Start:lexeme.End] == "with"
		}
	}
	return false
}

// selectorFields is what a selector being written can name, sort_by(xs, .|):
// the fields of the items of the list its call reads, or of the record the
// fields it names before reach, read off the program with the call replaced
// by that list.
func (s *Server) selectorFields(doc *document, offset int) ([]completionItem, bool) {
	if !typingSelector(doc, offset) {
		return nil, false
	}
	return s.fieldsAt(doc, offset, placeholder, func(tree syntax.Expr) (base, around syntax.Span, record reach, written []string, ok bool) {
		base, around, path, ok := syntax.SelectedListAt(tree, offset, placeholder)
		return base, around, itemRecord(path), nil, ok
	})
}

// typingSelector reports a cursor in a selector's name, after its dot or in
// what is typed of the name — .|, .fe|, .a.|, or past spaces and comments
// the parser reads between them, . fe| — and nowhere a selector cannot be.
func typingSelector(doc *document, offset int) bool {
	at := offset
	for at > 0 && kit.IsNameChar(doc.text[at-1]) {
		at--
	}
	if at > 0 && doc.text[at-1] == '.' {
		return true
	}
	dot := false
	for _, lexeme := range lexemesOf(doc) {
		if lexeme.End > at {
			break
		}
		if lexeme.Class != syntax.ClassComment {
			dot = doc.text[lexeme.Start:lexeme.End] == "."
		}
	}
	return dot
}

// itemRecord reaches, from a list's type, the record path leads to from its
// items.
func itemRecord(path []string) reach {
	return func(list machine.Type) (machine.Type, bool) {
		typ, ok := list.Elem()
		for _, name := range path {
			at := typ.FieldIndex(name)
			if !ok || at < 0 {
				return machine.Type{}, false
			}
			typ = typ.Fields()[at].Type()
		}
		if !ok {
			return machine.Type{}, false
		}
		return recordOf(typ)
	}
}

// fieldsAt is the fields of the record find locates, with filler typed at
// the cursor: find reads the span whose type leads to the record (base),
// the span to stand the base in for (around), how the record is reached from
// the base's type, and the fields already written.
func (s *Server) fieldsAt(doc *document, offset int, filler string, find func(syntax.Expr) (base, around syntax.Span, record reach, written []string, ok bool)) ([]completionItem, bool) {
	kept, closed := repaired(doc.text, offset, filler)
	for i, candidate := range []string{kept, closed} {
		tree, err := doc.parse(candidate)
		if err != nil {
			continue
		}
		// Either text may be the one whose fields are found: the one kept
		// as typed, or the one closed after the cursor.
		base, around, record, written, ok := find(tree)
		if !ok {
			continue
		}
		// The closed text has lost what follows the cursor, where a
		// comprehension binds the names its yield reads, [order with {|}
		// for order in xs]: it would read the argument for the local. It
		// is asked only when no comprehension around the base binds a name
		// the base reads there.
		if i > 0 && bindsAfter(doc, offset, base.Start, candidate[base.Start:base.End]) {
			continue
		}
		typ, ok := s.baseType(doc, candidate, syntax.ScopeAt(tree, base.Start), base, around, record)
		if !ok {
			continue
		}
		return fieldItems(typ, written), true
	}
	return nil, false
}

// bindsAfter reports a name expr, the base at baseStart, reads bound after
// offset in the document by a comprehension around it — one of the names
// written between a for and its in, for order, i in, where the innermost [
// left open before the for opens before the base — and an expr that does not
// parse. A for whose [ opens after the base binds nothing the base reads, nor
// does a reduce: it binds its names in its own arguments, all after offset.
// A name merely read or written as a field label binds nothing.
func bindsAfter(doc *document, offset, baseStart int, expr string) bool {
	tree, err := syntax.Parse(expr)
	if err != nil {
		return true
	}
	names, lexemes := syntax.FreeVariables(tree), lexemesOf(doc)
	for i, lexeme := range lexemes {
		if lexeme.Start < offset || lexeme.Class != syntax.ClassIdentifier || doc.text[lexeme.Start:lexeme.End] != "for" {
			continue
		}
		if opensBefore(doc, lexemes[:i], baseStart) && bindsAny(doc, lexemes[i+1:], names) {
			return true
		}
	}
	return false
}

// opensBefore reports that the innermost [ lexemes leave open starts before
// at.
func opensBefore(doc *document, lexemes []syntax.Lexeme, at int) bool {
	depth := 0
	for _, lexeme := range slices.Backward(lexemes) {
		if lexeme.Class != syntax.ClassPunctuation {
			continue
		}
		switch doc.text[lexeme.Start:lexeme.End] {
		case "]":
			depth++
		case "[":
			if depth == 0 {
				return lexeme.Start < at
			}
			depth--
		}
	}
	return false
}

// bindsAny reports one of names among the binders lexemes start with, the
// names up to the in.
func bindsAny(doc *document, lexemes []syntax.Lexeme, names []string) bool {
	for _, lexeme := range lexemes {
		if lexeme.Class == syntax.ClassComment {
			continue
		}
		text := doc.text[lexeme.Start:lexeme.End]
		switch {
		case lexeme.Class == syntax.ClassIdentifier && text != "in":
			if slices.Contains(names, text) {
				return true
			}
		case text != ",":
			return false // the in, or anything no binder is written in
		}
	}
	return false
}

// reach is how a record is found from a type: the record, and false when the
// type leads to none.
type reach func(machine.Type) (machine.Type, bool)

func recordOf(typ machine.Type) (machine.Type, bool) { return typ, typ.Kind() == machine.RecordKind }

// baseType is the record reached from a base's type, read off the program
// with "(base)" where around was, or, when that program does not compile —
// the base stood in for the body a let or an if still lacks, let(x = (order))
// — off "(base)" alone. locals are the names bound where the base is. An
// argument named as the base is read off the contract at once; "(base)" alone
// is asked only when every name the base reads is an argument none of locals
// hides, since alone it knows no locals and would give an argument's fields
// for a local of the same name.
func (s *Server) baseType(doc *document, candidate string, locals []string, base, around syntax.Span, record reach) (machine.Type, bool) {
	written := candidate[base.Start:base.End]
	if arg, ok := s.argument(written, locals); ok {
		return record(arg.Type)
	}
	stand := "(" + written + ")"
	text := candidate[:around.Start] + stand + candidate[around.End:]
	// The base's type follows from the arguments alone. A declared result is
	// left out: a program half written rarely returns it yet, and holding it
	// to one would lose the answer for no reason.
	options := s.contract
	options.Result, options.ResultDoc = nil, ""
	analysis, err := doc.analyze(text, s.registry, options)
	if typ, ok := wholeType(analysis, around.Start, len(stand)); ok || err == nil {
		return reached(typ, ok, record)
	}
	if !s.readsArguments(written, locals) {
		return machine.Type{}, false
	}
	analysis, _ = doc.analyze(stand, s.registry, options)
	typ, ok := wholeType(analysis, 0, len(stand))
	return reached(typ, ok, record)
}

// argument is the contract's argument called name, unless locals hide it.
func (s *Server) argument(name string, locals []string) (compile.ArgSpec, bool) {
	at := slices.IndexFunc(s.contract.Args, func(arg compile.ArgSpec) bool { return arg.Name == name })
	if at < 0 || slices.Contains(locals, name) {
		return compile.ArgSpec{}, false
	}
	return s.contract.Args[at], true
}

// readsArguments reports that every name expr reads is an argument of the
// contract, none hidden by locals.
func (s *Server) readsArguments(expr string, locals []string) bool {
	tree, err := syntax.Parse(expr)
	if err != nil {
		return false
	}
	for _, name := range syntax.FreeVariables(tree) {
		if _, ok := s.argument(name, locals); !ok {
			return false
		}
	}
	return true
}

// reached is the record reached from typ, when there is a type to reach it
// from.
func reached(typ machine.Type, ok bool, record reach) (machine.Type, bool) {
	if !ok {
		return machine.Type{}, false
	}
	return record(typ)
}

// wholeType is the type of the node over the whole of the size bytes at
// start — its span may take in the parentheses — never a part of it: in
// order.fee with {…} the base is an int, whatever order is.
func wholeType(analysis *compile.Analysis, start, size int) (machine.Type, bool) {
	if analysis == nil {
		return machine.Type{}, false
	}
	end := start + size
	for _, node := range analysis.Nodes {
		whole := node.Span.Start >= start && node.Span.Start <= start+1 && node.Span.End >= end-1 && node.Span.End <= end
		if whole && node.Type != nil {
			return *node.Type, true
		}
	}
	return machine.Type{}, false
}

func fieldItems(typ machine.Type, written []string) []completionItem {
	items := []completionItem{}
	for i, field := range typ.Fields() {
		if slices.Contains(written, field.Name()) {
			continue
		}
		items = append(items, completionItem{
			Label: field.Name(), Kind: kindField, SortText: fmt.Sprintf("0%04d", i), Detail: field.Type().String(),
		})
	}
	return items
}
