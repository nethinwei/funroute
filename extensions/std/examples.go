package std

import "github.com/nethinwei/funroute"

// examples are the pack's uses, by name: Register gives each
// overload of a name the same list. Between them a name's examples choose
// every one of its overloads (a test in internal/machine runs each and
// asks the compiler what it chose). They are closed expressions over the
// kernel and this pack, so they run as written in any console that registers
// both and declares ISO 4217 money rounding half up.
var examples = map[string][]funroute.Example{
	// The money overloads of the kernel library's names: the kernel shows
	// the others.
	"sum":          {{Source: "sum([USD 1, USD 0.50])", Result: `"USD 1.50"`}},
	"min":          {{Source: "min([USD 1, USD 2])", Result: `"USD 1.00"`}, {Source: "min(USD 1, USD 2)", Result: `"USD 1.00"`}},
	"max":          {{Source: "max([USD 1, USD 2])", Result: `"USD 2.00"`}, {Source: "max(USD 1, USD 2)", Result: `"USD 2.00"`}},
	"abs":          {{Source: "abs(USD -1.70)", Result: `"USD 1.70"`}},
	"sort":         {{Source: "sort([USD 2, USD 3, USD 1])", Result: `["USD 1.00","USD 2.00","USD 3.00"]`}},
	"sort_desc":    {{Source: "sort_desc([USD 2, USD 3, USD 1])", Result: `["USD 3.00","USD 2.00","USD 1.00"]`}},
	"avg":          {{Source: "avg([USD 1, USD 2], @half_even)", Result: `"USD 1.50"`}, {Source: "avg([USD 0.01, USD 0.02], @down)", Result: `"USD 0.01"`}},
	"median":       {{Source: "median([USD 1, USD 3], @half_even)", Result: `"USD 2.00"`}, {Source: "median([USD 0.01, USD 0.02], @down)", Result: `"USD 0.01"`}},
	"arg_min":      {{Source: "arg_min([USD 4, USD 9, USD 9, USD 1])", Result: "3"}},
	"arg_max":      {{Source: "arg_max([USD 4, USD 9, USD 9, USD 1])", Result: "1"}},
	"top_k":        {{Source: `top_k(["a", "b", "c"], [USD 2, USD 3, USD 1], 2)`, Result: `["b","a"]`}},
	"bottom_k":     {{Source: `bottom_k(["a", "b", "c"], [USD 2, USD 3, USD 1], 2)`, Result: `["c","a"]`}},
	"sort_by":      {{Source: `sort_by(["a", "b", "c"], [USD 2, USD 3, USD 1])`, Result: `["c","a","b"]`}},
	"sort_by_desc": {{Source: `sort_by_desc(["a", "b", "c"], [USD 2, USD 3, USD 1])`, Result: `["b","a","c"]`}},
	// The pack's own.
	"pad_left":  {{Source: `pad_left("7", 3, "0")`, Result: `"007"`}, {Source: `pad_left("1234", 3, "0")`, Result: `"1234"`}},
	"pad_right": {{Source: `pad_right("ab", 4, ".")`, Result: `"ab.."`}},
	"intersect": {{Source: "intersect([1, 2, 3, 2], [2, 3, 4])", Result: "[2,3]"}},
	"except":    {{Source: "except([1, 2, 3, 2], [2])", Result: "[1,3]"}},
	"chunk":     {{Source: "chunk([1, 2, 3, 4, 5], 2)", Result: "[[1,2],[3,4],[5]]"}},
	"windows":   {{Source: "windows([1, 2, 3, 4], 3)", Result: "[[1,2,3],[2,3,4]]"}, {Source: "[sum(w) for w in windows([1, 2, 3, 4], 2)]", Result: "[3,5,7]"}},
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
	"take_while": {{Source: "take_while([1, 2, 5, 1], [x < 3 for x in [1, 2, 5, 1]])", Result: "[1,2]"}},
	"drop_while": {{Source: "drop_while([1, 2, 5, 1], [x < 3 for x in [1, 2, 5, 1]])", Result: "[5,1]"}},
	"group_by": {
		{Source: `group_by([1, 2, 3], ["x", "y", "x"])`, Result: `{"x":[1,3],"y":[2]}`},
		{Source: `{k: sum(v) for k, v in group_by([1, 2, 3], ["x", "y", "x"])}`, Result: `{"x":4,"y":2}`},
	},
}
