package lsp

import (
	"encoding/json"
	"errors"

	"github.com/nethinwei/funroute/internal/compile"
	"github.com/nethinwei/funroute/internal/syntax"
)

// publish sends a document's diagnostics: what compiling it against the
// contract reports, placed where the error says it is.
func (s *Server) publish(doc *document) {
	s.notify("textDocument/publishDiagnostics", map[string]any{
		"uri": doc.uri, "version": doc.version, "diagnostics": s.diagnose(doc),
	})
}

func (s *Server) diagnose(doc *document) []diagnostic {
	err := s.contractErr
	if err == nil {
		_, err = s.analysisOf(doc)
	}
	if err == nil {
		return []diagnostic{}
	}
	return []diagnostic{{Range: s.errorRange(doc, err), Severity: 1, Source: "funroute", Message: err.Error()}}
}

// analysisOf is what compiling the document against the contract found. The
// analysis is partial when the program has an error, and nil when it does not
// parse.
func (s *Server) analysisOf(doc *document) (*compile.Analysis, error) {
	if !doc.analyzed {
		doc.analysis, doc.analysisErr = compile.Analyze(doc.text, s.registry, s.contract)
		doc.analyzed = true
	}
	return doc.analysis, doc.analysisErr
}

// lexemesOf is every piece of the document, as the lexer and parser read it.
func lexemesOf(doc *document) []syntax.Lexeme {
	if !doc.lexed {
		doc.lexemes, _ = syntax.Lexemes(doc.text)
		doc.lexed = true
	}
	return doc.lexemes
}

// errorRange is the source an error is about, or the start of the document
// for one that is about no particular place, such as a malformed contract.
func (s *Server) errorRange(doc *document, err error) Range {
	var positioned *syntax.PosError
	if !errors.As(err, &positioned) {
		return Range{}
	}
	start, end := positioned.Span()
	return doc.rangeOf(syntax.Span{Start: start, End: end}, s.encoding)
}

// applyContract reads the contract a client set. A nil one declares nothing,
// so every argument is inferred. A contract that is wrong is not half-kept:
// until it is fixed, hover and completion see no contract at all and the
// diagnostics say what is wrong with it.
func (s *Server) applyContract(contract *compile.TextContract) {
	options, err := contract.Options()
	if err == nil {
		err = compile.ValidateContract(options)
	}
	if err != nil {
		options = compile.CompileOptions{}
	}
	s.contract, s.contractErr = options, err
}

// setContract replaces the contract and publishes every open document again,
// since what each one means has changed.
func (s *Server) setContract(params json.RawMessage) error {
	var in struct {
		Contract *compile.TextContract `json:"contract"`
	}
	if err := decode(params, &in); err != nil {
		return err
	}
	s.applyContract(in.Contract)
	for _, doc := range s.documents {
		doc.analyzed = false
		s.publish(doc)
	}
	return nil
}
