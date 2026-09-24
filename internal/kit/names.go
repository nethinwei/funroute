package kit

import "strings"

// IsDigit reports an ASCII decimal digit.
func IsDigit(c byte) bool { return c >= '0' && c <= '9' }

// IsDigits reports text made of decimal digits only; the empty text is.
func IsDigits(text string) bool { return strings.Trim(text, "0123456789") == "" }

// IsNameStart reports a byte a name may start with: a letter or _.
func IsNameStart(c byte) bool { return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') }

// IsNameChar reports a byte a name may go on with: a letter, a digit or _.
func IsNameChar(c byte) bool { return IsNameStart(c) || IsDigit(c) }
