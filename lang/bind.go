package lang

import (
	"funroute/lang/internal/compile"
	"funroute/lang/internal/machine"
)

// A contract can also be two Go types. Bind reads it off them once: the
// `funroute:"name"` fields of In are the arguments, in declaration order —
// which is the ABI — and Out is the result. The Binding compiles and loads
// only programs of that contract, and a Program runs on the types directly:
//
//	binding, err := lang.Bind[RouteIn, Decision](registry)
//	program, err := binding.Compile(source)
//	decision, err := program.Run(ctx, &request, lang.RunOptions{})
//
// No name is looked up and nothing is reflected per call. Scalars cost no
// allocation, a slice or map is wrapped rather than copied, and an argument
// the program does not read is not converted at all. A container in the
// result is the program's backing and is read-only.
//
// Load takes an artifact compiled elsewhere, matched by name: it may declare
// fewer arguments, in another order, and records with fewer fields, as a
// console that declares only what a rule reads does; a Go string carries an
// enum. Program.RunBatch runs requests the host already holds, sharing each
// model call; RunBatchInto writes the results into the host's own objects and
// RunBatchFunc reaches requests and results wherever they live. One index
// names a request, its result and its failure in all three. Program.Batch
// does the same for requests from many goroutines.
type (
	Binding[In, Out any]      = compile.Binding[In, Out]
	Program[In, Out any]      = machine.Program[In, Out]
	ProgramBatch[In, Out any] = machine.ProgramBatch[In, Out]
)

func Bind[In, Out any](registry *Registry) (*Binding[In, Out], error) {
	return compile.Bind[In, Out](registry)
}
