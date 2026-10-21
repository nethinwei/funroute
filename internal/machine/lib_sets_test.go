package machine

import (
	"math/rand/v2"
	"slices"
	"testing"
)

// intersect and except find an item by its key where it has one — a ratio's
// zero by either of its forms — and answer as comparing each item with Equal
// does: an item equal to nothing — a NaN,
// or a record that holds one — is never found, and kept by except each time,
// even with the array on both sides.
func TestHeldByKeyAnswersAsEqual(t *testing.T) {
	t.Parallel()
	random := rand.New(rand.NewPCG(7, 8))
	array, other := aloneRecords(t, random), aloneRecords(t, random)
	ratios, otherRatios := ratioRecords(t, random), ratioRecords(t, random)
	for _, pair := range [][2]Value{{array, other}, {array, array}, {ratios, otherRatios}} {
		keys, keyed := itemKeys(pair[0], 'a')
		others, otherKeyed := itemKeys(pair[1], 'b')
		if !keyed || !otherKeyed {
			t.Fatal("records holding a NaN, a time, a currency or a ratio are not keyed")
		}
		for _, shared := range []bool{true, false} {
			if got, want := keptBy(keys, others, shared), keptByEqual(valuesOf(pair[0]), valuesOf(pair[1]), shared); !slices.Equal(got, want) {
				t.Errorf("shared %v: the keys keep %v, Equal keeps %v", shared, got, want)
			}
		}
	}
}
