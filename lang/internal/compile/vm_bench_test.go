package compile

import (
	"context"
	"fmt"
	"testing"

	"funroute/lang/internal/machine"
)

// The two ways a host can run the same artifact. Run converts by name, which
// is what a console does with a filled form; RunValues takes typed values in
// ABI order, which is what a service does on the hot path.
func BenchmarkRunPaths(b *testing.B) {
	registry := benchRegistry(b)
	artifact, err := CompileExpr(
		`if(country == "SG", amount * 2, amount)`,
		registry,
		CompileOptions{Args: []ArgSpec{
			{Name: "country", Type: machine.StringType},
			{Name: "amount", Type: machine.IntType},
		}},
	)
	if err != nil {
		b.Fatal(err)
	}
	runtime, err := machine.Instantiate(artifact, registry)
	if err != nil {
		b.Fatal(err)
	}

	b.Run("Run/byName", func(b *testing.B) {
		args := map[string]any{"country": "SG", "amount": 1000}
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if _, err := runtime.Run(context.Background(), args, machine.RunOptions{Fuel: 1000}); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("RunValues/byOrder", func(b *testing.B) {
		args := []machine.Value{machine.String("SG"), machine.Int(1000)}
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if _, err := runtime.RunValues(context.Background(), args, machine.RunOptions{Fuel: 1000}); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("Program/typed", func(b *testing.B) { benchTyped(b, registry, artifact) })
}

// benchTyped is the same artifact through a Program: the host's struct in,
// an int64 out.
func benchTyped(b *testing.B, registry *machine.Registry, artifact *machine.Artifact) {
	binding, err := Bind[scalarIn, int64](registry)
	if err != nil {
		b.Fatal(err)
	}
	program, err := binding.Load(artifact)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		in := scalarIn{Country: "SG", Amount: 1000}
		if _, err := program.Run(context.Background(), &in, machine.RunOptions{Fuel: 1000}); err != nil {
			b.Fatal(err)
		}
	}
}

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
	binding, err := Bind[benchIn, benchDecision](registry)
	if err != nil {
		b.Fatal(err)
	}
	program, err := binding.Compile(`{channel: if(order.risk < 0.5, order.currency, "manual"), net: order.amount - 30}`)
	if err != nil {
		b.Fatal(err)
	}
	in := benchIn{Order: benchOrder{Amount: 1000, Currency: "SGD", Risk: 0.2, Country: "SG"}}
	b.Run("RunValues+FromValue", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			order, err := machine.ToValue(in.Order)
			if err != nil {
				b.Fatal(err)
			}
			value, err := program.Runtime().RunValues(context.Background(), []machine.Value{order}, machine.RunOptions{})
			if err != nil {
				b.Fatal(err)
			}
			if _, err := machine.FromValue[benchDecision](value); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("Program", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if _, err := program.Run(context.Background(), &in, machine.RunOptions{}); err != nil {
				b.Fatal(err)
			}
		}
	})
}

// BenchmarkVectorPassThrough is the deep-learning shape: a feature vector goes
// from the host through the VM into an extension and a score comes back. The
// three sizes must print the same ns/op — nothing along the way converts,
// copies, or even reads the elements. The allocations that remain are
// reflect.Call's, because this extension is registered with Logic; they are
// the same count at every size, and a FunctionSpec has none.
func BenchmarkVectorPassThrough(b *testing.B) {
	for _, size := range []int{16, 1024, 65536} {
		b.Run(fmt.Sprintf("n=%d", size), func(b *testing.B) { benchVector(b, size) })
	}
}

func benchVector(b *testing.B, size int) {
	features := make([]float64, size)
	for i := range features {
		features[i] = float64(i) / float64(size)
	}
	registry := benchRegistry(b)
	err := machine.Logic(registry, "model.score_v1", machine.Doc{Cost: 10}, func(xs []float64) (float64, error) {
		return xs[0] + xs[len(xs)-1], nil
	})
	if err != nil {
		b.Fatal(err)
	}
	artifact, err := CompileExpr(`model.score_v1(features)`, registry, CompileOptions{
		Args: []ArgSpec{{Name: "features", Type: machine.ArrayOf(machine.FloatType)}},
	})
	if err != nil {
		b.Fatal(err)
	}
	runtime, err := machine.Instantiate(artifact, registry)
	if err != nil {
		b.Fatal(err)
	}
	value, err := machine.ToValue(features)
	if err != nil {
		b.Fatal(err)
	}
	args := []machine.Value{value}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := runtime.RunValues(context.Background(), args, machine.RunOptions{Fuel: 1000}); err != nil {
			b.Fatal(err)
		}
	}
}
