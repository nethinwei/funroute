package main

import (
	"context"
	"fmt"

	"github.com/nethinwei/funroute"
)

// route is a channel as a host holds it: what a rule reads of it are plain
// fields.
type route struct {
	Name string `funroute:"name"`
	Fee  int64  `funroute:"fee"`
	Ok   bool   `funroute:"ok"`
}

// routesIn is a request with channels to choose among and a series of
// numbers.
type routesIn struct {
	Channels []route `funroute:"channels"`
	Xs       []int64 `funroute:"xs"`
}

// feeOnly is a channel cut to its fee.
type feeOnly struct {
	Fee int64 `funroute:"fee"`
}

// shapeRows are the shapes a routing rule is mostly made of, over 500
// channels or numbers: a sum of a field, a filter on fields, a projection,
// an update of every item, a cap and a walk by index.
func shapeRows(registry *funroute.Registry) ([]row, error) {
	in := &routesIn{Channels: make([]route, 500), Xs: make([]int64, 500)}
	for i := range in.Channels {
		in.Channels[i] = route{Name: fmt.Sprintf("c%d", i), Fee: int64(i % 97), Ok: i%3 != 0}
		in.Xs[i] = int64(i)
	}
	sums, err := programRow[int64](registry, in, "500 个渠道的费用之和 `sum([c.fee for c in channels])`", "sum([c.fee for c in channels])",
		func() { keep(nativeFeeSum(in.Channels)) })
	if err != nil {
		return nil, err
	}
	filtered, err := programRow[[]route](registry, in, "500 个渠道按字段筛选 `[c for c in channels if c.ok && c.fee < 50]`", "[c for c in channels if c.ok && c.fee < 50]",
		func() { keep(nativeCheapUp(in.Channels)) })
	if err != nil {
		return nil, err
	}
	projected, err := programRow[[]feeOnly](registry, in, "500 个渠道的投影 `[{fee: c.fee} for c in channels]`", "[{fee: c.fee} for c in channels]",
		func() { keep(nativeFees(in.Channels)) })
	if err != nil {
		return nil, err
	}
	updated, err := programRow[[]route](registry, in, "500 个渠道逐个更新 `[c with {fee: 0} for c in channels]`", "[c with {fee: 0} for c in channels]",
		func() { keep(nativeFree(in.Channels)) })
	if err != nil {
		return nil, err
	}
	capped, err := programRow[[]int64](registry, in, "500 个数逐个封顶 `[min(x, 10) for x in xs]`", "[min(x, 10) for x in xs]",
		func() { keep(nativeCapped(in.Xs, 10)) })
	if err != nil {
		return nil, err
	}
	indexed, err := programRow[int64](registry, in, "按下标遍历 500 个数 `sum([xs[i] for i in indices(xs)])`", "sum([xs[i] for i in indices(xs)])",
		func() { keep(nativeSum(in.Xs)) })
	if err != nil {
		return nil, err
	}
	return []row{sums, filtered, projected, updated, capped, indexed}, nil
}

// programRow is source run as a Program on in, answering Out.
func programRow[Out any](registry *funroute.Registry, in *routesIn, name, source string, native func()) (row, error) {
	binding, err := funroute.Bind[routesIn, Out](registry)
	if err != nil {
		return row{}, err
	}
	program, err := binding.Compile(source)
	if err != nil {
		return row{}, fmt.Errorf("%s: %w", source, err)
	}
	return row{name, bench(func(ctx context.Context) error {
		_, err := program.Run(ctx, in)
		return err
	}), benchNative(native)}, nil
}

func nativeFeeSum(channels []route) int64 {
	total := int64(0)
	for _, c := range channels {
		total += c.Fee
	}
	return total
}

func nativeCheapUp(channels []route) []route {
	var out []route
	for _, c := range channels {
		if c.Ok && c.Fee < 50 {
			out = append(out, c)
		}
	}
	return out
}

func nativeFees(channels []route) []feeOnly {
	out := make([]feeOnly, len(channels))
	for i, c := range channels {
		out[i] = feeOnly{c.Fee}
	}
	return out
}

func nativeFree(channels []route) []route {
	out := make([]route, len(channels))
	for i, c := range channels {
		c.Fee = 0
		out[i] = c
	}
	return out
}

func nativeCapped(xs []int64, limit int64) []int64 {
	out := make([]int64, len(xs))
	for i, x := range xs {
		out[i] = min(x, limit)
	}
	return out
}
