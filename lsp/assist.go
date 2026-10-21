package lsp

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/nethinwei/funroute/internal/compile"
	"github.com/nethinwei/funroute/internal/kit"
	"github.com/nethinwei/funroute/internal/machine"
	"github.com/nethinwei/funroute/internal/money"
	"github.com/nethinwei/funroute/internal/syntax"
)

func (s *Server) formatting(doc *document) (any, error) {
	formatted, err := syntax.FormatSource(doc.text)
	switch {
	case err == nil && formatted != doc.text:
		return []textEdit{{Range: doc.whole(s.encoding), NewText: formatted}}, nil
	case err != nil:
		// A text that does not parse is left as it is, like one already
		// formatted: the diagnostics already say why, and a failed request
		// would only add a popup. One that parses has nothing in the
		// diagnostics to say why — a comment inside the expression — so the
		// request says it.
		if _, parsed := syntax.Parse(doc.text); parsed == nil {
			return nil, err
		}
	}
	return []textEdit{}, nil
}

// hover is what the compiler knows about the innermost node under the
// cursor: its type, the signature a call resolved to and what the host says
// about that function or argument.
func (s *Server) hover(doc *document, offset int) (any, error) {
	analysis, _ := s.analysisOf(doc)
	if analysis == nil {
		return null, nil
	}
	fact, ok := analysis.At(offset)
	if !ok {
		return null, nil
	}
	name := doc.text[fact.Span.Start:fact.Span.End]
	if fact.Reference != "" {
		// A name read in parentheses spans them: (fee) is fee.
		name = strings.Trim(name, "() \t")
	}
	text := name
	if fact.Type != nil {
		text += ": " + fact.Type.String()
	}
	explained := s.explain(doc, fact, name)
	lines := make([]string, 0, 1+len(explained))
	lines = append(lines, "```funroute\n"+text+"\n```")
	lines = append(lines, explained...)
	span := doc.rangeOf(fact.Span, s.encoding)
	return hover{Contents: markupContent{Kind: "markdown", Value: strings.Join(lines, "\n\n")}, Range: &span}, nil
}

func (s *Server) explain(doc *document, fact compile.NodeFact, name string) []string {
	var out []string
	if function, ok := machine.ResolveOf(s.registry, fact.Signature); ok {
		out = append(out, "`"+fact.Signature+"`", describe(function.Doc)+examples(function.Doc.Examples))
	}
	if fact.Reference == "argument" {
		for _, arg := range s.arguments(doc) {
			if arg.Name() == name && arg.Doc() != "" {
				out = append(out, arg.Doc())
			}
		}
	}
	if fact.Reference == "local" {
		out = append(out, "局部名")
	}
	if fact.Type != nil && fact.Type.Kind() == machine.MoneyKind {
		if note := s.minorUnits(name); note != "" {
			out = append(out, note)
		}
	}
	return out
}

// minorUnits says what a money literal is in its currency's minor unit:
// USD 1.70 is 170, because the registry gives dollars two places. The text
// is the literal as written, so any space may part the code from the figure
// and the figure may carry a sign and _ separators (JPY -1_000).
func (s *Server) minorUnits(text string) string {
	// A literal written in parens covers them: (USD 1.70) is USD 1.70.
	for text = strings.TrimSpace(text); len(text) >= 2 && text[0] == '(' && text[len(text)-1] == ')'; {
		text = strings.TrimSpace(text[1 : len(text)-1])
	}
	words := strings.Fields(text)
	if len(words) != 2 {
		return ""
	}
	code, amount := words[0], strings.ReplaceAll(words[1], "_", "")
	value, err := machine.ParseMoneyAmount(s.registry, code, amount)
	spec, _ := s.registry.Money()
	index := slices.IndexFunc(spec.Currencies, func(currency money.CurrencySpec) bool { return currency.Code == code })
	if err != nil || index < 0 {
		return ""
	}
	money, _ := value.Money()
	return fmt.Sprintf("最小单位 %d（%s 保留 %d 位小数）", money.Minor(), code, spec.Currencies[index].Digits)
}

func describe(doc machine.Doc) string {
	if doc.Description == "" {
		return doc.Label
	}
	return doc.Label + "：" + doc.Description
}

