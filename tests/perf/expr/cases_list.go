package main

// The scenarios on arrays and dictionaries: comprehensions and the functions
// over them, 500 elements unless the source says otherwise.

func loopGroup() group {
	in := newList()
	integer, boolean, ints := on[listIn, int64], on[listIn, bool], on[listIn, []int64]
	return group{"推导式、聚合与 reduce（500 个元素）", []scenario{
		ints(in, "[x + 1 for x in xs]", "map(xs, # + 1)"),
		ints(in, "[x for x in xs if x % 3 == 0]", "filter(xs, # % 3 == 0)"),
		on[listIn, []float64](in, "[f * 2.0 for f in fs]", "map(fs, # * 2.0)"),
		integer(in, "sum([x * 2 for x in xs if x % 3 == 0])", "sum(filter(xs, # % 3 == 0), # * 2)"),
		integer(in, "reduce(x in xs, total = 0, total + x)", "reduce(xs, #acc + #, 0)"),
		integer(in, "len([x for x in xs if x % 2 == 0])", "count(xs, # % 2 == 0)"),
		boolean(in, "any([x > 1000 for x in xs])", "any(xs, # > 1000)"),
		boolean(in, "all([x >= 0 for x in xs])", "all(xs, # >= 0)"),
		boolean(in, "!any([x < 0 for x in xs])", "none(xs, # < 0)"),
		boolean(in, "len([x for x in xs if x == 250]) == 1", "one(xs, # == 250)"),
		integer(in, "first([x for x in xs if x > 400])", "find(xs, # > 400)"),
		ints(in, "[y + z for y in ys for z in zs]", "flatten(map(ys, let y = #; map(zs, y + #)))"),
		on[listIn, map[string]int64](in, "{string(x): x for x in xs}", "fromPairs(map(xs, [string(#), #]))"),
		on[listIn, map[string][]int64](in, "group_by(xs, [string(x % 3) for x in xs])", "groupBy(xs, string(# % 3))"),
		on[listIn, []string](in, "[upper(n) for n in names]", "map(names, upper(#))"),
	}}
}

func arrayGroup() group {
	in := newList()
	integer, float, boolean, ints := on[listIn, int64], on[listIn, float64], on[listIn, bool], on[listIn, []int64]
	return group{"数组与字典函数（500 个元素）", []scenario{
		integer(in, "sum(xs)", "sum(xs)"),
		integer(in, "min(xs)", "min(xs)"),
		integer(in, "max(xs)", "max(xs)"),
		float(in, "avg(xs)", "mean(xs)"),
		float(in, "median(fs)", "median(fs)"),
		float(in, "sum(fs)", "sum(fs)"),
		ints(in, "sort(xs)", "sort(ints)"),
		ints(in, "sort_desc(xs)", `sort(ints, "desc")`),
		ints(in, "top_k(xs, xs, 5)", `take(sort(ints, "desc"), 5)`),
		ints(in, "reverse(xs)", "reverse(xs)"),
		ints(in, "unique(concat(xs, xs))", "uniq(concat(xs, xs))"),
		ints(in, "take(xs, 10)", "take(xs, 10)"),
		integer(in, "first(xs) + last(xs)", "first(xs) + last(xs)"),
		ints(in, "concat(ys, zs)", "concat(ys, zs)"),
		ints(in, "flatten([ys, zs])", "flatten([ys, zs])"),
		ints(in, "range(len(ys))", "0..len(ys)-1"),
		integer(in, "len(xs)", "len(xs)"),
		integer(in, "xs[250]", "xs[250]"),
		boolean(in, "250 in xs", "250 in xs"),
		integer(in, "index_of(xs, 250)", "findIndex(xs, # == 250)"),
		integer(in, "arg_max(xs)", "reduce(xs, # > xs[#acc] ? #index : #acc, 0)"),
		ints(in, "intersect(xs, ys)", "filter(xs, # in ys)"),
		ints(in, "except(ys, zs)", "filter(ys, not (# in zs))"),
		on[listIn, string](in, `join(names, ",")`, `join(names, ",")`),
		integer(in, `d["k042"]`, `d["k042"]`),
		boolean(in, `"k042" in d`, `"k042" in d`),
		integer(in, `get(d, "zzz", 0)`, `d["zzz"] ?? 0`),
		integer(in, "len(d)", "len(d)"),
		on[listIn, []string](in, "sort([k for k, v in d])", "sort(keys(d))"),
		integer(in, "sum([v for k, v in d])", "sum(values(d))"),
	}}
}
