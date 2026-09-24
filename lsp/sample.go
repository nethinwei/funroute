package lsp

import (
	"encoding/json"
	"strings"

	"github.com/nethinwei/funroute/internal/machine"
	"github.com/nethinwei/funroute/internal/money"
)

// sampler writes a value of a type as JSON: what a client shows where an
// argument's value is typed, so the shape comes from the type the language
// read, not from its text. Its money is in the registry's own currencies, so
// every sample is a value the registry reads.
type sampler struct {
	money, currency, fxRate string
}

// samplerFor writes money in the registry's first currency by code, and an
// exchange rate from it to the second — to itself, at 1, when it declares
// one. A registry that declares none takes no money, and its samples are
// never asked for.
func samplerFor(registry *machine.Registry) sampler {
	spec, declared := registry.Money()
	if !declared || len(spec.Currencies) == 0 {
		return sampler{money: `"USD 1.70"`, currency: `"USD"`, fxRate: `{"base": "USD", "quote": "JPY", "rate": "150.25"}`}
	}
	base, quote, rate := spec.Currencies[0], spec.Currencies[0], "1"
	if len(spec.Currencies) > 1 {
		quote, rate = spec.Currencies[1], "150.25"
	}
	return sampler{
		money:    quoted(base.Code + " " + amountIn(base)),
		currency: quoted(base.Code),
		fxRate:   `{"base": ` + quoted(base.Code) + `, "quote": ` + quoted(quote.Code) + `, "rate": "` + rate + `"}`,
	}
}

// amountIn is 1.70 written with the currency's places: 1 in yen, 1.700 in
// dinars.
func amountIn(currency money.CurrencySpec) string {
	if currency.Digits == 0 {
		return "1"
	}
	return "1." + ("7" + strings.Repeat("0", currency.Digits))[:currency.Digits]
}

// sample is typ's sample. An array's element, a dictionary's entry and each
// field of a record are shown once, each with a sample of its own type. A
// type JSON cannot give has none: a handle, which only the host passes, a
// type not inferred yet, and so a record with a field of one. A container of
// one is sampled empty.
func (s sampler) sample(typ machine.Type) string {
	switch typ.Kind() {
	case machine.ArrayKind:
		elem, _ := typ.Elem()
		return "[" + s.sample(elem) + "]"
	case machine.DictKind:
		elem, _ := typ.Elem()
		if entry := s.sample(elem); entry != "" {
			return `{"key": ` + entry + "}"
		}
		return "{}"
	case machine.RecordKind:
		return s.record(typ.Fields())
	case machine.EnumKind:
		if values := typ.Values(); len(values) > 0 {
			return quoted(values[0])
		}
	case machine.MoneyKind:
		return s.money
	case machine.CurrencyKind:
		return s.currency
	case machine.FxRateKind:
		return s.fxRate
	}
	return scalarSamples[typ.Kind()]
}

// scalarSamples are the samples of the kinds that have no parts and no
// currency. A ratio is written the way arguments pass it.
var scalarSamples = map[machine.Kind]string{
	machine.BoolKind: "true", machine.IntKind: "0", machine.FloatKind: "0.5", machine.StringKind: `"…"`, machine.EnumKind: `"…"`,
	machine.RatioKind: `"0.029"`,
}

func (s sampler) record(fields []machine.Field) string {
	parts := make([]string, len(fields))
	for i, field := range fields {
		value := s.sample(field.Type())
		if value == "" {
			return ""
		}
		parts[i] = quoted(field.Name()) + ": " + value
	}
	return "{" + strings.Join(parts, ", ") + "}"
}

// quoted is text as a JSON string.
func quoted(text string) string {
	encoded, _ := json.Marshal(text)
	return string(encoded)
}
