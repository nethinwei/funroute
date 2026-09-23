package machine

import (
	"strings"
	"testing"
)

type firstHandle struct{}
type secondHandle struct{}

func TestHandleNamesAndGoTypesAreOneToOne(t *testing.T) {
	t.Parallel()
	registry := NewRegistry()
	if err := DefineHandle[*firstHandle](registry, "engine.tensor"); err != nil {
		t.Fatal(err)
	}
	if err := DefineHandle[*secondHandle](registry, "engine.tensor"); err == nil || !strings.Contains(err.Error(), "already Go type") {
		t.Fatalf("DefineHandle[*secondHandle](%q) error = %v, want one containing %q", "engine.tensor", err, "already Go type")
	}
	if err := DefineHandle[*firstHandle](registry, "engine.other"); err == nil || !strings.Contains(err.Error(), "already handle") {
		t.Fatalf("DefineHandle[*firstHandle](%q) error = %v, want one containing %q", "engine.other", err, "already handle")
	}
	if handles := registry.Handles(); len(handles) != 1 || handles[0].Name != "engine.tensor" {
		t.Fatalf("Handles() = %v, want only engine.tensor", handles)
	}
}
