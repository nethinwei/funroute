package machine

import (
	"fmt"
	"reflect"
)

// The money types' Go forms are structs and named scalars, so reflection
// would otherwise read Money as a record and Currency as a string. They are
// recognised first, everywhere a Go type crosses the boundary.

var (
	moneyGoType    = reflect.TypeFor[Money]()
	rateGoType     = reflect.TypeFor[Rate]()
	fxRateGoType   = reflect.TypeFor[FxRate]()
	currencyGoType = reflect.TypeFor[Currency]()
)

// moneyGoKind is the FunRoute type of a money Go type. Reflection cannot see
// a currency, so the unit is unknown: a signature that needs money<c> is a
// FunctionSpec.
func moneyGoKind(typ reflect.Type) (Type, bool) {
	switch typ {
	case moneyGoType:
		return MoneyOf(""), true
	case rateGoType:
		return RateType, true
	case fxRateGoType:
		return FxRateOf("", ""), true
	case currencyGoType:
		return CurrencyOf(""), true
	default:
		return Type{}, false
	}
}

// intoMoneyGo converts a value into a money Go type; ok is false for any
// other Go type.
func intoMoneyGo(value Value, typ reflect.Type) (reflect.Value, bool, error) {
	want, ok := moneyGoKind(typ)
	if !ok {
		return reflect.Value{}, false, nil
	}
	if value.kind != want.kind {
		return reflect.Value{}, true, fmt.Errorf("argument is %s, want %s", value.Type(), want)
	}
	switch typ {
	case moneyGoType:
		money, _ := value.Money()
		return reflect.ValueOf(money), true, nil
	case rateGoType:
		return reflect.ValueOf(newRate(value.i)), true, nil
	case fxRateGoType:
		rate, _ := value.FxRate()
		return reflect.ValueOf(rate), true, nil
	default:
		return reflect.ValueOf(Currency{code: value.s}), true, nil
	}
}

// outOfMoneyGo converts a money Go value into a Value.
func outOfMoneyGo(value reflect.Value) (Value, error) {
	converted, err := fromGo(value.Interface())
	if err != nil {
		return Value{}, fmt.Errorf("result %s: %w", value.Type(), err)
	}
	return converted, nil
}
