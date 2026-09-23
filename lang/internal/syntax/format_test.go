package syntax

import (
	"encoding/json"
	"os"
	"testing"
)

func TestFormatBreaksWhatDoesNotFit(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		`switch(case amount > 10_000 && route.is_healthy_v1(primary_channel_status) => "manual_review", case risk > 0.8 => "reject", else "auto")`: `switch(
  case amount > 10000 && route.is_healthy_v1(primary_channel_status) => "manual_review",
  case risk > 0.8 => "reject",
  else "auto"
)`,
		`route.is_healthy_v1(primary_channel_status) && route.is_healthy_v1(secondary_channel_status) && amount > 1000`: `route.is_healthy_v1(primary_channel_status)
  && route.is_healthy_v1(secondary_channel_status)
  && amount > 1000`,
		`[{channel: c, currency: k, fee: route.fee_v1(c, k)} for c in channels if healthy(c) for k in currencies]`: `[
  {channel: c, currency: k, fee: route.fee_v1(c, k)}
  for c in channels
  if healthy(c)
  for k in currencies
]`,
		`let(bps = 250, base = 3 * 100 + 50, total = bps * 2, amount * total / 10000 + base)`: `let(
  bps = 250,
  base = 3 * 100 + 50,
  total = bps * 2,
  amount * total / 10000 + base
)`,
		`{...order, amount: order.amount - route.fee_v1(order.channel, order.currency), currency: route.settlement_currency_v1(order.channel)}`: `{
  ...order,
  amount: order.amount - route.fee_v1(order.channel, order.currency),
  currency: route.settlement_currency_v1(order.channel)
}`,
		`a + b`: `a + b`,
	}
	for source, want := range cases {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			if got := Format(mustParse(t, source)); got != want {
				t.Errorf("format of %q:\n%s\nwant:\n%s", source, got, want)
			}
		})
	}
}

func TestFormatSourceNeverLosesAComment(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"// amount: int\n\namount*2": "// amount: int\n\namount * 2",
		"a+b // why\n":               "a + b // why\n",
		"  f( x )  ":                 "f(x)",
		"// only\n// header\nf(x,y)": "// only\n// header\nf(x, y)",
	}
	for source, want := range cases {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			if got, err := FormatSource(source); err != nil || got != want {
				t.Errorf("FormatSource(%q) = %q, %v, want %q, nil", source, got, err, want)
			}
		})
	}
	for _, source := range []string{"f(a, // why\n b)", "let(x = 1, // c\n x)", "f(a,"} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			if got, err := FormatSource(source); err == nil {
				t.Errorf("FormatSource(%q) = %q, nil, want an error", source, got)
			}
		})
	}
}

// examplesFile is the workbench's example set; its sources seed the fuzzers
// with the programs people actually write.
const examplesFile = "../../../web/funroute-examples.json"

// FuzzFormatRoundTrip holds the printer to its promise on any source: every
// program Parse accepts prints, on one line and laid out, as text that parses
// back to the same ExprJSON. The language facts a front end reads from the
// same text must not panic on it, parsable or not.
func FuzzFormatRoundTrip(f *testing.F) {
	for _, source := range fuzzSources(f) {
		f.Add(source)
	}
	f.Fuzz(func(t *testing.T, source string) {
		_, _ = Lexemes(source)
		_, _ = SyntaxTree(source)
		expr, err := Parse(source)
		if err != nil {
			return
		}
		want := exportFuzzed(t, source, expr)
		checkReparses(t, "Inline", source, Inline(expr), want)
		checkReparses(t, "Format", source, Format(expr), want)
		if formatted, err := FormatSource(source); err == nil {
			checkReparses(t, "FormatSource", source, formatted, want)
		}
	})
}

// fuzzSources is the seed corpus: the printer's and the lexer's test corpora
// and the source of every workbench example.
func fuzzSources(t testing.TB) []string {
	t.Helper()
	data, err := os.ReadFile(examplesFile)
	if err != nil {
		t.Fatal(err)
	}
	var examples struct {
		Examples []struct {
			Source string `json:"source"`
		} `json:"examples"`
	}
	if err := json.Unmarshal(data, &examples); err != nil {
		t.Fatalf("json.Unmarshal(%s) error = %v, want nil", examplesFile, err)
	}
	sources := append(append([]string{}, printCorpus...), lexemeCorpus...)
	for _, example := range examples.Examples {
		sources = append(sources, example.Source)
	}
	return sources
}

func exportFuzzed(t *testing.T, input string, expr Expr) string {
	t.Helper()
	encoded, err := ExportExprJSON(expr)
	if err != nil {
		t.Fatalf("ExportExprJSON of %q error = %v, want nil", input, err)
	}
	return string(encoded)
}

// checkReparses fails unless text, printed by printer from input, parses back
// to the program whose ExprJSON is want.
func checkReparses(t *testing.T, printer, input, text, want string) {
	t.Helper()
	back, err := Parse(text)
	if err != nil {
		t.Fatalf("Parse(%s of %q = %q) error = %v, want nil", printer, input, text, err)
	}
	if got := exportFuzzed(t, text, back); got != want {
		t.Fatalf("%s of %q = %q parses to %s, want %s", printer, input, text, got, want)
	}
}
