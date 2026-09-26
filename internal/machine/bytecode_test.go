package machine_test

import (
	"encoding/json"
	"testing"

	"github.com/nethinwei/funroute/internal/compile"
	"github.com/nethinwei/funroute/internal/machine"
	"github.com/nethinwei/funroute/internal/money"
)

// snapshotRegistry is a small money console whose artifacts, manifest and
// catalog are fixed as snapshots.
func snapshotRegistry(t *testing.T) *machine.Registry {
	t.Helper()
	registry := machine.CoreRegistry()
	err := registry.DeclareMoney(money.MoneySpec{Currencies: []money.CurrencySpec{
		{Code: "USD", Digits: 2}, {Code: "JPY", Digits: 0},
	}})
	if err != nil {
		t.Fatal(err)
	}
	return registry
}

// artifactSnapshot is an artifact with constants, calls, a conversion at
// the quotes of an argument, a record, a local and a money stamp, as JSON:
// the digest covers all of it.
const artifactSnapshot = `{"version":3,"digest":"sha256:b833c8a1b710e24e367afb3eb554705f5aadce5ab45ccde68c4ef5025f9bbe0f","expr_json":{"version":1,"expr":{"node":"let","bindings":[{"name":"x","value":{"node":"record","fields":[{"name":"fee","value":{"node":"call","name":"round","args":[{"node":"call","name":"mul","args":[{"node":"var","name":"amount"},{"node":"ratio","value":"2.9","unit":"%"}]},{"node":"enum","member":"half_even"}]}},{"name":"yen","value":{"node":"using","quotes":[{"node":"var","name":"rates"}],"body":{"node":"call","name":"round","args":[{"node":"call","name":"convert","args":[{"node":"var","name":"amount"},{"node":"currency","code":"JPY"}]},{"node":"enum","member":"half_even"}]}}}]}}],"body":{"node":"array","items":[{"node":"field","value":{"node":"var","name":"x"},"field":"fee"},{"node":"money","currency":"USD","amount":"1"}]}}},"args":[{"name":"amount","type":{"kind":"money"},"doc":"金额"},{"name":"rates","type":{"kind":"array","elem":{"kind":"fxrate"}},"doc":"报价"}],"result":{"kind":"array","elem":{"kind":"money"}},"constants":[{"type":{"kind":"ratio"},"value":"0.029"},{"type":{"kind":"enum","name":"rounding","values":["ceiling","down","floor","half_down","half_even","half_up","up"]},"value":"half_even"},{"type":{"kind":"currency"},"value":"JPY"},{"type":{"kind":"enum","name":"rounding","values":["ceiling","down","floor","half_down","half_even","half_up","up"]},"value":"half_even"},{"type":{"kind":"money"},"value":{"currency":"USD","minor":100}}],"calls":[{"name":"mul","signature":"mul(money,ratio)-\u003emoney"},{"name":"round","signature":"round(money,enum\u003crounding\u003e{ceiling,down,floor,half_down,half_even,half_up,up})-\u003emoney"},{"name":"convert","signature":"convert(money,currency)-\u003emoney"}],"locals":1,"instructions":[{"op":"load_arg"},{"op":"const"},{"op":"call","b":2,"type":{"kind":"money"}},{"op":"const","a":1},{"op":"call","a":1,"b":2,"type":{"kind":"money"}},{"op":"load_arg","a":1},{"op":"fx_push","b":1},{"op":"load_arg"},{"op":"const","a":2},{"op":"call","a":2,"b":2,"type":{"kind":"money"}},{"op":"const","a":3},{"op":"call","a":1,"b":2,"type":{"kind":"money"}},{"op":"fx_pop"},{"op":"make_record","a":2,"type":{"kind":"record","fields":[{"name":"fee","type":{"kind":"money"}},{"name":"yen","type":{"kind":"money"}}]}},{"op":"store_local"},{"op":"load_local"},{"op":"field","type":{"kind":"money"}},{"op":"const","a":4},{"op":"make_array","a":2,"type":{"kind":"array","elem":{"kind":"money"}}}],"money":{"currencies":[{"code":"JPY","digits":0},{"code":"USD","digits":2}]}}`

