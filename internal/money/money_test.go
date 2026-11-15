package money

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"testing"
)

// A currency's JSON is its code, and reads back to it; the zero Currency has
// no code, so it writes none rather than "", which would not read back.
func TestACurrencysJSONReadsBack(t *testing.T) {
	t.Parallel()
	encoded, err := json.Marshal(CurrencyOf("USD"))
	if err != nil || string(encoded) != `"USD"` {
		t.Fatalf("json.Marshal(USD) = %s, %v, want \"USD\"", encoded, err)
	}
	var back Currency
	if err := json.Unmarshal(encoded, &back); err != nil || back.Code() != "USD" {
		t.Fatalf("json.Unmarshal(%s) = %v, %v, want USD", encoded, back, err)
	}
	if encoded, err := json.Marshal(Currency{}); !errors.Is(err, ErrCurrency) {
		t.Errorf("json.Marshal(Currency{}) = %s, %v, want ErrCurrency", encoded, err)
	}
}

// testDigits is the places the tests' usual tables declare.
var testDigits = map[string]int{"USD": 2, "EUR": 2, "GBP": 2, "JPY": 0, "KWD": 3, "BTC": 8, "VND": 0, "ETH": 8}

// amount is minor units of code in its usual places, as a table of the
// tests' would make it; a code the tests give no places has none.
func amount(code string, minor int64) Money {
	m := newMoney(code, minor)
	if digits, ok := testDigits[code]; ok {
		m.places = placesOf(digits)
	}
	return m
}

// Amounts carry their places through arithmetic, and two places for one
// currency do not combine.
func TestAmountsCarryTheirPlaces(t *testing.T) {
	t.Parallel()
	sum, err := amount("USD", 170).Add(newMoney("USD", 30))
	if err != nil || sum != amount("USD", 200) {
		t.Fatalf("USD 1.70 + 30 minor = %v, %v, want USD 2.00 in two places", sum, err)
	}
	if _, err := amount("USD", 1).Add(Money{currency: "USD", minor: 1, places: placesOf(3)}); err == nil {
		t.Fatal("USD in two places and in three added")
	}
	if got, _ := (Money{}).Add(amount("JPY", 5)); got != amount("JPY", 5) {
		t.Fatalf("0 + JPY 5 = %#v", got)
	}
}

// Equal is one amount whether its places are known or not; == is not.
func TestEqualIsOneAmount(t *testing.T) {
	t.Parallel()
	known, unknown := amount("USD", 170), newMoney("USD", 170)
	if known == unknown || !known.Equal(unknown) || !unknown.Equal(known) {
		t.Fatal("USD 1.70 with and without places: want != and Equal")
	}
	inThree := Money{currency: "USD", minor: 170, places: placesOf(3)}
	for name, other := range map[string]Money{
		"another currency": amount("EUR", 170),
		"another amount":   amount("USD", 171),
		"other places":     inThree,
	} {
		if known.Equal(other) {
			t.Errorf("%s: USD 1.70 Equal %#v", name, other)
		}
	}
	if !(Money{}).Equal(amount("JPY", 0)) || (Money{}).Equal(amount("JPY", 1)) {
		t.Error("the currency-less zero is the zero of every currency, and no other amount")
	}
}

// An amount is written in the places it carries, whatever else the process
// declares; one without places is written in minor units, never in guessed
// places.
func TestStringUsesTheAmountsOwnPlaces(t *testing.T) {
	t.Parallel()
	table, err := NewCurrencies(MoneySpec{Currencies: []CurrencySpec{{Code: "SAR", Digits: 2}, {Code: "HUF", Digits: 0}, {Code: "BHD", Digits: 3}}})
	if err != nil {
		t.Fatal(err)
	}
	of := func(code string, minor int64) Money {
		m, err := table.Minor(code, minor)
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
	for _, tc := range []struct {
		money Money
		want  string
	}{
		{of("SAR", 212), "SAR 2.12"},
		{of("SAR", -5), "SAR -0.05"},
		{of("HUF", 1600), "HUF 1600"},
		{of("BHD", 1234), "BHD 1.234"},
		{newMoney("SAR", 212), "SAR 212 minor"},
		{Money{}, "0"},
	} {
		if got := tc.money.String(); got != tc.want {
			t.Errorf("String() = %q, want %q", got, tc.want)
		}
	}
}

// The places go through JSON: an amount reads back as it was written, and
// the shape without "digits" — what an amount without places writes, and
// what amounts were written as before they carried places — reads back as
// an amount without places.
func TestJSONCarriesThePlaces(t *testing.T) {
	t.Parallel()
	in := amount("JPY", 1600)
	encoded, err := json.Marshal(in)
	if err != nil || string(encoded) != `{"currency":"JPY","minor":1600,"digits":0}` {
		t.Fatalf("json.Marshal = %s, %v", encoded, err)
	}
	var back Money
	if err := json.Unmarshal(encoded, &back); err != nil || back != in {
		t.Fatalf("read back %s = %#v, %v", encoded, back, err)
	}
	var old Money
	if err := json.Unmarshal([]byte(`{"currency":"JPY","minor":1600}`), &old); err != nil {
		t.Fatal(err)
	}
	if _, ok := old.Digits(); ok || old.String() != "JPY 1600 minor" {
		t.Fatalf("old shape = %#v", old)
	}
	if encoded, _ := json.Marshal(old); string(encoded) != `{"currency":"JPY","minor":1600}` {
		t.Fatalf("amount without places writes %s", encoded)
	}
	for _, bad := range []string{
		`{"currency":"JPY","minor":1,"digits":-1}`,
		`{"currency":"JPY","minor":1,"digits":19}`,
		`{"currency":"","minor":0,"digits":2}`,
	} {
		if err := json.Unmarshal([]byte(bad), &old); err == nil {
			t.Errorf("%s read", bad)
		}
	}
}

// slog writes Money and FxRate as their text, in JSON and text logs alike,
// and %v uses String too.
func TestLogsWriteMoneyAsText(t *testing.T) {
	t.Parallel()
	fee := amount("USD", 212)
	rate, err := ReadFxRate("USD", "JPY", "150.5")
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	slog.New(slog.NewJSONHandler(&out, nil)).Info("settled", "fee", fee, "rate", rate)
	var line map[string]any
	if err := json.Unmarshal(out.Bytes(), &line); err != nil {
		t.Fatal(err)
	}
	if line["fee"] != "USD 2.12" || line["rate"] != rate.String() {
		t.Errorf("JSON log = %s", out.Bytes())
	}
	out.Reset()
	slog.New(slog.NewTextHandler(&out, nil)).Info("settled", "fee", fee)
	if !bytes.Contains(out.Bytes(), []byte(`fee="USD 2.12"`)) {
		t.Errorf("text log = %s", out.Bytes())
	}
	if got := fmt.Sprintf("%v", fee); got != "USD 2.12" {
		t.Errorf("%%v = %q", got)
	}
}
