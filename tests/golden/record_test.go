package golden

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"
)

// The record is two files, each written one item a line so that a change
// reads as the lines it changes. outcomes.json is every program and what it
// gave. phrases.json is the wording of every failure, once: an outcome names
// its failure's phrase, P3, with the values that fill it, so a change of
// wording is one line of phrases.json and no outcome. A phrase is numbered
// where it first appears, which a fixed run makes the same every time. An
// outcome too long to read is kept as its start, its length and its digest:
// a change anywhere in it still shows.

const (
	outcomesPath = "testdata/outcomes.json"
	phrasesPath  = "testdata/phrases.json"
	// longOutcome is the length past which an outcome is kept shortened, and
	// shownOfLong how much of its start is kept.
	longOutcome = 2000
	shownOfLong = 200
)

// filled are the parts of a failure's words that differ from one failure of
// a phrase to another: quoted text and numbers.
var filled = regexp.MustCompile(`"(?:[^"\\]|\\.)*"|-?\b\d+(?:\.\d+)?(?:[eE][+-]?\d+)?\b`)

// record is what the two files hold.
type record struct {
	entries []entry
	phrases []string
}

// recorded is entries as the record keeps them.
func recorded(entries []entry) record {
	ids := map[string]int{}
	var phrases []string
	kept := make([]entry, len(entries))
	for i, e := range entries {
		outcomes := make([]string, len(e.Outcomes))
		for j, outcome := range e.Outcomes {
			outcomes[j] = shortened(phrased(outcome, ids, &phrases))
		}
		kept[i] = entry{Type: e.Type, Source: e.Source, Outcomes: outcomes}
	}
	return record{kept, phrases}
}

// phrased is a failure as its head, its phrase and the values that fill it —
// error[domain] P3 ["7","\"sg\""] — and anything else as itself. Words that
// already hold the placeholder are kept whole: two of them could otherwise
// read as one.
func phrased(outcome string, ids map[string]int, phrases *[]string) string {
	head, words, ok := splitFailure(outcome)
	if !ok || strings.Contains(words, "{}") {
		return outcome
	}
	values := filled.FindAllString(words, -1)
	phrase := filled.ReplaceAllString(words, "{}")
	id, seen := ids[phrase]
	if !seen {
		*phrases = append(*phrases, phrase)
		id = len(*phrases)
		ids[phrase] = id
	}
	text := fmt.Sprintf("%s P%d", head, id)
	if len(values) > 0 {
		encoded, _ := json.Marshal(values) // strings only: it cannot fail
		text += " " + string(encoded)
	}
	return text
}

// splitFailure splits "error[class] words", after "refused " when RunValues
// refused the input, into its head and its words.
func splitFailure(outcome string) (head, words string, ok bool) {
	prefix, rest := "", outcome
	if after, refused := strings.CutPrefix(outcome, "refused "); refused {
		prefix, rest = "refused ", after
	}
	end := strings.Index(rest, "] ")
	if !strings.HasPrefix(rest, "error[") || end < 0 {
		return "", "", false
	}
	return prefix + rest[:end+1], rest[end+2:], true
}

// shortened is an outcome too long to read as its start, its length and its
// digest; any other as itself.
func shortened(outcome string) string {
	if len(outcome) <= longOutcome {
		return outcome
	}
	start := shownOfLong
	for !utf8.RuneStart(outcome[start]) {
		start--
	}
	return fmt.Sprintf("%s… (%d bytes, sha256 %x)", outcome[:start], len(outcome), sha256.Sum256([]byte(outcome)))
}

func write(t *testing.T, r record) {
	t.Helper()
	if err := os.MkdirAll("testdata", 0o755); err != nil {
		t.Fatal(err)
	}
	for path, value := range map[string]any{outcomesPath: r.entries, phrasesPath: r.phrases} {
		var out bytes.Buffer
		encoder := json.NewEncoder(&out)
		encoder.SetEscapeHTML(false)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(value); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, out.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func read(t *testing.T) record {
	t.Helper()
	var r record
	for path, into := range map[string]any{outcomesPath: &r.entries, phrasesPath: &r.phrases} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%v; go test -update writes the record", err)
		}
		if err := json.Unmarshal(data, into); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
	}
	if len(r.entries) == 0 {
		t.Fatal("the record holds no programs")
	}
	return r
}
