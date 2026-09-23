package syntax

import (
	"errors"
	"strings"
	"testing"
)

// A record update is {...base, name: value, …}: the spread first and once, and
// at least one field after it. ExprJSON holds it to the same rules.
func TestRecordUpdateIsWrittenOneWay(t *testing.T) {
	for source, want := range map[string]string{
		`{...r}`:             "changes nothing",
		`{...r, a: 1, a: 2}`: `duplicate record field "a"`,
		`{a: 1, ...r}`:       "record fields are written as name: value",
		`{...r, ...s, a: 1}`: "record fields are written as name: value",
		`{...r, "a": 1}`:     "record fields are written as name: value",
		`{...r, case: 1}`:    `invalid field name "case"`,
	} {
		if _, err := Parse(source); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: error = %v, want %q", source, err, want)
		}
	}
	document := `{"version":1,"expr":{"node":"record_update","base":{"node":"var","name":"r"},"fields":[]}}`
	if _, err := ImportExprJSON([]byte(document)); err == nil {
		t.Fatal("an update that changes nothing imported from ExprJSON")
	}
}

// A reserved word as a field name is reported where it is written, in an
// update and after a record literal's first field alike.
func TestReservedFieldNamesPointAtTheName(t *testing.T) {
	for source, at := range map[string]int{`{...r, for: 1}`: 7, `{a: 1, for: 2}`: 7} {
		_, err := Parse(source)
		var positioned *PosError
		if !errors.As(err, &positioned) || positioned.Start != at {
			t.Errorf("%s: error = %v, want it at %d", source, err, at)
		}
	}
}
