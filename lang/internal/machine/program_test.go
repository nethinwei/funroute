package machine_test

import (
	"testing"

	"funroute/lang/internal/compile"
	"funroute/lang/internal/machine"
)

type benchOrder struct {
	Amount   int64   `funroute:"amount"`
	Currency string  `funroute:"currency"`
	Risk     float64 `funroute:"risk"`
	Country  string  `funroute:"country"`
}

type benchIn struct {
	Order benchOrder `funroute:"order"`
}

type benchDecision struct {
	Channel string `funroute:"channel"`
	Net     int64  `funroute:"net"`
}

// BenchmarkRecordBoundary is where the typed path pays off: a struct in and a
// struct out. The untyped path reflects over both structs on every call.
func BenchmarkRecordBoundary(b *testing.B) {
	registry := benchRegistry(b)
	binding, err := compile.Bind[benchIn, benchDecision](registry)
	if err != nil {
		b.Fatal(err)
	}
	program, err := binding.Compile(`{channel: if(order.risk < 0.5, order.currency, "manual"), net: order.amount - 30}`)
	if err != nil {
		b.Fatal(err)
	}
	in := benchIn{Order: benchOrder{Amount: 1000, Currency: "SGD", Risk: 0.2, Country: "SG"}}
	b.Run("RunValues+FromValue", func(b *testing.B) {
		ctx := b.Context()
		b.ReportAllocs()
		for b.Loop() {
			order, err := machine.ToValue(in.Order)
			if err != nil {
				b.Fatal(err)
			}
			value, err := program.Runtime().RunValues(ctx, []machine.Value{order}, machine.RunOptions{})
			if err != nil {
				b.Fatal(err)
			}
			if _, err := machine.FromValue[benchDecision](value); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("Program", func(b *testing.B) {
		ctx := b.Context()
		b.ReportAllocs()
		for b.Loop() {
			if _, err := program.Run(ctx, &in, machine.RunOptions{}); err != nil {
				b.Fatal(err)
			}
		}
	})
}
