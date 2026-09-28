package machine_test

import (
	"math"
	"testing"

	"github.com/nethinwei/funroute/internal/compile"
	"github.com/nethinwei/funroute/internal/machine"
)

// Loading the host's slice of records whole costs the same allocations for
// ten records as for a thousand: the records and their fields are made at
// once, not one by one.
func TestLoadingRecordsAllocatesByTheArray(t *testing.T) {
	binding, err := compile.Bind[viewedIn, int64](viewedRegistry(t))
	if err != nil {
		t.Fatal(err)
	}
	// An index takes the array whole: it is no view.
	program, err := binding.Compile(`channels[3].fee + limit`)
	if err != nil {
		t.Fatal(err)
	}
	allocs := func(n int) float64 {
		in := &viewedIn{Channels: make([]viewedChannel, n)}
		ctx := t.Context()
		return testing.AllocsPerRun(100, func() {
			if _, err := program.Run(ctx, in); err != nil {
				t.Fatal(err)
			}
		})
	}
	if few, many := allocs(10), allocs(1000); few != many || few > 4 {
		t.Errorf("a run allocated %v times for 10 records and %v for 1000, want the same and at most 4", few, many)
	}
}

// An infinity goes into a float32 as it is, as it goes into a float64: it
// is a float32 too. Only a finite float past a float32's range does not.
func TestAnInfinityGoesIntoAFloat32(t *testing.T) {
	t.Parallel()
	type in struct {
		X float64 `funroute:"x"`
	}
	binding, err := compile.Bind[in, float32](machine.CoreRegistry())
	if err != nil {
		t.Fatal(err)
	}
	program, err := binding.Compile(`x + x`)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		x    float64
		want float32
		fits bool
	}{
		{math.Inf(1), float32(math.Inf(1)), true},
		{math.Inf(-1), float32(math.Inf(-1)), true},
		{1.5, 3, true},
		{math.MaxFloat64 / 4, 0, false},
	} {
		got, err := program.Run(t.Context(), &in{X: test.x})
		if (err == nil) != test.fits || test.fits && got != test.want {
			t.Errorf("x + x at x = %v into a float32 = %v, %v; want %v, fits %v", test.x, got, err, test.want, test.fits)
		}
	}
}
