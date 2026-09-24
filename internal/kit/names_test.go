package kit

import "testing"

// A name starts with a letter or _ and goes on with digits too; digits are
// ASCII only.
func TestNameAndDigitBytes(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		c                  byte
		start, char, digit bool
	}{
		{'a', true, true, false}, {'Z', true, true, false}, {'_', true, true, false},
		{'7', false, true, true}, {'.', false, false, false}, {0xC3, false, false, false},
	} {
		if IsNameStart(test.c) != test.start || IsNameChar(test.c) != test.char || IsDigit(test.c) != test.digit {
			t.Errorf("%q: IsNameStart, IsNameChar, IsDigit = %v, %v, %v, want %v, %v, %v",
				test.c, IsNameStart(test.c), IsNameChar(test.c), IsDigit(test.c), test.start, test.char, test.digit)
		}
	}
	if !IsDigits("") || !IsDigits("0123") || IsDigits("1.5") {
		t.Errorf(`IsDigits: "" and "0123" are digits, "1.5" is not`)
	}
}
