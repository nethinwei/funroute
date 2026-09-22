package machine

import "errors"

// The errors a host tells apart. A caller uses errors.Is and never parses a
// message: malformed source, a contract violation, a rule that reached its
// fuel limit, a request that ran out of time, and an extension that failed are
// five different things to report or monitor. fallback catches only the last
// two, never a program's own limits or authoring errors.
var (
	ErrCompile   = errors.New("expression compilation failed")
	ErrContract  = errors.New("runtime contract failed")
	ErrFuel      = errors.New("execution fuel exhausted")
	ErrDeadline  = errors.New("deadline exceeded")
	ErrExtension = errors.New("extension failed")
)
