package machine

import (
	"slices"
)

// Currencies is a declared currency table for Go: the operations on money
// that need a currency's decimal places — reading and writing "USD 1.70",
// converting through an exchange rate. A registry's is the one its rules
// use (Registry.Currencies); NewCurrencies builds one for a service that runs
// no rules but must agree with those that do.
type Currencies struct{ table *currencyTable }

// NewCurrencies builds a table from the same spec DeclareMoney takes.
func NewCurrencies(spec MoneySpec) (*Currencies, error) {
	table, err := newCurrencyTable(spec)
	if err != nil {
		return nil, err
	}
	return &Currencies{table: table}, nil
}

// Currencies is the table the registry declared, if it declared one.
func (r *Registry) Currencies() (*Currencies, bool) {
	table := r.currencies()
	if table == nil {
		return nil, false
	}
	return &Currencies{table: table}, true
}

// Spec is the table as DeclareMoney takes it, currencies sorted by code.
func (c *Currencies) Spec() MoneySpec {
	spec := c.table.spec
	spec.Currencies = slices.Clone(spec.Currencies)
	return spec
}

// Rounding is the table's default rounding.
func (c *Currencies) Rounding() Rounding { return c.table.spec.Rounding }

// Places is how many decimal places a declared currency has.
func (c *Currencies) Places(code string) (int, error) { return c.table.places(code) }

// Parse reads "USD 1.70" exactly; more places than the currency has is an
// error, not a rounding.
func (c *Currencies) Parse(text string) (Money, error) {
	value, err := c.table.parseMoney(text)
	money, _ := value.Money()
	return money, err
}

// Of reads an amount written in a currency's major unit: Of("USD", "1.70").
func (c *Currencies) Of(code, amount string) (Money, error) {
	value, err := c.table.moneyOf(code, amount)
	money, _ := value.Money()
	return money, err
}

// Minor is an amount given in its currency's minor units: Minor("USD", 170)
// is USD 1.70. The currency must be declared.
func (c *Currencies) Minor(code string, minor int64) (Money, error) {
	if _, err := c.table.places(code); err != nil {
		return Money{}, err
	}
	return newMoney(code, minor), nil
}

// Currency is a declared currency as a value.
func (c *Currencies) Currency(code string) (Currency, error) {
	if _, err := c.table.places(code); err != nil {
		return Currency{}, err
	}
	return Currency{code: code}, nil
}

// Format writes money as "USD 1.70"; a currency-less zero is "0". Money in a
// currency the table does not have, or not zero without a currency, is an
// ErrCurrency: there are no places to write it in that Parse would read back.
func (c *Currencies) Format(m Money) (string, error) {
	return c.table.formatMoney(MoneyValue(m.minor, m.currency))
}

func pow10(n int) int64 {
	out := int64(1)
	for range n {
		out *= 10
	}
	return out
}