// examples is a doc's examples as a block of the language, each call with
// the value it gives, for a hover or a completion's documentation.
func examples(list []machine.Example) string {
	if len(list) == 0 {
		return ""
	}
	lines := kit.Map(list, func(example machine.Example) string { return example.Source + "  // " + example.Result })
	return "\n\n```funroute\n" + strings.Join(lines, "\n") + "\n```"
}

// namedExamples is every example the overloads of a name carry, once each:
// a name the kernel and a pack share has examples from both.
func (s *Server) namedExamples(name string) []machine.Example {
	var out []machine.Example
	for _, function := range machine.OverloadsOf(s.registry, name) {
		for _, example := range function.Doc.Examples {
			if !slices.Contains(out, example) {
				out = append(out, example)
			}
		}
	}
	return out
}

// completion offers what the position can name: the contract's arguments,
// the locals in scope there, the registry's functions and forms and its
// currencies — or, after @, the members of the contract's enums. sortText
// orders them by kind, nearest first — locals, arguments, functions, forms,
// currencies — then by name.
func (s *Server) completion(doc *document, offset int) (any, error) {
	if inTextOrComment(doc, offset) {
		return []completionItem{}, nil
	}
	if typingEnum(doc, offset) {
		return s.enumMembers(), nil
	}
	// Where only a field can be named — of the record being updated, or of
	// the items a selector reads — only fields are offered.
	// Only fields are offered where only a field can be named: of the record
	// being updated, after a with, or of the items a selector reads, after
	// its dot. Elsewhere neither is looked for: each parses the text twice.
	if fields, ok := s.updateFields(doc, offset); ok {
		return fields, nil
	}
	if fields, ok := s.selectorFields(doc, offset); ok {
		return fields, nil
	}
	items := []completionItem{}
	for _, arg := range s.arguments(doc) {
		items = append(items, completionItem{Label: arg.Name(), Kind: kindVariable, SortText: "1" + arg.Name(), Detail: arg.Type().String(), Documentation: markdown(arg.Doc())})
	}
	for _, name := range localsAt(doc.text, offset) {
		items = append(items, completionItem{Label: name, Kind: kindVariable, SortText: "0" + name, Detail: "局部名"})
	}
	return withCallables(items, s.callables())
}

// withCallables is items followed by the callables, as the one JSON array
// they make: the callables, the same on every completion, were encoded once.
func withCallables(items []completionItem, callables json.RawMessage) (json.RawMessage, error) {
	if len(items) == 0 {
		return callables, nil
	}
	head, err := json.Marshal(items)
	if err != nil || string(callables) == "[]" {
		return head, err
	}
	return slices.Concat(head[:len(head)-1], []byte(","), callables[1:]), nil
}

// currencies are the registry's declared currencies, written as their codes.
func (s *Server) currencies() []completionItem {
	spec, declared := s.registry.Money()
	if !declared {
		return nil
	}
	return kit.Map(spec.Currencies, func(currency money.CurrencySpec) completionItem {
		return completionItem{Label: currency.Code, Kind: kindConstant, SortText: "4" + currency.Code, Detail: fmt.Sprintf("currency，%d 位小数", currency.Digits)}
	})
}

// arguments is what the program takes: the contract's, or, when it declares
// none, what the compiler inferred from the text as it last compiled.
func (s *Server) arguments(doc *document) []machine.Parameter {
	if len(s.contract.Args) > 0 {
		return kit.Map(s.contract.Args, func(arg compile.ArgSpec) machine.Parameter { return machine.NewParameter(arg.Name, arg.Type, arg.Doc) })
	}
	if analysis, _ := s.analysisOf(doc); analysis != nil {
		return analysis.Params
	}
	return nil
}

// placeholder stands in for the name being typed, so a program cut off at
// the cursor can still be parsed and asked what it sees there.
const placeholder = "funroute_completion"

// localsAt is the locals visible at offset. Scope is syntax, so it needs a
// parse and nothing more — not a contract, not types. Text being typed rarely
// parses, so when it does not, the text is tried again with a name at the
// cursor and the brackets still open there closed, the way the parser would
// need to see it: let(rate = fee, | becomes let(rate = fee, funroute_completion).
func localsAt(text string, offset int) []string {
	for _, candidate := range completionCandidates(text, offset) {
		if tree, err := syntax.Parse(candidate); err == nil {
			return syntax.ScopeAt(tree, offset)
		}
	}
	return nil
}

