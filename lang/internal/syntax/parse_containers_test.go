package syntax

import (
	"errors"
	"strings"
	"testing"
)

// A record update is base with {name: value, …}: a postfix, as .field is,
// with at least one field in its braces. ExprJSON holds it to the same rules,
// and nothing is written with "..." any more.
func TestRecordUpdateIsWrittenOneWay(t *testing.T) {
	t.Parallel()
	for source, want := range map[string]string{
		`r with {}`:            "changes nothing",
		`r with a: 1`:          "'{' after with",
		`r with {a: 1, a: 2}`:  `duplicate record field "a"`,
		`r with {"a": 1}`:      "record fields are written as name: value",
		`r with {case: 1}`:     `invalid field name "case"`,
		`r with {with: 1}`:     `invalid field name "with"`,
		`let(with = 1, with)`:  "",
		`{...r, a: 1}`:         "",
		`using(..., a / b, x)`: "",
	} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			if _, err := Parse(source); err == nil || !strings.Contains(err.Error(), want) {
				t.Errorf("Parse(%q) error = %v, want %q", source, err, want)
			}
		})
	}
	document := `{"version":1,"expr":{"node":"record_update","base":{"node":"var","name":"r"},"fields":[]}}`
	if _, err := ImportExprJSON([]byte(document)); err == nil {
		t.Fatal("an update that changes nothing imported from ExprJSON")
	}
}

// A reserved word as a field name is reported where it is written, in an
// update and after a record literal's first field alike.
func TestReservedFieldNamesPointAtTheName(t *testing.T) {
	t.Parallel()
	for source, at := range map[string]int{`r with {for: 1}`: 8, `{a: 1, for: 2}`: 7} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			_, err := Parse(source)
			var positioned *PosError
			if !errors.As(err, &positioned) || positioned.start != at {
				t.Errorf("Parse(%q) error = %v, want it at %d", source, err, at)
			}
		})
	}
}

// A dictionary comprehension takes one clause: splicing dictionaries would
// have to answer what a repeated key means, and nesting a list comprehension
// inside the value says the same thing without that question.
func TestDictionaryComprehensionTakesOneClause(t *testing.T) {
	t.Parallel()
	_, err := Parse(`{k: v for k, v in rates for x in xs}`)
	if err == nil {
		t.Fatal("a dictionary comprehension accepted two for clauses")
	}
	if !strings.Contains(err.Error(), "one 'for' clause") {
		t.Fatalf("error = %v, want one saying one 'for' clause", err)
	}
}

// A field shaped like a code is a field, first or not: {USD: 1} is a record.
func TestARecordFieldMayBeShapedLikeACode(t *testing.T) {
	t.Parallel()
	for _, source := range []string{"{USD: 1}", "{a: 1, USD: 2}", "{USD: 1}.USD"} {
		if _, err := Parse(source); err != nil {
			t.Errorf("Parse(%q) error = %v, want a record", source, err)
		}
	}
}

// if names no variable, so a brace that starts with it and a colon is a
// record; nowhere else can if be read as a name.
func TestIfIsAFieldAndNoVariable(t *testing.T) {
	t.Parallel()
	for _, source := range []string{"{if: 1}.if", "{a: 1, if: 2}", "order.if", "[x for x in xs if x > 1]", "if(a, b, c)"} {
		if _, err := Parse(source); err != nil {
			t.Errorf("Parse(%q) error = %v, want it read", source, err)
		}
	}
	for _, source := range []string{"if + 1", "let(if = 1, if)", "[if for if in xs]", "reduce(if in xs, acc = 0, acc)"} {
		if _, err := Parse(source); err == nil {
			t.Errorf("Parse(%q) = nil error, want if refused as a variable", source)
		}
	}
}
