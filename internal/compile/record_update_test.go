package compile

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/nethinwei/funroute/internal/machine"
)

func runRecord(t *testing.T, source, contract string, args map[string]any) string {
	t.Helper()
	artifact := compileText(t, source, contract)
	runtime, err := machine.Instantiate(artifact, consoleRegistry(t))
	if err != nil {
		t.Fatal(err)
	}
	value, err := runtime.Run(t.Context(), args)
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
	got := runRecord(t, `r with {channel: @stripe, customer: r.customer with {amount: r.customer.amount - 30}}`, basket, args)
	if want := `{"customer":{"amount":1170,"currency":"SGD"},"tag":"vip","channel":"stripe"}`; got != want {
		t.Fatalf("value = %s, want %s", got, want)
	}
}

// Updating a record the compiler can see whole is folded like any other
// closed expression: one constant, no record_with at run time.
func TestRecordUpdateFolds(t *testing.T) {
	t.Parallel()
	artifact := compileText(t, `let(o = {amount: 1, currency: "SGD"}, o with {amount: o.amount + 1})`, `int`)
	if len(machine.PartsOf(artifact).Instructions) != 1 || machine.PartsOf(artifact).Instructions[0].Op != machine.OpConstant {
		t.Fatalf("instructions = %v, want one constant", machine.PartsOf(artifact).Instructions)
	}
}

func TestRecordUpdateKeepsTheType(t *testing.T) {
	t.Parallel()
	const order = `record{amount: int, currency: string}`
	for source, want := range map[string]string{
		`r with {fee: 1}`:         `has no field "fee" to update`,
		`r with {amount: 1.5}`:    `field "amount" is int, and an update keeps its type`,
		`r.amount with {fee: 1}`:  "not a record with a known type",
		`[r][0] with {amount: r}`: `field "amount" is int`,
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
	artifact := compileText(t, `r with {amount: 1}`, `record{amount: int, currency: string}`)
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
	parts := machine.PartsOf(artifact)
	parts.Instructions = slices.Clone(parts.Instructions)
	for i, instruction := range parts.Instructions {
		if instruction.Op == machine.OpRecordWith {
			parts.Instructions[i].Keys = fields
		}
	}
	tampered, err := machine.SealArtifact(parts, nil)
	if err != nil {
		t.Fatal(err)
	}
	return tampered
}

// An update puts in money of any currency; where it meets money of another
// currency later, that operation refuses it.
func TestAnUpdateKeepsTheFieldsCurrency(t *testing.T) {
	t.Parallel()
	_, err := runMoney(t, "let(r = {fee: a}, r with {fee: money(minor, cur)}).fee + a", "a: money; minor: int; cur: currency",
		map[string]any{"a": "EUR 2.00", "minor": 100, "cur": "USD"})
	if !errors.Is(err, machine.ErrCurrency) {
		t.Fatalf("a dollar put into a euro field: error = %v, want ErrCurrency", err)
	}
}

// updateContract is a record with money, a rate and a count, and two records
// of money alone.
const updateContract = "r: record{fee: money, rt: ratio, n: int}; q: record{fee: money}; w: record{fee: money}; " +
	"usd: money; eur: money; u: money; a: money; risk: float"

// An update keeps the record's type: a zero or a decimal takes the field's
// kind, and money of any currency goes into a money field.
func TestUpdatingMoneyFields(t *testing.T) {
	t.Parallel()
	for _, source := range []string{
		"r with {fee: r.fee * 2}",
		"r with {rt: 2.9%, fee: round(r.fee * 2.9%, @half_even)}",
		"r with {fee: 0}",
		"r with {rt: 0.5}",
		"r with {n: 0}",
		"r with {fee: u}",
		"r with {fee: r.fee + u}",
		"r with {fee: if(risk > 0.5, usd, eur)}",
		"q with {fee: usd}",
		"q with {fee: 0}",
		"w with {fee: a}",
		"w with {fee: money(5, currency(w.fee))}",
		"w with {fee: round(w.fee * 1.5%, @half_even)}",
		"w with {fee: u}",
		"r with {fee: u, rt: 1%}",
	} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			artifact, err := compileMoney(t, source, updateContract, "")
			if err != nil {
				t.Fatalf("CompileExpr(%q) error = %v", source, err)
			}
			if base := argumentType(artifact, source[:1]); !artifact.Result().Equal(base) {
				t.Fatalf("CompileExpr(%q) result %s, want the updated record's type", source, artifact.Result())
			}
		})
	}
}

