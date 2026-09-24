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
	if handles := registry.Handles(); len(handles) != 1 || handles[0].name != "engine.tensor" {
		t.Fatalf("Handles() = %v, want only engine.tensor", handles)
	}
}

// if is a function's name and a field may be called it, but no variable or
// local may: in a comprehension or a reduce it starts the filter clause.
func TestIfIsNoVariableName(t *testing.T) {
	t.Parallel()
	if IsValidVariableName("if") || !IsValidFunctionName("if") || !IsValidFieldName("if") {
		t.Fatalf("if: variable %v, function %v, field %v, want false, true, true",
			IsValidVariableName("if"), IsValidFunctionName("if"), IsValidFieldName("if"))
	}
}

// A name's shape is a word: a letter or underscore first, then letters,
// digits and underscores — and, in a function's name, dots.
func TestNameShapes(t *testing.T) {
	t.Parallel()
	for name, want := range map[string][2]bool{
		"amount": {true, true}, "_x1": {true, true}, "route.score_v1": {false, true}, "1x": {false, false},
		"": {false, false}, "a-b": {false, false}, "a b": {false, false}, "é": {false, false}, "a.": {false, true},
	} {
		if got := [2]bool{nameShape(name, false), nameShape(name, true)}; got != want {
			t.Errorf("nameShape(%q) = %v (variable, function), want %v", name, got, want)
		}
	}
}
