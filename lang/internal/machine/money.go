package machine

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

// Money is an amount in a currency's minor unit: USD 1.70 is 170 minor
// units of USD. Its fields are the machine's: a host gets one from a
// currency table (Currencies.Parse, Of, Minor), from a rule's result, or as
// the zero value — the currency-less zero, the one amount with no currency.
// Whatever a host holds is therefore well formed, and []Money is an
// array<money<…>>'s backing, handed across the boundary without a copy.
type Money struct {
	currency string
	minor    int64
}

// newMoney is the machine's own constructor; everything outside builds money
// through a currency table, which checks it.
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
		return fmt.Errorf("money needs a \"currency\" and a \"minor\"")
	}
	parsed := newMoney(*shape.Currency, *shape.Minor)
	if err := parsed.wellFormed(); err != nil {
		return err
	}
	*m = parsed
	return nil
}

// wellFormed is what an amount must be to exist at all: a currency code, or
// no currency and zero.
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

// MoneyValue builds a money value. The currency is checked against the
// registry where the value enters a program, not here: a Value does not know
// which console it belongs to. The machine's and the compiler's own entry; a
// host goes through ToValue with a Money a table made.
func MoneyValue(minor int64, currency string) Value {
	return Value{kind: MoneyKind, i: minor, s: currency}
}

// RateValue builds a rate value.
func RateValue(rate Rate) Value { return Value{kind: RateKind, i: rate.scaled} }

// CurrencyValue builds a currency value.
func CurrencyValue(code string) Value { return Value{kind: CurrencyKind, s: code} }

// FxRateValue builds an exchange rate value: the base in s, the rate itself
// boxed — the one allocation a rate costs where it is made.
func FxRateValue(rate FxRate) Value { return Value{kind: FxRateKind, s: rate.base, box: rate} }

// FxRate returns an exchange rate value's Go form.
func (v Value) FxRate() (FxRate, bool) {
	rate, ok := v.box.(FxRate)
	return rate, ok && v.kind == FxRateKind
}

// quote is an exchange rate value's quote currency.
func (v Value) quote() string {
	rate, _ := v.box.(FxRate)
	return rate.quote
}

// Money returns a money value's Go form.
func (v Value) Money() (Money, bool) {
	return newMoney(v.s, v.i), v.kind == MoneyKind
}

// Rate returns a rate value.
func (v Value) Rate() (Rate, bool) { return newRate(v.i), v.kind == RateKind }

// Currency returns a currency value's Go form.
func (v Value) Currency() (Currency, bool) { return Currency{code: v.s}, v.kind == CurrencyKind }

// hasUnits is hasType's answer for the kinds that carry a currency. A unit
// the type leaves open — unknown, or a contract variable the frame checks —
// admits any currency; a code admits that one. Money's zero with no currency
// fits every money type: nothing is lost by calling it dollars.
func (v Value) hasUnits(t Type) bool {
	switch t.kind {
	case MoneyKind:
		if v.s == "" {
			return v.i == 0 // only nothing is in no currency
		}
		return unitAdmits(t.name, v.s)
	case FxRateKind:
		return len(t.values) == 2 && unitAdmits(t.values[0], v.s) && unitAdmits(t.values[1], v.quote())
	default:
		return unitAdmits(t.name, v.s)
	}
}

func unitAdmits(unit, currency string) bool {
	return !IsCurrencyCode(unit) || unit == currency
}

// unitType is Type for a unit-carrying value.
func (v Value) unitType() Type {
	switch v.kind {
	case MoneyKind:
		return MoneyOf(v.s)
	case FxRateKind:
		return FxRateOf(v.s, v.quote())
	default:
		return CurrencyOf(v.s)
	}
}

// equalUnits compares two values of one unit-carrying kind. A currency-less
// zero equals any currency's zero.
func (v Value) equalUnits(other Value) bool {
	switch v.kind {
	case MoneyKind:
		return v.i == other.i && (v.s == other.s || v.i == 0)
	case FxRateKind:
		first, _ := v.FxRate()
		second, _ := other.FxRate()
		return first.base == second.base && first.quote == second.quote && first.rate.Cmp(second.rate) == 0
	default:
		return v.s == other.s
	}
}

// sameCurrency reports whether two currencies can meet: equal, or one of them
// the currency-less zero's.
func sameCurrency(a, b string) bool { return a == b || a == "" || b == "" }

// sameUnits refuses to compare amounts in different currencies: USD 1 ==
// EUR 1 is not false, it is a question that has no answer. A currency-less
// zero meets anything. Two currencies themselves compare as the values they
// are: whether this order is in dollars is a fine question.
func sameUnits(left, right Value) error {
	if left.kind != right.kind || !IsUnitKind(left.kind) || left.kind == CurrencyKind {
		return nil
	}
	if left.kind == FxRateKind {
		if left.s != right.s || left.quote() != right.quote() {
			return fmt.Errorf("%w: cannot compare %s with %s", ErrCurrency, left.unitType(), right.unitType())
		}
		return nil
	}
	if left.kind == MoneyKind && sameCurrency(left.s, right.s) {
		return nil
	}
	if left.s != right.s {
		return fmt.Errorf("%w: cannot compare %s with %s", ErrCurrency, left.unitType(), right.unitType())
	}
	return nil
}

