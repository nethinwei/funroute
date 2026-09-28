package machine

import "testing"

// Every kernel operation — every one before rCall — has its row in
// coldKernels: the hot loop sends any it does not hold there, and a missing
// row would be a call of nil.
func TestEveryKernelOpHasAColdStep(t *testing.T) {
	t.Parallel()
	for op := range rCall {
		if coldKernels[op] == nil {
			t.Errorf("%s has no row in coldKernels", op)
		}
	}
}
