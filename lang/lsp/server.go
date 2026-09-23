// Package lsp is FunRoute's language server. It answers in the Language
// Server Protocol with what the language itself knows — the lexer's and the
// parser's reading of every piece of a program, the compiler's diagnostics,
// types and signatures, the formatter's layout — and nothing about how any of
// it is shown. The server keeps the open documents and the contract; every
// answer is computed from them by the language, which keeps no state.
//
// The protocol is the only interface a front end needs, over any transport:
// Serve frames it on a stream for stdio, and Handle takes one message at a
// time for a transport that already has messages, such as a browser worker.
package lsp

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"funroute/lang/internal/compile"
	"funroute/lang/internal/machine"
)

// Server is one client's session: the documents it opened, the contract it
// set and the position encoding it agreed to.
type Server struct {
	registry *machine.Registry
	send     func([]byte)

	mu        sync.Mutex
	documents map[string]*document
	encoding  string
	contract  compile.CompileOptions
	// contractErr is why the contract the client set could not be read. Every
	// document shows it, since nothing compiles against a broken contract.
	contractErr error
	exited      bool
	// callableItems is the completion list of the registry's functions and
	// forms, built on first use.
	callableItems []completionItem
}

// New starts a session for registry. send receives every message the server
// sends, one JSON document each: responses, and the diagnostics it publishes.
func New(registry *machine.Registry, send func(message []byte)) *Server {
	return &Server{registry: registry, send: send, documents: map[string]*document{}, encoding: utf16Encoding}
}

type requestHandler func(*Server, json.RawMessage) (any, error)

type notificationHandler func(*Server, json.RawMessage) error

var requests = map[string]requestHandler{
	"initialize":                       (*Server).initialize,
	"shutdown":                         func(*Server, json.RawMessage) (any, error) { return nil, nil },
	"textDocument/semanticTokens/full": (*Server).semanticTokens,
	"textDocument/formatting":          (*Server).formatting,
	"textDocument/hover":               (*Server).hover,
	"textDocument/completion":          (*Server).completion,
	"textDocument/signatureHelp":       (*Server).signatureHelp,
	"funroute/syntaxTree":              (*Server).syntaxTree,
	"funroute/catalog":                 (*Server).catalog,
	"funroute/arguments":               (*Server).argumentList,
	"workspace/executeCommand":         (*Server).executeCommand,
}

var notifications = map[string]notificationHandler{
	"initialized":            func(*Server, json.RawMessage) error { return nil },
	"exit":                   (*Server).exit,
	"textDocument/didOpen":   (*Server).didOpen,
	"textDocument/didChange": (*Server).didChange,
	"textDocument/didClose":  (*Server).didClose,
	"funroute/setContract":   (*Server).setContract,
}

// Handle processes one message. A request is answered through send; a
// notification the server does not know is ignored, as the protocol asks.
func (s *Server) Handle(message []byte) {
	var in incoming
	if err := json.Unmarshal(message, &in); err != nil {
		s.write(failure{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: responseError{codeParseError, err.Error()}})
		return
	}
	if len(in.ID) == 0 {
		// A notification has no reply to carry its failure, so the server
		// says it in the client's log rather than dropping it.
		if handle, ok := notifications[in.Method]; ok {
			var err error
			s.lock(func() { err = handle(s, in.Params) })
			if err != nil {
				s.notify("window/logMessage", map[string]any{"type": 1, "message": in.Method + ": " + err.Error()})
			}
		}
		return
	}
	handle, ok := requests[in.Method]
	if !ok {
		s.write(failure{JSONRPC: "2.0", ID: in.ID, Error: responseError{codeMethodNotFound, "unknown method " + in.Method}})
		return
	}
	var result any
	var err error
	s.lock(func() { result, err = handle(s, in.Params) })
	if err != nil {
		s.write(failure{JSONRPC: "2.0", ID: in.ID, Error: responseError{errorCode(err), err.Error()}})
		return
	}
	s.write(response{JSONRPC: "2.0", ID: in.ID, Result: result})
}

// Exited reports whether the client has sent exit.
func (s *Server) Exited() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.exited
}

func (s *Server) lock(run func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	run()
}

