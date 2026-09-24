package std

import "funroute/lang"

// examples are the pack's uses, by name: logic and register give each
// overload of a name the same list. Between them a name's examples choose
// every one of its overloads (a test in lang/internal/machine runs each and
// asks the compiler what it chose). They are closed expressions over the
// kernel and this pack, so they run as written in any console that registers
// both and declares ISO 4217 money rounding half up.
var examples = map[string][]lang.Example{
	"sum": {
		{Source: "sum([1, 2, 3])", Result: "6"},
		{Source: "sum([0.5, 0.25])", Result: "0.75"},
		{Source: "sum([USD 1, USD 0.50])", Result: `"USD 1.50"`},
		{Source: "sum([x * 2 for x in [1, 2]])", Result: "6"},
	},
	"min":         extremes("min", "1", "0.5", `"USD 1.00"`, `"adyen"`),
	"max":         extremes("max", "5", "1.5", `"USD 2.00"`, `"stripe"`),
	"any":         {{Source: "any([false, 2 > 1])", Result: "true"}, {Source: "any([])", Result: "false"}},
	"all":         {{Source: "all([true, 2 > 1])", Result: "true"}, {Source: "all([])", Result: "true"}},
	"range":       {{Source: "range(3)", Result: "[0,1,2]"}, {Source: "range(1, 4)", Result: "[1,2,3]"}, {Source: "range(0, 10, 3)", Result: "[0,3,6,9]"}},
	"upper":       {{Source: `upper("adyen")`, Result: `"ADYEN"`}},
	"lower":       {{Source: `lower("SGD")`, Result: `"sgd"`}},
	"trim":        {{Source: `trim("  adyen ")`, Result: `"adyen"`}},
	"contains":    {{Source: `contains("adyen-sg", "sg")`, Result: "true"}},
	"starts_with": {{Source: `starts_with("411111", "4111")`, Result: "true"}},
	"ends_with":   {{Source: `ends_with("adyen-sg", "-sg")`, Result: "true"}},
	"split":       {{Source: `split("a,b,c", ",")`, Result: `["a","b","c"]`}},
	"join":        {{Source: `join(["R01", "R07"], "|")`, Result: `"R01|R07"`}},
	"replace":     {{Source: `replace("a-b-c", "-", "_")`, Result: `"a_b_c"`}},
	"pad_left":    {{Source: `pad_left("7", 3, "0")`, Result: `"007"`}, {Source: `pad_left("1234", 3, "0")`, Result: `"1234"`}},
	"pad_right":   {{Source: `pad_right("ab", 4, ".")`, Result: `"ab.."`}},
	"slice":       {{Source: "slice([1, 2, 3, 4], 1, 3)", Result: "[2,3]"}, {Source: `slice("adyen", 0, 2)`, Result: `"ad"`}},
	"first":       {{Source: "first([3, 4])", Result: "3"}},
	"last":        {{Source: "last([3, 4])", Result: "4"}},
	"take":        {{Source: "take([1, 2, 3], 2)", Result: "[1,2]"}, {Source: "take([1], 5)", Result: "[1]"}},
	"reverse":     {{Source: "reverse([1, 2, 3])", Result: "[3,2,1]"}},
	"concat":      {{Source: "concat([1, 2], [3])", Result: "[1,2,3]"}},
	"flatten":     {{Source: "flatten([[1], [2, 3]])", Result: "[1,2,3]"}},
	"unique":      {{Source: "unique([1, 2, 1, 3])", Result: "[1,2,3]"}},
	"intersect":   {{Source: "intersect([1, 2, 3, 2], [2, 3, 4])", Result: "[2,3]"}},
	"except":      {{Source: "except([1, 2, 3, 2], [2])", Result: "[1,3]"}},
	"chunk":       {{Source: "chunk([1, 2, 3, 4, 5], 2)", Result: "[[1,2],[3,4],[5]]"}},
	"windows":     {{Source: "windows([1, 2, 3, 4], 3)", Result: "[[1,2,3],[2,3,4]]"}, {Source: "[sum(w) for w in windows([1, 2, 3, 4], 2)]", Result: "[3,5,7]"}},
	"sort":        numericOrder("sort", "[1,2,3]", "[0.5,1.5,2.5]", `["USD 1.00","USD 2.00","USD 3.00"]`, `["a","b","c"]`),
	"sort_desc":   numericOrder("sort_desc", "[3,2,1]", "[2.5,1.5,0.5]", `["USD 3.00","USD 2.00","USD 1.00"]`, `["c","b","a"]`),
	"deltas": {
		{Source: "deltas([1, 4, 9])", Result: "[3,5]"},
		{Source: "deltas([0.5, 2.0])", Result: "[1.5]"},
		{Source: "deltas([USD 1, USD 3])", Result: `["USD 2.00"]`},
		{Source: "deltas([7])", Result: "[]"},
	},
	"cumsum": {
		{Source: "cumsum([1, 2, 3])", Result: "[1,3,6]"},
		{Source: "cumsum([0.5, 0.25])", Result: "[0.5,0.75]"},
		{Source: "cumsum([USD 1, USD 2])", Result: `["USD 1.00","USD 3.00"]`},
	},
	"rank": {
		{Source: "rank([10, 30, 10])", Result: "[1,3,1]"},
		{Source: "rank([0.5, 0.1])", Result: "[2,1]"},
		{Source: `rank(["b", "a"])`, Result: "[2,1]"},
	},
	"abs":   {{Source: "abs(-3)", Result: "3"}, {Source: "abs(-2.5)", Result: "2.5"}, {Source: "abs(USD -1.70)", Result: `"USD 1.70"`}},
	"ceil":  {{Source: "ceil(1.2)", Result: "2"}, {Source: "ceil(-1.2)", Result: "-1"}},
	"floor": {{Source: "floor(1.8)", Result: "1"}, {Source: "floor(-1.2)", Result: "-2"}},
	"pow":   {{Source: "pow(2, 10)", Result: "1024"}, {Source: "pow(2.0, 0.5)", Result: "1.4142135623730951"}},
	"avg": {
		{Source: "avg([1, 2])", Result: "1.5"},
		{Source: "avg([1.0, 2.0, 4.5])", Result: "2.5"},
		{Source: "avg([USD 1, USD 2])", Result: `"USD 1.50"`},
		{Source: "round(avg([USD 0.01, USD 0.02]), @down)", Result: `"USD 0.01"`},
	},
	"median": {
		{Source: "median([1, 2, 3, 4])", Result: "2.5"},
		{Source: "median([1.0, 5.0, 2.0])", Result: "2"},
		{Source: "median([USD 1, USD 3])", Result: `"USD 2.00"`},
		{Source: "round(median([USD 0.01, USD 0.02]), @down)", Result: `"USD 0.01"`},
	},
	"stddev":       {{Source: "stddev([2, 4, 4, 4, 5, 5, 7, 9])", Result: "2"}, {Source: "stddev([1.0, 3.0])", Result: "1"}},
	"percentile":   {{Source: "percentile([1, 2, 3, 4], 0.5)", Result: "2.5"}, {Source: "percentile([10.0, 20.0], 0.95)", Result: "19.5"}},
	"mod":          {{Source: "7.5 % 2.0", Result: "1.5"}, {Source: "mod(-7.5, 2.0)", Result: "-1.5"}},
	"round":        {{Source: "round(2.5)", Result: "3"}, {Source: "round(-2.5)", Result: "-3"}},
	"arg_min":      positions("arg_min", "3"),
	"arg_max":      positions("arg_max", "1"),
	"top_k":        byKey("top_k", ", 2)", `["b","a"]`),
	"bottom_k":     byKey("bottom_k", ", 2)", `["c","a"]`),
	"sort_by":      byKey("sort_by", ")", `["c","a","b"]`),
	"sort_by_desc": byKey("sort_by_desc", ")", `["b","a","c"]`),
	"take_while":   {{Source: "take_while([1, 2, 5, 1], [x < 3 for x in [1, 2, 5, 1]])", Result: "[1,2]"}},
	"drop_while":   {{Source: "drop_while([1, 2, 5, 1], [x < 3 for x in [1, 2, 5, 1]])", Result: "[5,1]"}},
	"indices":      {{Source: `indices(["a", "b", "c"])`, Result: "[0,1,2]"}},
	"index_of":     {{Source: `index_of(["adyen", "stripe"], "stripe")`, Result: "1"}},
	"group_by": {
		{Source: `group_by([1, 2, 3], ["x", "y", "x"])`, Result: `{"x":[1,3],"y":[2]}`},
		{Source: `{k: sum(v) for k, v in group_by([1, 2, 3], ["x", "y", "x"])}`, Result: `{"x":4,"y":2}`},
	},
	"get":   {{Source: `get({"adyen": 1}, "stripe", 0)`, Result: "0"}, {Source: `get({"adyen": 1}, "adyen", 0)`, Result: "1"}},
	"merge": {{Source: `merge({"a": 1, "b": 2}, {"b": 3})`, Result: `{"a":1,"b":3}`}},
}

