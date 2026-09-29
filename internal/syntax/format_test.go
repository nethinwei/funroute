package syntax

import (
	"encoding/json"
	"os"
	"testing"
)

func TestFormatBreaksWhatDoesNotFit(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		`switch(case amount > 10_000 && route.is_healthy_v1(primary_channel_status) => "manual_review", case risk > 0.8 => "reject", else => "auto")`: `switch(
  case amount > 10000 && route.is_healthy_v1(primary_channel_status) => "manual_review",
  case risk > 0.8 => "reject",
  else            => "auto"
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
  bps   = 250,
  base  = 3 * 100 + 50,
  total = bps * 2,
  amount * total / 10000 + base
)`,
		`order with {amount: order.amount - route.fee_v1(order.channel, order.currency), currency: route.settlement_currency_v1(order.channel)}`: `order with {
  amount:   order.amount - route.fee_v1(order.channel, order.currency),
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

// A split switch lines up its => at the widest label of a run; a label far
// wider than the others, and one over more than one line, each end a run.
func TestFormatLinesUpTheArrowsOfASwitch(t *testing.T) {
	t.Parallel()
	formatsTo(t, map[string]string{
		`switch(country, case "SG", "MY", "TH" => "asia_pacific_primary_channel", case "US" => "stripe_north_america_primary", else => "stripe_global_fallback_channel")`: `switch(country,
  case "SG", "MY", "TH" => "asia_pacific_primary_channel",
  case "US"             => "stripe_north_america_primary",
  else                  => "stripe_global_fallback_channel"
)`,
		`switch(channel, case @adyen => route.primary_channel_v1(amount), case @stripe => route.backup_channel_v1(amount))`: `switch(channel,
  case @adyen  => route.primary_channel_v1(amount),
  case @stripe => route.backup_channel_v1(amount)
)`,
		`switch(case amount >= 100000 => switch(case vip => "large_vip_review", else => "large_standard_review"), case amount >= 1000 => "medium", else => "small")`: `switch(
  case amount >= 100000 => switch(
    case vip => "large_vip_review",
    else     => "large_standard_review"
  ),
  case amount >= 1000   => "medium",
  else                  => "small"
)`,
		`switch(case route.is_healthy_v1(primary_channel_status) && route.is_healthy_v1(backup_channel_status) => "both", case down => "none", case slow => "degraded", else => "one")`: `switch(
  case route.is_healthy_v1(primary_channel_status)
    && route.is_healthy_v1(backup_channel_status) => "both",
  case down => "none",
  case slow => "degraded",
  else      => "one"
)`,
	})
}

// A split let, record, update or dictionary lines up what follows its labels
// — the =, the values — as a switch does its =>; a part with no label, such
// as a let's body or a loop clause, ends a run.
func TestFormatLinesUpBindingsAndFields(t *testing.T) {
	t.Parallel()
	formatsTo(t, map[string]string{
		`let(fee = round(amount * 2.9%, @half_even), cap = USD 25.00, floor_fee = USD 0.50, min(max(fee, floor_fee), cap))`: `let(
  fee       = round(amount * 2.9%, @half_even),
  cap       = USD 25.00,
  floor_fee = USD 0.50,
  min(max(fee, floor_fee), cap)
)`,
		`{cheapest: min_by(quotes, .fee).channel, surest: max_by(quotes, .success).channel, by_fee: [q.channel for q in sort_by(quotes, .fee)]}`: `{
  cheapest: min_by(quotes, .fee).channel,
  surest:   max_by(quotes, .success).channel,
  by_fee:   [q.channel for q in sort_by(quotes, .fee)]
}`,
		`{"SG": "adyen_sg", "MY": "stripe_my", "TH": "omise_th", "US_EU": "stripe_us", "CA": "stripe_ca"}`: `{
  "CA":    "stripe_ca",
  "MY":    "stripe_my",
  "SG":    "adyen_sg",
  "TH":    "omise_th",
  "US_EU": "stripe_us"
}`,
		`[{channel: c, fee: route.fee_quote_v1(c, amount)} for c in candidate_channels if route.is_healthy_v1(c)]`: `[
  {channel: c, fee: route.fee_quote_v1(c, amount)}
  for c in candidate_channels
  if route.is_healthy_v1(c)
]`,
	})
}

// formatsTo holds Format to the text each source is laid out as.
func formatsTo(t *testing.T, cases map[string]string) {
	t.Helper()
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
const examplesFile = "../../web/funroute-examples.json"

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

// An amount and a ratio are atoms to the layout: a line breaks around them,
// never inside them, so a code stays on the line of its figure and a unit on
// the line of its number, however long the program.
func TestFormatKeepsMoneyLiteralsWhole(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		`let(fee = amount * 2.9% + USD 0.30, cap = USD 25.00, floor = USD -0.50, switch(case fee > cap => cap, case fee < floor => floor, else => fee))`: `let(
  fee   = amount * 2.9% + USD 0.30,
  cap   = USD 25.00,
  floor = USD -0.50,
  switch(case fee > cap => cap, case fee < floor => floor, else => fee)
)`,
		`route.quote_v1(amount * 2.9% + USD 0.30, amount * 25bps + EUR 0.25, amount * 0.5bps - JPY 1_000)`: `route.quote_v1(
  amount * 2.9% + USD 0.30,
  amount * 25bps + EUR 0.25,
  amount * 0.5bps - JPY 1000
)`,
		`primary_amount_in_settlement * 2.9% + secondary_amount_in_settlement * 25bps - USD 1_000_000.00`: `primary_amount_in_settlement * 2.9% + secondary_amount_in_settlement * 25bps
  - USD 1000000.00`,
		`USD 1.70 + 2.9%`: `USD 1.70 + 2.9%`,
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

// FormatSource keeps a comment after a ratio, where the lexer had to look past
// the space to know the % was a unit.
func TestFormatSourceKeepsACommentAfterARatio(t *testing.T) {
	t.Parallel()
	for source, want := range map[string]string{
		"amount*2.9% // card\n":         "amount * 2.9% // card\n",
		"// fee\nUSD 1_000+x*25bps":     "// fee\nUSD 1000 + x * 25bps",
		"USD  -1.70 // refund":          "USD -1.70 // refund",
		"2.9%\n  - fee // after a line": "2.9% - fee // after a line",
	} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			if got, err := FormatSource(source); err != nil || got != want {
				t.Errorf("FormatSource(%q) = %q, %v, want %q, nil", source, got, err, want)
			}
		})
	}
}
