package lsp

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/nethinwei/funroute/internal/compile"
	"github.com/nethinwei/funroute/internal/machine"
	"github.com/nethinwei/funroute/internal/money"
	"github.com/nethinwei/funroute/internal/syntax"
)

func (s *Server) formatting(params json.RawMessage) (any, error) {
	doc, err := s.documentOf(params)
	if err != nil {
		return nil, err
	}
	// A text that cannot be formatted is left as it is: the diagnostics
	// already say why, and a failed request would only add a popup.
	formatted, err := syntax.FormatSource(doc.text)
	if err != nil {
		return []textEdit{}, nil
	}
	if formatted == doc.text {
		return []textEdit{}, nil
	}
	return []textEdit{{Range: doc.whole(s.encoding), NewText: formatted}}, nil
}

// at is the open document and the byte a position request names.
func (s *Server) at(params json.RawMessage) (*document, int, error) {
	var in textDocumentPosition
	if err := decode(params, &in); err != nil {
		return nil, 0, err
	}
	doc, err := s.document(in.TextDocument.URI)
	if err != nil {
		return nil, 0, err
	}
	return doc, doc.offset(in.Position, s.encoding), nil
}

// hover is what the compiler knows about the innermost node under the
// cursor: its type, the signature a call resolved to and what the host says
// about that function or argument.
func (s *Server) hover(params json.RawMessage) (any, error) {
	doc, offset, err := s.at(params)
	if err != nil {
		return nil, err
	}
	analysis, _ := s.analysisOf(doc)
	if analysis == nil {
		return nil, nil
	}
	fact, ok := analysis.At(offset)
	if !ok {
		return nil, nil
	}
	text := doc.text[fact.Span.Start:fact.Span.End]
	if fact.Type != nil {
		text += ": " + fact.Type.String()
	}
	lines := []string{"```funroute\n" + text + "\n```"}
	lines = append(lines, s.explain(fact, doc.text[fact.Span.Start:fact.Span.End])...)
	span := doc.rangeOf(fact.Span, s.encoding)
	return hover{Contents: markupContent{Kind: "markdown", Value: strings.Join(lines, "\n\n")}, Range: &span}, nil
}

