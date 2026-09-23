package lsp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"funroute/lang/internal/compile"
	"funroute/lang/internal/machine"
	"funroute/lang/internal/syntax"
)

// The requests the protocol has no word for: the syntax tree an editor shows
// as structure, and running a program against its contract.

// rangedTree is syntax.Tree with its spans in the client's positions.
type rangedTree struct {
	Node     string        `json:"node"`
	Range    Range         `json:"range"`
	Operator string        `json:"operator,omitempty"`
	Fields   []rangedField `json:"fields,omitempty"`
}

type rangedField struct {
	Name      string          `json:"name"`
	Nodes     []rangedTree    `json:"nodes,omitempty"`
	Items     [][]rangedField `json:"items,omitempty"`
	Text      string          `json:"text,omitempty"`
	TextRange *Range          `json:"textRange,omitempty"`
	Flag      bool            `json:"flag,omitempty"`
}

// syntaxTree answers funroute/syntaxTree with the tree of the document, or
// null when it does not parse; the diagnostics say why.
func (s *Server) syntaxTree(params json.RawMessage) (any, error) {
	doc, err := s.documentOf(params)
	if err != nil {
		return nil, err
	}
	tree, err := syntax.SyntaxTree(doc.text)
	if err != nil {
		return nil, nil
	}
	return s.ranged(doc, *tree), nil
}

func (s *Server) ranged(doc *document, tree syntax.Tree) rangedTree {
	out := rangedTree{Node: tree.Node, Range: doc.rangeOf(tree.Span, s.encoding), Operator: tree.Operator}
	out.Fields = s.rangedFields(doc, tree.Fields)
	return out
}

func (s *Server) rangedFields(doc *document, fields []syntax.TreeField) []rangedField {
	out := make([]rangedField, len(fields))
	for i, field := range fields {
		out[i] = rangedField{Name: field.Name, Text: field.Text, Flag: field.Flag}
		for _, node := range field.Nodes {
			out[i].Nodes = append(out[i].Nodes, s.ranged(doc, node))
		}
		for _, item := range field.Items {
			out[i].Items = append(out[i].Items, s.rangedFields(doc, item))
		}
		if field.TextSpan != nil {
			span := doc.rangeOf(*field.TextSpan, s.encoding)
			out[i].TextRange = &span
		}
	}
	return out
}

// runCommand runs a document against the contract with the arguments given:
// {"uri": ..., "args": {...}, "fuel": n}.
const runCommand = "funroute.run"

type runRequest struct {
	URI  string          `json:"uri"`
	Args json.RawMessage `json:"args"`
	Fuel uint64          `json:"fuel"`
}

// runResult says what a run returned, or why it did not, and which functions
// it called that exist here only as signatures: a result that went through
// fallback past one of them is not the one the host would compute.
type runResult struct {
	Value       json.RawMessage `json:"value,omitempty"`
	Type        *machine.Type   `json:"type,omitempty"`
	Error       *runError       `json:"error,omitempty"`
	Unavailable []string        `json:"unavailable"`
}

type runError struct {
	Kind    string `json:"kind"`
	Message string `json:"message"`
}

// renderCommand writes a document with the contract as comments above it,
// for a rule that leaves the editor: {"uri": ...} gives {"source": ...}.
const renderCommand = "funroute.render"

// executeCommand runs one of the server's commands. Each takes one argument,
// read into the shape that command takes: a run its document, arguments and
// fuel, a render only its document.
func (s *Server) executeCommand(params json.RawMessage) (any, error) {
	var in struct {
		Command   string            `json:"command"`
		Arguments []json.RawMessage `json:"arguments"`
	}
	if err := decode(params, &in); err != nil {
		return nil, err
	}
	if len(in.Arguments) != 1 {
		return nil, fmt.Errorf("%w: %s takes one argument", errInvalidParams, in.Command)
	}
	switch in.Command {
	case runCommand:
		var request runRequest
		if err := decode(in.Arguments[0], &request); err != nil {
			return nil, err
		}
		doc, err := s.document(request.URI)
		if err != nil {
			return nil, err
		}
		return s.run(doc, request), nil
	case renderCommand:
		return s.render(in.Arguments[0])
	default:
		return nil, fmt.Errorf("%w: unknown command %s", errInvalidParams, in.Command)
	}
}

