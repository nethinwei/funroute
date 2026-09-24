package lsp

import (
	"fmt"
	"slices"

	"github.com/nethinwei/funroute/internal/compile"
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
	prefix := doc.text[:offset]
	entry := placeholder + ": 0"
	for _, candidate := range []string{prefix + entry + doc.text[offset:], prefix + entry + openBrackets(prefix)} {
		tree, err := syntax.Parse(candidate)
		if err != nil {
			continue
		}
		base, update, written, ok := syntax.UpdatedRecordAt(tree, offset, placeholder)
		if !ok {
			return nil, false
		}
		typ, ok := s.baseType(candidate, base, update)
		if !ok {
			return nil, false
		}
		return fieldItems(typ, written), true
	}
	return nil, false
}

// baseType is the record type of an update's base, read off the program with
// "(base)" where the update was.
func (s *Server) baseType(candidate string, base, update syntax.Span) (machine.Type, bool) {
	stand := "(" + candidate[base.Start:base.End] + ")"
	text := candidate[:update.Start] + stand + candidate[update.End:]
	// The base's type follows from the arguments alone. A declared result is
	// left out: a program half written rarely returns it yet, and holding it
	// to one would lose the answer for no reason.
	options := s.contract
	options.Result, options.ResultDoc = nil, ""
	analysis, _ := compile.Analyze(text, s.registry, options)
	if analysis == nil {
		return machine.Type{}, false
	}
	// Nodes are in pre-order, so the first one inside the parentheses is the
	// base itself rather than a part of it.
	end := update.Start + len(stand)
	for _, node := range analysis.Nodes {
		if node.Span.Start >= update.Start && node.Span.End <= end && node.Type != nil && node.Type.Kind() == machine.RecordKind {
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
