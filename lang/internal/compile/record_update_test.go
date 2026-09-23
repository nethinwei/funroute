package compile

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"funroute/lang/internal/machine"
)

func runRecord(t *testing.T, source, contract string, args map[string]any) string {
	t.Helper()
	artifact := compileText(t, source, contract)
	runtime, err := machine.Instantiate(artifact, consoleRegistry(t))
	if err != nil {
		t.Fatal(err)
	}
	value, err := runtime.Run(t.Context(), args, machine.RunOptions{Fuel: 10_000})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

func compileText(t *testing.T, source, contract string) *machine.Artifact {
	t.Helper()
	typ, err := machine.ParseType(contract)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := CompileExpr(source, consoleRegistry(t), CompileOptions{Args: []ArgSpec{{Name: "r", Type: typ}}})
	if err != nil {
		t.Fatal(err)
	}
	return artifact
}

// An update keeps the record's type and field order, whatever order the new
// values are written in, and nests by nesting.
func TestRecordUpdateReplacesFieldsInPlace(t *testing.T) {
	t.Parallel()
	const basket = `record{customer: record{amount: int, currency: string}, tag: string, channel: enum<channel>{adyen,stripe}}`
	args := map[string]any{"r": map[string]any{
		"customer": map[string]any{"amount": 1200, "currency": "SGD"}, "tag": "vip", "channel": "adyen",
	}}
	got := runRecord(t, `{...r, channel: @stripe, customer: {...r.customer, amount: r.customer.amount - 30}}`, basket, args)
	if want := `{"customer":{"amount":1170,"currency":"SGD"},"tag":"vip","channel":"stripe"}`; got != want {
		t.Fatalf("value = %s, want %s", got, want)
	}
}

// Updating a record the compiler can see whole is folded like any other
// closed expression: one constant, no record_with at run time.
func TestRecordUpdateFolds(t *testing.T) {
	t.Parallel()
	artifact := compileText(t, `let(o = {amount: 1, currency: "SGD"}, {...o, amount: o.amount + 1})`, `int`)
	if len(artifact.Instructions) != 1 || artifact.Instructions[0].Op != machine.OpConstant {
		t.Fatalf("instructions = %v, want one constant", artifact.Instructions)
	}
}

func TestRecordUpdateKeepsTheType(t *testing.T) {
	t.Parallel()
	const order = `record{amount: int, currency: string}`
	for source, want := range map[string]string{
		`{...r, fee: 1}`:         `has no field "fee" to update`,
		`{...r, amount: 1.5}`:    `field "amount" is int, and an update keeps its type`,
		`{...r.amount, fee: 1}`:  "not a record with a known type",
		`{...[r][0], amount: r}`: `field "amount" is int`,
	} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			typ, _ := machine.ParseType(order)
			_, err := CompileExpr(source, consoleRegistry(t), CompileOptions{Args: []ArgSpec{{Name: "r", Type: typ}}})
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Errorf("%s: error = %v, want %q", source, err, want)
			}
		})
	}
}

// The loader checks the instruction as it checks every other: a field the
// record does not have, or one replaced twice, is refused before anything runs.
func TestRecordWithIsValidatedOnLoad(t *testing.T) {
	t.Parallel()
	artifact := compileText(t, `{...r, amount: 1}`, `record{amount: int, currency: string}`)
	for _, fields := range [][]string{{"fee"}, {"amount", "amount"}, {}} {
		t.Run(fmt.Sprint(fields), func(t *testing.T) {
			t.Parallel()
			tampered := withRecordWithKeys(t, artifact, fields)
			if _, err := machine.Instantiate(tampered, consoleRegistry(t)); err == nil {
				t.Errorf("fields %v loaded, want Instantiate to refuse them", fields)
			}
		})
	}
}

// withRecordWithKeys is a copy of artifact whose record_with instructions
// replace fields instead, sealed with a digest that matches.
func withRecordWithKeys(t *testing.T, artifact *machine.Artifact, fields []string) *machine.Artifact {
	t.Helper()
	tampered := *artifact
	tampered.Instructions = append([]machine.Instruction(nil), artifact.Instructions...)
	for i, instruction := range tampered.Instructions {
		if instruction.Op == machine.OpRecordWith {
			tampered.Instructions[i].Keys = fields
		}
	}
	digest, err := machine.ArtifactDigest(&tampered)
	if err != nil {
		t.Fatal(err)
	}
	tampered.Digest = digest
	return &tampered
}