// render writes the document with the contract as comments above it. A
// contract the server refused is not written out as if it held.
func (s *Server) render(argument json.RawMessage) (any, error) {
	var request struct {
		URI string `json:"uri"`
	}
	if err := decode(argument, &request); err != nil {
		return nil, err
	}
	doc, err := s.document(request.URI)
	if err != nil {
		return nil, err
	}
	if s.contractErr != nil {
		return nil, s.contractErr
	}
	return map[string]string{"source": compile.RenderWithContract(doc.text, s.contract.Args, s.contract.Result, s.contract.ResultDoc)}, nil
}

// argumentList answers funroute/arguments: what the document's program takes,
// in order — the contract's arguments, or the ones inferred from the text
// when the contract declares none — for a client that asks for their values.
func (s *Server) argumentList(params json.RawMessage) (any, error) {
	doc, err := s.documentOf(params)
	if err != nil {
		return nil, err
	}
	out := []map[string]string{}
	for _, arg := range s.arguments(doc) {
		out = append(out, map[string]string{"name": arg.Name, "type": arg.Type.String(), "doc": arg.Doc})
	}
	return out, nil
}

// catalog is what the registry offers — its functions and forms, with what
// the host wrote about each — for a client that lists them.
func (s *Server) catalog(json.RawMessage) (any, error) {
	return s.registry.Catalog(), nil
}

func (s *Server) run(doc *document, request runRequest) runResult {
	result := runResult{Unavailable: []string{}}
	artifact, err := s.compile(doc)
	if err != nil {
		result.Error = describeError(err)
		return result
	}
	result.Type = &artifact.Result
	args, err := decodeArgs(request.Args)
	if err == nil {
		err = s.execute(artifact, args, request.Fuel, &result)
	}
	if err != nil {
		result.Error = describeError(err)
	}
	return result
}

func (s *Server) compile(doc *document) (*machine.Artifact, error) {
	if s.contractErr != nil {
		return nil, s.contractErr
	}
	return compile.CompileExpr(doc.text, s.registry, s.contract)
}

func (s *Server) execute(artifact *machine.Artifact, args map[string]any, fuel uint64, result *runResult) error {
	runtime, err := machine.Instantiate(artifact, s.registry)
	if err != nil {
		return err
	}
	if fuel == 0 {
		fuel = machine.DefaultFuel
	}
	ctx, calls := machine.TrackUnavailable(context.Background())
	value, err := runtime.Run(ctx, args, machine.RunOptions{Fuel: fuel})
	result.Unavailable = calls()
	if err != nil {
		return err
	}
	result.Value, err = json.Marshal(value)
	return err
}

// decodeArgs reads the arguments as an object, or as the text of one: a
// page that passes along what was typed keeps every digit of a large integer,
// which parsing it in JavaScript first would not.
func decodeArgs(raw json.RawMessage) (map[string]any, error) {
	args := map[string]any{}
	if len(raw) == 0 {
		return args, nil
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		raw = json.RawMessage(text)
	}
	return machine.DecodeArgs(raw)
}

// errorKinds are the errors a host tells apart, most specific first.
var errorKinds = []struct {
	kind string
	err  error
}{
	{"unavailable", machine.ErrUnavailable}, {"contract", machine.ErrContract}, {"compile", machine.ErrCompile},
	{"fuel", machine.ErrFuel}, {"deadline", machine.ErrDeadline}, {"extension", machine.ErrExtension},
}

func describeError(err error) *runError {
	for _, known := range errorKinds {
		if errors.Is(err, known.err) {
			return &runError{Kind: known.kind, Message: err.Error()}
		}
	}
	return &runError{Kind: "run", Message: err.Error()}
}