func completionCandidates(text string, offset int) []string {
	kept, closed := repaired(text, offset, placeholder)
	// A let binding being written still needs its body after it.
	binding := closed[:offset] + placeholder + ", " + closed[offset:]
	return []string{text, kept, closed, binding}
}

// repaired is text with filler written at offset: kept before the rest of
// the text, closed in place of it with the brackets still open there closed.
func repaired(text string, offset int, filler string) (kept, closed string) {
	prefix := text[:offset] + filler
	return prefix + text[offset:], prefix + openBrackets(text[:offset])
}

var closerOf = map[string]byte{"(": ')', "[": ']', "{": '}'}

// openBrackets is what closes the brackets prefix leaves open, innermost first.
func openBrackets(prefix string) string {
	lexemes, _ := syntax.Lexemes(prefix)
	var closers []byte
	for _, lexeme := range lexemes {
		if lexeme.Class != syntax.ClassPunctuation {
			continue
		}
		switch text := prefix[lexeme.Start:lexeme.End]; text {
		case "(", "[", "{":
			closers = append(closers, closerOf[text])
		case ")", "]", "}":
			if len(closers) > 0 {
				closers = closers[:len(closers)-1]
			}
		}
	}
	slices.Reverse(closers)
	return string(closers)
}

// callables is every function, form and currency the registry offers. The
// registry is fixed for the session, so the list is built once.
func (s *Server) callables() json.RawMessage {
	if s.callableJSON == nil {
		// Never null: no callable is an empty list.
		items := append([]completionItem{}, s.buildCallables()...)
		s.callableJSON, _ = json.Marshal(items) // strings and ints: it cannot fail
	}
	return s.callableJSON
}

func (s *Server) buildCallables() []completionItem {
	var items []completionItem
	seen := map[string]bool{}
	for _, function := range s.registry.Catalog().Functions() {
		if seen[function.Name()] {
			continue
		}
		seen[function.Name()] = true
		overloads := len(machine.OverloadsOf(s.registry, function.Name()))
		detail := function.Signature()
		if overloads > 1 {
			detail = fmt.Sprintf("%s（共 %d 个签名）", detail, overloads)
		}
		documentation := describe(function.Doc()) + examples(s.namedExamples(function.Name()))
		items = append(items, completionItem{Label: function.Name(), Kind: kindFunction, SortText: "2" + function.Name(), Detail: detail, Documentation: markdown(documentation)})
	}
	// A form written like a call, switch(…) or let(…), completes as its
	// keyword; the others are operators and brackets.
	for _, form := range s.registry.Catalog().SpecialForms() {
		if name, doc := form.Name(), form.Doc(); strings.HasPrefix(form.Syntax(), name+"(") {
			items = append(items, completionItem{Label: name, Kind: kindKeyword, SortText: "3" + name, Documentation: markdown(describe(doc) + examples(doc.Examples))})
		}
	}
	return append(items, s.currencies()...)
}

// enumMembers lists every member of every enum the contract declares, and
// the registry's currencies and rounding modes when it declares money. A
// member two enums share is offered qualified, as it has to be written.
func (s *Server) enumMembers() []completionItem {
	owners := map[string][]string{}
	var order []string
	enums := compile.EnumNamespace(s.contract, s.registry)
	for _, name := range slices.Sorted(maps.Keys(enums)) {
		enum := enums[name]
		for _, member := range enum.Values() {
			if len(owners[member]) == 0 {
				order = append(order, member)
			}
			if !slices.Contains(owners[member], enum.Name()) {
				owners[member] = append(owners[member], enum.Name())
			}
		}
	}
	items := []completionItem{}
	for _, member := range order {
		if len(owners[member]) == 1 {
			items = append(items, completionItem{Label: member, Kind: kindEnumMember, Detail: "enum<" + owners[member][0] + ">"})
			continue
		}
		for _, enum := range owners[member] {
			items = append(items, completionItem{Label: enum + "." + member, Kind: kindEnumMember, Detail: "enum<" + enum + ">"})
		}
	}
	return items
}

func markdown(text string) *markupContent {
	if text == "" {
		return nil
	}
	return &markupContent{Kind: "markdown", Value: text}
}