func (s *Server) explain(fact compile.NodeFact, name string) []string {
	var out []string
	if function, ok := s.registry.Resolve(fact.Signature); ok {
		out = append(out, "`"+fact.Signature+"`", describe(function.Doc)+examples(function.Doc.Examples))
	}
	if fact.Reference == "argument" {
		for _, arg := range s.contract.Args {
			if arg.Name == name && arg.Doc != "" {
				out = append(out, arg.Doc)
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
	lines := make([]string, len(list))
	for i, example := range list {
		lines[i] = example.Source + "  // " + example.Result
	}
	return "\n\n```funroute\n" + strings.Join(lines, "\n") + "\n```"
}

// namedExamples is every example the overloads of a name carry, once each:
// a name the kernel and a pack share has examples from both.
func (s *Server) namedExamples(name string) []machine.Example {
	var out []machine.Example
	for _, function := range s.registry.Overloads(name) {
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
func (s *Server) completion(params json.RawMessage) (any, error) {
	doc, offset, err := s.at(params)
	if err != nil {
		return nil, err
	}
	if offset > 0 && doc.text[offset-1] == '@' {
		return s.enumMembers(), nil
	}
	// Where only a field of the record being updated can be named, only
	// fields are offered.
	if fields, ok := s.updateFields(doc, offset); ok {
		return fields, nil
	}
	items := []completionItem{}
	for _, arg := range s.arguments(doc) {
		items = append(items, completionItem{Label: arg.Name(), Kind: kindVariable, SortText: "1" + arg.Name(), Detail: arg.Type().String(), Documentation: markdown(arg.Doc())})
	}
	for _, name := range localsAt(doc.text, offset) {
		items = append(items, completionItem{Label: name, Kind: kindVariable, SortText: "0" + name, Detail: "局部名"})
	}
	return append(append(items, s.callables()...), s.currencies()...), nil
}

// currencies are the registry's declared currencies, written as their codes.
func (s *Server) currencies() []completionItem {
	spec, declared := s.registry.Money()
	if !declared {
		return nil
	}
	items := make([]completionItem, len(spec.Currencies))
	for i, currency := range spec.Currencies {
		items[i] = completionItem{Label: currency.Code, Kind: kindConstant, SortText: "4" + currency.Code, Detail: fmt.Sprintf("currency，%d 位小数", currency.Digits)}
	}
	return items
}

// arguments is what the program takes: the contract's, or, when it declares
// none, what the compiler inferred from the text as it last compiled.
func (s *Server) arguments(doc *document) []machine.Parameter {
	if len(s.contract.Args) > 0 {
		out := make([]machine.Parameter, len(s.contract.Args))
		for i, arg := range s.contract.Args {
			out[i] = machine.NewParameter(arg.Name, arg.Type, arg.Doc)
		}
		return out
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
	prefix := text[:offset]
	closers := openBrackets(prefix)
	return []string{
		text,
		prefix + placeholder + text[offset:],
		prefix + placeholder + closers,
		// A let binding being written still needs its body after it.
		prefix + placeholder + ", " + placeholder + closers,
	}
}

// openBrackets is what closes the brackets prefix leaves open, innermost first.
func openBrackets(prefix string) string {
	lexemes, _ := syntax.Lexemes(prefix)
	var open []byte
	for _, lexeme := range lexemes {
		if lexeme.Class != syntax.ClassPunctuation {
			continue
		}
		switch text := prefix[lexeme.Start:lexeme.End]; text {
		case "(", "[", "{":
			open = append(open, text[0])
		case ")", "]", "}":
			if len(open) > 0 {
				open = open[:len(open)-1]
			}
		}
	}
	closers := make([]byte, len(open))
	for i := range open {
		closers[i] = map[byte]byte{'(': ')', '[': ']', '{': '}'}[open[len(open)-1-i]]
	}
	return string(closers)
}

// callables is every function and form the registry offers. The registry is
// fixed for the session, so the list is built once.
func (s *Server) callables() []completionItem {
	if s.callableItems == nil {
		s.callableItems = s.buildCallables()
	}
	return s.callableItems
}

func (s *Server) buildCallables() []completionItem {
	var items []completionItem
	seen := map[string]bool{}
	for _, function := range s.registry.Catalog().Functions() {
		if seen[function.Name()] {
			continue
		}
		seen[function.Name()] = true
		overloads := len(s.registry.Overloads(function.Name()))
		detail := function.Signature()
		if overloads > 1 {
			detail = fmt.Sprintf("%s（共 %d 个签名）", detail, overloads)
		}
		documentation := describe(function.Doc()) + examples(s.namedExamples(function.Name()))
		items = append(items, completionItem{Label: function.Name(), Kind: kindFunction, SortText: "2" + function.Name(), Detail: detail, Documentation: markdown(documentation)})
	}
	forms := map[string]machine.Doc{}
	for _, form := range s.registry.Catalog().SpecialForms() {
		forms[form.Name()] = form.Doc()
	}
	for _, form := range append(s.registry.EnabledForms(), "let") {
		if form != machine.ForForm {
			doc := forms[string(form)]
			items = append(items, completionItem{Label: string(form), Kind: kindKeyword, SortText: "3" + string(form), Documentation: markdown(describe(doc) + examples(doc.Examples))})
		}
	}
	return items
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

// signatureHelp names the call the cursor is in and which argument it is on.
// A call being typed does not parse yet, so it is found from the lexemes,
// which the lexer produces for any text.
func (s *Server) signatureHelp(params json.RawMessage) (any, error) {
	doc, offset, err := s.at(params)
	if err != nil {
		return nil, err
	}
	lexemes := lexemesOf(doc)
	name, active, ok := enclosingCall(doc.text, lexemes, offset)
	if !ok {
		return nil, nil
	}
	overloads := s.registry.Overloads(name)
	if len(overloads) == 0 {
		return nil, nil
	}
	help := signatureHelp{ActiveParameter: active}
	// The active signature is the first that has the argument being typed.
	help.ActiveSignature = -1
	for i, function := range overloads {
		help.Signatures = append(help.Signatures, signatureOf(function))
		if help.ActiveSignature < 0 && len(function.Params) > active {
			help.ActiveSignature = i
		}
	}
	help.ActiveSignature = max(help.ActiveSignature, 0)
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
// in, counting the commas at that level: the argument the cursor is on. A
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
			if depth == 0 {
				return callee(text, lexemes, i), commas, i > 0 && lexemes[i-1].Class == syntax.ClassIdentifier
			}
			depth--
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

func callee(text string, lexemes []syntax.Lexeme, paren int) string {
	if paren == 0 {
		return ""
	}
	return text[lexemes[paren-1].Start:lexemes[paren-1].End]
}
