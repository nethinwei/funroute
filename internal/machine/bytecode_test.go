package machine_test

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
var update = flag.Bool("update", false, "write the JSON snapshots anew into testdata/")

// expectSnapshot holds encoded, a value's JSON, to the snapshot in
// testdata/name: the JSON indented, so a change reads as a diff of lines, and
// whatever the encoder writes — every byte of it — is in it. A difference is
// shown where it starts; make snapshots (go test -update) writes the files
// anew, and the diff is then read like any other.
func expectSnapshot(t *testing.T, name string, encoded []byte) {
	t.Helper()
	var indented bytes.Buffer
	if err := json.Indent(&indented, encoded, "", "  "); err != nil {
		t.Fatal(err)
	}
	indented.WriteByte('\n')
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.WriteFile(path, indented.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	stored, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v: make snapshots writes it", err)
	}
	if !bytes.Equal(stored, indented.Bytes()) {
		t.Fatalf("%s is not what this build writes (make snapshots writes it anew; read the diff):\n%s",
			path, firstDifference(string(stored), indented.String()))
	}
}

// firstDifference is the first line two texts differ at, both ways.
func firstDifference(stored, written string) string {
	storedLines, writtenLines := strings.Split(stored, "\n"), strings.Split(written, "\n")
	for i := range min(len(storedLines), len(writtenLines)) {
		if storedLines[i] != writtenLines[i] {
			return fmt.Sprintf("line %d\n  the snapshot: %s\n  this build:   %s", i+1, storedLines[i], writtenLines[i])
		}
	}
	return fmt.Sprintf("the snapshot has %d lines, this build %d", len(storedLines), len(writtenLines))
}

// An artifact's JSON is what is stored and what its digest covers, so it is
// fixed byte for byte (testdata/artifact.json), and it reads back to itself.
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
	if err != nil {
		t.Fatal(err)
	}
	expectSnapshot(t, "artifact.json", encoded)
	var back machine.Artifact
	if err := json.Unmarshal(encoded, &back); err != nil {
		t.Fatal(err)
	}
	if again, _ := json.Marshal(&back); string(again) != string(encoded) {
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

// A handle has no constant form, inside a container or a record as well as
// alone: a constexpr host function that returns one is called again at run
// time, and the artifact always loads.
func TestAHandleInsideAValueIsNoConstant(t *testing.T) {
	t.Parallel()
	handle := machine.HandleOf("demo.token")
	held := machine.RecordOf(machine.FieldOf("h", handle), machine.FieldOf("n", machine.IntType))
	registry := machine.CoreRegistry()
	for name, result := range map[string]machine.Type{"t.held_v1": held, "t.tokens_v1": machine.ArrayOf(handle)} {
		err := registry.Register(machine.FunctionSpec{
			Name: name, Result: result, Doc: machine.Doc{Constexpr: true},
			Eval: func(context.Context, []machine.Value) (machine.Value, error) {
				token := machine.NewHandle("demo.token", "secret")
				if result.Kind() == machine.ArrayKind {
					return machine.Array(handle, []machine.Value{token})
				}
				return machine.Record(held, []machine.Value{token, machine.Int(1)})
			},
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, source := range []string{"t.held_v1()", "t.tokens_v1()", "t.held_v1().n"} {
		artifact, err := compile.CompileExpr(source, registry, compile.CompileOptions{})
		if err != nil {
			t.Fatalf("CompileExpr(%s): %v", source, err)
		}
		if _, err := machine.Instantiate(artifact, registry); err != nil {
			t.Errorf("Instantiate(%s) = %v, want the artifact its compile made to load", source, err)
		}
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

// A scalar constant is written as json.Marshal writes its value, byte for
// byte: the digest covers every constant.
func TestAScalarConstantIsItsValuesJSON(t *testing.T) {
	t.Parallel()
	for _, value := range []machine.Value{
		machine.Int(0), machine.Int(-9223372036854775808), machine.Int(42), machine.Bool(true), machine.Bool(false),
		machine.String(""), machine.String("adyen"), machine.String("<a&b>"), machine.String("银行卡😀"), machine.String("\xff\t\"\\ "),
	} {
		constant, ok := machine.ConstantOf(value, value.Type())
		want, err := json.Marshal(value)
		if !ok || err != nil || string(constant.Value) != string(want) {
			t.Errorf("ConstantOf(%#v) = %s, %v; json.Marshal says %s, %v", value.Any(), constant.Value, ok, want, err)
		}
	}
}

// A constant's amounts are written without their places, and nothing else
// about it changes: a record keeps its type's field order, a field that is
// called digits keeps its value. The digest covers every constant, so the
// same rule compiles to the same artifact as before amounts carried places.
// The wants are written out, not read back through ConstantOf.
func TestAConstantsAmountsAreWrittenWithoutTheirPlaces(t *testing.T) {
	t.Parallel()
	usd, err := machine.ToValue(money.MakeIn("USD", 170, 3))
	if err != nil {
		t.Fatal(err)
	}
	priced := machine.RecordOf(machine.FieldOf("zeta", machine.MoneyType), machine.FieldOf("alpha", machine.IntType))
	labelled := machine.RecordOf(machine.FieldOf("m", machine.MoneyType), machine.FieldOf("digits", machine.IntType))
	built := func(value machine.Value, err error) machine.Value {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	record := built(machine.Record(priced, []machine.Value{usd, machine.Int(1)}))
	for want, value := range map[string]machine.Value{
		`{"currency":"USD","minor":170}`:                          usd,
		`{"zeta":{"currency":"USD","minor":170},"alpha":1}`:       record,
		`[{"zeta":{"currency":"USD","minor":170},"alpha":1}]`:     built(machine.Array(priced, []machine.Value{record})),
		`[{"currency":"USD","minor":170}]`:                        built(machine.Array(machine.MoneyType, []machine.Value{usd})),
		`{"k":{"currency":"USD","minor":170}}`:                    built(machine.Dict(machine.MoneyType, map[string]machine.Value{"k": usd})),
		`{"m":{"currency":"USD","minor":170},"digits":2}`:         built(machine.Record(labelled, []machine.Value{usd, machine.Int(2)})),
		`{"k":{"zeta":{"currency":"USD","minor":170},"alpha":1}}`: built(machine.Dict(priced, map[string]machine.Value{"k": record})),
	} {
		constant, ok := machine.ConstantOf(value, value.Type())
		if !ok || string(constant.Value) != want {
			t.Errorf("ConstantOf(%s) = %s, %v; want %s", value.Type(), constant.Value, ok, want)
		}
	}
}
