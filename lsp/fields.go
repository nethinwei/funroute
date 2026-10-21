package lsp

import (
	"fmt"
	"slices"
	"strings"

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
	if !strings.Contains(doc.text[:offset], "with") {
		return nil, false
	}
	return s.fieldsAt(doc, offset, placeholder+": 0", func(tree syntax.Expr) (base, around syntax.Span, record reach, written []string, ok bool) {
		base, around, written, ok = syntax.UpdatedRecordAt(tree, offset, placeholder)
		return base, around, recordOf, written, ok
	})
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
	for _, candidate := range []string{kept, closed} {
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
		typ, ok := s.baseType(doc, candidate, base, around, record)
		if !ok {
			continue
		}
		return fieldItems(typ, written), true
	}
	return nil, false
}

// reach is how a record is found from a type: the record, and false when the
// type leads to none.
type reach func(machine.Type) (machine.Type, bool)

func recordOf(typ machine.Type) (machine.Type, bool) { return typ, typ.Kind() == machine.RecordKind }

// baseType is the record reached from a base's type, read off the program
// with "(base)" where around was, or, when that program does not compile —
// the base stood in for the body a let or an if still lacks, let(x = (order))
// — off "(base)" alone.
func (s *Server) baseType(doc *document, candidate string, base, around syntax.Span, record reach) (machine.Type, bool) {
	stand := "(" + candidate[base.Start:base.End] + ")"
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
	analysis, _ = doc.analyze(stand, s.registry, options)
	typ, ok := wholeType(analysis, 0, len(stand))
	return reached(typ, ok, record)
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
