package machine

import (
	"strings"
	"testing"
)

type firstHandle struct{}
type secondHandle struct{}

func TestHandleNamesAndGoTypesAreOneToOne(t *testing.T) {
	registry := NewRegistry()
	if err := DefineHandle[*firstHandle](registry, "engine.tensor"); err != nil {
		t.Fatal(err)
	}
	if err := DefineHandle[*secondHandle](registry, "engine.tensor"); err == nil || !strings.Contains(err.Error(), "already Go type") {
		t.Fatalf("duplicate handle name = %v", err)
	}
	if err := DefineHandle[*firstHandle](registry, "engine.other"); err == nil || !strings.Contains(err.Error(), "already handle") {
		t.Fatalf("duplicate Go type = %v", err)
	}
	if handles := registry.Handles(); len(handles) != 1 || handles[0].Name != "engine.tensor" {
		t.Fatalf("handles = %v", handles)
	}
}
