package compile

import (
	"errors"
	"maps"
	"strings"
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

const orderContract = "x: int; f: bool; ba: record{b: int, a: int}; ab: record{a: int, b: int}"

// A record literal's fields are in the order they were written only until
// it meets a record with the same fields: then it takes that record's order,
// and every value lands in its own field. A record type's order is still
// the type: two declared records in two orders do not meet.
func TestALiteralTakesTheOrderOfTheRecordItMeets(t *testing.T) {
	t.Parallel()
	args := func(x int64) map[string]any {
		return map[string]any{"x": x, "f": x > 1, "ba": map[string]any{"b": 20, "a": 10}, "ab": map[string]any{"a": 30, "b": 40}}
	}
	for _, test := range []struct {
		source, result string
		x              int64
		want           string
	}{
		{"switch(case x > 1 => {a: 1, b: 2}, else => {b: 3, a: 4})", "", 2, "map[a:1 b:2]"},
		{"switch(case x > 1 => {a: 1, b: 2}, else => {b: 3, a: 4})", "", 0, "map[a:4 b:3]"},
		{"if(f, {a: 1, b: 2}, {b: 3, a: 4}).a", "", 0, "4"},
		// Two literals meet first; the declared record after them settles
		// the order, and it reaches both.
		{"switch(case x > 1 => {a: 1, b: 2}, case x > 0 => {b: 3, a: 4}, else => ba)", "", 1, "map[a:4 b:3]"},
		{"switch(case x > 1 => {a: 1, b: 2}, case x > 0 => {b: 3, a: 4}, else => ba)", "", 0, "map[a:10 b:20]"},
		{"[{a: 1, b: 2}, {b: 3, a: 4}, ab][1].a", "", 0, "4"},
		// The contract's result settles a literal's order too.
		{"{a: x, b: 2}", "record{b: int, a: int}", 5, "map[a:5 b:2]"},
		{"let(r = {b: 1, a: x}, if(f, r, ab)).a", "", 0, "30"},
	} {
		t.Run(test.source, func(t *testing.T) {
			t.Parallel()
			artifact, err := compileMoney(t, test.source, orderContract, test.result)
			if err != nil {
				t.Fatalf("CompileExpr(%q) error = %v", test.source, err)
			}
			if got, err := runArtifact(t, artifact, args(test.x)); err != nil || got != test.want {
				t.Fatalf("%s with x = %d = %s, %v, want %s", test.source, test.x, got, err, test.want)
			}
		})
	}
	if _, err := compileMoney(t, "if(f, ab, ba)", orderContract, ""); err == nil ||
		!strings.Contains(err.Error(), "字段相同但顺序不同") {
		t.Fatalf("two declared records in two orders: error = %v, want refused, saying the order differs", err)
	}
}

// A branch that does not return the type the others do is said where it is,
// with both types and, for two records, what tells them apart.
func TestABranchOfAnotherRecordSaysHowItDiffers(t *testing.T) {
	t.Parallel()
	for source, want := range map[string][]string{
		"switch(case x > 1 => {rate: 1, cost_plus: true}, else => {rate: 2})": {
			"switch default must match", "else returns record{rate: int}", "缺少字段 cost_plus"},
		"switch(case x > 1 => {rate: 1}, case x > 0 => {rate: 2, n: 1}, else => {rate: 3})": {
			"branch 2 returns", "多出字段 n"},
		"switch(case x > 1 => {rate: 1}, else => {rate: true})": {
			"字段 rate 的类型不同：int 与 bool"},
		"if(f, {rate: 1, cost_plus: true}, {rate: 2})": {
			"no overload if", "else 分支与 then 分支相比：缺少字段 cost_plus"},
		"switch(case x > 1 => 1, else => true)": {
			"else returns bool, the branches before it int"},
	} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			_, err := compileMoney(t, source, orderContract, "")
			expectErrorSays(t, source, err, want)
		})
	}
}

// expectErrorSays requires err to say every part of want.
func expectErrorSays(t *testing.T, source string, err error, want []string) {
	t.Helper()
	for _, part := range want {
		if err == nil || !strings.Contains(err.Error(), part) {
			t.Fatalf("CompileExpr(%q) error = %v, want one containing %q", source, err, part)
		}
	}
}
