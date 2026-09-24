package compile

import (
	"errors"
	"fmt"

	"github.com/nethinwei/funroute/internal/machine"
)

func compileError(err error) error {
	if err == nil || errors.Is(err, machine.ErrCompile) || errors.Is(err, machine.ErrContract) {
		return err
	}
	// Both are wrapped: errors.Is finds the class, errors.As finds the
	// position the lexer or the type checker recorded.
	return fmt.Errorf("%w: %w", machine.ErrCompile, err)
}

func contractErrorf(format string, args ...any) error {
	return fmt.Errorf("%w: %s", machine.ErrContract, fmt.Sprintf(format, args...))
}
