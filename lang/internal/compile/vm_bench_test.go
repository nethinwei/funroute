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
