package machine

import (
	"errors"
	"fmt"
	"reflect"
	"time"
	"unsafe"

	"github.com/nethinwei/funroute/internal/money"
)

// The language's own Go types — money.Money, money.Ratio, money.FxRate,
// money.Currency, time.Time and time.Duration, ownGoKind's list — read and
// written in the host's memory, each by a case of its own.

// loadOwn reads one of the language's own Go types. An exchange rate boxes
// its quote; a time.Time outside the years a time spans is refused.
func loadOwn(typ reflect.Type, p unsafe.Pointer) (Value, error) {
	switch typ {
	case moneyGoType:
		amount := (*money.Money)(p)
		return MoneyValue(amount.Minor(), amount.Currency()), nil
	case ratioGoType:
		return RatioValue(*(*money.Ratio)(p)), nil
	case fxRateGoType:
		return FxRateValue(*(*money.FxRate)(p)), nil
	case timeGoType:
		return timeValue(*(*time.Time)(p))
	case durationGoType:
		return Duration(*(*time.Duration)(p)), nil
	case currencyGoType:
		return CurrencyValue((*money.Currency)(p).Code()), nil
	default:
		// A type ownGoKind names and no case here reads: reading it as
		// another type's memory would be a quiet wrong answer.
		return Value{}, fmt.Errorf("internal error: no reading of %s", typ)
	}
}

// storeOwn writes one of the language's own Go types.
func storeOwn(typ reflect.Type, p unsafe.Pointer, v Value) error {
	if want, _ := ownGoKind(typ); v.kind != want.kind {
		return fmt.Errorf("value is %s, want %s", v.Type().Summary(), want)
	}
	switch typ {
	case moneyGoType:
		amount, ok := v.Money()
		if !ok {
			// Only a program not from the compiler hands over an amount
			// between minor units: the compiler keeps it inside round.
			return errors.New("value is an exact amount, which has no Go form until it is rounded")
		}
		*(*money.Money)(p) = amount
	case ratioGoType:
		*(*money.Ratio)(p) = ratioFrom(v)
	case fxRateGoType:
		*(*money.FxRate)(p), _ = v.FxRate()
	case timeGoType:
		*(*time.Time)(p), _ = v.Time()
	case durationGoType:
		*(*time.Duration)(p) = time.Duration(v.i)
	case currencyGoType:
		*(*money.Currency)(p) = money.CurrencyOf(v.s)
	default:
		return fmt.Errorf("internal error: no writing of %s", typ)
	}
	return nil
}

// newOwnGo is a new Go value of one of those types, typ, holding v, whose kind is typ's.
func newOwnGo(typ reflect.Type, v Value) (reflect.Value, error) {
	out := reflect.New(typ)
	if err := storeOwn(typ, out.UnsafePointer(), v); err != nil {
		return reflect.Value{}, err
	}
	return out.Elem(), nil
}
