package machine

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
)

// A Go struct is a record. That is the whole point of records: a host already
// holds its order as a struct, and making it build a map[string]any first
// would be a conversion the boundary exists to avoid.
//
// Nothing here is guessed: a field is in the record because it says so, with a
// `funroute:"name"` tag that gives the name it goes by. A field without one is
// simply not part of the record — and leaving one out by accident is not
// silent, because any expression that reads it fails to compile ("record{…}
// has no field"). Deriving the name from the Go one would be the other way
// round: a rename in Go would quietly rename a contract's field, which is the
// same reason handles are registered by name rather than derived from a type.
//
// Declaration order is the record's field order, which is its type. Unexported
// fields are never in it.
//
// Fields are matched by name, and a source that carries more than the contract
// asks for is fine: one host struct serves many rules, each declaring only the
// fields it reads. What is not fine is a field the contract asks for and the
// source does not have — and a misspelled name lands there, so nothing is lost
// by being generous about the extras.

// structFields describes one Go struct as record fields, in declaration order.
// A record has at least one field, so a struct that tags none is refused.
func structFields(registry *Registry, typ reflect.Type) ([]Field, []int, error) {
	fields, indexes, err := taggedFields(registry, typ)
	if err == nil && len(fields) == 0 {
		return nil, nil, fmt.Errorf(`struct %s declares no record fields: tag the ones the language may read with `+"`funroute:\"name\"`", typ)
	}
	return fields, indexes, err
}

// taggedFields is structFields without the lower bound, for a struct that is
// a program's arguments: a rule may take none.
func taggedFields(registry *Registry, typ reflect.Type) ([]Field, []int, error) {
	var fields []Field
	var indexes []int
	for i := 0; i < typ.NumField(); i++ {
		structField := typ.Field(i)
		if !structField.IsExported() {
			continue
		}
		name, mapped, err := fieldNameOf(structField)
		if err != nil {
			return nil, nil, err
		}
		if !mapped {
			continue
		}
		// A record's field names are its keys; two fields cannot share one.
		if at := slices.IndexFunc(fields, func(field Field) bool { return field.Name == name }); at >= 0 {
			return nil, nil, fmt.Errorf("fields %s and %s of %s are both tagged %q",
				typ.Field(indexes[at]).Name, structField.Name, typ, name)
		}
		fieldType, err := reflectType(registry, structField.Type)
		if err != nil {
			return nil, nil, fmt.Errorf("field %s: %w", structField.Name, err)
		}
		fields = append(fields, Field{Name: name, Type: fieldType})
		indexes = append(indexes, i)
	}
	return fields, indexes, nil
}

// fieldNameOf reads the name a field declares, and reports false for a field
// that declares none — that field is not in the record.
func fieldNameOf(field reflect.StructField) (string, bool, error) {
	tag, ok := field.Tag.Lookup("funroute")
	if !ok {
		return "", false, nil
	}
	name := strings.TrimSpace(tag)
	if name == "-" {
		return "", false, fmt.Errorf(
			`field %s is tagged "-": a field without a funroute tag is already left out`, field.Name)
	}
	if !IsValidFieldName(name) {
		return "", false, fmt.Errorf("field %s is tagged %q, which is not a usable field name", field.Name, name)
	}
	return name, true, nil
}

// intoStruct fills a Go struct from a record, matching by name. A struct field
// the record does not carry keeps its zero value; a record that shares no
// field with the struct is not that struct at all.
func intoStruct(registry *Registry, value Value, typ reflect.Type) (reflect.Value, error) {
	record, ok := value.box.(*recordValue)
	if !ok {
		return reflect.Value{}, fmt.Errorf("argument is %s, want %s", value.Type().Summary(), typ)
	}
	fields, indexes, err := structFields(registry, typ)
	if err != nil {
		return reflect.Value{}, err
	}
	out := reflect.New(typ).Elem()
	filled := 0
	for position, index := range indexes {
		source := record.typ.FieldIndex(fields[position].Name)
		if source < 0 {
			continue
		}
		converted, err := intoGo(registry, record.fields[source], typ.Field(index).Type)
		if err != nil {
			return reflect.Value{}, fmt.Errorf("field %s: %w", typ.Field(index).Name, err)
		}
		out.Field(index).Set(converted)
		filled++
	}
	if filled == 0 {
		return reflect.Value{}, fmt.Errorf("%s shares no field with %s", record.typ.Summary(), typ)
	}
	return out, nil
}

// outOfStruct is intoStruct's counterpart: the record takes the fields it
// declares from the struct, and ignores whatever else the struct carries.
func outOfStruct(registry *Registry, value reflect.Value, typ Type) (Value, error) {
	declared, indexes, err := structFields(registry, value.Type())
	if err != nil {
		return Value{}, err
	}
	available := make(map[string]int, len(declared))
	for position, field := range declared {
		available[field.Name] = indexes[position]
	}
	fields := make([]Value, len(typ.Fields))
	for i, wanted := range typ.Fields {
		index, ok := available[wanted.Name]
		if !ok {
			return Value{}, fmt.Errorf("%s has no field %q for %s", value.Type(), wanted.Name, typ.Summary())
		}
		converted, err := outOfGo(registry, value.Field(index), wanted.Type)
		if err != nil {
			return Value{}, fmt.Errorf("field %q: %w", wanted.Name, err)
		}
		fields[i] = converted
	}
	return Record(typ, fields)
}

// structValue turns a Go struct into a record without a type in hand, which is
// what ToValue and Run's argument coercion need.
func structValue(value reflect.Value) (Value, error) {
	registry := (*Registry)(nil)
	fields, indexes, err := structFields(registry, value.Type())
	if err != nil {
		return Value{}, err
	}
	typ := RecordOf(fields...)
	values := make([]Value, len(indexes))
	for position, index := range indexes {
		converted, err := outOfGo(registry, value.Field(index), fields[position].Type)
		if err != nil {
			return Value{}, fmt.Errorf("field %s: %w", value.Type().Field(index).Name, err)
		}
		values[position] = converted
	}
	return Record(typ, values)
}
