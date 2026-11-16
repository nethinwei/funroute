package money

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
)

// Money is an amount in a currency's minor unit: USD 1.70 is 170 minor
// units of USD. Its fields are its own: a host gets one from a currency
// table (Currencies.Parse, Of, Minor), from a rule's result, or as the zero
// value — the currency-less zero, the one amount with no currency. Whatever
// a host holds is therefore well formed, and []Money is an array<money>'s
// backing, handed across the boundary without a copy.
//
// An amount carries its currency's decimal places, as the table that made it
// declares them: 170 minor units are USD 1.70 only where USD has two places,
// and two tables may say otherwise. The places travel with the amount — in
// its JSON, through arithmetic, into a rule and out — so wherever it goes it
// reads one way. An amount read from JSON written without them has none
// (Digits reports false) until it meets a table or an amount that has them.
//
// Compare amounts with Equal, not ==. As with time.Time, == compares every
// field: an amount without places and the same amount with them are two
// values to ==, and one amount to Equal.
type Money struct {
	currency string
	minor    int64
	places   uint8 // decimal places + 1; 0 is not known
}

// newMoney is the package's own constructor; everything outside builds money
// through a currency table, which checks it, or through Make.
func newMoney(currency string, minor int64) Money { return Money{currency: currency, minor: minor} }

// placesOf is digits as Money holds them.
func placesOf(digits int) uint8 { return uint8(digits + 1) }

// Digits is how many decimal places the amount's currency has, as the table
// that made it declares them; ok is false when the amount does not say — the
// currency-less zero, or one read from JSON without them.
func (m Money) Digits() (digits int, ok bool) { return int(m.places) - 1, m.places != 0 }

// of is minor units in m's currency and places.
func (m Money) of(minor int64) Money {
	m.minor = minor
	return m
}

// Currency is the amount's currency code, "" for the currency-less zero.
func (m Money) Currency() string { return m.currency }

// Minor is the amount in its currency's minor unit: 170 for USD 1.70.
func (m Money) Minor() int64 { return m.minor }

// Decimal writes the amount without its currency, keeping the places it
// carries: "1.70" for USD 1.70, "1600" for JPY 1600. It is exact, with no
// rounding; the currency-less zero is "0". An amount without known places
// returns ErrCurrency; use Currencies.Adopt to supply them first.
func (m Money) Decimal() (string, error) {
	if m.currency == "" {
		return "0", nil
	}
	digits, ok := m.Digits()
	if !ok {
		return "", fmt.Errorf("%w: %s has unknown decimal places; use Currencies.Adopt first", ErrCurrency, m.currency)
	}
	return FormatDecimal(m.minor, digits, false), nil
}

// String writes the amount as a person reads it, in the places it carries:
// "SAR 2.12", "JPY 1600". An amount that does not carry its places is written
// in minor units, "SAR 212 minor", rather than in places guessed; the
// currency-less zero is "0". It reads the amount alone, so an amount reads
// one way in every process. Reading and writing amounts that a program parses
// back goes through a table (Currencies.Format, Parse).
func (m Money) String() string {
	text, err := m.Decimal()
	if m.currency == "" {
		return text
	}
	if err != nil {
		return m.currency + " " + strconv.FormatInt(m.minor, 10) + " minor"
	}
	return m.currency + " " + text
}

// LogValue makes slog write String instead of the JSON shape: slog resolves
// a LogValuer before any handler, so text and JSON logs both read "SAR 2.12".
func (m Money) LogValue() slog.Value { return slog.StringValue(m.String()) }

// Currency is a currency code as a value of its own, the Go form of
// currency. A host gets one from a currency table (Currencies.currency); the
// zero value is no currency.
type Currency struct{ code string }

// String is the currency's code, such as "USD", or "" for the zero Currency.
func (c Currency) String() string { return c.code }

// Code is the currency's code.
//
// Deprecated: use String instead.
func (c Currency) Code() string { return c.String() }

// A value's JSON — {"currency", "minor", "digits"}, {"base", "quote",
// "rate"}, "USD" — and reading one back checks what the JSON alone can say:
// a currency code's shape, an amount without a currency only zero and
// without places, places a table could declare, a rate positive and 1 from a
// currency to itself. Whether a currency is declared is a table's to say,
// where the value meets one. "digits" is left out where the amount has no
// places, so such an amount writes the shape it had before amounts carried
// them, and that shape reads back as an amount without places.

func (m Money) MarshalJSON() ([]byte, error) {
	var digits *int
	if d, ok := m.Digits(); ok {
		digits = &d
	}
	return json.Marshal(struct {
		Currency string `json:"currency"`
		Minor    int64  `json:"minor"`
		Digits   *int   `json:"digits,omitempty"`
	}{m.currency, m.minor, digits})
}

func (m *Money) UnmarshalJSON(data []byte) error {
	var shape struct {
		Currency *string `json:"currency"`
		Minor    *int64  `json:"minor"`
		Digits   *int    `json:"digits"`
	}
	if err := json.Unmarshal(data, &shape); err != nil {
		return err
	}
	if shape.Currency == nil || shape.Minor == nil {
		return errors.New("money needs a \"currency\" and a \"minor\"")
	}
	parsed, err := fromJSON(*shape.Currency, *shape.Minor, shape.Digits)
	if err != nil {
		return err
	}
	*m = parsed
	return nil
}

// fromJSON is an amount from what its JSON says — a code, minor units and,
// when written, the places — held to what an amount is held to: a code, or
// none for a zero, and places a table could declare. Money's JSON and
// ExactMoney's read through it.
func fromJSON(code string, minor int64, digits *int) (Money, error) {
	m := newMoney(code, minor)
	if digits != nil {
		places, err := PlacesOf(code, int64(*digits))
		if err != nil {
			return Money{}, err
		}
		m.places = places
	}
	if err := m.wellFormed(); err != nil {
		return Money{}, err
	}
	return m, nil
}

// checkDigits refuses places no table could declare.
func checkDigits(code string, digits int64) error {
	if digits < 0 || digits > maxCurrencyDigits {
		return fmt.Errorf("money: %s has %d decimal places, want 0 to %d", code, digits, maxCurrencyDigits)
	}
	return nil
}

func (m Money) wellFormed() error {
	switch {
	case m.currency == "" && m.minor != 0:
		return fmt.Errorf("%w: %d minor units with no currency", ErrCurrency, m.minor)
	case m.currency == "" && m.places != 0:
		return fmt.Errorf("%w: decimal places with no currency", ErrCurrency)
	case m.currency != "" && !IsCurrencyCode(m.currency):
		return fmt.Errorf("%w: %q is not a currency code", ErrCurrency, m.currency)
	}
	return nil
}

// MarshalJSON writes the code. The zero Currency has none to write: "" would
// not read back, so it is an ErrCurrency, as the zero FxRate's is.
func (c Currency) MarshalJSON() ([]byte, error) {
	if c.code == "" {
		return nil, fmt.Errorf("%w: the zero Currency is no currency; take one from a currency table", ErrCurrency)
	}
	return json.Marshal(c.code)
}

func (c *Currency) UnmarshalJSON(data []byte) error {
	var code string
	if err := json.Unmarshal(data, &code); err != nil {
		return err
	}
	if !IsCurrencyCode(code) {
		return fmt.Errorf("%w: %q is not a currency code", ErrCurrency, code)
	}
	c.code = code
	return nil
}
