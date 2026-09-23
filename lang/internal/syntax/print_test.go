package syntax

import "testing"

// printCorpus is what the language can say, plus the spellings that are easy
// to print wrong: an operator under a field access or an index, a minus in
// front of a number, a chain long enough to be broken across lines.
var printCorpus = []string{
	`if(a, b, add(1, 1))`,
	// A call without arguments, fields read off a field, and a field read off
	// an enum member — each once printed as something that did not read back.
	`f()`, `route.now_v1()`, `f(x).a.b`, `xs[0].a.b`, `{a: {b: 1}}.a.b`, `(@A).A`, `(@A).a.b`,
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
	t.Parallel()
	for _, source := range printCorpus {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			checkPrintedParsesBack(t, source)
		})
	}
}

// checkPrintedParsesBack prints source both ways and reads each printing back.
func checkPrintedParsesBack(t *testing.T, source string) {
	t.Helper()
	expr := mustParse(t, source)
	want := mustExport(t, expr)
	for layout, text := range map[string]string{"inline": Inline(expr), "format": Format(expr)} {
		back, err := Parse(text)
		if err != nil {
			t.Errorf("%s of %q does not parse: %v\n%s", layout, source, err, text)
			continue
		}
		if got := mustExport(t, back); got != want {
			t.Errorf("%s of %q = %q parses to %s, want %s", layout, source, text, got, want)
		}
	}
}

func TestPrintSpellsOperatorsTheWayTheyAreWritten(t *testing.T) {
	t.Parallel()
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
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			if got := Inline(mustParse(t, source)); got != want {
				t.Errorf("Inline(%q) = %q, want %q", source, got, want)
			}
		})
	}
}
