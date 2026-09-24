package machine

// kernelExamples are the kernel's uses, by name: mustRegister gives each
// overload of a name the same list. Between them a name's examples choose
// every one of its overloads — a rounding variant by round(…) — which
// examples_test.go checks by running each one and asking the compiler what it
// chose. They are closed expressions, so they run as written in any console
// that declares ISO 4217 money rounding half up; a conversion takes its rates
// from using.
var kernelExamples = map[string][]Example{
	"add": {
		{"1 + 2", "3"},
		{"1.5 + 2", "3.5"},
		{"2 + 0.5", "2.5"},
		{"0.5 + 0.25", "0.75"},
		{"USD 1.70 + USD 0.30", `"USD 2.00"`},
		{"2.9% + 30bps", `"0.032"`},
		{`"pay" + "out"`, `"payout"`},
	},
	"sub": {
		{"5 - 3", "2"},
		{"5.5 - 2", "3.5"},
		{"5 - 2.5", "2.5"},
		{"5.5 - 0.5", "5"},
		{"USD 2 - USD 0.30", `"USD 1.70"`},
		{"100% - 2.9%", `"0.971"`},
	},
	"mul": {
		{"6 * 7", "42"},
		{"1.5 * 2", "3"},
		{"2 * 1.5", "3"},
		{"1.5 * 2.5", "3.75"},
		{"USD 1.25 * 3", `"USD 3.75"`},
		{"3 * USD 1.25", `"USD 3.75"`},
		{"USD 10 * 2.9%", `"USD 0.29"`},
		{"2.9% * USD 10", `"USD 0.29"`},
		{"round(USD 0.50 * 2.9%, @up)", `"USD 0.02"`},
		{"round(2.9% * USD 0.50, @down)", `"USD 0.01"`},
		{"50% * 50%", `"0.25"`},
		{"150 JPY / USD * 101%", `{"base":"USD","quote":"JPY","rate":"151.5"}`},
		{"101% * 150 JPY / USD", `{"base":"USD","quote":"JPY","rate":"151.5"}`},
	},
	"div": {
		{"7 / 2", "3"},
		{"7.0 / 2", "3.5"},
		{"7 / 2.0", "3.5"},
		{"1.0 / 4.0", "0.25"},
		{"USD 0.29 / USD 10", `"0.029"`},
		{"JPY 15000 / USD 100", `{"base":"USD","quote":"JPY","rate":"150"}`},
		{"USD 10 / 50%", `"USD 20.00"`},
		{"round(USD 1 / 3%, @down)", `"USD 33.33"`},
		{"10% / 40%", `"0.25"`},
	},
	"mod": {
		{"7 % 3", "1"},
		{"-7 % 3", "-1"},
	},
	"eq": {
		{"1 == 1", "true"},
		{`"adyen" == "stripe"`, "false"},
		{"USD 1 == USD 1.00", "true"},
		{"[1, 2] == [1, 2]", "true"},
	},
	"lt": orderings("<", false),
	"le": orderings("<=", false),
	"gt": orderings(">", true),
	"ge": orderings(">=", true),
	"if": {
		{`if(1 < 2, "yes", "no")`, `"yes"`},
		{`if(false, USD 1, USD 2)`, `"USD 2.00"`},
	},
	"fallback": {
		{"fallback(USD 1 -> JPY, JPY 150)", `"JPY 150"`},
		{"fallback(USD 1 -> JPY, using(150 JPY / USD, USD 1 -> JPY))", `"JPY 150"`},
		{"using(160 JPY / USD, fallback(USD 1 -> JPY, JPY 150))", `"JPY 160"`},
	},
	"at": {
		{"[10, 20, 30][1]", "20"},
		{`{"adyen": 1, "stripe": 2}["stripe"]`, "2"},
		{`{"USD": 2, "JPY": 0}[JPY]`, "0"},
		{`"adyen"[0]`, `"a"`},
	},
	"len": {
		{"len([1, 2, 3])", "3"},
		{`len({"adyen": 1})`, "1"},
		{`len("日本円")`, "3"},
	},
	"member": {
		{"2 in [1, 2, 3]", "true"},
		{`"adyen" in {"adyen": 1}`, "true"},
		{`USD in {"USD": 2, "JPY": 0}`, "true"},
		{`"sg" in "adyen-sg"`, "true"},
	},
	"bool":  {{`bool("true")`, "true"}, {"bool(1 < 2)", "true"}},
	"int":   {{"int(3.0)", "3"}, {`int("42")`, "42"}, {"int(7)", "7"}},
	"float": {{"float(3)", "3"}, {`float("2.5")`, "2.5"}, {"float(0.5)", "0.5"}},
	"string": {
		{"string(true)", `"true"`},
		{"string(42)", `"42"`},
		{"string(2.5)", `"2.5"`},
		{`string("adyen")`, `"adyen"`},
		{"string(USD)", `"USD"`},
		{"string(@half_up)", `"half_up"`},
	},
	"money":       {{"money(170, USD)", `"USD 1.70"`}, {`money(500, currency_of("JPY"))`, `"JPY 500"`}},
	"currency":    {{"currency(USD 1.70)", `"USD"`}, {"currency(JPY 5) == JPY", "true"}},
	"currency_of": {{`currency_of("EUR")`, `"EUR"`}},
	"minor":       {{"minor(USD 1.70)", "170"}, {"minor(JPY 500)", "500"}},
	"sign":        {{"sign(USD -1.70)", "-1"}, {"sign(USD 0)", "0"}, {"sign(JPY 5)", "1"}},
	"like":        {{"like(JPY 1, 500)", `"JPY 500"`}, {"like(USD 5, 150)", `"USD 1.50"`}},
	"rate":        {{`rate("0.029")`, `"0.029"`}, {`USD 10 * rate("0.025")`, `"USD 0.25"`}},
	"allocate": {
		{"allocate(USD 0.10, [3, 3, 1])", `["USD 0.04","USD 0.04","USD 0.02"]`},
		{"allocate(USD 1.00, 3)", `["USD 0.34","USD 0.33","USD 0.33"]`},
		{"allocate(USD 0.10, [3, 3, 1], @largest_weight)", `["USD 0.05","USD 0.04","USD 0.01"]`},
		{"allocate(USD 1.00, 3, @reverse_order)", `["USD 0.33","USD 0.33","USD 0.34"]`},
		{"allocate(USD 0.05, [1, 1], @all_first)", `["USD 0.03","USD 0.02"]`},
	},
	"prorate": {
		{"prorate(USD 100, 1, 3)", `"USD 33.33"`},
		{"round(prorate(USD 100, 2, 3), @down)", `"USD 66.66"`},
		{"prorate(USD 30, EUR 25, EUR 100)", `"USD 7.50"`},
		{"round(prorate(USD 1, EUR 1, EUR 3), @up)", `"USD 0.34"`},
	},
	"round_to": {
		{"round_to(CHF 1.03, CHF 0.05)", `"CHF 1.05"`},
		{"round(round_to(CHF 1.03, CHF 0.05), @down)", `"CHF 1.00"`},
	},
	"round": {
		{"round(USD 0.25 * 50%, @half_even)", `"USD 0.12"`},
		{"round(USD 0.25 * 50%, @half_up)", `"USD 0.13"`},
	},
	"convert": {
		{"using(150 JPY / USD, USD 1.50 -> JPY)", `"JPY 225"`},
		{"using(150 JPY / USD, JPY 100 -> USD)", `"USD 0.67"`},
		{"using(0.9 EUR / USD, round(USD 0.05 -> EUR, @down))", `"EUR 0.04"`},
		{"using(7 CNY / USD, 20 JPY / CNY, JPY 14000 -> CNY -> USD)", `"USD 100.00"`},
		{`using(150 JPY / USD, USD 1 -> currency_of("JPY"))`, `"JPY 150"`},
	},
	"fx": {
		{"using(150 JPY / USD, fx(USD, JPY))", `{"base":"USD","quote":"JPY","rate":"150"}`},
		{"using(150 JPY / USD, fx(JPY, USD))", `{"base":"JPY","quote":"USD","rate":"1/150"}`},
		{"using(150 JPY / USD, using(fx(USD, JPY) * 102%, USD 1 -> JPY))", `"JPY 153"`},
	},
}

