package machine_test

import (
	"testing"

	"github.com/nethinwei/funroute/internal/compile"
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
