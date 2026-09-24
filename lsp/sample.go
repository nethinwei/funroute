package lsp

import (
	"encoding/json"
	"strings"

	"github.com/nethinwei/funroute/internal/machine"
)

// sample is a value of typ written as JSON: what a client shows where an
// argument's value is typed, so the shape comes from the type the language
// read, not from its text. An array's element, a dictionary's entry and each
// field of a record are shown once, each with a sample of its own type. A
// type JSON cannot give has none: a handle, which only the host passes, a
// type not inferred yet, and so a record with a field of one. A container of
// one is sampled empty.
func sample(typ machine.Type) string {
	switch typ.Kind() {
	case machine.ArrayKind:
		elem, _ := typ.Elem()
		return "[" + sample(elem) + "]"
	case machine.DictKind:
		elem, _ := typ.Elem()
		if entry := sample(elem); entry != "" {
			return `{"key": ` + entry + "}"
		}
		return "{}"
	case machine.RecordKind:
		return recordSample(typ.Fields())
	case machine.EnumKind:
		if values := typ.Values(); len(values) > 0 {
			return quoted(values[0])
		}
	}
	return scalarSamples[typ.Kind()]
}

// scalarSamples are the samples of the kinds that have no parts. Money and
// the kinds that go with it are written the way arguments pass them.
var scalarSamples = map[machine.Kind]string{
	machine.BoolKind: "true", machine.IntKind: "0", machine.FloatKind: "0.5", machine.StringKind: `"…"`, machine.EnumKind: `"…"`,
	machine.MoneyKind: `"USD 1.70"`, machine.RatioKind: `"0.029"`, machine.CurrencyKind: `"USD"`,
	machine.FxRateKind: `{"base": "USD", "quote": "JPY", "rate": "150.25"}`,
}

func recordSample(fields []machine.Field) string {
	parts := make([]string, len(fields))
	for i, field := range fields {
		value := sample(field.Type())
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
