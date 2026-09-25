package main

// The scenarios on scalars and on text: operators, conditions, bindings,
// numeric functions, conversions and host calls.

func scalarGroup() group {
	in := newScalar()
	integer, float, boolean, text := on[scalarIn, int64], on[scalarIn, float64], on[scalarIn, bool], on[scalarIn, string]
	return group{"标量：运算符、条件、绑定、数值函数、转换、宿主函数", []scenario{
		integer(in, "a + b", "a + b"),
		integer(in, "a * b - a % b + 1", "a * b - a % b + 1"),
		integer(in, "a / b", "int(a / b)"),
		integer(in, "-a + b", "-a + b"),
		float(in, "x * y + x / y - 1.5", "x * y + x / y - 1.5"),
		float(in, "float(a) / float(b)", "a / b"),
		boolean(in, "a > b && x < y || !flag", "a > b && x < y || !flag"),
		boolean(in, "a == 7 && b != 4", "a == 7 && b != 4"),
		integer(in, "if(a > b, a, b)", "a > b ? a : b"),
		text(in, `switch(a, case 1 => "one", case 2, 3 => "few", else => "many")`, `a == 1 ? "one" : a in [2, 3] ? "few" : "many"`),
		text(in, `switch(case a > 10 => "big", case a > 5 => "mid", else => "small")`, `a > 10 ? "big" : a > 5 ? "mid" : "small"`),
		integer(in, "let(s = a + b, d = a - b, s * d)", "let s = a + b; let d = a - b; s * d"),
		integer(in, "7 * 24 * 3600 + a", "7 * 24 * 3600 + a"),
		integer(in, "abs(b - a)", "abs(b - a)"),
		integer(in, "max(a, b)", "max(a, b)"),
		integer(in, "ceil(x)", "ceil(x)"),
		integer(in, "floor(x) + round(y)", "floor(x) + round(y)"),
		float(in, "pow(x, 2.0)", "x ** 2"),
		integer(in, "int(y) + a", "int(y) + a"),
		float(in, "float(a) * x", "float(a) * x"),
		text(in, "string(a)", "string(a)"),
		integer(in, "host.add_v1(a, b)", "hostAdd(a, b)"),
		float(in, "host.scale_v1(x, a)", "hostScale(x, a)"),
	}}
}

func textGroup() group {
	in := newText()
	integer, boolean, text, texts := on[textIn, int64], on[textIn, bool], on[textIn, string], on[textIn, []string]
	return group{"字符串", []scenario{
		boolean(in, `country == "MY"`, `country == "MY"`),
		boolean(in, `country in ["SG", "MY", "TH"]`, `country in ["SG", "MY", "TH"]`),
		boolean(in, `starts_with(card, "4111")`, `card startsWith "4111"`),
		boolean(in, `ends_with(s, "-sg")`, `s endsWith "-sg"`),
		boolean(in, `contains(s, "sg")`, `s contains "sg"`),
		boolean(in, `"sg" in s`, `s contains "sg"`),
		text(in, "upper(s)", "upper(s)"),
		text(in, "lower(trim(name))", "lower(trim(name))"),
		text(in, `replace(s, "-", "_")`, `replace(s, "-", "_")`),
		texts(in, `split(csv, ",")`, `split(csv, ",")`),
		text(in, `join(split(csv, ","), "|")`, `join(split(csv, ","), "|")`),
		text(in, `s + ":" + country`, `s + ":" + country`),
		integer(in, "len(s)", "len(s)"),
		text(in, "slice(s, 0, 5)", "s[0:5]"),
		text(in, `if(starts_with(card, "4"), "visa", "other")`, `card startsWith "4" ? "visa" : "other"`),
		text(in, "host.label_v1(country)", "hostLabel(country)"),
	}}
}
