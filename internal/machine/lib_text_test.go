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

// A program's s[i], len(s) and slice(s, i, j) answer, and fail, word for
// word as the functions do: called alone, kept in a let and read twice,
// with computed positions, and down a text in a loop.
func TestTextOperationsInAProgramAnswerAsTheirFunctions(t *testing.T) {
	t.Parallel()
	texts, positions := []string{"", "a", "aé名", "\xffb𝄞"}, []int64{-1, 0, 1, 2, 5}
	cases := make([]textCut, 0, len(texts)*len(positions)*len(positions))
	for _, text := range texts {
		for _, i := range positions {
			for _, j := range positions {
				cases = append(cases, textCut{text, i, j})
			}
		}
	}
	for _, in := range cases {
		assertTextOperations(t, in)
	}
	specs := []compile.ArgSpec{{Name: "s", Type: machine.StringType}}
	got, err := libRun(t, `[slice(s, k, k + 1) + s[k] + slice(s, 0, k + 1) for k in range(len(s))]`, map[string]any{"s": "aé名"}, specs...)
	if want := []string{"aaa", "ééaé", "名名aé名"}; err != nil || !equalStrings(got, want) {
		t.Errorf("the loop down the text = %v, %v; want %v", got, err, want)
	}
}

// textCut is a text and two positions in it.
type textCut struct {
	text string
	i, j int64
}

// assertTextOperations runs the text operations on the text and the two
// positions, as s, i and j, and holds each to its function.
func assertTextOperations(t *testing.T, in textCut) {
	t.Helper()
	registry := machine.CoreRegistry()
	slice, _ := registry.Resolve("slice(string,int,int)->string")
	at, _ := registry.Resolve("at(string,int)->string")
	args := map[string]any{"s": in.text, "i": in.i, "j": in.j}
	text, i, j := machine.String(in.text), machine.Int(in.i), machine.Int(in.j)
	want, wantErr := slice.Eval(t.Context(), []machine.Value{text, i, j})
	char, charErr := at.Eval(t.Context(), []machine.Value{text, i})
	cut := textOf(want)
	specs := []compile.ArgSpec{{Name: "s", Type: machine.StringType}, {Name: "i", Type: machine.IntType}, {Name: "j", Type: machine.IntType}}
	for source, expected := range map[string]string{
		`slice(s, i, j)`: cut, `let(x = slice(s, i, j), x + x)`: cut + cut, `slice(s, i + 0, j * 1)`: cut, `s[i]`: textOf(char),
	} {
		failure := wantErr
		if source == `s[i]` {
			failure = charErr
		}
		got, err := libRun(t, source, args, specs...)
		switch {
		case failure != nil && (err == nil || !strings.HasSuffix(err.Error(), failure.Error())):
			t.Errorf("%s with %v = %v, %v; want %v", source, args, got, err, failure)
		case failure == nil && (err != nil || got != expected):
			t.Errorf("%s with %v = %v, %v; want %q", source, args, got, err, expected)
		}
	}
}

// textOf is a string value's text, "" for the zero value a failure leaves.
func textOf(value machine.Value) string {
	text, _ := value.Any().(string)
	return text
}

func equalStrings(got any, want []string) bool {
	items, ok := got.([]string)
	return ok && strings.Join(items, "|") == strings.Join(want, "|")
}
