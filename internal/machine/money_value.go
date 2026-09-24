package machine

import (
	"fmt"
	"math"

	"github.com/nethinwei/funroute/internal/money"
)

// moneyValueOf is money as a value.
func moneyValueOf(m money.Money) Value { return MoneyValue(m.Minor(), m.Currency()) }

// MoneyValue builds a money value. The currency is checked against the
// registry where the value enters a program, not here: a Value does not know
// which console it belongs to. The machine's and the compiler's own entry; a
// host goes through ToValue with a Money a table made.
func MoneyValue(minor int64, currency string) Value {
	return Value{kind: MoneyKind, i: minor, s: currency}
}

// RatioValue builds a ratio value without allocating: the numerator in i and
// the denominator's bits in f, which no other kind of value reads as a ratio.
// f is storage here, never a number: the int64 goes in and comes out bit for
// bit, and no floating-point operation ever touches a ratio.
func RatioValue(r money.Ratio) Value {
	num, den := money.RatioParts(r)
	return Value{kind: RatioKind, i: num, f: math.Float64frombits(uint64(den))}
}

// ratioFrom is the ratio RatioValue stored.
func ratioFrom(value Value) money.Ratio {
	return money.RatioFromParts(value.i, int64(math.Float64bits(value.f)))
}

// CurrencyValue builds a currency value.
func CurrencyValue(code string) Value { return Value{kind: CurrencyKind, s: code} }

// FxRateValue builds an exchange rate value without allocating: the base in
// s, the rate as a rate value holds one, and the rate's shared pair in box.
func FxRateValue(fx money.FxRate) Value {
	pair, r := money.FxRateParts(fx)
	value := RatioValue(r)
	value.kind, value.s = FxRateKind, fx.Base()
	if pair != nil {
		value.box = pair
	}
	return value
}

// FxRate returns an exchange rate value's Go form.
func (v Value) FxRate() (money.FxRate, bool) {
	pair, ok := v.box.(*money.Pair)
	if !ok || v.kind != FxRateKind {
		return money.FxRate{}, false
	}
	return money.FxRateFrom(pair, ratioFrom(v)), true
}

// quote is an exchange rate value's quote currency.
func (v Value) quote() string {
	rate, _ := v.FxRate()
	return rate.Quote()
}

// Money returns a money value's Go form.
func (v Value) Money() (money.Money, bool) {
	return money.Make(v.s, v.i), v.kind == MoneyKind && !IsExact(v)
}

// Ratio returns a ratio value.
func (v Value) Ratio() (money.Ratio, bool) { return ratioFrom(v), v.kind == RatioKind }

// Currency returns a currency value's Go form.
func (v Value) Currency() (money.Currency, bool) {
	return money.CurrencyOf(v.s), v.kind == CurrencyKind
}

// hasUnits is hasType's answer for the kinds that carry a currency: one
// type admits every currency, and only money's zero may have none.
func (v Value) hasUnits() bool { return v.kind != MoneyKind || v.s != "" || v.i == 0 }

// equalUnits compares two values of one unit-carrying kind. A currency-less
// zero equals any currency's zero.
func (v Value) equalUnits(other Value) bool {
	switch v.kind {
	case MoneyKind:
		if IsExact(v) || IsExact(other) {
			order, err := exactOf(v).Cmp(exactOf(other))
			return err == nil && order == 0
		}
		return v.i == other.i && (v.s == other.s || v.i == 0)
	case FxRateKind:
		first, _ := v.FxRate()
		second, _ := other.FxRate()
		a, x := money.FxRateParts(first)
		b, y := money.FxRateParts(second)
		return a != nil && b != nil && *a == *b && x == y
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
			return fmt.Errorf("%w: cannot compare an exchange rate for %s with one for %s", ErrCurrency, pairText(left), pairText(right))
		}
		return nil
	}
	if left.kind == MoneyKind && sameCurrency(left.s, right.s) {
		return nil
	}
	if left.s != right.s {
		return fmt.Errorf("%w: cannot compare %s with %s", ErrCurrency, left.s, right.s)
	}
	return nil
}

// equalInUnits compares two values of one type that hold money somewhere:
// amounts in different currencies are an ErrCurrency, not unequal, inside a
// container as on their own. Items are compared in order and the first
// difference decides (equalItems).
func equalInUnits(left, right Value) (bool, error) {
	switch left.kind {
	case MoneyKind, CurrencyKind, FxRateKind:
		return left.Equal(right), sameUnits(left, right)
	case ArrayKind, DictKind, RecordKind:
		return equalItems(left, right, equalInUnits)
	default:
		return left.Equal(right), nil
	}
}

// pairText is an exchange rate's pair as its literal writes it: JPY / USD is
// how many yen one dollar buys.
func pairText(v Value) string { return v.quote() + " / " + v.s }