// extremes shows min or max over an array of every element type and between
// two values of each.
func extremes(name, integer, float, money, text string) []lang.Example {
	return []lang.Example{
		{Source: name + "([1, 5, 3])", Result: integer},
		{Source: name + "([0.5, 1.5])", Result: float},
		{Source: name + "([USD 1, USD 2])", Result: money},
		{Source: name + `(["adyen", "stripe"])`, Result: text},
		{Source: name + "(1, 5)", Result: integer},
		{Source: name + "(0.5, 1.5)", Result: float},
		{Source: name + "(USD 1, USD 2)", Result: money},
		{Source: name + `("adyen", "stripe")`, Result: text},
	}
}

func numericOrder(name, integers, floats, money, texts string) []lang.Example {
	return []lang.Example{
		{Source: name + "([2, 3, 1])", Result: integers},
		{Source: name + "([1.5, 2.5, 0.5])", Result: floats},
		{Source: name + "([USD 2, USD 3, USD 1])", Result: money},
		{Source: name + `(["b", "c", "a"])`, Result: texts},
	}
}

// positions shows arg_min or arg_max over [4, 9, 9, 1] and its likes; ties
// take the first.
func positions(name, integer string) []lang.Example {
	return []lang.Example{
		{Source: name + "([4, 9, 9, 1])", Result: integer},
		{Source: name + "([4.5, 9.5, 9.5, 1.5])", Result: integer},
		{Source: name + "([USD 4, USD 9, USD 9, USD 1])", Result: integer},
		{Source: name + `(["d", "i", "i", "a"])`, Result: integer},
	}
}

// byKey shows a function that orders ["a", "b", "c"] by keys of every type,
// the keys ranking them b, a, c from the largest down.
func byKey(name, tail, result string) []lang.Example {
	keys := []string{"[2, 3, 1]", "[0.2, 0.3, 0.1]", "[USD 2, USD 3, USD 1]", `["m", "z", "a"]`}
	out := make([]lang.Example, 0, len(keys))
	for _, key := range keys {
		out = append(out, lang.Example{Source: name + `(["a", "b", "c"], ` + key + tail, Result: result})
	}
	return out
}
