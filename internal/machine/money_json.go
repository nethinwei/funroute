package machine

import (
	"encoding/json"

	"github.com/nethinwei/funroute/internal/money"
)

// EncodeJSON writes a value the way a person reads it: money as "USD 1.70",
// which needs the currency's decimal places and so the registry; a
// currency-less zero as 0. Everything else is what json.Marshal writes. A
// Value on its own writes money as {"currency", "minor"} — the machine form,
// exact without a table.
func (r *Registry) EncodeJSON(value Value) ([]byte, error) {
	table := r.currencies()
	if table == nil {
		return json.Marshal(value)
	}
	return json.Marshal(readable(table, value))
}

// readable is value with its money written out as text, containers and
// records walked; a record keeps its field order.
func readable(t *money.Currencies, value Value) any {
	switch value.kind {
	case MoneyKind:
		if value.s == "" && value.i == 0 {
			return value.i
		}
		text, err := t.Format(money.Make(value.s, value.i))
		if err != nil {
			// No places to write it in: the machine form keeps it exact.
			return map[string]any{"currency": value.s, "minor": value.i}
		}
		return text
	case ArrayKind:
		items := make([]any, value.length())
		for i := range items {
			items[i] = readable(t, value.at(i))
		}
		return items
	case DictKind:
		entries := make(map[string]any, value.length())
		for _, key := range value.keys() {
			entry, _ := value.lookup(key)
			entries[key] = readable(t, entry)
		}
		return entries
	case RecordKind:
		return readableRecord(t, value)
	default:
		return value
	}
}

// orderedFields is a record written in its type's field order.
type orderedFields struct {
	names  []string
	values []any
}

func (o orderedFields) MarshalJSON() ([]byte, error) {
	out := []byte{'{'}
	for i, name := range o.names {
		if i > 0 {
			out = append(out, ',')
		}
		key, err := json.Marshal(name)
		if err != nil {
			return nil, err
		}
		item, err := json.Marshal(o.values[i])
		if err != nil {
			return nil, err
		}
		out = append(append(append(out, key...), ':'), item...)
	}
	return append(out, '}'), nil
}

func readableRecord(t *money.Currencies, value Value) any {
	record, ok := value.box.(*recordValue)
	if !ok {
		return nil
	}
	out := orderedFields{names: make([]string, len(record.fields)), values: make([]any, len(record.fields))}
	for i, field := range record.fields {
		out.names[i] = record.typ.fields[i].name
		out.values[i] = readable(t, field)
	}
	return out
}
