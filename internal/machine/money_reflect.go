package machine

import (
	"fmt"
	"reflect"

	"github.com/nethinwei/funroute/internal/money"
)

// The Go forms of the language's own types — money's, and time.Time and
// time.Duration — are structs and named scalars, so reflection would
// otherwise read Money and time.Time as records and Currency as a string.
// They are recognised first, everywhere a Go type crosses the boundary.

var (
	moneyGoType    = reflect.TypeFor[money.Money]()
	ratioGoType    = reflect.TypeFor[money.Ratio]()
	fxRateGoType   = reflect.TypeFor[money.FxRate]()
	currencyGoType = reflect.TypeFor[money.Currency]()
)

// ownGoKind is the FunRoute type of one of those Go types. Reflection cannot
// see a currency, so the unit is unknown: a signature that needs money<C> is
// a FunctionSpec.
func ownGoKind(typ reflect.Type) (Type, bool) {
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
		return timeGoKind(typ)
	}
}

// intoOwnGo converts a value into one of those Go types; ok is false for any
// other Go type.
func intoOwnGo(value Value, typ reflect.Type) (reflect.Value, bool, error) {
	want, ok := ownGoKind(typ)
	if !ok {
		return reflect.Value{}, false, nil
	}
	if value.kind != want.kind {
		return reflect.Value{}, true, fmt.Errorf("argument is %s, want %s", value.Type(), want)
	}
	return newOwnGo(typ, value), true, nil
}

// outOfOwnGo converts one of those Go values into a Value.
func outOfOwnGo(value reflect.Value) (Value, error) {
	converted, err := fromGo(value.Interface())
	if err != nil {
		return Value{}, fmt.Errorf("result %s: %w", value.Type(), err)
	}
	return converted, nil
}
