package lsp

import (
	"encoding/json"
	"errors"
	"slices"
	"testing"

	"github.com/nethinwei/funroute/internal/machine"
)

func TestInitializeAgreesOnAnEncoding(t *testing.T) {
	t.Parallel()
	utf8 := newSession(t, standard(t), `{"capabilities":{"general":{"positionEncodings":["utf-16","utf-8"]}}}`)
	if got := as[map[string]any](t, as[map[string]any](t, utf8.out[0]["result"])["capabilities"])["positionEncoding"]; got != "utf-8" {
		t.Errorf("offered utf-16 and utf-8, agreed on %v, want utf-8", got)
	}
	utf16 := newSession(t, standard(t), `{}`)
	if got := as[map[string]any](t, as[map[string]any](t, utf16.out[0]["result"])["capabilities"])["positionEncoding"]; got != "utf-16" {
		t.Errorf("offered nothing, agreed on %v, want utf-16", got)
	}
}

// A handler that panics fails its own message, with an internal error the
// client can tell apart, and leaves the server — and the lock — as they were.
func TestAHandlerPanicFailsOnlyItsMessage(t *testing.T) {
	t.Parallel()
	server := New(nil, func([]byte) {})
	err := server.guarded(func() error { panic("a bug in a handler") })
	if !errors.Is(err, errInternal) || errorCode(err) != codeInternalError {
		t.Fatalf("guarded(panic) = %v (code %d), want errInternal (code %d)", err, errorCode(err), codeInternalError)
	}
	if err := server.guarded(func() error { return nil }); err != nil {
		t.Fatalf("guarded after a panic = %v, want nil: the lock was left held", err)
	}
}

// session drives a server the way a client does and keeps what it sent back.
// Every test and subtest builds its own, so they share nothing.
type session struct {
	t      *testing.T
	server *Server
	out    []map[string]any
	nextID int
}

func (s *session) request(method string, params any) any {
	s.t.Helper()
	s.nextID++
	s.send(map[string]any{"jsonrpc": "2.0", "id": s.nextID, "method": method, "params": params})
	for _, message := range s.out {
		if id, ok := message["id"].(float64); ok && int(id) == s.nextID {
			if failure, failed := message["error"]; failed {
				return failure
			}
			return message["result"]
		}
	}
	s.t.Fatalf("%s got no response", method)
	return nil
}

// as is v as a T, and fails the test when v is something else.
func as[T any](t *testing.T, v any) T {
	t.Helper()
	got, ok := v.(T)
	if !ok {
		t.Fatalf("%#v is not a %T", v, got)
	}
	return got
}

func (s *session) notify(method string, params any) {
	s.send(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
}

func (s *session) send(message any) {
	encoded, err := json.Marshal(message)
	if err != nil {
		s.t.Fatal(err)
	}
	s.server.Handle(encoded)
}

func (s *session) open(uri, text string) {
	s.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{"uri": uri, "version": 1, "text": text}})
}

// diagnostics is what was last published for uri.
func (s *session) diagnostics(uri string) []any {
	for _, message := range slices.Backward(s.out) {
		params, _ := message["params"].(map[string]any)
		if message["method"] == "textDocument/publishDiagnostics" && params["uri"] == uri {
			return as[[]any](s.t, params["diagnostics"])
		}
	}
	s.t.Fatalf("nothing was published for %s", uri)
	return nil
}

func newSession(t *testing.T, registry *machine.Registry, capabilities string) *session {
	t.Helper()
	s := &session{t: t}
	s.server = New(registry, func(message []byte) {
		var decoded map[string]any
		if err := json.Unmarshal(message, &decoded); err != nil {
			t.Fatalf("the server sent %s: %v", message, err)
		}
		s.out = append(s.out, decoded)
	})
	s.request("initialize", json.RawMessage(capabilities))
	return s
}

func standard(t *testing.T) *machine.Registry {
	t.Helper()
	registry := machine.CoreRegistry()
	if err := registry.EnableForm(machine.SwitchForm, machine.ForForm, machine.ReduceForm); err != nil {
		t.Fatal(err)
	}
	return registry
}

func docParams(uri string) map[string]any {
	return map[string]any{"textDocument": map[string]string{"uri": uri}}
}
