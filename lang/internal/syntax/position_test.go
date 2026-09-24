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
			if got := source[positioned.start:positioned.end]; got != want {
				t.Errorf("%q: the error covers %q, want %q (%v)", source, got, want, err)
			}
		})
	}
}

// An error placed on a node keeps what it was, for errors.Is, and says
// where it happened.
func TestAroundErrorKeepsTheCause(t *testing.T) {
	t.Parallel()
	cause := errors.New("division by zero")
	expr, err := Parse("a + 1 / 0")
	if err != nil {
		t.Fatal(err)
	}
	placed := AroundError(expr, cause)
	positioned, ok := errors.AsType[*PosError](placed)
	if !errors.Is(placed, cause) || !ok || positioned.start != 0 || positioned.end != 9 || placed.Error() != cause.Error() {
		t.Fatalf("AroundError = %v (%+v), want the cause over the whole program", placed, positioned)
	}
}
