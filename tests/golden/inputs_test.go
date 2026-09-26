package golden

import (
	"fmt"
	"math"

	"github.com/nethinwei/funroute"
)

// request is the contract every program is compiled against, read off this
// struct by Bind.
type request struct {
	A     int64            `funroute:"a"`
	B     int64            `funroute:"b"`
	F     float64          `funroute:"f"`
	G     float64          `funroute:"g"`
	S     string           `funroute:"s"`
	T     string           `funroute:"t"`
	Flag  bool             `funroute:"flag"`
	Xs    []int64          `funroute:"xs"`
	Ys    []int64          `funroute:"ys"`
	Fs    []float64        `funroute:"fs"`
	Ss    []string         `funroute:"ss"`
	D     map[string]int64 `funroute:"d"`
	Order order            `funroute:"order"`
	Chans []channel        `funroute:"chans"`
}

type order struct {
	Amount int64  `funroute:"amount" json:"amount"`
	Fee    int64  `funroute:"fee" json:"fee"`
	Tag    string `funroute:"tag" json:"tag"`
}

type channel struct {
	Name string  `funroute:"name" json:"name"`
	Fee  int64   `funroute:"fee" json:"fee"`
	OK   bool    `funroute:"ok" json:"ok"`
	Risk float64 `funroute:"risk" json:"risk"`
}

// inputs are the requests every program runs on: plain, empty, at the
// extremes, long enough to cross a vector's blocks, with floats that are not
// finite, and with negatives and repeats.
func inputs() []*request {
	return []*request{
		{A: 7, B: 3, F: 2.5, G: -1.25, S: "adyen-sg", T: "sg", Flag: true,
			Xs: []int64{5, 1, 4, 1, 5, 9, 2, 6}, Ys: []int64{3, 0, -2}, Fs: []float64{0.5, 1.5, -2.25, 4},
			Ss: []string{"b", "a", "c", "a"}, D: map[string]int64{"k": 1, "sg": -3},
			Order: order{Amount: 120000, Fee: 350, Tag: "vip"}, Chans: channels(5)},
		{Xs: []int64{}, Ys: []int64{}, Fs: []float64{}, Ss: []string{}, D: map[string]int64{}, Chans: []channel{}, G: math.Copysign(0, -1)},
		{A: math.MaxInt64, B: -1, F: 1e308, G: -1e308, S: "zz", T: "z", Xs: []int64{math.MaxInt64, math.MinInt64, 0},
			Ys: []int64{1}, Fs: []float64{1e308, 1e308}, Ss: []string{"zz"}, D: map[string]int64{"zz": math.MaxInt64},
			Order: order{Amount: math.MaxInt64, Fee: math.MinInt64}, Chans: channels(1)},
		{A: 300, B: 7, F: 0.5, G: 3, S: "a,b,c", T: ",", Xs: series(300, 7), Ys: series(40, 3), Fs: floats(300),
			Ss: []string{"x", "y"}, D: map[string]int64{"a": 1}, Order: order{Amount: 1, Fee: 1, Tag: "a"}, Chans: channels(60)},
		{A: 1, B: 2, F: math.NaN(), G: math.Inf(1), S: "sg", T: "sg", Xs: []int64{1, 2}, Ys: []int64{2}, Fs: []float64{math.NaN(), 1, math.Inf(-1)},
			Ss: []string{"a"}, D: map[string]int64{"sg": 2}, Order: order{Amount: 5}, Chans: []channel{{Name: "n", Fee: 1, Risk: math.NaN()}}},
		{A: -7, B: 2, F: -0.5, G: 0.5, S: "SG ", T: " ", Flag: true, Xs: []int64{-3, -3, 0, -1, -3}, Ys: []int64{-3, 7},
			Fs: []float64{-1, -1, 2}, Ss: []string{"b", "b"}, D: map[string]int64{"b": -1}, Order: order{Amount: -5, Fee: 2, Tag: "b"}, Chans: channels(3)},
	}
}

func channels(n int) []channel {
	out := make([]channel, n)
	for i := range out {
		out[i] = channel{Name: fmt.Sprintf("ch%02d", i), Fee: int64(i*37%101 + 1), OK: i%3 != 0, Risk: float64(i%10) / 10}
	}
	return out
}

func series(n, step int) []int64 {
	out := make([]int64, n)
	for i := range out {
		out[i] = int64(i*step%97 - 40)
	}
	return out
}

func floats(n int) []float64 {
	out := make([]float64, n)
	for i := range out {
		out[i] = float64(i%17) / 4
	}
	return out
}

// valuesOf is the request as the values RunValues takes, in the contract's
// order, built through the public constructors — or why one of them refused
// it.
func valuesOf(in *request, params []funroute.Parameter) ([]funroute.Value, error) {
	values := make([]funroute.Value, 0, len(params))
	for _, param := range params {
		value, err := valueOf(in, param)
		if err != nil {
			return nil, fmt.Errorf("argument %s: %w", param.Name(), err)
		}
		values = append(values, value)
	}
	return values, nil
}

func valueOf(in *request, param funroute.Parameter) (funroute.Value, error) {
	switch param.Name() {
	case "a":
		return funroute.Int(in.A), nil
	case "b":
		return funroute.Int(in.B), nil
	case "f":
		return funroute.Float(in.F)
	case "g":
		return funroute.Float(in.G)
	case "s":
		return funroute.String(in.S), nil
	case "t":
		return funroute.String(in.T), nil
	case "flag":
		return funroute.Bool(in.Flag), nil
	case "order":
		return orderValue(param.Type(), in.Order)
	case "chans":
		return channelsValue(param.Type(), in.Chans)
	}
	return containerValue(in, param.Name())
}

func containerValue(in *request, name string) (funroute.Value, error) {
	switch name {
	case "xs":
		return funroute.ToValue(in.Xs)
	case "ys":
		return funroute.ToValue(in.Ys)
	case "fs":
		return funroute.ToValue(in.Fs)
	case "ss":
		return funroute.ToValue(in.Ss)
	}
	return funroute.ToValue(in.D)
}

func orderValue(typ funroute.Type, o order) (funroute.Value, error) {
	return funroute.Record(typ, []funroute.Value{funroute.Int(o.Amount), funroute.Int(o.Fee), funroute.String(o.Tag)})
}

func channelsValue(typ funroute.Type, chans []channel) (funroute.Value, error) {
	elem, _ := typ.Elem()
	items := make([]funroute.Value, len(chans))
	for i, c := range chans {
		risk, err := funroute.Float(c.Risk)
		if err != nil {
			return funroute.Value{}, fmt.Errorf("item %d: %w", i, err)
		}
		item, err := funroute.Record(elem, []funroute.Value{funroute.String(c.Name), funroute.Int(c.Fee), funroute.Bool(c.OK), risk})
		if err != nil {
			return funroute.Value{}, err
		}
		items[i] = item
	}
	return funroute.Array(elem, items)
}