// argumentType is the type the artifact gives the argument name.
func argumentType(artifact *machine.Artifact, name string) machine.Type {
	for _, param := range artifact.Args() {
		if param.Name() == name {
			return param.Type()
		}
	}
	return machine.Type{}
}

// A new value proven in another currency, or not money at all, is refused.
func TestUpdatingMoneyFieldsRefusesOtherKinds(t *testing.T) {
	t.Parallel()
	for source, want := range map[string]string{
		"r with {fee: 5}":    `field "fee" is money, and an update keeps its type`,
		"r with {fee: risk}": `field "fee" is money`,
		"r with {rt: risk}":  `field "rt" is ratio`,
		"r with {rt: r.fee}": `field "rt" is ratio`,
		"r with {n: r.fee}":  `field "n" is int`,
	} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			_, err := compileMoney(t, source, updateContract, "")
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("CompileExpr(%q) error = %v, want %q", source, err, want)
			}
		})
	}
}

// What an update puts in is what comes out, a checked value included.
func TestUpdatingMoneyFieldsRuns(t *testing.T) {
	t.Parallel()
	args := func(extra map[string]any) map[string]any {
		all := map[string]any{
			"r": map[string]any{"fee": "USD 1.00", "rt": "0.01", "n": 2}, "q": map[string]any{"fee": "JPY 5"},
			"w": map[string]any{"fee": "EUR 2.00"}, "usd": "USD 3.00", "eur": "EUR 3.00", "u": "USD 4.00", "a": "EUR 1.00", "risk": 0.1,
		}
		maps.Copy(all, extra)
		return all
	}
	for _, test := range []struct {
		source string
		extra  map[string]any
		want   string // "" for ErrCurrency
	}{
		{"(r with {fee: r.fee * 2}).fee", nil, "{USD 200}"},
		{"(r with {rt: 2.9%, fee: round(r.fee * 2.9%, @half_even)}).fee", nil, "{USD 3}"},
		{"(r with {fee: u}).fee", nil, "{USD 400}"},
		{"(r with {fee: u}).fee", map[string]any{"u": "EUR 4.00"}, "{EUR 400}"},
		{"(r with {fee: if(risk > 0.5, usd, eur)}).fee", map[string]any{"risk": 0.9}, "{USD 300}"},
		{"(r with {fee: if(risk > 0.5, usd, eur)}).fee", nil, "{EUR 300}"},
		{"(q with {fee: usd}).fee", nil, "{USD 300}"},
		{"(w with {fee: round(w.fee * 1.5%, @half_even)}).fee", nil, "{EUR 3}"},
		{"(w with {fee: u}).fee", map[string]any{"u": "EUR 4.00"}, "{EUR 400}"},
		{"(r with {rt: 0.5}).rt", nil, "0.5"},
		{"minor((r with {fee: 0}).fee)", nil, "0"},
		{"(r with {fee: 0}).fee", nil, "{ 0}"},
		{"r with {fee: 0}", nil, "map[fee:{ 0} n:2 rt:0.01]"},
	} {
		t.Run(fmt.Sprint(test.source, test.extra), func(t *testing.T) {
			t.Parallel()
			got, err := runMoney(t, test.source, updateContract, args(test.extra))
			if test.want == "" && !errors.Is(err, machine.ErrCurrency) {
				t.Fatalf("%s = %s, %v, want ErrCurrency", test.source, got, err)
			}
			if test.want != "" && (err != nil || got != test.want) {
				t.Fatalf("%s = %s, %v, want %s", test.source, got, err, test.want)
			}
		})
	}
}
