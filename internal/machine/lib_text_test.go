package machine_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/nethinwei/funroute/internal/compile"
	"github.com/nethinwei/funroute/internal/machine"
	"github.com/nethinwei/funroute/internal/syntax"
)

// A position counts characters, and a part the text lacks is no position.
func TestAPartOfATextIsFoundByCharacters(t *testing.T) {
	t.Parallel()
	text := []compile.ArgSpec{{Name: "s", Type: machine.StringType}}
	args := map[string]any{"s": "支付-SG-01"}
	for source, want := range map[string]any{
		`index_of(s, "-")`:      int64(2),
		`last_index_of(s, "-")`: int64(5),
		`index_of(s, "")`:       int64(0),
		`trim_prefix(s, "支付")`:  "-SG-01",
		`trim_suffix(s, "-02")`: "支付-SG-01",
	} {
		if got, err := libRun(t, source, args, text...); err != nil || got != want {
			t.Errorf("%s = %v, %v; want %v", source, got, err, want)
		}
	}
	if _, err := libRun(t, `index_of(s, "US")`, args, text...); !errors.Is(err, machine.ErrDomain) {
		t.Errorf("a part the text lacks: %v, want ErrDomain", err)
	}
}

// A pattern is fixed where the rule is written and checked there; the text
// is capped, since RE2's time is linear in it.
func TestAPatternIsFixedWhenTheRuleCompiles(t *testing.T) {
	t.Parallel()
	specs := []compile.ArgSpec{{Name: "s", Type: machine.StringType}, {Name: "p", Type: machine.StringType}}
	for source, want := range map[string]string{
		`matches(s, p)`:    "fixed when the rule is compiled",
		`matches(s, "(a")`: `invalid pattern "(a"`,
	} {
		_, err := compile.CompileExpr(source, machine.CoreRegistry(), compile.CompileOptions{Args: specs})
		if line, column, ok := syntax.LineColumn(err, source); !ok || line != 1 || column != 12 || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v at %d:%d, want %q at 1:12", source, err, line, column, want)
		}
	}
	if got, err := libRun(t, `let(bin = "^4", matches(s, bin + "[0-9]{5}$"))`, map[string]any{"s": "411111", "p": ""}, specs...); err != nil || got != true {
		t.Errorf("a pattern that folds: %v, %v; want true", got, err)
	}
	long := map[string]any{"s": strings.Repeat("a", 10_001), "p": ""}
	if _, err := libRun(t, `matches(s, "a")`, long, specs...); !errors.Is(err, machine.ErrDomain) {
		t.Errorf("a text of 10001 bytes: %v, want ErrDomain", err)
	}
}

// A function names only arguments it has as ones to fix, and a manifest
// keeps them, so a registry built from one refuses what the host's does.
func TestConstArgsAreKeptByTheManifest(t *testing.T) {
	t.Parallel()
	pick := machine.FunctionSpec{Name: "t.pick", Go: func(s, key string) (string, error) { return s + key, nil }, Doc: machine.Doc{ConstArgs: []int{1}}}
	host := machine.CoreRegistry()
	if err := host.Register(pick); err != nil {
		t.Fatal(err)
	}
	pick.Name, pick.Doc.ConstArgs = "t.bad", []int{2}
	if err := host.Register(pick); err == nil {
		t.Fatal("ConstArgs past the parameters registered")
	}
	applied := machine.CoreRegistry()
	if err := host.Manifest().Apply(applied); err != nil {
		t.Fatal(err)
	}
	options := compile.CompileOptions{Args: []compile.ArgSpec{{Name: "s", Type: machine.StringType}, {Name: "k", Type: machine.StringType}}}
	for _, registry := range []*machine.Registry{host, applied} {
		if _, err := compile.CompileExpr(`t.pick(s, k)`, registry, options); err == nil {
			t.Fatal("an argument to fix was taken from the data")
		}
		if _, err := compile.CompileExpr(`t.pick(s, "k")`, registry, options); err != nil {
			t.Fatal(err)
		}
	}
}
