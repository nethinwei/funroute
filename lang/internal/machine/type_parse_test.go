package machine

import (
	"strings"
	"testing"
)

// A unit-carrying type always writes its units: a bare money, currency or
// fxrate is refused with the spelling to use, and ? reads and prints back as
// a currency only the run knows.
func TestUnitsAreAlwaysWritten(t *testing.T) {
	t.Parallel()
	for text, want := range map[string]string{
		"money":               "write money<?>",
		"currency":            "write currency<?>",
		"fxrate":              "write fxrate<?,?>",
		"array<money>":        "write money<?>",
		"record{fee: fxrate}": "write fxrate<?,?>",
	} {
		t.Run(text, func(t *testing.T) {
			t.Parallel()
			if _, err := ParseType(text); err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("ParseType(%q) error = %v, want one containing %q", text, err, want)
			}
		})
	}
	for text, name := range map[string]string{
		"money<?>":            "",
		"currency<?>":         "",
		"money<USD>":          "USD",
		"array<money<?>>":     "",
		"fxrate<?,?>":         "",
		"fxrate<?,USD>":       "",
		"dict<currency<c>>":   "",
		"record{a: money<?>}": "",
	} {
		t.Run(text, func(t *testing.T) {
			t.Parallel()
			typ, err := ParseType(text)
			if err != nil || typ.String() != text {
				t.Fatalf("ParseType(%q) = %v, %v, want it printed back as written", text, typ, err)
			}
			if typ.Kind() == MoneyKind && typ.Name() != name {
				t.Fatalf("ParseType(%q) unit = %q, want %q", text, typ.Name(), name)
			}
		})
	}
}
