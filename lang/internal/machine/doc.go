// Package machine is FunRoute's runtime: values and types, bytecode and the
// VM that runs it, the registry that is the one authority on what functions
// exist and what they mean, and the host boundary — Run, RunValues, Batch and
// the typed Program. It depends on no other package of the language; syntax
// and compile build on it.
//
// A container's value is the Go value a host already has — array<float> is a
// []float64 — so the boundary converts nothing and copies nothing. That is
// why values, containers, conversion, the VM and its frame live in this one
// package: they work on the backing directly, which a package boundary would
// turn into accessor calls.
package machine
