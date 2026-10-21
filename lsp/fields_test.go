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
		"if(true, order with {fee: 0, ":   "amount currency",
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
	// Past an update, a with written before the cursor offers no fields.
	text := "let(o = order with {fee: 1}, o.fee + "
	s.open("file:///b.fr", text)
	if got := labels(t, s.request("textDocument/completion", position("file:///b.fr", len(text)))); !slices.Contains(got, "o") || !slices.Contains(got, "fee") {
		t.Errorf("%q: completion is %v, want the local o and the argument fee among them", text, got)
	}
}

// In a selector, the fields of the items of the list its call reads are
// offered, and after a field of theirs, that field's — with the cursor, |,
// at the end of the text or in the middle of a name already written.
func TestCompletionOffersTheFieldsASelectorCanRead(t *testing.T) {
	t.Parallel()
	for text, want := range map[string]string{
		"sort_by(quotes, .":           "fee channel meta",
		"top_k(take(quotes, 2), .fe":  "fee channel meta",
		"sort_by(quotes, .meta.":      "rank",
		"let(q = quotes, min_by(q, .": "fee channel meta",
		"sort_by(quotes, .f|ee)":      "fee channel meta",
		"sort_by(quotes, . ":          "fee channel meta",
	} {
		t.Run(text, func(t *testing.T) {
			t.Parallel()
			s := newSession(t, standard(t), `{}`)
			s.notify("funroute/setContract", map[string]any{"contract": map[string]any{"args": []map[string]string{
				{"name": "quotes", "type": "array<record{fee: int, channel: string, meta: record{rank: int}}>"},
			}}})
			cursor := strings.Index(text, "|")
			source := strings.Replace(text, "|", "", 1)
			if cursor < 0 {
				cursor = len(source)
			}
			s.open("file:///a.fr", source)
			if got := strings.Join(labels(t, s.request("textDocument/completion", position("file:///a.fr", cursor))), " "); got != want {
				t.Errorf("%q: completion is %q, want %q", text, got, want)
			}
		})
	}
}

// The base of an update is the whole of what is written before with: a
// record field's own fields are offered, and none of the record it is read
// from — of a base that is no record, none at all.
func TestCompletionOffersTheFieldsOfTheWholeBase(t *testing.T) {
	t.Parallel()
	for text, want := range map[string]string{
		"order.meta with {":          "rank",
		"order.fee with {":           "",
		"if(true, order.meta with {": "rank",
	} {
		t.Run(text, func(t *testing.T) {
			t.Parallel()
			s := newSession(t, standard(t), `{}`)
			s.notify("funroute/setContract", map[string]any{"contract": map[string]any{"args": []map[string]string{
				{"name": "order", "type": "record{fee: int, meta: record{rank: int}}"},
			}}})
			s.open("file:///a.fr", text)
			got := labels(t, s.request("textDocument/completion", position("file:///a.fr", len(text))))
			if want == "" && slices.Contains(got, "fee") || want != "" && strings.Join(got, " ") != want {
				t.Errorf("%q: completion is %q, want %q", text, strings.Join(got, " "), want)
			}
		})
	}
}

// A local named as an argument hides it: where the rule has a type error
// elsewhere, or does not parse past the cursor, so the base's type cannot be
// read off it, the argument's fields are not offered for the local's — no
// fields rather than the wrong ones — whether the local is updated or a
// selector reads it, and whether it is bound before the cursor or, by a
// comprehension, after it. The cursor, |, is at the end of the text where
// none is written.
func TestCompletionOffersNoArgumentsFieldsForALocal(t *testing.T) {
	t.Parallel()
	for _, text := range []string{
		`let(x = 1 + "s", let(order = {a: 1, b: 2}, order with {a: 3, `,
		`let(x = 1 + "s", let(quotes = [{a: 1}], sort_by(quotes, .`,
		`[order with {fee: 0, | } for order in [{a: 1}]] + `,
		`let(x = 1 + "s", [order with {fee: 0, | } for order in [{a: 1}]])`,
		`[sort_by(quotes, .| ) for quotes in [[{a: 1}]]] + `,
	} {
		t.Run(text, func(t *testing.T) {
			t.Parallel()
			s := newSession(t, standard(t), `{}`)
			s.notify("funroute/setContract", map[string]any{"contract": map[string]any{"args": []map[string]string{
				{"name": "order", "type": "record{amount: int, fee: int, currency: string}"},
				{"name": "quotes", "type": "array<record{amount: int, currency: string}>"},
			}}})
			cursor := strings.Index(text, "|")
			source := strings.Replace(text, "|", "", 1)
			if cursor < 0 {
				cursor = len(source)
			}
			s.open("file:///a.fr", source)
			got := labels(t, s.request("textDocument/completion", position("file:///a.fr", cursor)))
			if slices.Contains(got, "amount") || slices.Contains(got, "currency") {
				t.Errorf("%q: completion is %v, want none of the argument's fields", text, got)
			}
		})
	}
}

