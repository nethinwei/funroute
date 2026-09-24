package money

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/nethinwei/funroute/internal/kit"
)

// CurrencySpec is one currency a console accepts: its code and how many
// decimal places its minor unit has (USD 2, JPY 0, KWD 3). The places are
// the host's to state because channels disagree with ISO 4217.
type CurrencySpec struct {
	Code   string `json:"code"`
	Digits int    `json:"digits"`
}

// MoneySpec is what DeclareMoney takes: the currencies a console accepts.
// There is no default rounding: a rule writes every rounding it does.
type MoneySpec struct {
	Currencies []CurrencySpec `json:"currencies"`
}

// Codes is the declared currency codes, in the spec's order.
func (spec MoneySpec) Codes() []string {
	return kit.Map(spec.Currencies, func(currency CurrencySpec) string { return currency.Code })
}

// maxCurrencyDigits is as many places as any currency in use has, a
// bitcoin's satoshi included, and keeps the factor between two currencies'
// places small enough that most conversions stay in int64 arithmetic.
const maxCurrencyDigits = 8

// Currencies is a declared currency table: the currencies a console accepts
// and their places, and the operations on money that need them — reading and
// writing "USD 1.70", converting through an exchange rate. A registry's is
// the one its rules use; NewCurrencies builds one for a service that runs no
// rules but must agree with those that do.
type Currencies struct {
	spec   MoneySpec
	digits map[string]int
	codes  []string
	// identity is a digest of the currencies and their places: two tables
	// with one identity hold the same amounts.
	identity string
	// pairs holds one Pair per pair of declared currencies asked for.
	pairsMu sync.RWMutex
	pairs   map[[2]string]*Pair
}

// NewCurrencies builds a table from a spec, checking every currency.
func NewCurrencies(spec MoneySpec) (*Currencies, error) {
	if len(spec.Currencies) == 0 {
		return nil, errors.New("money: declare at least one currency")
	}
	table := &Currencies{digits: map[string]int{}, pairs: map[[2]string]*Pair{}}
	for _, currency := range spec.Currencies {
		if err := table.add(currency); err != nil {
			return nil, err
		}
	}
	slices.Sort(table.codes)
	for _, code := range table.codes {
		table.spec.Currencies = append(table.spec.Currencies, CurrencySpec{Code: code, Digits: table.digits[code]})
	}
	encoded, err := json.Marshal(table.spec.Currencies)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(encoded)
	table.identity = "sha256:" + hex.EncodeToString(sum[:])
	return table, nil
}

func (c *Currencies) add(currency CurrencySpec) error {
	if !IsCurrencyCode(currency.Code) {
		return fmt.Errorf("money: invalid currency code %q", currency.Code)
	}
	if currency.Digits < 0 || currency.Digits > maxCurrencyDigits {
		return fmt.Errorf("money: %s has %d decimal places, want 0 to %d", currency.Code, currency.Digits, maxCurrencyDigits)
	}
	if _, exists := c.digits[currency.Code]; exists {
		return fmt.Errorf("money: currency %s is declared twice", currency.Code)
	}
	c.digits[currency.Code] = currency.Digits
	c.codes = append(c.codes, currency.Code)
	return nil
}

// Identity is a digest of the table's currencies and their places: two
// tables with one identity hold the same amounts.
func Identity(c *Currencies) string { return c.identity }

// Spec is the table as DeclareMoney takes it, currencies sorted by code.
func (c *Currencies) Spec() MoneySpec {
	spec := c.spec
	spec.Currencies = slices.Clone(spec.Currencies)
	return spec
}

// Places is how many decimal places a declared currency has.
func (c *Currencies) Places(code string) (int, error) {
	digits, ok := c.digits[code]
	if !ok {
		return 0, fmt.Errorf("%w: currency %q is not declared", ErrCurrency, code)
	}
	return digits, nil
}

// Parse reads "USD 1.70" exactly; more places than the currency has is an
// error, not a rounding. The sign goes on the figure, USD -1.70, as in a
// rule's literal.
func (c *Currencies) Parse(text string) (Money, error) {
	code, amount, ok := strings.Cut(strings.TrimSpace(text), " ")
	if !ok {
		return Money{}, fmt.Errorf("money %q: want a currency and an amount, like \"USD 1.70\"", text)
	}
	return c.Of(code, strings.TrimSpace(amount))
}

// Of reads an amount written in a currency's major unit: Of("USD", "1.70").
func (c *Currencies) Of(code, amount string) (Money, error) {
	digits, err := c.Places(code)
	if err != nil {
		return Money{}, err
	}
	minor, err := ParseDecimal(amount, digits)
	if err != nil {
		return Money{}, fmt.Errorf("%s %s: %w", code, amount, err)
	}
	return newMoney(code, minor), nil
}

// Minor is an amount given in its currency's minor units: Minor("USD", 170)
// is USD 1.70. The currency must be declared.
func (c *Currencies) Minor(code string, minor int64) (Money, error) {
	if _, err := c.Places(code); err != nil {
		return Money{}, err
	}
	return newMoney(code, minor), nil
}

// Currency is a declared currency as a value.
func (c *Currencies) Currency(code string) (Currency, error) {
	if _, err := c.Places(code); err != nil {
		return Currency{}, err
	}
	return Currency{code: code}, nil
}

// Format writes money as "USD 1.70"; a currency-less zero is "0". Money in a
// currency the table does not have, or not zero without a currency, is an
// ErrCurrency: there are no places to write it in that Parse would read back.
func (c *Currencies) Format(m Money) (string, error) {
	if m.currency == "" {
		return FormatDecimal(m.minor, 0, false), m.wellFormed()
	}
	digits, err := c.Places(m.currency)
	if err != nil {
		return "", err
	}
	return m.currency + " " + FormatDecimal(m.minor, digits, false), nil
}

// pow10 is 10^n for n from 0 to 18.
func pow10(n int) int64 { return powersOfTen[n] }

var powersOfTen = [19]int64{1, 1e1, 1e2, 1e3, 1e4, 1e5, 1e6, 1e7, 1e8, 1e9, 1e10, 1e11, 1e12, 1e13, 1e14, 1e15, 1e16, 1e17, 1e18}

// IsCurrencyCode reports whether name has the shape of a currency code: an
// upper-case letter, then two to seven upper-case letters or digits. It is
// the one rule; the type parser, the source parser and the registry use it.
func IsCurrencyCode(name string) bool {
	if len(name) < 3 || len(name) > 8 || name[0] < 'A' || name[0] > 'Z' {
		return false
	}
	for i := 1; i < len(name); i++ {
		if (name[i] < 'A' || name[i] > 'Z') && !kit.IsDigit(name[i]) {
			return false
		}
	}
	return true
}
