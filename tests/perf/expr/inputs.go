package main

import (
	"errors"
	"fmt"

	"github.com/expr-lang/expr"
)

// The inputs, one struct per group, tagged for both sides: each reads its
// arguments from the same host struct. A struct holds only what its group
// uses, because FunRoute reads every tagged field of the struct it is given
// (the wide group measures what that costs).

type scalarIn struct {
	A    int64   `funroute:"a" expr:"a"`
	B    int64   `funroute:"b" expr:"b"`
	X    float64 `funroute:"x" expr:"x"`
	Y    float64 `funroute:"y" expr:"y"`
	Flag bool    `funroute:"flag" expr:"flag"`
}

type textIn struct {
	S       string `funroute:"s" expr:"s"`
	Card    string `funroute:"card" expr:"card"`
	Country string `funroute:"country" expr:"country"`
	Name    string `funroute:"name" expr:"name"`
	Csv     string `funroute:"csv" expr:"csv"`
}

type listIn struct {
	Xs    []int64          `funroute:"xs" expr:"xs"`
	Fs    []float64        `funroute:"fs" expr:"fs"`
	Ys    []int64          `funroute:"ys" expr:"ys"`
	Zs    []int64          `funroute:"zs" expr:"zs"`
	Names []string         `funroute:"names" expr:"names"`
	D     map[string]int64 `funroute:"d" expr:"d"`
	// Ints is xs as []int, for expr alone: its sort takes no []int64.
	Ints []int `expr:"ints"`
}

type order struct {
	Amount   int64  `funroute:"amount" expr:"amount" json:"amount"`
	Currency string `funroute:"currency" expr:"currency" json:"currency"`
	Fee      int64  `funroute:"fee" expr:"fee" json:"fee"`
}

type channel struct {
	Name    string `funroute:"name" expr:"name" json:"name"`
	Fee     int64  `funroute:"fee" expr:"fee" json:"fee"`
	Healthy bool   `funroute:"healthy" expr:"healthy" json:"healthy"`
}

type recordIn struct {
	Order    order     `funroute:"order" expr:"order"`
	Channels []channel `funroute:"channels" expr:"channels"`
}

// netOut is a record a rule builds.
type netOut struct {
	Net      int64  `funroute:"net" json:"net"`
	Currency string `funroute:"currency" json:"currency"`
}

// wideIn is a host's request struct: many fields, of which a rule reads two.
type wideIn struct {
	A        int64            `funroute:"a" expr:"a"`
	B        int64            `funroute:"b" expr:"b"`
	X        float64          `funroute:"x" expr:"x"`
	S        string           `funroute:"s" expr:"s"`
	Country  string           `funroute:"country" expr:"country"`
	Xs       []int64          `funroute:"xs" expr:"xs"`
	Fs       []float64        `funroute:"fs" expr:"fs"`
	Names    []string         `funroute:"names" expr:"names"`
	D        map[string]int64 `funroute:"d" expr:"d"`
	Order    order            `funroute:"order" expr:"order"`
	Channels []channel        `funroute:"channels" expr:"channels"`
}

// bigIn is one long vector.
type bigIn struct {
	Fs []float64 `funroute:"fs" expr:"fs"`
}

func newScalar() *scalarIn { return &scalarIn{A: 7, B: 3, X: 2.5, Y: 4, Flag: false} }

func newText() *textIn {
	return &textIn{S: "adyen-sg", Card: "411111", Country: "MY", Name: "  Adyen SG  ", Csv: "sg,my,th,id,ph"}
}

// newList holds 500 distinct integers out of order, their quarters, two short
// vectors for pairs and set operations, 100 names and a dictionary of 100.
func newList() *listIn {
	in := &listIn{Xs: make([]int64, 500), Fs: make([]float64, 500), Ys: make([]int64, 20), Zs: make([]int64, 25), D: map[string]int64{}}
	for i := range in.Xs {
		in.Xs[i] = int64(i * 7919 % 500)
		in.Fs[i] = float64(in.Xs[i]) / 4
		in.Ints = append(in.Ints, int(in.Xs[i]))
	}
	for i := range in.Ys {
		in.Ys[i] = int64(i * 3)
	}
	for i := range in.Zs {
		in.Zs[i] = int64(i * 2)
	}
	for i := range 100 {
		name := fmt.Sprintf("n%03d", i*37%100)
		in.Names = append(in.Names, name)
		in.D[fmt.Sprintf("k%03d", i)] = int64(i)
	}
	return in
}

// newRecord is an order and 50 channels with distinct fees.
func newRecord() *recordIn {
	in := &recordIn{Order: order{Amount: 120000, Currency: "USD", Fee: 350}}
	for i := range 50 {
		in.Channels = append(in.Channels, channel{Name: fmt.Sprintf("ch%02d", i), Fee: int64(i*37%101 + 1), Healthy: i%3 != 0})
	}
	return in
}

func newWide() *wideIn {
	list, record := newList(), newRecord()
	return &wideIn{A: 7, B: 3, X: 2.5, S: "adyen-sg", Country: "MY", Xs: list.Xs, Fs: list.Fs, Names: list.Names, D: list.D,
		Order: record.Order, Channels: record.Channels}
}

func newBig() *bigIn {
	in := &bigIn{Fs: make([]float64, 65536)}
	for i := range in.Fs {
		in.Fs[i] = float64(i % 100)
	}
	return in
}

var errArguments = errors.New("unexpected argument types")

// hostFunctions are the host functions for expr, through expr.Function the
// way it documents them, each calling the same Go function FunRoute calls.
var hostFunctions = []expr.Option{
	expr.Function("hostAdd", func(params ...any) (any, error) {
		a, aOK := params[0].(int64)
		b, bOK := params[1].(int64)
		if !aOK || !bOK {
			return nil, errArguments
		}
		return hostAdd(a, b), nil
	}, new(func(int64, int64) int64)),
	expr.Function("hostScale", func(params ...any) (any, error) {
		x, xOK := params[0].(float64)
		n, nOK := params[1].(int64)
		if !xOK || !nOK {
			return nil, errArguments
		}
		return hostScale(x, n), nil
	}, new(func(float64, int64) float64)),
	expr.Function("hostLabel", func(params ...any) (any, error) {
		s, ok := params[0].(string)
		if !ok {
			return nil, errArguments
		}
		return hostLabel(s), nil
	}, new(func(string) string)),
	expr.Function("hostTotal", func(params ...any) (any, error) {
		xs, ok := params[0].([]float64)
		if !ok {
			return nil, errArguments
		}
		return hostTotal(xs), nil
	}, new(func([]float64) float64)),
}
