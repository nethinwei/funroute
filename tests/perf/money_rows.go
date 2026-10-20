package main

import (
	"fmt"
	"slices"

	"github.com/nethinwei/funroute"
)

// channel is one of the candidates a routing rule picks among: a fee in
// money, and whether it is up.
type channel struct {
	name string
	fee  funroute.Money
	ok   bool
}

// moneyRows are the selections over money and records a routing rule makes
// over many channels: the cheapest few, the distinct ones, the first that
// is up, and windows over a long series.
func moneyRows(registry *funroute.Registry) ([]row, error) {
	table, _ := registry.Currencies()
	channels, fees := make([]channel, 4096), make([]funroute.Money, 4096)
	records := make([]funroute.Value, len(channels))
	typ := funroute.RecordOf(funroute.FieldOf("name", funroute.StringType), funroute.FieldOf("fee", funroute.MoneyType), funroute.FieldOf("ok", funroute.BoolType))
	for i := range channels {
		fee, err := table.Minor("USD", int64(i*7919%4096))
		if err != nil {
			return nil, err
		}
		channels[i], fees[i] = channel{fmt.Sprintf("c%d", i), fee, i%2 == 1}, fee
		if records[i], err = funroute.Record(typ, []funroute.Value{funroute.String(channels[i].name), must(funroute.ToValue(fee)), funroute.Bool(channels[i].ok)}); err != nil {
			return nil, err
		}
	}
	recordArray, err := funroute.Array(typ, records)
	if err != nil {
		return nil, err
	}
	feeArray := must(funroute.ToValue(fees))
	return moneyRowsOf(registry, typ, recordArray, feeArray, channels, fees)
}

func moneyRowsOf(registry *funroute.Registry, typ funroute.Type, recordArray, feeArray funroute.Value, channels []channel, fees []funroute.Money) ([]row, error) {
	byChannel := funroute.ArgSpec{Name: "channels", Type: funroute.ArrayOf(typ)}
	byFee := funroute.ArgSpec{Name: "fees", Type: funroute.ArrayOf(funroute.MoneyType)}
	var rows []row
	for _, c := range []struct {
		name, source string
		arg          funroute.ArgSpec
		value        funroute.Value
		native       func()
	}{
		{"4096 个渠道按金额费用取最便宜的 10 个（`bottom_k`）", "bottom_k(channels, [c.fee for c in channels], 10)", byChannel, recordArray, func() { keep(nativeCheapest(channels, 10)) }},
		{"4096 笔金额去重（`unique`）", "unique(fees)", byFee, feeArray, func() { keep(nativeDistinct(fees)) }},
		{"4096 个渠道 record 去重（`unique`）", "unique(channels)", byChannel, recordArray, func() { keep(nativeDistinct(channels)) }},
		{"4096 笔金额去掉前一半（`except`）", "except(fees, take(fees, 2048))", byFee, feeArray, func() { keep(nativeExcept(fees, fees[:2048])) }},
		{"4096 个渠道里第一个可用的（`first` 推导式，一半可用）", "first([c for c in channels if c.ok])", byChannel, recordArray, func() { keep(nativeFirstUp(channels)) }},
		{"4096 笔费用每 10 笔一个窗口（`windows`）", "len(windows(fees, 10))", byFee, feeArray, func() { keep(nativeWindows(fees, 10)) }},
	} {
		runtime, err := loaded(registry, c.source, c.arg)
		if err != nil {
			return nil, err
		}
		rows = append(rows, row{c.name, runValues(runtime, c.value), benchNative(c.native)})
	}
	return rows, nil
}

func must(value funroute.Value, err error) funroute.Value {
	if err != nil {
		panic(err)
	}
	return value
}

func nativeCheapest(channels []channel, k int) []channel {
	order := make([]int, len(channels))
	for i := range order {
		order[i] = i
	}
	slices.SortStableFunc(order, func(a, b int) int { return int(channels[a].fee.Minor() - channels[b].fee.Minor()) })
	out := make([]channel, k)
	for i := range out {
		out[i] = channels[order[i]]
	}
	return out
}

func nativeDistinct[T comparable](items []T) []T {
	out, seen := []T{}, make(map[T]bool, len(items))
	for _, item := range items {
		if !seen[item] {
			seen[item] = true
			out = append(out, item)
		}
	}
	return out
}

func nativeExcept[T comparable](items, others []T) []T {
	had := make(map[T]bool, len(others))
	for _, item := range others {
		had[item] = true
	}
	out := []T{}
	for _, item := range items {
		if !had[item] {
			had[item] = true
			out = append(out, item)
		}
	}
	return out
}

func nativeFirstUp(channels []channel) channel {
	for _, c := range channels {
		if c.ok {
			return c
		}
	}
	return channel{}
}

func nativeWindows(fees []funroute.Money, size int) int {
	var windows [][]funroute.Money
	for start := 0; start+size <= len(fees); start++ {
		windows = append(windows, fees[start:start+size])
	}
	return len(windows)
}
