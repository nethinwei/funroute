package machine

import "errors"

// The errors a host tells apart. Everything the VM reports wraps one of them,
// so a caller uses errors.Is and never parses a message: a rule that reached
// its fuel limit, a request that ran out of time, and an extension that failed
// are three different things to monitor — and fallback catches only the last
// two, never a program's own limits.
var (
	ErrFuel      = errors.New("execution fuel exhausted")
	ErrDeadline  = errors.New("deadline exceeded")
	ErrExtension = errors.New("extension failed")
)
