package main

// The scenarios on records, on the host boundary and on compiling.

func recordGroup() group {
	in := newRecord()
	integer, text := on[recordIn, int64], on[recordIn, string]
	return group{"记录（1 个订单、50 个渠道）", []scenario{
		integer(in, "order.amount", "order.amount"),
		integer(in, "order.amount * 2 - order.fee", "order.amount * 2 - order.fee"),
		on[recordIn, netOut](in, "{net: order.amount - order.fee, currency: order.currency}", "{net: order.amount - order.fee, currency: order.currency}"),
		on[recordIn, order](in, "order with {fee: 0}", "{amount: order.amount, currency: order.currency, fee: 0}"),
		on[recordIn, []string](in, "[c.name for c in channels if c.healthy]", "map(filter(channels, .healthy), .name)"),
		integer(in, "sum([c.fee for c in channels])", "sum(channels, .fee)"),
		integer(in, "len([c for c in channels if c.healthy && c.fee < 50])", "count(channels, .healthy && .fee < 50)"),
		on[recordIn, []channel](in, "sort_by(channels, [c.fee for c in channels])", "sortBy(channels, .fee)"),
		text(in, "channels[arg_min([c.fee for c in channels])].name", "find(channels, .fee == min(map(channels, .fee))).name"),
	}}
}

func boundaryGroup() group {
	wide, big := newWide(), newBig()
	return group{"宿主边界：宽 struct 与长向量", []scenario{
		on[wideIn, int64](wide, "a + b", "a + b"),
		on[wideIn, int64](wide, "order.amount", "order.amount"),
		on[wideIn, int64](wide, "len(xs)", "len(xs)"),
		on[bigIn, int64](big, "len(fs)", "len(fs)"),
		on[bigIn, float64](big, "host.total_v1(fs)", "hostTotal(fs)"),
		on[bigIn, float64](big, "sum(fs)", "sum(fs)"),
	}}
}

func compileGroup() group {
	return group{"编译", []scenario{
		compiling[scalarIn, int64]("a + b", "a + b"),
		compiling[scalarIn, string](`switch(case a > 10 => "big", case a > 5 => "mid", else => "small")`, `a > 10 ? "big" : a > 5 ? "mid" : "small"`),
		compiling[listIn, int64]("sum([x * 2 for x in xs if x % 3 == 0])", "sum(filter(xs, # % 3 == 0), # * 2)"),
		compiling[recordIn, []string]("[c.name for c in channels if c.healthy]", "map(filter(channels, .healthy), .name)"),
	}}
}