// A dictionary comprehension's for binds the name its value reads, though no
// [ opens it: the argument's fields are not offered for the local.
func TestCompletionOffersNoArgumentsFieldsForADictionaryComprehensionsLocal(t *testing.T) {
	t.Parallel()
	for _, text := range []string{
		`{"k": order with {| } for order in xs`,
		`let(x = 1 + "s", {"k": order with {| } for order in xs})`,
	} {
		t.Run(text, func(t *testing.T) {
			t.Parallel()
			s := newSession(t, standard(t), `{}`)
			s.notify("funroute/setContract", map[string]any{"contract": map[string]any{"args": []map[string]string{
				{"name": "order", "type": "record{amount: int, fee: int, currency: string}"},
				{"name": "xs", "type": "array<record{name: string, amount: int}>"},
			}}})
			cursor := strings.Index(text, "|")
			s.open("file:///a.fr", strings.Replace(text, "|", "", 1))
			got := labels(t, s.request("textDocument/completion", position("file:///a.fr", cursor)))
			if slices.Contains(got, "fee") || slices.Contains(got, "currency") {
				t.Errorf("%q: completion is %v, want none of the argument's fields", text, got)
			}
		})
	}
}

// A name the base reads written after the cursor but bound by no
// comprehension around the base there — read again, a field label, in a
// string or a comment, or bound by a for or a reduce beside the base — does
// not stop the fields being offered, where the text as typed lacks the comma
// before the next field or ends in an operator; the fields written after the
// cursor are not offered again.
func TestCompletionOffersTheFieldsWhereTheBaseIsOnlyReadAfter(t *testing.T) {
	t.Parallel()
	for text, want := range map[string]string{
		"order with {\n  fee: 0,\n  |\n  currency: order.fee\n}":                                        "amount",
		"order with {\n  fee: 0,\n  |\n  meta: {order: 0}\n}":                                           "amount currency",
		"order with {\n  |\n  meta: [x for order in xs]\n}":                                             "amount fee currency",
		"[order with {\n  |\n  currency: \"USD\"\n}, [x for order in xs]]":                              "amount fee",
		"[order with {\n  |\n  currency: \"USD\"\n}, reduce(order in xs, total = 0, total)]":            "amount fee",
		"order with {\n  fee: 0,\n  |\n  meta: [x for order in xs]\n}":                                  "amount currency",
		"[order with {\n  fee: 0,\n  |\n  currency: \"USD\"\n}, [x for order in xs]]":                   "amount",
		"[order with {\n  fee: 0,\n  |\n  currency: \"USD\"\n}, reduce(order in xs, total = 0, total)]": "amount",
		"[order with {| }, {a: b for order in xs}] + ":                                                  "amount fee currency",
		"[order with {| } + \"for order in xs\"] + ":                                                    "amount fee currency",
		"[order with {| } // for order in xs\n] + ":                                                     "amount fee currency",
	} {
		t.Run(text, func(t *testing.T) {
			t.Parallel()
			s := newSession(t, standard(t), `{}`)
			s.notify("funroute/setContract", map[string]any{"contract": map[string]any{"args": []map[string]string{
				{"name": "order", "type": "record{amount: int, fee: int, currency: string}"},
				{"name": "xs", "type": "array<record{amount: int}>"},
			}}})
			cursor := strings.Index(text, "|")
			s.open("file:///a.fr", strings.Replace(text, "|", "", 1))
			got := labels(t, s.request("textDocument/completion", position("file:///a.fr", cursor)))
			if strings.Join(got, " ") != want {
				t.Errorf("%q: completion is %v, want the fields %s", text, got, want)
			}
		})
	}
}

