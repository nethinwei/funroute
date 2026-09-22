package mvp

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"funroute/lang"
	webui "funroute/web"
)

const maxRequestBytes = 1 << 20

// Server exposes exactly one registry: what that registry enables is what this
// console can express.
type Server struct {
	registry *lang.Registry
	handler  http.Handler
}

func NewServer(registry *lang.Registry) (*Server, error) {
	if registry == nil {
		return nil, fmt.Errorf("registry is required")
	}
	server := &Server{registry: registry}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", server.health)
	mux.HandleFunc("GET /api/catalog", server.catalog)
	mux.HandleFunc("POST /api/contract/check", server.checkContract)
	mux.HandleFunc("POST /api/parse", server.parse)
	mux.HandleFunc("POST /api/compile", server.compile)
	mux.HandleFunc("POST /api/run", server.run)
	mux.Handle("/", http.FileServer(http.FS(webui.Files)))
	server.handler = securityHeaders(mux)
	return server, nil
}

func (s *Server) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	s.handler.ServeHTTP(response, request)
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("X-Content-Type-Options", "nosniff")
		response.Header().Set("X-Frame-Options", "DENY")
		response.Header().Set("Referrer-Policy", "no-referrer")
		response.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self'; script-src 'self'; connect-src 'self'; img-src 'self' data:")
		next.ServeHTTP(response, request)
	})
}

func (s *Server) health(response http.ResponseWriter, _ *http.Request) {
	writeJSON(response, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) catalog(response http.ResponseWriter, _ *http.Request) {
	writeJSON(response, http.StatusOK, lang.Catalog(s.registry))
}

// The contract is the host's, and this server is the host: a console would
// read it from its rule record, so the API takes it alongside the expression.
// Contract and expression arrive as separate fields and are never merged into
// one text, which is why the page needs no source-to-panel synchronisation.
type expressionRequest struct {
	Source   string          `json:"source,omitempty"`
	ExprJSON json.RawMessage `json:"expr_json,omitempty"`
	Contract *contractJSON   `json:"contract,omitempty"`
	Args     map[string]any  `json:"args,omitempty"`
	Fuel     uint64          `json:"fuel,omitempty"`
}

type contractJSON struct {
	Args   []argJSON   `json:"args,omitempty"`
	Result *resultJSON `json:"result,omitempty"`
}

type argJSON struct {
	Name string `json:"name"`
	Type string `json:"type"`
	Doc  string `json:"doc,omitempty"`
}

type resultJSON struct {
	Type string `json:"type"`
	Doc  string `json:"doc,omitempty"`
}

// compileOptions turns the request's contract into what the compiler takes.
func (c *contractJSON) compileOptions() (lang.CompileOptions, error) {
	var options lang.CompileOptions
	if c == nil {
		return options, nil
	}
	for _, arg := range c.Args {
		typ, err := lang.ParseType(arg.Type)
		if err != nil {
			return options, fmt.Errorf("argument %q: %w", arg.Name, err)
		}
		options.Args = append(options.Args, lang.ArgSpec{Name: arg.Name, Type: typ, Doc: arg.Doc})
	}
	if c.Result != nil {
		typ, err := lang.ParseType(c.Result.Type)
		if err != nil {
			return options, fmt.Errorf("result: %w", err)
		}
		options.Result = &typ
		options.ResultDoc = c.Result.Doc
	}
	return options, nil
}

func (c *contractJSON) checkedOptions() (lang.CompileOptions, error) {
	if c == nil {
		return lang.CompileOptions{}, fmt.Errorf("runtime contract is required")
	}
	if c.Result == nil || strings.TrimSpace(c.Result.Type) == "" {
		return lang.CompileOptions{}, fmt.Errorf("result type is required")
	}
	options, err := c.compileOptions()
	if err != nil {
		return options, err
	}
	if err := lang.ValidateContract(options); err != nil {
		return options, err
	}
	return options, nil
}

func (s *Server) checkContract(response http.ResponseWriter, request *http.Request) {
	payload, ok := s.decodeRequest(response, request)
	if !ok {
		return
	}
	options, err := payload.Contract.checkedOptions()
	if err != nil {
		writeAPIError(response, http.StatusUnprocessableEntity, "CONTRACT_ERROR", err)
		return
	}
	args := make([]lang.Parameter, len(options.Args))
	for i, arg := range options.Args {
		args[i] = lang.Parameter{Name: arg.Name, Type: arg.Type, Doc: arg.Doc}
	}
	writeJSON(response, http.StatusOK, map[string]any{
		"valid": true, "arguments": len(args), "args": args,
		"result": options.Result, "result_doc": options.ResultDoc,
	})
}

type compileResponse struct {
	Digest       string               `json:"digest"`
	Args         []lang.Parameter     `json:"args"`
	Result       lang.Type            `json:"result"`
	Instructions int                  `json:"instructions"`
	Calls        []lang.CallReference `json:"calls"`
	ExprJSON     json.RawMessage      `json:"expr_json"`
}

// parse turns source text into canonical ExprJSON without type checking it, so
// the designer can render a node tree the operator is still editing.
func (s *Server) parse(response http.ResponseWriter, request *http.Request) {
	payload, ok := s.decodeRequest(response, request)
	if !ok {
		return
	}
	encoded, err := s.parsePayload(payload)
	if err != nil {
		writeAPIError(response, http.StatusUnprocessableEntity, "PARSE_ERROR", err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"expr_json": json.RawMessage(encoded)})
}

func (s *Server) parsePayload(payload expressionRequest) ([]byte, error) {
	if strings.TrimSpace(payload.Source) == "" {
		return nil, fmt.Errorf("source is required")
	}
	return lang.ParseToJSON(payload.Source)
}

func (s *Server) compile(response http.ResponseWriter, request *http.Request) {
	payload, ok := s.decodeRequest(response, request)
	if !ok {
		return
	}
	artifact, err := s.compilePayload(payload)
	if err != nil {
		writeAPIError(response, http.StatusUnprocessableEntity, "COMPILE_ERROR", err)
		return
	}
	writeJSON(response, http.StatusOK, summarize(artifact))
}

func (s *Server) run(response http.ResponseWriter, request *http.Request) {
	payload, ok := s.decodeRequest(response, request)
	if !ok {
		return
	}
	artifact, err := s.compilePayload(payload)
	if err != nil {
		writeAPIError(response, http.StatusUnprocessableEntity, "COMPILE_ERROR", err)
		return
	}
	runtime, err := lang.Instantiate(artifact, s.registry)
	if err != nil {
		writeAPIError(response, http.StatusInternalServerError, "INSTANTIATE_ERROR", err)
		return
	}
	result, err := runtime.Run(request.Context(), payload.Args, lang.RunOptions{Fuel: payload.Fuel})
	if err != nil {
		writeAPIError(response, http.StatusUnprocessableEntity, "RUN_ERROR", err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{
		"artifact": summarize(artifact),
		"value":    result.Any(),
		"type":     result.Type(),
	})
}

func (s *Server) decodeRequest(response http.ResponseWriter, request *http.Request) (expressionRequest, bool) {
	request.Body = http.MaxBytesReader(response, request.Body, maxRequestBytes)
	decoder := json.NewDecoder(request.Body)
	decoder.UseNumber()
	decoder.DisallowUnknownFields()
	var payload expressionRequest
	if err := decoder.Decode(&payload); err != nil {
		writeAPIError(response, http.StatusBadRequest, "BAD_REQUEST", err)
		return expressionRequest{}, false
	}
	if err := ensureJSONEOF(decoder); err != nil {
		writeAPIError(response, http.StatusBadRequest, "BAD_REQUEST", err)
		return expressionRequest{}, false
	}
	return payload, true
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("request contains a trailing JSON value")
		}
		return err
	}
	return nil
}