// inTextOrComment reports a cursor inside a string or a comment, where no
// name is written: past a string's opening quote and before its closing
// one — or anywhere after it, in one being typed that has none yet — or
// after a comment's // up to its line's end.
func inTextOrComment(doc *document, offset int) bool {
	for _, lexeme := range lexemesOf(doc) {
		open := lexeme.Class == syntax.ClassInvalid && doc.text[lexeme.Start] == '"'
		switch {
		case lexeme.Start >= offset:
			return false
		case lexeme.Class == syntax.ClassString && offset < lexeme.End,
			(lexeme.Class == syntax.ClassComment || open) && offset <= lexeme.End:
			return true
		}
	}
	return false
}

// typingEnum reports a cursor at the end of an @ or of an @member being
// typed — @, @half_e — and not of an @ inside a string or a comment.
func typingEnum(doc *document, offset int) bool {
	for _, lexeme := range lexemesOf(doc) {
		if lexeme.Start < offset && offset <= lexeme.End {
			return lexeme.Class == syntax.ClassEnum || lexeme.Class == syntax.ClassInvalid && doc.text[lexeme.Start] == '@'
		}
	}
	return false
}

// signatureHelp names the call the cursor is in and which argument it is on.
// A call being typed does not parse yet, so it is found from the lexemes,
// which the lexer produces for any text.
func (s *Server) signatureHelp(doc *document, offset int) (any, error) {
	lexemes := lexemesOf(doc)
	name, active, ok := enclosingCall(doc.text, lexemes, offset)
	overloads := machine.OverloadsOf(s.registry, name)
	if !ok || len(overloads) == 0 {
		return null, nil
	}
	// The active signature is the first that has the argument being typed,
	// or the first of all when none has.
	first := max(slices.IndexFunc(overloads, func(function *machine.RegisteredFunction) bool { return len(function.Params) > active }), 0)
	// Past the last parameter of the signature shown, its last parameter is
	// the one being typed into: the index stays inside that signature.
	help := signatureHelp{ActiveSignature: first, ActiveParameter: max(min(active, len(overloads[first].Params)-1), 0)}
	for _, function := range overloads {
		help.Signatures = append(help.Signatures, signatureOf(function))
	}
	return help, nil
}

func signatureOf(function *machine.RegisteredFunction) signatureInformation {
	parameters := make([]parameterInformation, len(function.Params))
	labels := make([]string, len(function.Params))
	for i, param := range function.Params {
		labels[i] = function.Doc.Params[i] + ": " + param.String()
		parameters[i] = parameterInformation{Label: labels[i]}
	}
	label := fmt.Sprintf("%s(%s) -> %s", function.Name, strings.Join(labels, ", "), function.Result)
	return signatureInformation{Label: label, Documentation: describe(function.Doc), Parameters: parameters}
}

// enclosingCall walks back from offset to the "(" that opens the call it is
// in, past any that only group, counting the commas at that level: the
// argument the cursor is on. A
// bracket or brace on the way means the cursor is inside a container, whose
// commas are not the call's. Other punctuation (":" of a record, "." of a
// field) says nothing about where the call is.
func enclosingCall(text string, lexemes []syntax.Lexeme, offset int) (string, int, bool) {
	depth, commas := 0, 0
	for i, lexeme := range slices.Backward(lexemes) {
		if lexeme.End > offset || lexeme.Class != syntax.ClassPunctuation {
			continue
		}
		switch text[lexeme.Start:lexeme.End] {
		case ")", "]", "}":
			depth++
		case ",":
			if depth == 0 {
				commas++
			}
		case "(":
			if name, called := callee(text, lexemes, i); depth == 0 && called {
				return name, commas, true
			}
			// A paren that only groups holds one expression: the call the
			// cursor is in, if any, is further out, at the same argument.
			if depth > 0 {
				depth--
			}
		case "[", "{":
			if depth == 0 {
				commas = 0
			} else {
				depth--
			}
		}
	}
	return "", 0, false
}

// callee is the name a "(" calls, the name before it past any comment, and
// false for a paren that only groups.
func callee(text string, lexemes []syntax.Lexeme, paren int) (string, bool) {
	before := paren - 1
	for before >= 0 && lexemes[before].Class == syntax.ClassComment {
		before--
	}
	if before < 0 || lexemes[before].Class != syntax.ClassIdentifier {
		return "", false
	}
	return text[lexemes[before].Start:lexemes[before].End], true
}