// An artifact's JSON is what is stored and what its digest covers, so it is
// fixed byte for byte, and it reads back to itself.
func TestArtifactJSONShape(t *testing.T) {
	t.Parallel()
	registry := snapshotRegistry(t)
	amount, _ := machine.ParseType("money")
	artifact, err := compile.CompileExpr(`let(x = {fee: round(amount * 2.9%, @half_even), yen: using(rates, round(amount -> JPY, @half_even))}, [x.fee, USD 1])`, registry, compile.CompileOptions{
		Args: []compile.ArgSpec{{Name: "amount", Type: amount, Doc: "金额"}, {Name: "rates", Type: machine.ArrayOf(machine.FxRateType), Doc: "报价"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(artifact)
	if err != nil || string(encoded) != artifactSnapshot {
		t.Fatalf("json.Marshal(artifact) = %s, %v\nwant %s", encoded, err, artifactSnapshot)
	}
	var back machine.Artifact
	if err := json.Unmarshal(encoded, &back); err != nil {
		t.Fatal(err)
	}
	if again, _ := json.Marshal(&back); string(again) != artifactSnapshot {
		t.Fatalf("an artifact read back writes %s", again)
	}
	if _, err := machine.Instantiate(&back, registry); err != nil {
		t.Fatalf("Instantiate(the artifact read back) = %v", err)
	}
}

// Every value a program can hold as a constant reads back as itself, through
// the artifact's JSON: a constant is a value's type and its JSON, and holds
// each one exactly.
func TestEveryConstantRoundTripsExactly(t *testing.T) {
	t.Parallel()
	must := func(value machine.Value, err error) machine.Value {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	fees := machine.RecordOf(machine.FieldOf("fee", machine.MoneyType), machine.FieldOf("rate", machine.RatioType))
	for name, value := range map[string]machine.Value{
		"an int":                  machine.Int(-9007199254740993),
		"a float":                 machine.Float(0.1),
		"the smallest float":      machine.Float(5e-324),
		"a large float":           machine.Float(1.5e300),
		"a string":                machine.String("银行卡\n"),
		"a bool":                  machine.Bool(true),
		"dollars":                 machine.MoneyValue(-170, "USD"),
		"a currency-less zero":    machine.MoneyValue(0, ""),
		"the smallest amount":     machine.MoneyValue(-1<<63, "JPY"),
		"a ratio":                 machine.RatioValue(machine.NewRatio(9223372036854775807, 10_000_000_000)),
		"a third":                 machine.RatioValue(machine.NewRatio(1, 3)),
		"an exchange rate":        machine.FxRateValue(money.FxRateFrom(&money.Pair{Base: "USD", Quote: "JPY"}, machine.NewRatio(601, 4))),
		"a currency":              machine.CurrencyValue("KWD"),
		"an empty array of money": must(machine.Array(machine.MoneyType, nil)),
		"a dictionary of floats":  must(machine.Dict(machine.FloatType, map[string]machine.Value{"b": machine.Float(0.3), "a": machine.Float(2)})),
		"records in an array": must(machine.Array(fees, []machine.Value{
			must(machine.Record(fees, []machine.Value{machine.MoneyValue(1, "USD"), machine.RatioValue(machine.NewRatio(29, 1000))})),
		})),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			checkConstantRoundTrip(t, value)
		})
	}
}

// checkConstantRoundTrip interns value, writes the constant as an artifact
// does and reads it back.
func checkConstantRoundTrip(t *testing.T, value machine.Value) {
	t.Helper()
	constant, ok := machine.ConstantOf(value, value.Type())
	if !ok {
		t.Fatalf("%v has no constant form", value.Any())
	}
	encoded, err := json.Marshal(constant)
	if err != nil {
		t.Fatal(err)
	}
	var decoded machine.Constant
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	back, err := machine.ConstantValue(decoded)
	if err != nil || !back.Type().Equal(value.Type()) || !back.Equal(value) {
		t.Fatalf("the constant %s reads back as %v, %v, want %v", encoded, back.Any(), err, value.Any())
	}
}

// A constant that is not a value of its type is refused, not read as a zero.
func TestAConstantThatIsNotItsTypeIsRefused(t *testing.T) {
	t.Parallel()
	for name, document := range map[string]string{
		"no type":                  `{"value":1}`,
		"a float as an int":        `{"type":{"kind":"int"},"value":1.5}`,
		"money with no amount":     `{"type":{"kind":"money"},"value":{"currency":"USD"}}`,
		"an amount with no code":   `{"type":{"kind":"money"},"value":{"currency":"","minor":5}}`,
		"a ratio that is not one":  `{"type":{"kind":"ratio"},"value":"abc"}`,
		"a rate that is not whole": `{"type":{"kind":"fxrate"},"value":{"base":"USD","quote":"JPY"}}`,
		"no value":                 `{"type":{"kind":"int"}}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var constant machine.Constant
			if err := json.Unmarshal([]byte(document), &constant); err != nil {
				t.Fatal(err)
			}
			if value, err := machine.ConstantValue(constant); err == nil {
				t.Fatalf("the constant %s reads as %v, want a refusal", document, value.Any())
			}
		})
	}
}

// quickly are texts the quick way reads.
var quickly = map[string]bool{"true": true, "7": true, "-9223372036854775808": true, "1.5": true, "1e308": true, `"adyen"`: true, `"银行卡😀"`: true}

// A bool, int, float or string constant read straight from its JSON text is
// what the decoder and coerce read of it; what the quick way does not read,
// the decoder reads, with its own words.
func TestAScalarConstantReadsAsTheDecoderReadsIt(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		typ   machine.Type
		texts []string
	}{
		{machine.BoolType, []string{"true", "false", " true", "1"}},
		{machine.IntType, []string{"0", "-0", "7", "-9223372036854775808", "9223372036854775807", "9223372036854775808", "1e3", "1.0", "+1", "01", "-", "--1", "1-"}},
		{machine.FloatType, []string{"0", "-0", "1.5", "-2.25e-3", "1e308", "1e999", "5e-324", "NaN", "0x1p3", "1_0", "3", "1.", ".5", "1e", "1e+", "01.5", "-0.0e-0", "1E+2"}},
		{machine.StringType, []string{`""`, `"adyen"`, `"银行卡😀"`, `"a\"b"`, `"a\u0041"`, "\"\xff\"", "\"\t\""}},
	} {
		typ := test.typ
		for _, text := range test.texts {
			constant := machine.Constant{Type: typ, Value: json.RawMessage(text)}
			quick, ok, decoded, err := machine.ConstantBothWays(constant)
			if ok && (err != nil || !machine.Identical(quick, decoded)) {
				t.Errorf("%s %s: read quick as %v, the decoder %v, %v", typ, text, quick.Any(), decoded.Any(), err)
			}
			if quickly[text] && !ok {
				t.Errorf("%s %s: read the slow way, want the quick one", typ, text)
			}
		}
	}
}
