package machine

import "testing"

// A block that collects in a loop that only folds has no column to collect
// into: the vector declines it, and the scalar body runs the loop.
func TestTheVectorDeclinesACollectWithNothingToCollectInto(t *testing.T) {
	t.Parallel()
	run := &vecRun{plan: &vecPlan{item: 0}, item: vecValue{kind: vecInt}}
	block := &vecBlock{collect: 0, collectCol: -1, cond: -1}
	if run.resolveBlock(&vecBlockRun{}, block) {
		t.Fatal("resolveBlock of a collect with no answer array = true, want false")
	}
}
