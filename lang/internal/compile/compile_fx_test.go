package compile

import (
	"strings"
	"testing"

	"funroute/lang/internal/machine"
)

// A using pushes its quotes, opens the scope over them and closes it after
// the body.
func TestCompileUsingOpensAndClosesTheScope(t *testing.T) {
	t.Parallel()
	source := "using(170 JPY / USD, fx, y / a, a -> JPY)"
	artifact, err := compileMoney(t, source, "a: money<USD>; y: money<JPY>; fx: fxrate<USD,JPY>", "")
	if err != nil {
		t.Fatal(err)
	}
	var ops []string
	for _, in := range machine.PartsOf(artifact).Instructions {
		ops = append(ops, in.Op.String())
		if in.Op == machine.OpFxPush && (in.A != 0 || in.B != 3 || in.C != 0 || in.Keys != nil) {
			t.Errorf("%s: fx_push = %+v, want B 3 and nothing else", source, in)
		}
	}
	want := "const load_arg load_arg load_arg call fx_push load_arg const call fx_pop"
	if got := strings.Join(ops, " "); got != want {
		t.Fatalf("%s compiles to %s, want %s", source, got, want)
	}
}

// using(@name, …) names one of the contract's rate tables: it compiles to a
// fx_push that names it and pushes only the quotes, and the artifact records
// the tables the contract declares. A name the contract does not declare,
// or an enum member that is no table, does not compile.
func TestUsingANamedRateTable(t *testing.T) {
	t.Parallel()
	options := CompileOptions{Args: moneyContract(t, "a: money<USD>"), RateTables: []string{"settlement", "market"}}
	artifact, err := CompileExpr("using(@settlement, 151 JPY / USD, a -> JPY)", moneyRegistry(t), options)
	if err != nil {
		t.Fatal(err)
	}
	var push *machine.Instruction
	for _, in := range machine.PartsOf(artifact).Instructions {
		if in.Op == machine.OpFxPush {
			push = &in
		}
	}
	if push == nil || push.A != 0 || push.B != 1 || push.C != 0 || strings.Join(push.Keys, ",") != "settlement" {
		t.Fatalf("fx_push = %+v, want one quote over the table settlement", push)
	}
	if got := strings.Join(artifact.RateTables(), ","); got != "settlement,market" {
		t.Fatalf("RateTables() = %s, want settlement,market", got)
	}
	for source, want := range map[string]string{
		"using(@ledger, a -> JPY)":  "not a member",
		"using(@half_up, a -> JPY)": "is none",
	} {
		if _, err := CompileExpr(source, moneyRegistry(t), options); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("CompileExpr(%q) error = %v, want one saying %s", source, err, want)
		}
	}
}

// A contract's rate tables are names written once, and they need money.
func TestRateTablesAreCheckedWithTheContract(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		tables   []string
		registry *machine.Registry
		want     string
	}{
		{[]string{"a b"}, moneyRegistry(t), "invalid rate table name"},
		{[]string{"t", "t"}, moneyRegistry(t), "declared twice"},
		{[]string{"t"}, machine.CoreRegistry(), "need a registry that declares money"},
	} {
		_, err := CompileExpr("1", test.registry, CompileOptions{RateTables: test.tables})
		if err == nil || !strings.Contains(err.Error(), test.want) {
			t.Errorf("RateTables %v: error = %v, want one saying %s", test.tables, err, test.want)
		}
	}
	if _, err := CompileExpr("x", moneyRegistry(t), CompileOptions{Args: []ArgSpec{{Name: "x", Type: machine.EnumOf("rate_table", "a")}}}); err == nil {
		t.Fatal("a contract enum named rate_table compiled, want the name refused")
	}
}
