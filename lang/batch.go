package lang

import "funroute/lang/internal/machine"

// A Batch runs one artifact for many requests and calls each model once per
// batch: the calls the bytecode proves hoistable (arguments straight from the
// request, not in a loop, not behind a condition) go through the function's
// batch implementation before the programs run.
type (
	Batch        = machine.Batch
	BatchOptions = machine.BatchOptions
)

var NewBatch = machine.NewBatch
