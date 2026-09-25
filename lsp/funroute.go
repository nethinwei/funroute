package lsp

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/nethinwei/funroute/internal/compile"
	"github.com/nethinwei/funroute/internal/machine"
	"github.com/nethinwei/funroute/internal/syntax"
)

// The requests the protocol has no word for: the syntax tree an editor shows
// as structure, and running a program against its contract.

// rangedTree is syntax.Tree with its spans in the client's positions.
type rangedTree struct {
	Node     string        `json:"node"`
	Range    Range         `json:"range"`
	Operator string        `json:"operator,omitempty"`
	Form     bool          `json:"form,omitempty"`
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
func (s *Server) syntaxTree(doc *document) (any, error) {
	if tree, err := syntax.SyntaxTree(doc.text); err == nil {
		return s.ranged(doc, *tree), nil
	}
	return null, nil
}

func (s *Server) ranged(doc *document, tree syntax.Tree) rangedTree {
	out := rangedTree{Node: tree.Node, Range: doc.rangeOf(tree.Span, s.encoding), Operator: tree.Operator, Form: tree.Form}
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
// {"uri": ..., "args": {...}}.
const runCommand = "funroute.run"

type runRequest struct {
	URI  string          `json:"uri"`
	Args json.RawMessage `json:"args"`
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

type renderRequest struct {
	URI string `json:"uri"`
}

// commands are the server's commands, each reading its one argument into
// the shape it takes: a run its document and arguments, a render only
// its document.
var commands = map[string]requestHandler{
	runCommand:    withParams((*Server).run),
	renderCommand: withParams((*Server).render),
}

type executeCommandParams struct {
	Command   string            `json:"command"`
	Arguments []json.RawMessage `json:"arguments"`
}

// executeCommand runs one of the server's commands.
func (s *Server) executeCommand(in executeCommandParams) (any, error) {
	if len(in.Arguments) != 1 {
		return nil, fmt.Errorf("%w: %s takes one argument", errInvalidParams, in.Command)
	}
	command, ok := commands[in.Command]
	if !ok {
		return nil, fmt.Errorf("%w: unknown command %s", errInvalidParams, in.Command)
	}
	return command(s, in.Arguments[0])
}

// render writes the document with the contract as comments above it. A
// contract the server refused is not written out as if it held.
func (s *Server) render(request renderRequest) (any, error) {
	doc, err := s.document(request.URI)
	if err != nil {
		return nil, err
	}
	if s.contractErr != nil {
		return nil, s.contractErr
	}
	return map[string]string{"source": compile.RenderWithContract(doc.text, s.contract)}, nil
}

// argumentList answers funroute/arguments: what the document's program takes,
// in order — the contract's arguments, or the ones inferred from the text
// when the contract declares none — for a client that asks for their values,
// with a sample of each value's JSON.
func (s *Server) argumentList(doc *document) (any, error) {
	args := s.arguments(doc)
	samples := samplerFor(s.registry)
	out := make([]map[string]string, 0, len(args))
	for _, arg := range args {
		out = append(out, map[string]string{"name": arg.Name(), "type": arg.Type().String(), "doc": arg.Doc(), "example": samples.sample(arg.Type())})
	}
	return out, nil
}

// catalog is what the registry offers — its functions and forms, with what
// the host wrote about each — for a client that lists them.
func (s *Server) catalog(json.RawMessage) (any, error) {
	return s.registry.Catalog(), nil
}

func (s *Server) run(request runRequest) (any, error) {
	doc, err := s.document(request.URI)
	if err != nil {
		return nil, err
	}
	result := runResult{Unavailable: []string{}}
	artifact, err := s.compile(doc)
	if err != nil {
		result.Error = describeError(err)
		return result, nil
	}
	resultType := artifact.Result()
	result.Type = &resultType
	args, err := decodeArgs(request.Args)
	if err == nil {
		err = s.execute(artifact, args, &result)
	}
	if err != nil {
		result.Error = describeError(err)
	}
	return result, nil
}

func (s *Server) compile(doc *document) (*machine.Artifact, error) {
	if s.contractErr != nil {
		return nil, s.contractErr
	}
	return compile.CompileExpr(doc.text, s.registry, s.contract)
}

func (s *Server) execute(artifact *machine.Artifact, args map[string]any, result *runResult) error {
	runtime, err := machine.Instantiate(artifact, s.registry)
	if err != nil {
		return err
	}
	// The language service answers one message at a time, so a run that
	// hangs holds every document: a host function waiting on something, and
	// a long loop, are cut off at the deadline.
	timed, cancel := context.WithTimeout(context.Background(), runTimeout)
	defer cancel()
	ctx, calls := machine.TrackUnavailable(timed)
	value, err := runtime.Run(ctx, args)
	result.Unavailable = calls()
	if err != nil {
		return err
	}
	// Money comes back as "USD 1.70": the registry knows the places.
	result.Value, err = s.registry.EncodeJSON(value)
	return err
}

// runTimeout bounds one run the service executes: a wall-clock second.
const runTimeout = time.Second

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

// describeError is a run's error by its most specific class, "run" when it
// has none.
func describeError(err error) *runError {
	kind := machine.ClassName(err)
	if kind == "" {
		kind = "run"
	}
	return &runError{Kind: kind, Message: err.Error()}
}
