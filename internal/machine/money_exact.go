package machine

import (
	"math"

	"github.com/nethinwei/funroute/internal/money"
)

// exactValue is e as a value: plain money when it is a whole number of
// minor units, so a round with nothing to round has nothing to do; otherwise
// the numerator in i and the denominator's bits in f, which plain money
// leaves zero. It allocates nothing.
func exactValue(e money.ExactMoney) Value {
	num, den := money.RatioParts(e.Minor())
	if den == 1 || den == 0 {
		return MoneyValue(num, e.Currency())
	}
	return Value{kind: MoneyKind, s: e.Currency(), i: num, f: math.Float64frombits(uint64(den))}
}

// exactResult is exactValue with an error passed through.
func exactResult(e money.ExactMoney, err error) (Value, error) { return exactValue(e), err }

// IsExact reports money between minor units: a value only a step inside
// round(…) makes, and which the compiler keeps inside it.
func IsExact(v Value) bool { return v.kind == MoneyKind && v.f != 0 }

// exactOf is a money value as exact money, plain or not.
func exactOf(v Value) money.ExactMoney {
	if !IsExact(v) {
		return money.Make(v.s, v.i).Exact()
	}
	return money.ExactFrom(v.s, money.RatioFromParts(v.i, int64(math.Float64bits(v.f))))
}
