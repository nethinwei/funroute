package machine

import (
	"testing"

	"github.com/nethinwei/funroute/internal/money"
)

// A value holds exact money without allocating, and a whole number of minor
// units comes back as plain money, which a round has nothing to do for.
func TestExactValuesArePlainWhenWhole(t *testing.T) {
	t.Parallel()
	whole := exactValue(money.ExactFrom("USD", ratio(4, 2)))
	if IsExact(whole) || whole.i != 2 {
		t.Fatalf("2 minor units as a value = %+v, want plain money", whole)
	}
	half := exactValue(money.ExactFrom("USD", ratio(1, 2)))
	if !IsExact(half) || exactOf(half).Minor() != ratio(1, 2) {
		t.Fatalf("half a minor unit as a value = %+v, want exact money", half)
	}
	if _, ok := half.Money(); ok {
		t.Fatal("Money() of exact money reports ok: a host would see it rounded down")
	}
}
