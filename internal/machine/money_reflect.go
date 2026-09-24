package machine

import (
	"fmt"
	"reflect"

	"github.com/nethinwei/funroute/internal/money"
)

// The money types' Go forms are structs and named scalars, so reflection
// would otherwise read Money as a record and Currency as a string. They are
// recognised first, everywhere a Go type crosses the boundary.

var (
	moneyGoType    = reflect.TypeFor[money.Money]()
	ratioGoType    = reflect.TypeFor[money.Ratio]()
	fxRateGoType   = reflect.TypeFor[money.FxRate]()
	currencyGoType = reflect.TypeFor[money.Currency]()
)

// moneyGoKind is the FunRoute type of a money Go type. Reflection cannot see
// a currency, so the unit is unknown: a signature that needs money<C> is a
// FunctionSpec.
func moneyGoKind(typ reflect.Type) (Type, bool) {
	switch typ {
	case moneyGoType:
		return MoneyType, true
	case ratioGoType:
		return RatioType, true
	case fxRateGoType:
		return FxRateType, true
	case currencyGoType:
		return CurrencyType, true
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
	return newMoneyGo(typ, value), true, nil
}

// outOfMoneyGo converts a money Go value into a Value.
func outOfMoneyGo(value reflect.Value) (Value, error) {
	converted, err := fromGo(value.Interface())
	if err != nil {
		return Value{}, fmt.Errorf("result %s: %w", value.Type(), err)
	}
	return converted, nil
}
