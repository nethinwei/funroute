package limits

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/nethinwei/funroute"
	"github.com/nethinwei/funroute/extensions/std"
)

var update = flag.Bool("update", false, "write the generated tables into docs/limits.md")

const document = "../../docs/limits.md"

// The tables of docs/limits.md, each run into its region. It reads and may
// write one file, so it runs alone.
func TestTheLimitsAreTheDocuments(t *testing.T) {
	registry := newRegistry(t)
	tables := map[string]string{}
	for name, cases := range probeTables {
		tables[name] = probeTable(t, registry, cases)
	}
	for name, build := range computedTables {
		tables[name] = build(t, registry)
	}
	text, err := os.ReadFile(document)
	if err != nil {
		t.Fatal(err)
	}
	written, err := fill(string(text), tables)
	if err != nil {
		t.Fatal(err)
	}
	if *update {
		if err := os.WriteFile(document, []byte(written), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	if written != string(text) {
		t.Fatalf("docs/limits.md says other than a run does: run make limits and read the diff\n%s", firstDifference(string(text), written))
	}
}

func newRegistry(t *testing.T) *funroute.Registry {
	t.Helper()
	registry := funroute.CoreRegistry()
	if err := registry.EnableForm(funroute.SwitchForm, funroute.ForForm, funroute.ReduceForm); err != nil {
		t.Fatal(err)
	}
	if err := registry.DeclareMoney(funroute.MoneySpec{Currencies: []funroute.CurrencySpec{
		{Code: "USD", Digits: 2}, {Code: "JPY", Digits: 0}, {Code: "KWD", Digits: 3}, {Code: "BTC", Digits: 8}, {Code: "EUR", Digits: 2},
	}}); err != nil {
		t.Fatal(err)
	}
	if err := std.Register(registry); err != nil {
		t.Fatal(err)
	}
	return registry
}

// fill puts each table into its region, and refuses a region with no table
// or a table with no region: the file and this package name the same ones.
func fill(text string, tables map[string]string) (string, error) {
	for name, table := range tables {
		begin, end := "\n<!-- limits:"+name+" -->\n", "<!-- /limits:"+name+" -->"
		before, rest, found := strings.Cut(text, begin)
		_, after, closed := strings.Cut(rest, end)
		if !found || !closed {
			return "", fmt.Errorf("docs/limits.md has no region %q", name)
		}
		text = before + begin + table + end + after
	}
	// A region's mark stands at the start of its line; prose may name one.
	if extra := strings.Count(text, "\n<!-- limits:"); extra != len(tables) {
		return "", fmt.Errorf("docs/limits.md has %d regions, this package makes %d tables", extra, len(tables))
	}
	return text, nil
}

// firstDifference shows the first line the two texts differ on.
func firstDifference(have, want string) string {
	haveLines, wantLines := strings.Split(have, "\n"), strings.Split(want, "\n")
	for i := range min(len(haveLines), len(wantLines)) {
		if haveLines[i] != wantLines[i] {
			return fmt.Sprintf("line %d\n  the file: %s\n  a run:    %s", i+1, haveLines[i], wantLines[i])
		}
	}
	return "the texts differ in length"
}

// errorName is the name of err's most specific class, as a host tells them
// apart with errors.Is.
func errorName(err error) string {
	for _, class := range []struct {
		name string
		err  error
	}{
		{"ErrUnavailable", funroute.ErrUnavailable}, {"ErrNoFxRate", funroute.ErrNoFxRate}, {"ErrCurrency", funroute.ErrCurrency},
		{"ErrArithmetic", funroute.ErrArithmetic}, {"ErrDomain", funroute.ErrDomain}, {"ErrContract", funroute.ErrContract},
		{"ErrCompile", funroute.ErrCompile}, {"ErrDeadline", funroute.ErrDeadline},
		{"ErrExtension", funroute.ErrExtension},
	} {
		if errors.Is(err, class.err) {
			return class.name
		}
	}
	return "无类别"
}
