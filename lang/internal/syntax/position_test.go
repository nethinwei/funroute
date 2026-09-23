package syntax

import (
	"errors"
	"testing"
)

// A syntax error covers the token it is about; a lexical one covers what the
// lexer could not read.
func TestSyntaxErrorsCoverTheirToken(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		`f(1,, 2)`:     ",",
		`a ¥ b`:        "¥",
		`x + "open`:    `"open`,
		`let(x = 1)`:   ")",
		`[1, 2`:        "",
		`switch(x, 1)`: "switch",
	}
	for source, want := range cases {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			_, err := Parse(source)
			var positioned *PosError
			if !errors.As(err, &positioned) {
				t.Fatalf("Parse(%q) error = %v, want a *PosError", source, err)
			}
			if got := source[positioned.Start:positioned.End]; got != want {
				t.Errorf("%q: the error covers %q, want %q (%v)", source, got, want, err)
			}
		})
	}
}
