package compile

import (
	"errors"
	"maps"
	"testing"

	"github.com/nethinwei/funroute/internal/machine"
)

const recordContract = "usd: money; eur: money; u: money; f: bool"

// A record's money follows the rules money follows on its own: a known
// currency flows into a record declared as money, a zero into any currency,
// one known only at run time into money with a check where it arrives,
// and a proven other currency is refused.
func TestARecordResultFollowsTheScalarRules(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		source, result string
		args           map[string]any
		want           string
	}{
		{"{fee: 0}", "record{fee: money}", nil, "map[fee:{ 0}]"},
		{"{fee: usd}", "record{fee: money}", map[string]any{"usd": "USD 1.00"}, "map[fee:{USD 100}]"},
		{"{fee: USD 1}", "record{fee: money}", nil, "map[fee:{USD 100}]"},
		{"{fee: u}", "record{fee: money}", map[string]any{"u": "USD 2.00"}, "map[fee:{USD 200}]"},
		{"{fee: [usd, 0]}", "record{fee: array<money>}", map[string]any{"usd": "USD 1.00"}, "map[fee:[{USD 100} { 0}]]"},
		{"{fee: {inner: 0}}", "record{fee: record{inner: money}}", nil, "map[fee:map[inner:{ 0}]]"},
	} {
		t.Run(test.source+" as "+test.result, func(t *testing.T) {
			t.Parallel()
			checkRecordRun(t, test.source, test.result, test.args, test.want)
		})
	}
}

func checkRecordRun(t *testing.T, source, result string, args map[string]any, want string) {
	t.Helper()
	artifact, err := compileMoney(t, source, recordContract, result)
	if err != nil {
		t.Fatalf("CompileExpr(%q) as %s error = %v", source, result, err)
	}
	full := map[string]any{"usd": "USD 0.00", "eur": "EUR 0.00", "u": "USD 0.00", "f": true}
	maps.Copy(full, args)
	if got, err := runArtifact(t, artifact, full); err != nil || got != want {
		t.Fatalf("%s as %s with %v = %s, %v, want %s", source, result, args, got, err, want)
	}
}

// Records of one shape meet where branches meet, their currencies becoming
// unknown the way money's do, and what reads them later is checked.
func TestRecordsOfDifferentCurrenciesMeetAtRunTime(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		source string
		f      bool
		want   string
	}{
		{"if(f, {fee: usd}, {fee: eur}).fee + usd", true, "{USD 200}"},
		{"fallback({fee: usd}, {fee: eur}).fee", true, "{USD 100}"},
		{"len([{fee: usd}, {fee: eur}])", true, "2"},
		{`switch(f, case true => {fee: usd}, else => {fee: eur}).fee`, false, "{EUR 100}"},
	} {
		t.Run(test.source, func(t *testing.T) {
			t.Parallel()
			got, err := runMoney(t, test.source, recordContract, map[string]any{"usd": "USD 1.00", "eur": "EUR 1.00", "u": "USD 0.00", "f": test.f})
			if err != nil || got != test.want {
				t.Fatalf("%s = %s, %v, want %s", test.source, got, err, test.want)
			}
		})
	}
	got, err := runMoney(t, "if(f, {fee: usd}, {fee: eur}).fee + usd", recordContract, map[string]any{"usd": "USD 1.00", "eur": "EUR 1.00", "u": "USD 0.00", "f": false})
	if !errors.Is(err, machine.ErrCurrency) {
		t.Fatalf("euros from the other branch plus dollars = %s, %v, want ErrCurrency", got, err)
	}
}

// Records of different shapes still do not meet: only currencies flow.
func TestRecordsOfDifferentShapesStillRefuse(t *testing.T) {
	t.Parallel()
	for _, source := range []string{"if(f, {fee: usd}, {cost: usd})", "if(f, {fee: usd}, {fee: 1})", "[{fee: usd}, {fee: usd, n: 1}]"} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			if _, err := compileMoney(t, source, recordContract, ""); err == nil {
				t.Fatalf("CompileExpr(%q) compiled, want records of different shapes refused", source)
			}
		})
	}
}
