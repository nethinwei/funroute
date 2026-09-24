package machine

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/nethinwei/funroute/internal/money"
)

// The JSON shapes of the money types, for Run's by-name arguments. A Go host
// hands over Money, Ratio, FxRate and Currency and never reaches this file.

// coerceMoneyKind reads money as "USD 1.70" or {"currency": "USD", "minor":
// 170}, a rate as a decimal string or number, an exchange rate as {"base",
// "quote", "rate"}, and a currency as its code.
func coerceMoneyKind(input any, expected Type, table *money.Currencies) (Value, error) {
	if err := exactInput(input); err != nil {
		return Value{}, err
	}
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
			return Value{}, errors.New("money needs a \"currency\" code")
		}
		minor, err := exactInt(value["minor"])
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

// exactInput refuses a float64 where money, a ratio or a rate is read: JSON
// decoded into any has already turned what was written into the binary
// fraction nearest it, and these are what was written, digit for digit. A
// string, a json.Number — what DecodeArgs keeps every number as — or an int
// is read exactly.
func exactInput(input any) error {
	switch input.(type) {
	case float32, float64:
		return fmt.Errorf("got %T, which may already be rounded: pass money, ratios and exchange rates as strings or json.Number (DecodeArgs keeps numbers as written)", input)
	}
	return nil
}

// exactInt is coerceInt for a part of money: never a float64.
func exactInt(input any) (Value, error) {
	if err := exactInput(input); err != nil {
		return Value{}, err
	}
	return coerceInt(input)
}

// coerceRatio reads a ratio exactly, from the text of a JSON number or a
// string, or from an int.
func coerceRatio(input any) (money.Ratio, error) {
	if text, ok := numberText(input); ok {
		return money.ParseRatio(text)
	}
	if whole, err := coerceInt(input); err == nil {
		return money.RatioOf(whole.i, 1)
	}
	return money.Ratio{}, fmt.Errorf("got %T, want a ratio such as \"0.029\"", input)
}

// numberText is a decoded JSON number or string as the text it reads as.
func numberText(input any) (string, bool) {
	switch value := input.(type) {
	case string:
		return value, true
	case json.Number:
		return value.String(), true
	}
	return "", false
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
		return Value{}, errors.New("an exchange rate needs \"base\" and \"quote\" codes")
	}
	if err := exactInput(entries["rate"]); err != nil {
		return Value{}, err
	}
	text, ok := numberText(entries["rate"])
	if !ok {
		return Value{}, errors.New("an exchange rate needs a \"rate\", such as \"150.25\"")
	}
	fx, err := money.ReadFxRate(base, quote, text)
	return FxRateValue(fx), err
}
