package money

import (
	"encoding/json"
	"errors"
	"fmt"
)

// Money is an amount in a currency's minor unit: USD 1.70 is 170 minor
// units of USD. Its fields are its own: a host gets one from a currency
// table (Currencies.Parse, Of, Minor), from a rule's result, or as the zero
// value — the currency-less zero, the one amount with no currency. Whatever
// a host holds is therefore well formed, and []Money is an array<money>'s
// backing, handed across the boundary without a copy.
type Money struct {
	currency string
	minor    int64
}

// newMoney is the package's own constructor; everything outside builds money
// through a currency table, which checks it, or through Make.
func newMoney(currency string, minor int64) Money { return Money{currency: currency, minor: minor} }

// Currency is the amount's currency code, "" for the currency-less zero.
func (m Money) Currency() string { return m.currency }

// Minor is the amount in its currency's minor unit: 170 for USD 1.70.
func (m Money) Minor() int64 { return m.minor }

// Currency is a currency code as a value of its own, the Go form of
// currency. A host gets one from a currency table (Currencies.currency); the
// zero value is no currency.
type Currency struct{ code string }

// Code is the currency's code: "USD".
func (c Currency) Code() string { return c.code }

func (c Currency) String() string { return c.code }

// A value's JSON is the shape it has always had — {"currency", "minor"},
// {"base", "quote", "rate"}, "USD" — and reading one back checks what the
// JSON alone can say: a currency code's shape, an amount without a currency
// only zero, a rate positive and 1 from a currency to itself. Whether a
// currency is declared is a table's to say, where the value meets one.

func (m Money) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Currency string `json:"currency"`
		Minor    int64  `json:"minor"`
	}{m.currency, m.minor})
}

func (m *Money) UnmarshalJSON(data []byte) error {
	var shape struct {
		Currency *string `json:"currency"`
		Minor    *int64  `json:"minor"`
	}
	if err := json.Unmarshal(data, &shape); err != nil {
		return err
	}
	if shape.Currency == nil || shape.Minor == nil {
		return errors.New("money needs a \"currency\" and a \"minor\"")
	}
	parsed := newMoney(*shape.Currency, *shape.Minor)
	if err := parsed.wellFormed(); err != nil {
		return err
	}
	*m = parsed
	return nil
}

func (m Money) wellFormed() error {
	switch {
	case m.currency == "" && m.minor != 0:
		return fmt.Errorf("%w: %d minor units with no currency", ErrCurrency, m.minor)
	case m.currency != "" && !IsCurrencyCode(m.currency):
		return fmt.Errorf("%w: %q is not a currency code", ErrCurrency, m.currency)
	}
	return nil
}

func (c Currency) MarshalJSON() ([]byte, error) { return json.Marshal(c.code) }

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
