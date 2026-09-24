package machine

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/nethinwei/funroute/internal/money"
)

// The JSON shapes of the money types, for Run's by-name arguments. A Go host
// hands over Money, Ratio, FxRate and Currency and never reaches this file.

// coerceMoneyKind reads money as "USD 1.70" or {"currency": "USD", "minor":
// 170}, a rate as a decimal string or number, an exchange rate as {"base",
// "quote", "rate"}, and a currency as its code.
func coerceMoneyKind(input any, expected Type, table *money.Currencies) (Value, error) {
	switch expected.kind {
	case MoneyKind:
		return coerceMoney(input, table)
	case RatioKind:
		r, err := coerceRatio(input)
		return RatioValue(r), err
	case FxRateKind:
		return coerceFxRate(input)
	default:
		code, ok := input.(string)
		if !ok {
			return Value{}, fmt.Errorf("got %T, want a currency code", input)
		}
		return CurrencyValue(code), nil
	}
}

func coerceMoney(input any, table *money.Currencies) (Value, error) {
	switch value := input.(type) {
	case string:
		if table == nil {
			return Value{}, fmt.Errorf("money %q needs the registry's currency table", value)
		}
		m, err := table.Parse(value)
		return moneyValueOf(m), err
	case map[string]any:
		code, ok := value["currency"].(string)
		if !ok {
			return Value{}, fmt.Errorf("money needs a \"currency\" code")
		}
		minor, err := coerceInt(value["minor"])
		if err != nil {
			return Value{}, fmt.Errorf("money \"minor\": %w", err)
		}
		amount := MoneyValue(minor.i, code)
		if code == "" {
			return amount, noCurrencyIsZero(amount)
		}
		return amount, nil
	default:
		// Zero is the one amount with no currency: nothing is lost by it.
		if zero, err := coerceInt(input); err == nil && zero.i == 0 {
			return MoneyValue(0, ""), nil
		}
		return Value{}, fmt.Errorf("got %T, want money such as \"USD 1.70\" or {\"currency\": \"USD\", \"minor\": 170}", input)
	}
}

// coerceRatio reads a ratio exactly. A JSON number is read as the shortest
// decimal that round-trips it — what a person wrote as 0.03 is 0.03, not the
// binary fraction nearest to it.
func coerceRatio(input any) (money.Ratio, error) {
	switch value := input.(type) {
	case string:
		return money.ParseRatio(value)
	case json.Number:
		return money.ParseRatio(value.String())
	case float64:
		return money.ParseRatio(strconv.FormatFloat(value, 'f', -1, 64))
	default:
		if whole, err := coerceInt(input); err == nil {
			return money.RatioOf(whole.i, 1)
		}
		return money.Ratio{}, fmt.Errorf("got %T, want a ratio such as \"0.029\"", input)
	}
}

// coerceFxRate reads {"base", "quote", "rate"}, the rate as its exact text:
// a decimal or a fraction.
func coerceFxRate(input any) (Value, error) {
	entries, ok := input.(map[string]any)
	if !ok {
		return Value{}, fmt.Errorf("got %T, want an exchange rate {\"base\", \"quote\", \"rate\"}", input)
	}
	base, baseOK := entries["base"].(string)
	quote, quoteOK := entries["quote"].(string)
	if !baseOK || !quoteOK {
		return Value{}, fmt.Errorf("an exchange rate needs \"base\" and \"quote\" codes")
	}
	var text string
	switch rate := entries["rate"].(type) {
	case string:
		text = rate
	case json.Number:
		text = rate.String()
	case float64:
		text = strconv.FormatFloat(rate, 'f', -1, 64)
	default:
		return Value{}, fmt.Errorf("an exchange rate needs a \"rate\", such as \"150.25\"")
	}
	fx, err := money.ReadFxRate(base, quote, text)
	return FxRateValue(fx), err
}

// kindError is FromValue's failure for a value of another kind.
func kindError(ok bool, value Value, want any) error {
	if ok {
		return nil
	}
	return fmt.Errorf("argument is %s, want %T", value.Type(), want)
}