// orderings shows a comparison on every type it orders. larger writes each
// pair the larger first, so every example of > and >= holds as < and <= do.
func orderings(operator string, larger bool) []Example {
	pairs := [][2]string{
		{"1", "2"}, {"1.5", "2"}, {"1", "1.5"}, {"1.5", "2.5"}, {`"adyen"`, `"stripe"`},
		{"USD 1", "USD 2"}, {"2.9%", "3%"}, {"150 JPY / USD", "151 JPY / USD"},
	}
	out := make([]Example, 0, len(pairs))
	for _, pair := range pairs {
		if larger {
			pair[0], pair[1] = pair[1], pair[0]
		}
		out = append(out, Example{pair[0] + " " + operator + " " + pair[1], "true"})
	}
	return out
}

// formExamples are the forms' uses, by name, for the catalog's descriptors.
var formExamples = map[string][]Example{
	"switch": {
		{`switch(2, case 1 => "one", case 2, 3 => "few", else => "many")`, `"few"`},
		{`switch(case 1 > 2 => "up", else => "down")`, `"down"`},
		{"switch(currency(JPY 5), case JPY => 0, else => 2)", "0"},
	},
	"for": {
		{"[x * 2 for x in [1, 2, 3]]", "[2,4,6]"},
		{"[x for x in [1, 2, 3, 4] if x % 2 == 0]", "[2,4]"},
		{"[x + y for x in [10, 20] for y in [1, 2]]", "[11,12,21,22]"},
		{`{k: v * 10 for k, v in {"a": 1, "b": 2}}`, `{"a":10,"b":20}`},
	},
	"reduce": {
		{"reduce(x in [1, 2, 3], total = 0, total + x)", "6"},
		{"reduce(x in [1, 2, 3, 4] if x > 1, product = 1, product * x)", "24"},
	},
	"let": {
		{"let(fee = 2.9%, amount = USD 100, amount * fee)", `"USD 2.90"`},
		{"let(a = 2, b = a * 3, a + b)", "8"},
	},
	"and": {{`1 < 2 && "a" == "a"`, "true"}},
	"or":  {{"1 > 2 || 2 > 1", "true"}},
	"not": {{"!(1 > 2)", "true"}, {"1 != 2", "true"}},
	"using": {
		{"using(150 JPY / USD, USD 2 -> JPY)", `"JPY 300"`},
		{"using(150 JPY / USD, 0.9 EUR / USD, EUR 9 -> USD -> JPY)", `"JPY 1500"`},
		{"using(150 JPY / USD, using(fx(USD, JPY) * 102%, USD 1 -> JPY))", `"JPY 153"`},
		{"using(150 JPY / USD, using(0.9 EUR / USD, fallback(USD 1 -> JPY, JPY 0)))", `"JPY 0"`},
	},
}