func (s *Server) compilePayload(payload expressionRequest) (*lang.Artifact, error) {
	if len(payload.ExprJSON) > 0 && strings.TrimSpace(payload.Source) != "" {
		return nil, fmt.Errorf("provide either source or expr_json, not both")
	}
	options, err := payload.Contract.checkedOptions()
	if err != nil {
		return nil, err
	}
	// Source and ExprJSON meet at the same compile path, so one can never be
	// accepted while the other is refused.
	if len(payload.ExprJSON) > 0 {
		return lang.CompileJSON(payload.ExprJSON, s.registry, options)
	}
	if strings.TrimSpace(payload.Source) == "" {
		return nil, fmt.Errorf("source or expr_json is required")
	}
	return lang.CompileExpr(payload.Source, s.registry, options)
}

func summarize(artifact *lang.Artifact) compileResponse {
	return compileResponse{
		Digest: artifact.Digest, Args: artifact.Args, Result: artifact.Result,
		Instructions: len(artifact.Instructions), Calls: artifact.Calls, ExprJSON: artifact.ExprJSON,
	}
}

func writeAPIError(response http.ResponseWriter, status int, code string, err error) {
	writeJSON(response, status, map[string]any{
		"error": map[string]string{"code": code, "message": err.Error()},
	})
}

func writeJSON(response http.ResponseWriter, status int, value any) {
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(value)
}
