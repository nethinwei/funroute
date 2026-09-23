package syntax

import "testing"

// printCorpus is what the language can say, plus the spellings that are easy
// to print wrong: an operator under a field access or an index, a minus in
// front of a number, a chain long enough to be broken across lines.
var printCorpus = []string{
	`if(a, b, add(1, 1))`,
	`amount * bps / 10000 + fixed`,
	`a - (b - c)`, `(a - b) - c`, `a - b - c`, `-(a * b)`, `-a * b`, `a - -b`, `--x`,
	`!(a && b)`, `!a == b`, `a != b`, `!(a != b)`, `a || b && c`, `(a || b) && c`,
	`x in xs == true`, `(x in xs) == true`, `"b" in text`,
	`(a + b).x`, `(-a).x`, `(-a)[0]`, `(a + b)[0]`, `(1).x`, `(-1)[0]`, `(1.5).x`,
	`0 - 1`, `-(1)`, `-1`, `-1.5`, `a - -1`, `2.0`, `1e21`, `"quote \" and \\ and \n"`,
	`[1, 2][0].x`, `{a: 1}.a`, `order.items[0].price`, `route.score_v1(x).fee`,
	`switch(country, case "SG", "MY" => 1, else 2)`,
	`switch(case amount > 10_000 => "manual_review", case risk > 0.8 => "reject", else "auto")`,
	`switch(channel, case @adyen => @stripe, case @stripe => @channel.adyen)`,
	`[c for c in channels if healthy(c)]`,
	`[{channel: c, currency: k} for c in channels if healthy(c) for k in currencies]`,
	`{k: v * 2 for k, v in rates if v > 0}`,
	`reduce(price in prices if price >= minimum, total = 0, total + price)`,
	`reduce(name, weight in weights, sum = 0.0, sum + weight)`,
	`let(bps = 250, base_fee = 3 * 100 + 50, total_bps = bps * 2, amount * total_bps / 10000 + base_fee)`,
	`{"primary": 1, "backup": 2}`, `{net: order.amount - fee, currency: order.currency}`, `[]`, `{}`,
	`{...order, amount: order.amount - fee}`, `{...b, customer: {...b.customer, amount: 1}}`, `{...orders[0], fee: 0}.fee`,
	`{...order, amount: order.amount - route.fee_v1(order.channel, order.currency), currency: route.settlement_currency_v1(order.channel)}`,
	`fallback(primary.quote_v1(order), secondary.quote_v1(order), 0.0)`,
	`route.is_healthy_v1(primary_channel_status) && route.is_healthy_v1(secondary_channel_status) && amount > 1000`,
	`let(emb = model.embed_v2(features), switch(case model.fraud_v3(emb) > 0.9 => "reject", else "accept"))`,
}

// Every printed program parses back to the node it was printed from, on one
// line and laid out. That is the property a canvas relies on when it shows a
// subtree as source and takes the edited text back.
func TestPrintedSourceParsesBack(t *testing.T) {
	for _, source := range printCorpus {
		expr := mustParse(t, source)
		want := mustExport(t, expr)
		for layout, text := range map[string]string{"inline": Inline(expr), "format": Format(expr)} {
			back, err := Parse(text)
			if err != nil {
				t.Errorf("%s of %q does not parse: %v\n%s", layout, source, err, text)
				continue
			}
			if got := mustExport(t, back); got != want {
				t.Errorf("%s of %q parses to another program:\n%s\n%s", layout, source, text, got)
			}
		}
	}
}

func TestPrintSpellsOperatorsTheWayTheyAreWritten(t *testing.T) {
	cases := map[string]string{
		`-x`:                        `-x`,
		`-(a).x`:                    `-a.x`,
		`(-a).x`:                    `(-a).x`,
		`(a+b).x`:                   `(a + b).x`,
		`-(1)`:                      `0 - 1`,
		`- 1.5`:                     `-1.5`,
		`!(a != b)`:                 `!(a != b)`,
		`if(a, true, b)`:            `a || b`,
		`if(a, b, false)`:           `a && b`,
		`if(eq(a, b), false, true)`: `a != b`,
		`at(xs, 0)`:                 `xs[0]`,
		`member(x, xs)`:             `x in xs`,
		`if(a, b, c)`:               `if(a, b, c)`,
		`(1).x`:                     `(1).x`,
	}
	for source, want := range cases {
		expr, err := Parse(source)
		if err != nil {
			t.Fatalf("%q: %v", source, err)
		}
		if got := Inline(expr); got != want {
			t.Errorf("%q prints as %q, want %q", source, got, want)
		}
	}
}

func TestFormatBreaksWhatDoesNotFit(t *testing.T) {
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
		if got := Format(mustParse(t, source)); got != want {
			t.Errorf("format of %q:\n%s\nwant:\n%s", source, got, want)
		}
	}
}

func mustParse(t *testing.T, source string) Expr {
	t.Helper()
	expr, err := Parse(source)
	if err != nil {
		t.Fatalf("%q: %v", source, err)
	}
	return expr
}

func mustExport(t *testing.T, expr Expr) string {
	t.Helper()
	encoded, err := ExportExprJSON(expr)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

func TestFormatSourceNeverLosesAComment(t *testing.T) {
	cases := map[string]string{
		"// amount: int\n\namount*2": "// amount: int\n\namount * 2",
		"a+b // why\n":               "a + b // why\n",
		"  f( x )  ":                 "f(x)",
		"// only\n// header\nf(x,y)": "// only\n// header\nf(x, y)",
	}
	for source, want := range cases {
		if got, err := FormatSource(source); err != nil || got != want {
			t.Errorf("%q formats to %q (%v), want %q", source, got, err, want)
		}
	}
	for _, source := range []string{"f(a, // why\n b)", "let(x = 1, // c\n x)", "f(a,"} {
		if got, err := FormatSource(source); err == nil {
			t.Errorf("%q was formatted to %q", source, got)
		}
	}
}
