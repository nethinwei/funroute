package machine

import (
	"strings"
	"testing"
)

// money, currency and fxrate are whole types: a currency is the value's, so
// a type that tries to name one is refused with the spelling to use, and
// each reads and prints back as its bare name.
func TestTheMoneyTypesAreBare(t *testing.T) {
	t.Parallel()
	for _, text := range []string{"money", "currency", "fxrate", "array<money>", "dict<currency>", "record{a: money, b: fxrate}"} {
		t.Run(text, func(t *testing.T) {
			t.Parallel()
			typ, err := ParseType(text)
			if err != nil || typ.String() != text {
				t.Fatalf("ParseType(%q) = %v, %v, want it printed back as written", text, typ, err)
			}
		})
	}
	for text, want := range map[string]string{
		"money<USD>":            "the type is money",
		"money<?>":              "the type is money",
		"currency<?>":           "the type is currency",
		"fxrate<USD,JPY>":       "the type is fxrate",
		"array<money<C>>":       "the type is money",
		"record{fee: money<?>}": "the type is money",
	} {
		t.Run(text, func(t *testing.T) {
			t.Parallel()
			if _, err := ParseType(text); err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("ParseType(%q) error = %v, want one containing %q", text, err, want)
			}
		})
	}
}
