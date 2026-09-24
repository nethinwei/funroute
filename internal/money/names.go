package money

// IsCurrencyCode reports whether name has the shape of a currency code: an
// upper-case letter, then two to seven upper-case letters or digits. It is
// the one rule; the type parser, the source parser and the registry use it.
func IsCurrencyCode(name string) bool {
	if len(name) < 3 || len(name) > 8 || name[0] < 'A' || name[0] > 'Z' {
		return false
	}
	for i := 1; i < len(name); i++ {
		if (name[i] < 'A' || name[i] > 'Z') && (name[i] < '0' || name[i] > '9') {
			return false
		}
	}
	return true
}
