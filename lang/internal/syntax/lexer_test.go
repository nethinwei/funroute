package syntax

import (
	"strings"
	"testing"
)

// A string literal holds text, and text is UTF-8: a byte that is not would be
// replaced on the way through ExprJSON, and the program would not come back.
func TestStringsAreUTF8(t *testing.T) {
	t.Parallel()
	for _, source := range []string{"\"\\200\"", "\"\x80\"", "concat(\"a\", \"\xff\")"} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			if _, err := Parse(source); err == nil || !strings.Contains(err.Error(), "not valid UTF-8") {
				t.Errorf("Parse(%q) error = %v, want a UTF-8 error", source, err)
			}
		})
	}
	if _, err := Parse(`"支付 ✓"`); err != nil {
		t.Errorf("Parse of UTF-8 text error = %v, want none", err)
	}
}

// Whitespace is ASCII whitespace. A lone byte is not a rune, so 0x85 or 0xA0
// on its own is a character the lexer cannot read, not a space.
func TestOnlyASCIIWhitespaceSeparates(t *testing.T) {
	t.Parallel()
	for _, source := range []string{"a \x85 b", "a\xa0+ b"} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			if _, err := Parse(source); err == nil || !strings.Contains(err.Error(), "unexpected") {
				t.Errorf("Parse(%q) error = %v, want an unexpected character", source, err)
			}
		})
	}
	if _, err := Parse("a +\t\r\n\v\fb"); err != nil {
		t.Errorf("Parse with ASCII whitespace error = %v, want none", err)
	}
}
