package std_test

import (
	"fmt"
	"slices"
	"testing"

	"github.com/nethinwei/funroute"
	"github.com/nethinwei/funroute/extensions/std"
)

// Every entry is a three-letter ISO code with 0 to 8 places — the range
// DeclareMoney accepts — and no code appears twice.
func TestISO4217EntriesAreWellFormed(t *testing.T) {
	t.Parallel()
	seen := map[string]bool{}
	for _, currency := range std.ISO4217() {
		if seen[currency.Code] {
			t.Errorf("%s appears twice", currency.Code)
		}
		seen[currency.Code] = true
		t.Run(currency.Code, func(t *testing.T) {
			t.Parallel()
			if !isISOCode(currency.Code) {
				t.Errorf("code %q is not three upper-case letters", currency.Code)
			}
			if currency.Digits < 0 || currency.Digits > 8 {
				t.Errorf("%s has %d decimal places, want 0 to 8", currency.Code, currency.Digits)
			}
		})
	}
}

func isISOCode(code string) bool {
	if len(code) != 3 {
		return false
	}
	for _, letter := range []byte(code) {
		if letter < 'A' || letter > 'Z' {
			return false
		}
	}
	return true
}

// The table declares as it is, and a declared registry reads every amount in
// the places the table gave.
func TestISO4217IsAcceptedByDeclareMoney(t *testing.T) {
	t.Parallel()
	table := std.ISO4217()
	registry := funroute.CoreRegistry()
	if err := registry.DeclareMoney(funroute.MoneySpec{Currencies: table}); err != nil {
		t.Fatalf("DeclareMoney(ISO4217()): %v", err)
	}
	spec, declared := registry.Money()
	if !declared || len(spec.Currencies) != len(table) {
		t.Fatalf("declared %d currencies (%v), want %d", len(spec.Currencies), declared, len(table))
	}
	currencies, err := funroute.NewCurrencies(funroute.MoneySpec{Currencies: table})
	if err != nil {
		t.Fatal(err)
	}
	for _, currency := range table {
		t.Run(currency.Code, func(t *testing.T) {
			t.Parallel()
			checkOneUnit(t, currencies, currency)
		})
	}
}

// checkOneUnit reads "<code> 1" and expects 10^digits minor units.
func checkOneUnit(t *testing.T, currencies *funroute.Currencies, currency funroute.CurrencySpec) {
	t.Helper()
	want := int64(1)
	for range currency.Digits {
		want *= 10
	}
	got, err := currencies.Parse(currency.Code + " 1")
	if err != nil || got.Currency() != currency.Code || got.Minor() != want {
		t.Errorf("Parse(%q) = %+v, %v; want %d minor units", currency.Code+" 1", got, err, want)
	}
}

// The currencies a payment console meets most, with ISO 4217's places.
func TestISO4217KnowsTheCommonCurrencies(t *testing.T) {
	t.Parallel()
	places := map[string]int{}
	for _, currency := range std.ISO4217() {
		places[currency.Code] = currency.Digits
	}
	for code, want := range map[string]int{
		"USD": 2, "EUR": 2, "GBP": 2, "CNY": 2, "HKD": 2, "SGD": 2, "INR": 2, "BRL": 2,
		"JPY": 0, "KRW": 0, "CLP": 0, "ISK": 0, "VND": 0, "PYG": 0, "XOF": 0, "XAF": 0,
		"KWD": 3, "BHD": 3, "OMR": 3, "JOD": 3, "TND": 3, "IQD": 3, "LYD": 3,
	} {
		t.Run(code, func(t *testing.T) {
			t.Parallel()
			got, ok := places[code]
			if !ok || got != want {
				t.Fatalf("ISO4217()[%s] = %d (present %v), want %d places", code, got, ok, want)
			}
		})
	}
}

// Funds, precious metals, testing and no-currency codes are left out.
func TestISO4217LeavesOutFundsAndMetals(t *testing.T) {
	t.Parallel()
	codes := map[string]bool{}
	for _, currency := range std.ISO4217() {
		codes[currency.Code] = true
	}
	for _, code := range []string{"XAU", "XAG", "XPT", "XPD", "XDR", "XTS", "XXX", "CLF", "USN", "BOV", "COU", "UYI", "UYW", "CHE", "CHW", "MXV", "XSU", "XUA"} {
		t.Run(code, func(t *testing.T) {
			t.Parallel()
			if codes[code] {
				t.Fatalf("ISO4217() contains %s, want funds and metals left out", code)
			}
		})
	}
}

// Each call hands out a fresh table: what a host changes in one is not in the
// next.
func TestISO4217ReturnsACopy(t *testing.T) {
	t.Parallel()
	before := canonical(std.ISO4217())
	edited := std.ISO4217()
	for i := range edited {
		edited[i].Digits = 7
		edited[i].Code = "ZZZ"
	}
	if after := canonical(std.ISO4217()); !slices.Equal(before, after) {
		t.Fatalf("ISO4217() after editing a returned table = %v, want %v", after, before)
	}
}

// canonical is a table as sorted text, since the order is not part of it.
func canonical(table []funroute.CurrencySpec) []string {
	out := make([]string, len(table))
	for i, currency := range table {
		out[i] = fmt.Sprintf("%s:%d", currency.Code, currency.Digits)
	}
	slices.Sort(out)
	return out
}