// equalInUnits compares two values of one type that hold money somewhere:
// amounts in different currencies are an ErrCurrency, not unequal, inside a
// container as on their own. Items are compared in order and the first
// difference decides, so containers of different sizes are simply unequal.
func equalInUnits(left, right Value) (bool, error) {
	switch left.kind {
	case MoneyKind, CurrencyKind, FxRateKind:
		return left.Equal(right), sameUnits(left, right)
	case ArrayKind:
		if left.length() != right.length() {
			return false, nil
		}
		return allEqualInUnits(left.length(), func(i int) (Value, Value, bool) { return left.at(i), right.at(i), true })
	case DictKind:
		keys := left.keys()
		if len(keys) != right.length() {
			return false, nil
		}
		return allEqualInUnits(len(keys), func(i int) (Value, Value, bool) {
			a, _ := left.lookup(keys[i])
			b, ok := right.lookup(keys[i])
			return a, b, ok
		})
	case RecordKind:
		return allEqualInUnits(len(left.Type().fields), func(i int) (Value, Value, bool) { return left.Field(i), right.Field(i), true })
	default:
		return left.Equal(right), nil
	}
}

// allEqualInUnits compares n pairs until one differs; a pair that is not
// there (a key only one dictionary has) is a difference.
func allEqualInUnits(n int, pair func(int) (Value, Value, bool)) (bool, error) {
	for i := range n {
		a, b, ok := pair(i)
		if !ok {
			return false, nil
		}
		if equal, err := equalInUnits(a, b); err != nil || !equal {
			return false, err
		}
	}
	return true, nil
}

// moneyConstant interns a money-kind value: the integer in Int, the currency
// (an exchange rate's base) in String, and an exchange rate's quote and exact
// rate in Keys.
func moneyConstant(value Value) Constant {
	out := Constant{Type: value.kind}
	if rate, ok := value.FxRate(); ok {
		base := rate.base
		out.String = &base
		out.Keys = []string{rate.quote, rateText(rate.rate)}
		return out
	}
	if value.kind != CurrencyKind {
		i := value.i
		out.Int = &i
	}
	if value.kind != RateKind {
		s := value.s
		out.String = &s
	}
	return out
}

// moneyValue reads a constant moneyConstant wrote.
func (c Constant) moneyValue() (Value, error) {
	if c.Type == FxRateKind {
		return c.fxRateValue()
	}
	value := Value{kind: c.Type}
	if c.Type != CurrencyKind {
		if c.Int == nil {
			return Value{}, fmt.Errorf("%s constant is missing its amount", c.Type)
		}
		value.i = *c.Int
	}
	if c.Type != RateKind {
		if c.String == nil {
			return Value{}, fmt.Errorf("%s constant is missing its currency", c.Type)
		}
		value.s = *c.String
	}
	return value, nil
}

// parseDecimal reads a plain decimal — digits, an optional point, an optional
// sign — into an integer scaled by 10^digits, exactly. A figure with more
// decimal places than that is refused: it cannot be represented, and
// rounding it silently is the thing money types exist to prevent.
func parseDecimal(text string, digits int) (int64, error) {
	body, negative := strings.CutPrefix(text, "-")
	if !negative {
		body = strings.TrimPrefix(body, "+")
	}
	whole, fraction, _ := strings.Cut(body, ".")
	if whole == "" && fraction == "" {
		return 0, fmt.Errorf("is not a decimal")
	}
	if len(fraction) > digits {
		return 0, fmt.Errorf("has %d decimal places, at most %d fit", len(fraction), digits)
	}
	// A negative figure may reach one past MaxInt64: -2^63 is an int64.
	limit := uint64(math.MaxInt64)
	if negative {
		limit++
	}
	scaled, err := accumulateDigits(0, whole, limit)
	if err != nil {
		return 0, err
	}
	scaled, err = accumulateDigits(scaled, fraction+strings.Repeat("0", digits-len(fraction)), limit)
	if err != nil {
		return 0, err
	}
	if negative {
		return int64(-scaled), nil
	}
	return int64(scaled), nil
}

// accumulateDigits appends decimal digits to an unsigned total, refusing
// anything but digits and any total past limit.
func accumulateDigits(total uint64, digits string, limit uint64) (uint64, error) {
	for i := 0; i < len(digits); i++ {
		c := digits[i]
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("is not a decimal")
		}
		if total > (limit-uint64(c-'0'))/10 {
			return 0, errFixedOverflow
		}
		total = total*10 + uint64(c-'0')
	}
	return total, nil
}

// formatDecimal writes a scaled integer back as a decimal: 170 with two
// digits is "1.70". trim drops trailing zeros — a rate reads 0.029 — while
// money keeps its currency's places.
func formatDecimal(scaled int64, digits int, trim bool) string {
	sign := ""
	if scaled < 0 {
		sign = "-"
	}
	text := fmt.Sprintf("%0*d", digits+1, magnitude(scaled))
	whole, fraction := text[:len(text)-digits], text[len(text)-digits:]
	if trim {
		fraction = strings.TrimRight(fraction, "0")
	}
	if fraction == "" {
		return sign + whole
	}
	return sign + whole + "." + fraction
}

// fxRateValue reads an exchange rate constant back.
func (c Constant) fxRateValue() (Value, error) {
	if c.String == nil || len(c.Keys) != 2 {
		return Value{}, fmt.Errorf("exchange rate constant needs its base, quote and rate")
	}
	rate, err := parseRateText(c.Keys[1])
	if err != nil {
		return Value{}, err
	}
	fx := FxRate{base: *c.String, quote: c.Keys[0], rate: rate}
	if err := fx.wellFormed(); err != nil {
		return Value{}, err
	}
	return FxRateValue(fx), nil
}
