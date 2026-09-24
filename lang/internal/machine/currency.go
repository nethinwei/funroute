package machine

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// CurrencySpec is one currency a console accepts: its code and how many
// decimal places its minor unit has (USD 2, JPY 0, KWD 3). The places are
// the host's to state because channels disagree with ISO 4217.
type CurrencySpec struct {
	Code   string `json:"code"`
	Digits int    `json:"digits"`
}

// MoneySpec is what DeclareMoney takes: the currencies a console accepts and
// the rounding it applies when a product falls between two minor units.
type MoneySpec struct {
	Rounding   Rounding       `json:"rounding"`
	Currencies []CurrencySpec `json:"currencies"`
}

// maxCurrencyDigits is as many places as any currency in use has, a
// bitcoin's satoshi included, and keeps the factor between two currencies'
// places small enough that most conversions stay in int64 arithmetic.
const maxCurrencyDigits = 8

// currencyTable is a declared MoneySpec, indexed.
type currencyTable struct {
	spec   MoneySpec
	digits map[string]int
	codes  []string
	// identity is a digest of the currencies and their places — not the
	// rounding: two tables with one identity hold the same amounts, which is
	// what a rate table and the registry it converts for must agree on.
	identity string
}

func newCurrencyTable(spec MoneySpec) (*currencyTable, error) {
	if spec.Rounding.String() == "invalid" {
		return nil, fmt.Errorf("money: rounding is required")
	}
	if len(spec.Currencies) == 0 {
		return nil, fmt.Errorf("money: declare at least one currency")
	}
	table := &currencyTable{digits: map[string]int{}}
	for _, currency := range spec.Currencies {
		if err := table.add(currency); err != nil {
			return nil, err
		}
	}
	slices.Sort(table.codes)
	table.spec = MoneySpec{Rounding: spec.Rounding}
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

func (t *currencyTable) add(currency CurrencySpec) error {
	if !IsCurrencyCode(currency.Code) {
		return fmt.Errorf("money: invalid currency code %q", currency.Code)
	}
	if currency.Digits < 0 || currency.Digits > maxCurrencyDigits {
		return fmt.Errorf("money: %s has %d decimal places, want 0 to %d", currency.Code, currency.Digits, maxCurrencyDigits)
	}
	if _, exists := t.digits[currency.Code]; exists {
		return fmt.Errorf("money: currency %s is declared twice", currency.Code)
	}
	t.digits[currency.Code] = currency.Digits
	t.codes = append(t.codes, currency.Code)
	return nil
}

// places is a declared currency's number of decimal places.
func (t *currencyTable) places(code string) (int, error) {
	digits, ok := t.digits[code]
	if !ok {
		return 0, fmt.Errorf("%w: currency %q is not declared", ErrCurrency, code)
	}
	return digits, nil
}

// parseMoney reads "USD 1.70". The figure must fit the currency's places.
// The sign goes on the figure, USD -1.70, as in a rule's literal.
func (t *currencyTable) parseMoney(text string) (Value, error) {
	code, amount, ok := strings.Cut(strings.TrimSpace(text), " ")
	if !ok {
		return Value{}, fmt.Errorf("money %q: want a currency and an amount, like \"USD 1.70\"", text)
	}
	return t.moneyOf(code, strings.TrimSpace(amount))
}

// moneyOf reads an amount written in a currency's major unit.
func (t *currencyTable) moneyOf(code, amount string) (Value, error) {
	digits, err := t.places(code)
	if err != nil {
		return Value{}, err
	}
	minor, err := parseDecimal(amount, digits)
	if err != nil {
		return Value{}, fmt.Errorf("%s %s: %w", code, amount, err)
	}
	return MoneyValue(minor, code), nil
}

// formatMoney writes money back as "USD 1.70"; a currency-less zero is "0".
// A currency the table does not have has no places to be written in, and
// neither has an amount without a currency that is not zero.
func (t *currencyTable) formatMoney(value Value) (string, error) {
	if value.s == "" {
		return formatDecimal(value.i, 0, false), noCurrencyIsZero(value)
	}
	digits, err := t.places(value.s)
	if err != nil {
		return "", err
	}
	return value.s + " " + formatDecimal(value.i, digits, false), nil
}

// DeclareMoney turns the money feature on for a registry: the currencies it
// accepts, their decimal places, and the default rounding. Until it is
// called a registry admits no money type at all, and everything it compiles
// is what it was before the feature existed — digests included. It can be
// called once, before any function that takes or returns money is
// registered.
func (r *Registry) DeclareMoney(spec MoneySpec) error {
	table, err := newCurrencyTable(spec)
	if err != nil {
		return err
	}
	r.mu.Lock()
	if r.money != nil {
		r.mu.Unlock()
		return fmt.Errorf("money is already declared")
	}
	r.money = table
	r.mu.Unlock()
	return registerMoneyKernel(r, table)
}

// Money reports the declared money feature, if any.
func (r *Registry) Money() (MoneySpec, bool) {
	table := r.currencies()
	if table == nil {
		return MoneySpec{}, false
	}
	spec := table.spec
	spec.Currencies = slices.Clone(spec.Currencies)
	return spec, true
}

func (r *Registry) currencies() *currencyTable {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.money
}

// RoundingEnum is the enum @half_up is resolved in: an enum that comes from
// the registry rather than the contract, so a contract may not reuse its
// name. A currency is no enum; it is written as its code.
const RoundingEnum = "rounding"

// RoundingEnumType is the enum a rounding mode is written in.
func RoundingEnumType() Type { return EnumOf(RoundingEnum, RoundingModes()...) }

// AllocationEnum is the enum @last is resolved in, the second enum that comes
// from the registry: where allocate hands out the units rounding leaves.
const AllocationEnum = "allocation"

// RateTableEnum is the enum @settlement is resolved in: the named rate tables
// a contract declares, which using(@name, …) converts through. It comes from
// the contract's table list, not from a type, so no type may take its name.
const RateTableEnum = "rate_table"

// AllocationEnumType is the enum an allocation strategy is written in.
func AllocationEnumType() Type { return EnumOf(AllocationEnum, AllocationStrategies()...) }

// DeclaredCurrency checks a currency code against the registry: a program's
// USD and a contract's money<USD> both name a declared currency.
func DeclaredCurrency(r *Registry, code string) error {
	table := r.currencies()
	if table == nil {
		return fmt.Errorf("money is not declared for this registry")
	}
	_, err := table.places(code)
	return err
}

// ParseMoneyAmount reads an amount written in a currency's major unit — the
// "1.70" of USD 1.70 — into a money value, exactly.
func ParseMoneyAmount(r *Registry, code, amount string) (Value, error) {
	table := r.currencies()
	if table == nil {
		return Value{}, fmt.Errorf("money is not declared for this registry")
	}
	return table.moneyOf(code, amount)
}
