package lsp

import (
	"slices"
	"strings"
	"testing"
)

// In a record update, where a field name goes, the fields of the record being
// updated are offered — the base's, whether it is an argument or a local —
// without the ones already written.
func TestCompletionOffersTheFieldsOfARecordBeingUpdated(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"order with {":                    "amount fee currency",
		"order with {fee: 0, ":            "amount currency",
		"order with {cu":                  "amount fee currency",
		"let(o = order, o with {":         "amount fee currency",
		"order with {amount: order.fee, ": "fee currency",
	}
	for text, want := range cases {
		t.Run(text, func(t *testing.T) {
			t.Parallel()
			s := newSession(t, standard(t), `{}`)
			s.notify("funroute/setContract", map[string]any{"contract": map[string]any{"args": []map[string]string{
				{"name": "order", "type": "record{amount: int, fee: int, currency: string}"},
			}}})
			s.open("file:///a.fr", text)
			if got := strings.Join(labels(t, s.request("textDocument/completion", position("file:///a.fr", len(text)))), " "); got != want {
				t.Errorf("%q: completion is %q, want %q", text, got, want)
			}
		})
	}
	// A declared result the half-written program does not return yet does not
	// hide the base's fields.
	s := newSession(t, standard(t), `{}`)
	s.notify("funroute/setContract", map[string]any{"contract": map[string]any{
		"args":   []map[string]string{{"name": "order", "type": "record{amount: int, fee: int}"}},
		"result": map[string]string{"type": "int"},
	}})
	s.open("file:///a.fr", "order with {")
	if got := strings.Join(labels(t, s.request("textDocument/completion", position("file:///a.fr", 12))), " "); got != "amount fee" {
		t.Errorf("with a declared result the completion is %q, want \"amount fee\"", got)
	}
	// Anywhere else in an update the usual names are offered.
	s = newSession(t, standard(t), `{}`)
	s.notify("funroute/setContract", contract("fee:int"))
	s.open("file:///a.fr", "order with {amount: ")
	if got := labels(t, s.request("textDocument/completion", position("file:///a.fr", 20))); !slices.Contains(got, "fee") {
		t.Errorf("a field's value is offered %v, want fee among them", got)
	}
}

// In a selector, the fields of the items of the list its call reads are
// offered, and after a field of theirs, that field's.
func TestCompletionOffersTheFieldsASelectorCanRead(t *testing.T) {
	t.Parallel()
	for text, want := range map[string]string{
		"sort_by(quotes, .":           "fee channel meta",
		"top_k(take(quotes, 2), .fe":  "fee channel meta",
		"sort_by(quotes, .meta.":      "rank",
		"let(q = quotes, min_by(q, .": "fee channel meta",
	} {
		t.Run(text, func(t *testing.T) {
			t.Parallel()
			s := newSession(t, standard(t), `{}`)
			s.notify("funroute/setContract", map[string]any{"contract": map[string]any{"args": []map[string]string{
				{"name": "quotes", "type": "array<record{fee: int, channel: string, meta: record{rank: int}}>"},
			}}})
			s.open("file:///a.fr", text)
			if got := strings.Join(labels(t, s.request("textDocument/completion", position("file:///a.fr", len(text)))), " "); got != want {
				t.Errorf("%q: completion is %q, want %q", text, got, want)
			}
		})
	}
}