func (s *Server) write(message any) {
	encoded, err := json.Marshal(message)
	if err != nil {
		encoded, _ = json.Marshal(notification{JSONRPC: "2.0", Method: "window/logMessage", Params: map[string]any{"type": 1, "message": err.Error()}})
	}
	s.send(encoded)
}

func (s *Server) notify(method string, params any) {
	s.write(notification{JSONRPC: "2.0", Method: method, Params: params})
}

// errInvalidParams marks a request the server could not read.
var errInvalidParams = errors.New("invalid params")

func errorCode(err error) int {
	if errors.Is(err, errInvalidParams) {
		return codeInvalidParams
	}
	return codeRequestFailed
}

func decode(params json.RawMessage, into any) error {
	if err := json.Unmarshal(params, into); err != nil {
		return fmt.Errorf("%w: %v", errInvalidParams, err)
	}
	return nil
}

type initializeParams struct {
	Capabilities struct {
		General struct {
			PositionEncodings []string `json:"positionEncodings"`
		} `json:"general"`
	} `json:"capabilities"`
	InitializationOptions struct {
		Contract *compile.TextContract `json:"contract"`
	} `json:"initializationOptions"`
}

func (s *Server) initialize(params json.RawMessage) (any, error) {
	var in initializeParams
	if err := decode(params, &in); err != nil {
		return nil, err
	}
	for _, encoding := range in.Capabilities.General.PositionEncodings {
		if encoding == utf8Encoding {
			s.encoding = utf8Encoding
		}
	}
	s.applyContract(in.InitializationOptions.Contract)
	return map[string]any{"capabilities": s.capabilities(), "serverInfo": map[string]string{"name": "funroute"}}, nil
}

func (s *Server) capabilities() map[string]any {
	return map[string]any{
		"positionEncoding":           s.encoding,
		"textDocumentSync":           1, // the whole text on every change: a rule is short
		"semanticTokensProvider":     map[string]any{"legend": tokenLegend, "full": true},
		"documentFormattingProvider": true,
		"hoverProvider":              true,
		"completionProvider":         map[string]any{"triggerCharacters": []string{"@"}},
		"signatureHelpProvider":      map[string]any{"triggerCharacters": []string{"(", ","}},
		"executeCommandProvider":     map[string]any{"commands": []string{runCommand, renderCommand}},
	}
}

func (s *Server) exit(json.RawMessage) error {
	s.exited = true
	return nil
}

func (s *Server) didOpen(params json.RawMessage) error {
	var in didOpenParams
	if err := decode(params, &in); err != nil {
		return err
	}
	doc := newDocument(in.TextDocument.URI, in.TextDocument.Version, in.TextDocument.Text)
	s.documents[doc.uri] = doc
	s.publish(doc)
	return nil
}

// didChange takes the whole new text: the server asked for full sync.
func (s *Server) didChange(params json.RawMessage) error {
	var in didChangeParams
	if err := decode(params, &in); err != nil || len(in.ContentChanges) == 0 {
		return err
	}
	text := in.ContentChanges[len(in.ContentChanges)-1].Text
	doc := newDocument(in.TextDocument.URI, in.TextDocument.Version, text)
	s.documents[doc.uri] = doc
	s.publish(doc)
	return nil
}

func (s *Server) didClose(params json.RawMessage) error {
	var in struct {
		TextDocument textDocumentIdentifier `json:"textDocument"`
	}
	if err := decode(params, &in); err != nil {
		return err
	}
	delete(s.documents, in.TextDocument.URI)
	s.notify("textDocument/publishDiagnostics", map[string]any{"uri": in.TextDocument.URI, "diagnostics": []diagnostic{}})
	return nil
}

// documentOf is the open document a request's {"textDocument": {"uri"}} names.
func (s *Server) documentOf(params json.RawMessage) (*document, error) {
	var in struct {
		TextDocument textDocumentIdentifier `json:"textDocument"`
	}
	if err := decode(params, &in); err != nil {
		return nil, err
	}
	return s.document(in.TextDocument.URI)
}

// document is the open text a request names.
func (s *Server) document(uri string) (*document, error) {
	doc, ok := s.documents[uri]
	if !ok {
		return nil, fmt.Errorf("%s is not open", uri)
	}
	return doc, nil
}