// The texts fields are looked for in keep all that follows the cursor, |,
// past the rest of the name typed there, and close the brackets the whole
// text leaves open: those already closed after the cursor are not closed
// again. Last comes the text cut off at the cursor, unless a for or a reduce
// is written past it, outside strings and comments.
func TestFieldTextsKeepWhatFollowsTheCursor(t *testing.T) {
	t.Parallel()
	for text, want := range map[string][]string{
		"[o with {| } for o in xs": {
			"[o with {_ } for o in xs\n]",
			"[o with {_ } for o in xs\n_\n]",
			"[o with {_,  } for o in xs\n]",
			"[o with {_,  } for o in xs\n_\n]",
		},
		"[o with {|a: 1}, reduce(o in xs, t = 0, t)]": {
			"[o with {_: 1}, reduce(o in xs, t = 0, t)]\n",
			"[o with {_: 1}, reduce(o in xs, t = 0, t)]\n_\n",
			"[o with {_, : 1}, reduce(o in xs, t = 0, t)]\n",
			"[o with {_, : 1}, reduce(o in xs, t = 0, t)]\n_\n",
		},
		"f(.a|b) + ": {
			"f(.a_) + \n",
			"f(.a_) + \n_\n",
			"f(.a_, ) + \n",
			"f(.a_, ) + \n_\n",
			"f(.a_)",
		},
		"[o with {|a: 1} + \"for\"] // for": {
			"[o with {_: 1} + \"for\"] // for\n",
			"[o with {_: 1} + \"for\"] // for\n_\n",
			"[o with {_, : 1} + \"for\"] // for\n",
			"[o with {_, : 1} + \"for\"] // for\n_\n",
			"[o with {_}]",
		},
	} {
		cursor := strings.Index(text, "|")
		doc := newDocument("x", 0, strings.Replace(text, "|", "", 1))
		got := slices.Collect(fieldTexts(doc, cursor, placeholder))
		for i := range got {
			got[i] = strings.ReplaceAll(got[i], placeholder, "_")
		}
		if !slices.Equal(got, want) {
			t.Errorf("fieldTexts(%q) = %q, want %q", text, got, want)
		}
	}
}

// Right before a field written, or in its name, the fields are offered from
// the text cut off at the cursor — where a for or a reduce follows outside
// strings and comments, none, rather than the argument's for a local.
func TestCompletionOffersTheFieldsRightBeforeAFieldWritten(t *testing.T) {
	t.Parallel()
	for text, want := range map[string]string{
		"order with {|fee: 0, amount: 1}":                         "amount fee currency",
		"order with {fe|e: 0, amount: 1}":                         "amount fee currency",
		"order with {fee: 0,|currency: \"USD\"}":                  "amount currency",
		"[order with {|currency: \"USD\"} + \"for order in xs\"]": "amount fee currency",
		"order with {|fee: 0} // for order in xs":                 "amount fee currency",
		"{\"k\": order with {|fee: 0} for order in xs}":           "",
		"[order with {|fee: 0}, reduce(order in xs, t = 0, t)]":   "",
	} {
		t.Run(text, func(t *testing.T) {
			t.Parallel()
			s := newSession(t, standard(t), `{}`)
			s.notify("funroute/setContract", map[string]any{"contract": map[string]any{"args": []map[string]string{
				{"name": "order", "type": "record{amount: int, fee: int, currency: string}"},
				{"name": "xs", "type": "array<record{name: string, amount: int}>"},
			}}})
			cursor := strings.Index(text, "|")
			s.open("file:///a.fr", strings.Replace(text, "|", "", 1))
			got := labels(t, s.request("textDocument/completion", position("file:///a.fr", cursor)))
			if want == "" && (slices.Contains(got, "fee") || slices.Contains(got, "currency")) || want != "" && strings.Join(got, " ") != want {
				t.Errorf("%q: completion is %v, want the fields %q", text, got, want)
			}
		})
	}
}

// A selector's name is typed right after its dot: only there are the fields
// of the list looked for.
func TestTypingSelectorIsAfterTheDot(t *testing.T) {
	t.Parallel()
	for text, want := range map[string]bool{
		"sort_by(xs, .":            true,
		"sort_by(xs, .fe":          true,
		"sort_by(xs, .a.b_2":       true,
		"sort_by(xs, . ":           true,
		"sort_by(xs, . fe":         true,
		"sort_by(xs, .\n  // c\n ": true,
		"sort_by(xs, ":             false,
		"sort_by(xs, fe":           false,
		"x.":                       true,
		"":                         false,
	} {
		if got := typingSelector(&document{text: text}, len(text)); got != want {
			t.Errorf("typingSelector(%q) = %v, want %v", text, got, want)
		}
	}
}

// An update's fields are looked for only inside the braces after its with,
// wherever a with was written before.
func TestInUpdateIsInsideTheBracesAfterAWith(t *testing.T) {
	t.Parallel()
	for text, want := range map[string]bool{
		"order with {":                         true,
		"order with {fee: 0, ":                 true,
		"order with {cu":                       true,
		"order with // c\n{":                   true,
		"order with {meta: {rank: 1}, ":        true,
		"order with {fee: {a: 1, ":             false,
		"o = order with {fee: 1},\n  o.fee + ": false,
		"{a: 1, ":                              false,
		"\"with {\" + ":                        false,
		"// with {\n":                          false,
		"":                                     false,
	} {
		if got := inUpdate(&document{text: text}, len(text)); got != want {
			t.Errorf("inUpdate(%q) = %v, want %v", text, got, want)
		}
	}
}
