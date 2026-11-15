package machine_test

import (
	"encoding/json"
	"testing"

	"github.com/nethinwei/funroute/internal/machine"
)

// A type's JSON is part of every artifact's digest, so its shape is fixed
// here byte for byte: the keys, their order, and what is left out.
func TestTypeJSONShape(t *testing.T) {
	t.Parallel()
	for want, typ := range map[string]machine.Type{
		`{"kind":"record","fields":[{"name":"fee","type":{"kind":"money"}},{"name":"n","type":{"kind":"int"}}]}`: machine.RecordOf(
			machine.FieldOf("fee", machine.MoneyType), machine.FieldOf("n", machine.IntType)),
		`{"kind":"enum","name":"channel","values":["adyen","stripe"]}`:    machine.EnumOf("channel", "stripe", "adyen"),
		`{"kind":"array","elem":{"kind":"dict","elem":{"kind":"ratio"}}}`: machine.ArrayOf(machine.DictOf(machine.RatioType)),
		`{"kind":"fxrate"}`:                      machine.FxRateType,
		`{"kind":"handle","name":"onnx.tensor"}`: machine.HandleOf("onnx.tensor"),
		`{"kind":"currency"}`:                    machine.CurrencyType,
		`{"kind":"int"}`:                         machine.IntType,
	} {
		t.Run(want, func(t *testing.T) {
			t.Parallel()
			encoded, err := json.Marshal(typ)
			if err != nil || string(encoded) != want {
				t.Fatalf("json.Marshal(%s) = %s, %v, want %s", typ, encoded, err, want)
			}
			var back machine.Type
			if err := json.Unmarshal(encoded, &back); err != nil || !back.Equal(typ) {
				t.Fatalf("json.Unmarshal(%s) = %s, %v, want it back", encoded, back, err)
			}
		})
	}
}

// A type read from JSON is concrete only with the parts its kind has: an
// unknown kind, an int with an element or fields, an array without one.
func TestATypeFromJSONMustBeWellFormed(t *testing.T) {
	t.Parallel()
	for _, text := range []string{
		`{"kind":"invalid"}`, `{"kind":"int","elem":{"kind":"int"}}`, `{"kind":"int","fields":[{"name":"a","type":{"kind":"int"}}]}`,
		`{"kind":"array"}`, `{"kind":"money","name":"USD"}`, `{"kind":"fxrate","values":["USD","JPY"]}`, `{"kind":"var","name":"T"}`,
	} {
		var typ machine.Type
		if err := json.Unmarshal([]byte(text), &typ); err == nil && typ.IsConcrete() {
			t.Errorf("%s reads as a concrete type %s", text, typ)
		}
	}
	var typ machine.Type
	if err := json.Unmarshal([]byte(`{"kind":"array","elem":{"kind":"money"}}`), &typ); err != nil || !typ.IsConcrete() {
		t.Errorf("array<money> from JSON = %s, %v, want a concrete type", typ, err)
	}
}

// Every kind has a name of its own, and a kind written as a bare name reads
// back as itself. A kind added before kindCount without a name fails here.
func TestEveryKindHasANameThatReadsBack(t *testing.T) {
	t.Parallel()
	// The kinds no type is written as by name alone: they have parts or a
	// name of their own, or are no type a program writes.
	notBare := map[machine.Kind]bool{
		machine.InvalidKind: true, machine.VarKind: true, machine.ArrayKind: true, machine.DictKind: true,
		machine.HandleKind: true, machine.EnumKind: true, machine.RecordKind: true,
	}
	seen := map[string]machine.Kind{}
	for _, kind := range machine.Kinds() {
		name := kind.String()
		if other, taken := seen[name]; name == "" || taken {
			t.Errorf("kind %d is named %q, which is empty or %s's too", kind, name, other)
			continue
		}
		seen[name] = kind
		if notBare[kind] {
			continue
		}
		if typ, err := machine.ParseType(name); err != nil || typ.Kind() != kind {
			t.Errorf("ParseType(%q) = %v, %v, want the %s kind", name, typ, err, name)
		}
	}
}
