package syntax

import "testing"

// printCorpus is what the language can say, plus the spellings that are easy
// to print wrong: an operator under a field access or an index, a minus in
// front of a number, a chain long enough to be broken across lines.
var printCorpus = []string{
	`if(a, b, add(1, 1))`,
	`fee + amount -> JPY > USD 1 -> JPY`,
	`using(settled / paid, 150.25 JPY / USD, using(a / b, [x -> JPY for x in xs]))`, `[150 JPY / USD, (0.0067 USD / JPY).x]`,
	// A negated exchange rate and a negated item: a minus before a number
	// is a literal only when the number stands alone.
	`-150 JPY / USD`, `-2[0]`, `-(1.5)`,
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
	`switch(country, case "SG", "MY" => 1, else => 2)`,
	`switch(case amount > 10_000 => "manual_review", case risk > 0.8 => "reject", else => "auto")`,
	`switch(channel, case @adyen => @stripe, case @stripe => @channel.adyen)`,
	`[c for c in channels if healthy(c)]`,
	`[{channel: c, currency: k} for c in channels if healthy(c) for k in currencies]`,
	`{k: v * 2 for k, v in rates if v > 0}`,
	`reduce(price in prices if price >= minimum, total = 0, total + price)`,
	`reduce(name, weight in weights, sum = 0.0, sum + weight)`,
	`let(bps = 250, base_fee = 3 * 100 + 50, total_bps = bps * 2, amount * total_bps / 10000 + base_fee)`,
	`{"primary": 1, "backup": 2}`, `{net: order.amount - fee, currency: order.currency}`, `[]`, `{}`,
	`order with {amount: order.amount - fee}`, `b with {customer: b.customer with {amount: 1}}`, `(orders[0] with {fee: 0}).fee`,
	`order with {amount: order.amount - route.fee_v1(order.channel, order.currency), currency: route.settlement_currency_v1(order.channel)}`,
	`fallback(primary.quote_v1(order), secondary.quote_v1(order), 0.0)`,
	`route.is_healthy_v1(primary_channel_status) && route.is_healthy_v1(secondary_channel_status) && amount > 1000`,
	`let(emb = model.embed_v2(features), switch(case model.fraud_v3(emb) > 0.9 => "reject", else => "accept"))`,
	// Money and ratios: a ratio or an amount under an index or a field read
	// keeps its parentheses, a negative amount has its sign on the figure,
	// and a minus before the code negates an amount like any operand.
	`amount * 2.9% + USD 0.30`, `USD -1.70`, `-USD 1.70`, `USD-1.70`, `USD - 1`, "USD\n-1", `(USD -1).x`, `-USD -1`, `0 - USD 1`, `a - -USD 1`, `(2.9%)[0]`, `(USD 1).x`, `(25bps).x`,
	`x % 3`, `10 % 3`, `2.9% - fee`, `[x * 2.9% for x in xs if x > 0]`, `amount * 0.5bps`,
	`-2.9%`, `-(2.9%)`, `JPY 1_000 + JPY 5`,
	// Every spelling the lexer has to tell apart around a %, and money in
	// every position a number can take: under a minus, a field read, an
	// index, inside containers, cases and bindings.
	`2.9%`, `0.5bps`, `25bps`, `1_0%`, `2.9% in xs`, `7% - 2`, `7 % -2`, `10 % -3`, `2.9 % -fee`, `1% % 2`,
	"2.9%\n- fee", `2.9% // why`, `2.9% == r`, `2.9% * x - 25bps / y`, `(2.9%).x.y`, `(0.5bps)[0]`,
	`0 - -USD 1`, `-(0 - USD 1)`, `-(-USD 1)`, `(-USD 1).x`, `(-USD 1)[0]`, `(USD 1.70).x.y`, `[USD 1, -JPY 5][0]`,
	`[[USD 1]][0][0]`, `{a: USD 1, r: 2.9%}.r`, `{"k": -USD 0.01}`, `USD 10 % 3`, `ABCDEFGH 1`, `U2D 0.5`,
	`switch(x, case 2.9%, 25bps => USD 1, else => -USD 1)`, `reduce(x in xs if x > 1%, a = 0%, a + x)`,
	`let(fee = amount * 2.9% + USD 0.30, cap = USD 25.00, if(fee > cap, cap, fee))`,
	`{k: v * 2.9% for k, v in fees if v > USD 0}`, `money(170, @USD) + USD 1`, `round(amount * 2.9% * 25bps, @currency.USD)`,
	`USD + 1`, `f(USD)`, `a -USD 1`, `a - - USD 1`,
	// Selectors: a field read off one is not a longer one.
	`t + 90s`, `2h30m - 1s500ms`, `-2h`, `0 - 2h`, `(30m).x`, `d * 2 > 1h`, `[1ms, 1us, 1ns][0]`,
	`sort_by(xs, .fee)`, `top_k(xs, .meta.rank, 2)`, `(.a).b`, `(.a)[0]`, `f(xs, .a, .b.c)`,
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
		// A minus before the code negates the amount; the literal's sign
		// is on its figure.
		`sub(0, USD 1)`:  `-USD 1`,
		`-(USD 1)`:       `-USD 1`,
		`sub(0, USD -1)`: `-USD -1`,
		`- USD 1.70`:     `-USD 1.70`,
		`USD	-1.70`:      `USD -1.70`,
		`sub(0, 2.9%)`:   `-2.9%`,
		`at(2.9%, 0)`:    `(2.9%)[0]`,
		`at(USD 1, 0)`:   `(USD 1)[0]`,
		`USD 1.70.x`:     `(USD 1.70).x`,
		`2.9%.x`:         `(2.9%).x`,
		`JPY 1_000`:      `JPY 1000`,
		"USD\t1.70":      `USD 1.70`,
		`mod(7, -2)`:     `7 % -2`,
		`x%3`:            `x % 3`,
		`10%-3`:          `10% - 3`,
		`2.9%-fee`:       `2.9% - fee`,
		"2.9%\n- fee":    `2.9% - fee`,
		`mod(1%, 2)`:     `1% % 2`,
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

// A ratio before != prints as 2.9% != r, which has to read back.
func TestARatioBeforeNotEqualPrintsBack(t *testing.T) {
	t.Parallel()
	checkPrintedParsesBack(t, `!(2.9% == r)`)
}

// The arrow binds looser than arithmetic and tighter than comparison, a
// currency is its bare code, and 150 JPY / USD is one literal, bracketed next
// to * and / and before a field read.
func TestPrintSpellsExchangeTheWayItIsWritten(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		`convert(a, JPY)`:               `a -> JPY`,
		`convert(add(a, b), JPY)`:       `a + b -> JPY`,
		`mul(convert(a, JPY), 2)`:       `(a -> JPY) * 2`,
		`convert(a, mul(JPY, 2))`:       `a -> (JPY * 2)`,
		`eq(eq(a, b), c)`:               `(a == b) == c`,
		`lt(a, lt(b, c))`:               `a < (b < c)`,
		`eq(lt(a, b), c)`:               `a < b == c`,
		`member(member(a, b), c)`:       `(a in b) in c`,
		`convert(a, sub(0, c))`:         `a -> (-c)`,
		`convert(a, currency(b))`:       `a -> currency(b)`,
		`convert(a, o.currency)`:        `a -> o.currency`,
		`gt(convert(a, JPY), b)`:        `a -> JPY > b`,
		`convert(convert(a, USD), JPY)`: `a -> USD -> JPY`,
		`money(170,USD)`:                `money(170, USD)`,
		`using(150 JPY/USD, a -> JPY)`:  `using(150 JPY / USD, a -> JPY)`,
		`using(JPY 150/USD 1.00, x)`:    `using(JPY 150 / USD 1.00, x)`,
		`using((a+b)/c, d/(e*f), x)`:    `using((a + b) / c, d / (e * f), x)`,
		`using(s / p,x)`:                `using(s / p, x)`,
		`using(150.25 JPY/USD, x)`:      `using(150.25 JPY / USD, x)`,
		`using(0.0067 USD / JPY, x)`:    `using(0.0067 USD / JPY, x)`,
		`(150 JPY / USD).x`:             `(150 JPY / USD).x`,
		`div(a, 150 JPY / USD)`:         `a / (150 JPY / USD)`,
		`a / 150 JPY / USD`:             `a / (150 JPY / USD)`,
		`150 JPY / USD / a`:             `(150 JPY / USD) / a`,
		`150 JPY / USD == x`:            `150 JPY / USD == x`,
		`at(USD, 0)`:                    `(USD)[0]`,
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
